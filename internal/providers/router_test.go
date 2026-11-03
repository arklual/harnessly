package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// vendor is a fake subscription endpoint: it answers with the status it is given and remembers what it got.
type vendor struct {
	mu     sync.Mutex
	status int
	// verdict answers the classifier's question (a request with the classifier's system prompt) after hold.
	verdict string
	hold    time.Duration
	got     []map[string]json.RawMessage
	keys    []string
	srv     *httptest.Server
}

func newVendor(t *testing.T, status int) *vendor {
	v := &vendor{status: status}
	v.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		v.mu.Lock()
		v.got = append(v.got, body)
		v.keys = append(v.keys, r.Header.Get("X-Api-Key"))
		status, verdict, hold := v.status, v.verdict, v.hold
		v.mu.Unlock()
		if rawString(body["system"]) == classifyPrompt && status == http.StatusOK {
			select {
			case <-time.After(hold):
			case <-r.Context().Done():
				return
			}
			out, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": verdict}}})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(out)
			return
		}
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"type":"error"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"model\":"+string(body["model"])+"}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	t.Cleanup(v.srv.Close)
	return v
}

func (v *vendor) calls() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.got)
}

func (v *vendor) last() map[string]json.RawMessage {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.got[len(v.got)-1]
}

type routerRig struct {
	r       *Router
	store   *Store
	vendors map[string]*vendor
	srv     *httptest.Server
	key     string
	asked   int
	verdict Tier
	mu      sync.Mutex
}

// newRouterRig is a router with keys of the given profiles, each on its fake vendor, switched on.
func newRouterRig(t *testing.T, profiles ...string) *routerRig {
	t.Helper()
	rig := &routerRig{store: newStore(t), vendors: map[string]*vendor{}, verdict: TierHeavy}
	for _, id := range profiles {
		if _, err := rig.store.Set(id, fakeKey+"-"+id); err != nil {
			t.Fatal(err)
		}
		rig.vendors[id] = newVendor(t, http.StatusOK)
	}
	dir := t.TempDir()
	rig.r = &Router{
		Dir:    filepath.Join(dir, "auto-router"),
		OnFile: filepath.Join(dir, "auto.on"),
		Base:   "http://127.0.0.1:1" + RouterPath,
		Store:  rig.store,
		Upstream: func(id string) string {
			if v := rig.vendors[id]; v != nil {
				return v.srv.URL + "/v1/messages"
			}
			return ""
		},
		Classify: func(context.Context, ClassifyRequest) (Tier, string, error) {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			rig.asked++
			return rig.verdict, "test", nil
		},
	}
	if err := rig.r.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	key, err := rig.r.clientKey()
	if err != nil {
		t.Fatal(err)
	}
	rig.key = key
	mux := http.NewServeMux()
	mux.Handle(RouterPath+"/", rig.r)
	rig.srv = httptest.NewServer(mux)
	t.Cleanup(rig.srv.Close)
	return rig
}

func (rig *routerRig) post(t *testing.T, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+RouterPath+"/v1/messages", strings.NewReader(body))
	req.Header.Set("X-Api-Key", rig.key)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

const (
	turnBody = `{"model":"auto","stream":true,"metadata":{"user_id":"s1"},"messages":[{"role":"user","content":"rename the variable"}]}`
	loopBody = `{"model":"auto","stream":true,"metadata":{"user_id":"s1"},"messages":[` +
		`{"role":"user","content":"rename the variable"},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"hm","signature":"x"},{"type":"tool_use","id":"t1","name":"Read","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`
)

func TestRouterSwitchNeedsAVendorKey(t *testing.T) {
	dir := t.TempDir()
	r := &Router{Dir: filepath.Join(dir, "auto-router"), OnFile: filepath.Join(dir, "auto.on"), Base: "http://x" + RouterPath, Store: newStore(t)}
	var se *wire.ProfileSwitchError
	if err := r.SetOn(context.Background(), true); !errors.As(err, &se) || se.Reason != wire.SwitchNoLogin {
		t.Fatalf("on without a key: %v", err)
	}
	if r.On() || r.Ready() {
		t.Fatal("on without a key")
	}
	if _, err := r.Store.Set("kimi-code", fakeKey); err != nil {
		t.Fatal(err)
	}
	if err := r.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	base, keyFile, ok := r.Endpoint()
	if !ok || base != r.Base || keyFile == "" || !r.Ready() {
		t.Fatalf("endpoint %q %q %v", base, keyFile, ok)
	}
	if st, err := os.Stat(keyFile); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v", err)
	}
	if err := r.SetOn(context.Background(), false); err != nil || r.On() {
		t.Fatalf("off: %v", err)
	}
	if _, _, ok := r.Endpoint(); ok {
		t.Fatal("endpoint while off")
	}
}

func TestRouterRefusesAForeignKey(t *testing.T) {
	rig := newRouterRig(t, "glm")
	req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+RouterPath+"/v1/messages", strings.NewReader(turnBody))
	req.Header.Set("X-Api-Key", "hrn-nope")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || rig.vendors["glm"].calls() != 0 {
		t.Fatalf("status %d, vendor calls %d", res.StatusCode, rig.vendors["glm"].calls())
	}
}

func TestRouterPutsANewTurnOnItsTier(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.verdict = TierLight
	res, body := rig.post(t, turnBody)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "message_stop") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	glm := rig.vendors["glm"]
	if glm.calls() != 1 || rig.vendors["kimi-code"].calls() != 0 {
		t.Fatalf("light turn went to glm %d, kimi %d", glm.calls(), rig.vendors["kimi-code"].calls())
	}
	if m := rawString(glm.last()["model"]); m != "glm-5.3-flash" {
		t.Fatalf("model %q", m)
	}
	if k := glm.keys[0]; k != fakeKey+"-glm" {
		t.Fatal("the vendor got another key than its profile's")
	}

	rig.verdict = TierHeavy
	rig.post(t, `{"model":"auto","metadata":{"user_id":"s2"},"messages":[{"role":"user","content":"design the migration"}]}`)
	kimi := rig.vendors["kimi-code"]
	if kimi.calls() != 1 || rawString(kimi.last()["model"]) != "k3.1-pro" {
		t.Fatalf("heavy turn: kimi calls %d", kimi.calls())
	}
}

// Every step of a turn's tool loop is decided on its own: the plan can go heavy and the reads after it light.
func TestRouterDecidesEachStepOfATurn(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	var got []ClassifyRequest
	rig.r.Classify = func(_ context.Context, cr ClassifyRequest) (Tier, string, error) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		got = append(got, cr)
		return rig.verdict, "test", nil
	}
	rig.verdict = TierHeavy
	rig.post(t, turnBody)
	rig.verdict = TierLight
	rig.post(t, loopBody)
	if len(got) != 2 {
		t.Fatalf("the classifier was asked %d times", len(got))
	}
	step := got[1]
	if step.Step != 1 || step.Current != TierHeavy || step.Text != "rename the variable" ||
		!strings.Contains(step.Did, "call Read") || !strings.Contains(step.Did, "result: ok") {
		t.Fatalf("step request %+v", step)
	}
	kimi, glm := rig.vendors["kimi-code"], rig.vendors["glm"]
	if kimi.calls() != 1 || glm.calls() != 1 || rawString(glm.last()["model"]) != "glm-5.3-flash" {
		t.Fatalf("the plan went to kimi %d times, the read to glm %d", kimi.calls(), glm.calls())
	}
	// The history now mixes vendors: kimi's signed reasoning does not go to glm.
	if strings.Contains(string(glm.last()["messages"]), `"thinking"`) {
		t.Fatal("foreign reasoning sent to glm")
	}
	j, _ := os.ReadFile(rig.r.LogFile())
	if !strings.Contains(string(j), `"step":1`) || !strings.Contains(string(j), "step 1: test") {
		t.Fatalf("journal: %s", j)
	}
}

// A step whose classifier fails stays where the turn is instead of jumping heavy.
func TestRouterStepStaysWhenItsClassifierFails(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.verdict = TierLight
	rig.post(t, turnBody)
	rig.r.Classify = func(context.Context, ClassifyRequest) (Tier, string, error) {
		return "", "", errors.New("slow")
	}
	rig.post(t, loopBody)
	if rig.vendors["glm"].calls() != 2 || rig.vendors["kimi-code"].calls() != 0 {
		t.Fatal("a step without a verdict left its turn's target")
	}
}

func TestFailedStepsCountsErrorsInARow(t *testing.T) {
	msgs := []message{
		{Role: "user", Content: json.RawMessage(`"fix the test"`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"a","name":"Bash","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"a","content":"ok"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"b","name":"Bash","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"b","is_error":true,"content":"exit 1"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"c","name":"Bash","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"c","is_error":true,"content":[{"type":"text","text":"exit 1"}]}]`)},
	}
	if n := failedSteps(msgs); n != 2 {
		t.Fatalf("failed steps %d", n)
	}
	if d := stepText(msgs); !strings.Contains(d, "error result: exit 1") {
		t.Fatalf("step text %q", d)
	}
}

func TestRouterMovesOnAfterAQuotaAnswer(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.vendors["kimi-code"].status = http.StatusTooManyRequests
	res, body := rig.post(t, turnBody)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	glm := rig.vendors["glm"]
	if rig.vendors["kimi-code"].calls() != 1 || glm.calls() != 1 || rawString(glm.last()["model"]) != "glm-5.3" {
		t.Fatalf("failover: kimi %d glm %d", rig.vendors["kimi-code"].calls(), glm.calls())
	}
	// Kimi rests now: the next heavy turn goes straight to GLM.
	rig.post(t, `{"model":"auto","metadata":{"user_id":"s3"},"messages":[{"role":"user","content":"x"}]}`)
	if rig.vendors["kimi-code"].calls() != 1 || glm.calls() != 2 {
		t.Fatal("a resting target was tried")
	}
	if _, ok := rig.r.RouterStatus()["kimi-code"]; !ok {
		t.Fatal("kimi is not shown as resting")
	}
	// Its rest over, it is tried again.
	rig.r.now = func() time.Time { return time.Now().Add(downQuota + time.Minute) }
	rig.vendors["kimi-code"].status = http.StatusOK
	rig.post(t, `{"model":"auto","metadata":{"user_id":"s4"},"messages":[{"role":"user","content":"x"}]}`)
	if rig.vendors["kimi-code"].calls() != 2 {
		t.Fatal("kimi is not tried after its rest")
	}
}

func TestRouterDropsForeignReasoningWhenVendorsMix(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.verdict = TierLight
	rig.post(t, turnBody) // glm takes the turn
	rig.verdict = TierHeavy
	next := `{"model":"auto","metadata":{"user_id":"s1"},"messages":[` +
		`{"role":"user","content":"rename the variable"},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"hm","signature":"x"},{"type":"text","text":"done"}]},` +
		`{"role":"user","content":"now design the migration"}]}`
	rig.post(t, next)
	kimi := rig.vendors["kimi-code"]
	if kimi.calls() != 1 {
		t.Fatal("the heavy turn did not reach kimi")
	}
	msgs := string(kimi.last()["messages"])
	if strings.Contains(msgs, `"thinking"`) || !strings.Contains(msgs, `"done"`) {
		t.Fatalf("history sent to kimi: %s", msgs)
	}
}

func TestRouterFastModelSkipsTheClassifier(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.post(t, `{"model":"auto-fast","messages":[{"role":"user","content":"title this chat"}]}`)
	if rig.asked != 0 || rig.vendors["glm"].calls() != 1 {
		t.Fatalf("asked %d, glm %d", rig.asked, rig.vendors["glm"].calls())
	}
}

func TestRouterSaysWhenNothingIsUp(t *testing.T) {
	rig := newRouterRig(t, "glm")
	rig.vendors["glm"].status = http.StatusTooManyRequests
	res, _ := rig.post(t, turnBody)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the last target's answer is passed through: %d", res.StatusCode)
	}
	res, body := rig.post(t, turnBody)
	if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "error") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
}

func TestRouterJournalHoldsNoKey(t *testing.T) {
	rig := newRouterRig(t, "glm")
	rig.post(t, turnBody)
	data, err := os.ReadFile(rig.r.LogFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), fakeKey) || strings.Contains(string(data), rig.key) || strings.Contains(string(data), "rename") {
		t.Fatal("the journal holds a key or the chat's text")
	}
	var d decision
	if err := json.Unmarshal([]byte(strings.SplitN(string(data), "\n", 2)[0]), &d); err != nil || d.Target.Profile != "glm" || d.Status != 200 {
		t.Fatalf("decision %+v %v", d, err)
	}
}

func TestParseVerdict(t *testing.T) {
	for text, want := range map[string]Tier{
		`{"tier":"light","reason":"a lookup"}`:              TierLight,
		"Sure.\n```json\n{\"tier\":\"heavy\"}\n```":         TierHeavy,
		`{"tier": "light", "reason": "ok, go on"} trailing`: TierLight,
	} {
		got, _, err := parseVerdict(text)
		if err != nil || got != want {
			t.Errorf("%q: %q %v", text, got, err)
		}
	}
	for _, text := range []string{"", "heavy", `{"tier":"medium"}`} {
		if _, _, err := parseVerdict(text); err == nil {
			t.Errorf("%q: no error", text)
		}
	}
}

// Claude Code puts a note of role system after the person's message; the turn is still a new one.
func TestRouterSeesATurnPastATrailingSystemNote(t *testing.T) {
	rig := newRouterRig(t, "glm", "kimi-code")
	rig.verdict = TierLight
	body := `{"model":"auto","metadata":{"user_id":"s1"},"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"<system-reminder>x</system-reminder>"},{"type":"text","text":"what is 2+2"}]},` +
		`{"role":"system","content":[{"type":"text","text":"note"}]}]}`
	rig.post(t, body)
	if rig.asked != 1 || rig.vendors["glm"].calls() != 1 {
		t.Fatalf("asked %d, glm %d", rig.asked, rig.vendors["glm"].calls())
	}
	msgs := []message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"what is 2+2"}]`)},
		{Role: "system", Content: json.RawMessage(`"note"`)},
	}
	if got := turnAsk(msgs); got != "what is 2+2" {
		t.Fatalf("turn text %q", got)
	}
	loop := append([]message{
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"t1","name":"Read","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]`)},
	}, message{Role: "system", Content: json.RawMessage(`"note"`)})
	if startsTurn(loop) {
		t.Fatal("tool results behind a note were taken for a new turn")
	}
}

// The first light target is slow or failing: the classifier asks the next one rather than sending the turn heavy.
func TestRouterClassifierFallsToTheNextLightTarget(t *testing.T) {
	old := routerClassifyTry
	routerClassifyTry = 200 * time.Millisecond
	t.Cleanup(func() { routerClassifyTry = old })

	rig := newRouterRig(t, "glm", "kimi-code")
	rig.r.Classify = nil
	rig.vendors["glm"].verdict = `{"tier":"light","reason":"slow"}`
	rig.vendors["glm"].hold = 5 * time.Second
	rig.vendors["kimi-code"].verdict = `{"tier":"light","reason":"small ask"}`
	rig.post(t, turnBody)

	j, err := os.ReadFile(filepath.Join(rig.r.Dir, "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(j), `"tier":"light"`) || !strings.Contains(string(j), "by kimi-code") {
		t.Fatalf("journal: %s", j)
	}
}
