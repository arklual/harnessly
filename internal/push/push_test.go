package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/status"
)

const tagPrefix = "app:"

func writeJSONL(t *testing.T, lines ...any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		if s, ok := l.(string); ok { // a line written as it is (key order matters)
			raw = []byte(s)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	p := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type m = map[string]any

func user(text, at string) m {
	return m{"type": "user", "timestamp": at, "uuid": "u-" + at, "message": m{"role": "user", "content": text}}
}

func assistant(content []any, at string) m {
	return m{"type": "assistant", "timestamp": at, "uuid": "a-" + at, "message": m{"role": "assistant", "content": content, "stop_reason": "end_turn"}}
}

func tabEntry(path string, patch func(*status.Entry)) status.Entry {
	e := status.Entry{CliState: "ready-for-review", WorkspaceID: "ws-1", TabName: "Claude Code", TmuxSession: "s",
		AgentProviderID: status.Some("claude"), AgentSessionID: status.Some("session-1"), JSONLPath: path}
	if patch != nil {
		patch(&e)
	}
	return e
}

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// push-message: names the conversation by its sidebar title and quotes the last answer without markdown.
func TestMessageDone(t *testing.T) {
	p := writeJSONL(t,
		`{"type":"ai-title","aiTitle":"Уведомления платформы","sessionId":"s"}`,
		user("сделай пуши", "2026-09-25T10:00:00.000Z"),
		assistant([]any{m{"type": "text", "text": "## Готово\n\nПуши **работают** на `iPhone`."}}, "2026-09-25T10:05:00.000Z"),
	)
	msg := BuildMessage("tab-1", tabEntry(p, nil), "done", tagPrefix, now)
	if msg.Kind != "done" || msg.Title != "Уведомления платформы" || msg.Body != "Готово — Готово Пуши работают на iPhone." || msg.TabID != "tab-1" || msg.Tag != "app:tab-1" {
		t.Fatalf("%+v", msg)
	}
	raw, _ := json.Marshal(msg)
	if !strings.HasPrefix(string(raw), `{"kind":"done","title":"Уведомления платформы","body":`) || !strings.Contains(string(raw), `"agentSessionId":"session-1","tag":"app:tab-1","at":1792324800000}`) {
		t.Fatalf("%s", raw)
	}
}

// push-message: puts the pending question into the body when the agent asks.
func TestMessageQuestion(t *testing.T) {
	p := writeJSONL(t,
		`{"type":"ai-title","aiTitle":"Старое название"}`,
		`{"type":"ai-title","aiTitle":"Выкат \"шлюза\""}`,
		user("выкати", "2026-09-25T10:00:00.000Z"),
		assistant([]any{m{"type": "tool_use", "id": "toolu_1", "name": "AskUserQuestion", "input": m{"questions": []any{m{"question": "Катим сейчас или после проверки?", "header": "Выкат", "options": []any{}, "multiSelect": false}}}}}, "2026-09-25T10:01:00.000Z"),
	)
	msg := BuildMessage("tab-1", tabEntry(p, func(e *status.Entry) { e.CliState = "needs-input" }), "question", tagPrefix, now)
	if msg.Title != "Выкат \"шлюза\"" || msg.Body != "Вопрос: Катим сейчас или после проверки?" || msg.Detail != "Катим сейчас или после проверки?" {
		t.Fatalf("%+v", msg)
	}
	// A tool call waiting for permission says what it wants.
	p = writeJSONL(t,
		user("почисти", "2026-09-25T10:00:00.000Z"),
		assistant([]any{m{"type": "tool_use", "id": "t1", "name": "Bash", "input": m{"command": "rm -rf build\nls"}}}, "2026-09-25T10:01:00.000Z"),
	)
	msg = BuildMessage("tab-1", tabEntry(p, func(e *status.Entry) { e.AgentSummary = status.Some("Чистка") }), "question", tagPrefix, now)
	if msg.Title != "Чистка" || msg.Body != "Разрешить: $ rm -rf build" {
		t.Fatalf("%+v", msg)
	}
	// A finished call or a newer prompt is no question.
	p = writeJSONL(t,
		assistant([]any{m{"type": "tool_use", "id": "t1", "name": "Bash", "input": m{"command": "ls"}}}, "2026-09-25T10:01:00.000Z"),
		m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}}}},
	)
	if msg := BuildMessage("tab-1", tabEntry(p, nil), "question", tagPrefix, now); msg.Body != "Агент остановился и ждёт твоего решения" {
		t.Fatalf("%+v", msg)
	}
}

// push-message: falls back to a plain sentence without a transcript; says what a permission asks; marks a failed turn.
func TestMessageFallbacksAndPermission(t *testing.T) {
	msg := BuildMessage("tab-2", tabEntry("/nonexistent.jsonl", func(e *status.Entry) { e.AgentSummary = status.Some("Разбор логов") }), "question", tagPrefix, now)
	if msg.Title != "Разбор логов" || msg.Body != "Агент остановился и ждёт твоего решения" {
		t.Fatalf("%+v", msg)
	}
	asked := BuildMessage("tab-3", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.AgentSummary = status.Some("Сборка")
		e.PermissionRequest = status.Some(json.RawMessage(`{"type":"ExecApprovalRequest","command":"pnpm build"}`))
	}), "question", tagPrefix, now)
	if asked.Body != "Разрешить команду: pnpm build" {
		t.Fatalf("%+v", asked)
	}
	failed := BuildMessage("tab-4", tabEntry("/nonexistent.jsonl", func(e *status.Entry) {
		e.AgentSummary = status.Some("Сборка")
		e.LastAssistantMessage = status.Some("API Error 529")
		e.LastEvent = status.Some(status.LastEvent{Name: "stop-failure", At: 1, Seq: 1})
	}), "done", tagPrefix, now)
	if failed.Title != "Сборка" || failed.Body != "Ошибка — API Error 529" {
		t.Fatalf("%+v", failed)
	}
	plain := BuildMessage("tab-5", tabEntry("", func(e *status.Entry) { e.AgentSessionID = status.Null[string]() }), "done", tagPrefix, now)
	if plain.Title != "Claude Code" || plain.Body != "Готово — Агент закончил работу" || plain.AgentSessionID != nil {
		t.Fatalf("%+v", plain)
	}
}

func TestMessageCodex(t *testing.T) {
	ev := func(typ string, p m) m {
		p["type"] = typ
		return m{"type": "event_msg", "timestamp": "2026-09-25T10:00:00Z", "payload": p}
	}
	p := writeJSONL(t,
		ev("user_message", m{"message": "собери"}),
		ev("agent_message", m{"message": "Собрал **всё**."}),
	)
	codexEntry := func(patch func(*status.Entry)) status.Entry {
		return tabEntry(p, func(e *status.Entry) {
			e.AgentProviderID = status.Some("codex")
			if patch != nil {
				patch(e)
			}
		})
	}
	if msg := BuildMessage("t", codexEntry(nil), "done", tagPrefix, now); msg.Body != "Готово — Собрал всё." || msg.Provider != "codex" || msg.Title != "Claude Code" {
		t.Fatalf("%+v", msg)
	}
	p = writeJSONL(t,
		ev("user_message", m{"message": "почини"}),
		m{"type": "response_item", "payload": m{"type": "function_call", "name": "exec_command", "call_id": "c1", "arguments": `{"cmd":"make test"}`}},
	)
	if msg := BuildMessage("t", codexEntry(nil), "question", tagPrefix, now); msg.Body != "Разрешить: $ make test" {
		t.Fatalf("%+v", msg)
	}
	p = writeJSONL(t, ev("exec_approval_request", m{"command": "rm x", "call_id": "c2"}))
	if msg := BuildMessage("t", codexEntry(nil), "question", tagPrefix, now); msg.Body != "Агент остановился и ждёт твоего решения" {
		t.Fatalf("%+v", msg)
	}
}

func TestStripMarkdownAndClip(t *testing.T) {
	for in, want := range map[string]string{
		"## Итог\n\n- **жирно** и *курсив*\n- [ссылка](http://x) `код`":   "Итог жирно и курсив ссылка код",
		"Текст\n```go\nfunc x() {}\n```\nпосле":                           "Текст после",
		"> цитата\n1. первое\n---\n~~старое~~ __важно__ _снова_ ![](img)": "цитата первое старое важно снова",
		"a*b": "a*b",
	} {
		if got := StripMarkdown(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if got := Clip("  много   пробелов\nи строк  ", 80); got != "много пробелов и строк" {
		t.Fatalf("%q", got)
	}
	if got := Clip(strings.Repeat("я ", 100), 10); got != "я я я я я…" {
		t.Fatalf("%q", got)
	}
}

// push-topic: only characters Apple accepts, within 32 of them, one per conversation.
func TestTopic(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	for _, tag := range []string{"app:tab-6scbT7", "app:tab--GL2AC", "app:tab-_cOdB0"} {
		if !re.MatchString(Topic(tag)) {
			t.Errorf("%q", Topic(tag))
		}
	}
	if Topic("app:tab-a") != Topic("app:tab-a") || Topic("app:tab--GL2AC") == Topic("app:tab-_GL2AC") {
		t.Fatal("one per conversation")
	}
}

// push-message: push targets.
func TestSelectTargets(t *testing.T) {
	subs := []Subscription{{Endpoint: "laptop", DeviceID: "a"}, {Endpoint: "phone", DeviceID: "b"}, {Endpoint: "legacy"}}
	ends := func(s []Subscription) []string {
		var out []string
		for _, x := range s {
			out = append(out, x.Endpoint)
		}
		return out
	}
	if got := ends(SelectTargets(subs, map[string]bool{"a": true})); !slices.Equal(got, []string{"laptop"}) {
		t.Fatal(got)
	}
	for _, v := range []map[string]bool{{}, {"c": true}} {
		if got := ends(SelectTargets(subs, v)); !slices.Equal(got, []string{"laptop", "phone", "legacy"}) {
			t.Fatal(got)
		}
	}
	clock := now
	st := &Store{Dir: t.TempDir(), Now: func() time.Time { return clock }}
	st.MarkVisible("a")
	st.MarkVisible("b")
	st.MarkHidden("b")
	if v := st.Visible(); !v["a"] || v["b"] {
		t.Fatal(v)
	}
	clock = clock.Add(VisibilityTTL + time.Second)
	if v := st.Visible(); len(v) != 0 {
		t.Fatal("a device not heard from for a minute is not looked at")
	}
}

func TestVAPIDKeysFile(t *testing.T) {
	dir := t.TempDir()
	k, err := LoadOrCreateVAPID(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, VAPIDFile))
	if st.Mode().Perm() != 0o600 {
		t.Fatal("mode")
	}
	pub, _ := b64.DecodeString(k.PublicKey)
	priv, _ := b64.DecodeString(k.PrivateKey)
	if len(pub) != 65 || pub[0] != 4 || len(priv) != 32 {
		t.Fatalf("%d %d", len(pub), len(priv))
	}
	again, _ := LoadOrCreateVAPID(dir)
	if again != k {
		t.Fatal("existing keys are kept")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, VAPIDFile))
	if !strings.HasPrefix(string(raw), "{\n  \"publicKey\": ") {
		t.Fatalf("%s", raw)
	}
}

// --- a fake push service

type received struct {
	header http.Header
	body   []byte
	path   string
}

type fakeService struct {
	mu   sync.Mutex
	got  []received
	gone map[string]bool
	srv  *httptest.Server
}

func newFakeService(t *testing.T) *fakeService {
	f := &fakeService{gone: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.got = append(f.got, received{r.Header.Clone(), body, r.URL.Path})
		gone := f.gone[r.URL.Path]
		f.mu.Unlock()
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type device struct {
	key  *ecdh.PrivateKey
	auth []byte
	sub  map[string]any
}

func newDevice(t *testing.T, endpoint, deviceID string) device {
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	sub := map[string]any{"endpoint": endpoint, "keys": map[string]any{"p256dh": b64.EncodeToString(key.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)}}
	if deviceID != "" {
		sub["deviceId"] = deviceID
	}
	return device{key, auth, sub}
}

// verifyVAPID checks the Authorization header: an ES256 token for the endpoint's origin, signed by the key it names.
func verifyVAPID(t *testing.T, header, origin, subject string) {
	t.Helper()
	re := regexp.MustCompile(`^vapid t=([^.]+)\.([^.]+)\.([^,]+), k=(\S+)$`)
	mm := re.FindStringSubmatch(header)
	if mm == nil {
		t.Fatalf("authorization %q", header)
	}
	pub, _ := b64.DecodeString(mm[4])
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pub)
	if err != nil {
		t.Fatalf("vapid key: %v", err)
	}
	sig, _ := b64.DecodeString(mm[3])
	digest := sha256.Sum256([]byte(mm[1] + "." + mm[2]))
	if len(sig) != 64 || !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature")
	}
	hdr, _ := b64.DecodeString(mm[1])
	claimsRaw, _ := b64.DecodeString(mm[2])
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(claimsRaw, &claims)
	if string(hdr) != `{"typ":"JWT","alg":"ES256"}` || claims.Aud != origin || claims.Sub != subject || claims.Exp != now.Add(12*time.Hour).Unix() {
		t.Fatalf("%s %+v", hdr, claims)
	}
}

func TestPushToAFakeService(t *testing.T) {
	svc := newFakeService(t)
	dir := t.TempDir()
	laptop := newDevice(t, svc.srv.URL+"/laptop", "a")
	phone := newDevice(t, svc.srv.URL+"/phone", "b")
	old := newDevice(t, svc.srv.URL+"/old", "")
	raw, _ := json.MarshalIndent([]any{laptop.sub, phone.sub, old.sub}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, SubscriptionsFile), raw, 0o600)
	svc.gone["/old"] = true

	s := New(Config{Dir: dir, Subject: "mailto:ops@example.org", TagPrefix: tagPrefix, Now: func() time.Time { return now }})
	transcript := writeJSONL(t, user("go", "2026-09-25T10:00:00Z"), assistant([]any{m{"type": "text", "text": "Готово."}}, "2026-09-25T10:01:00Z"))
	s.Push("tab-9", tabEntry(transcript, nil), "done")

	if len(svc.got) != 3 {
		t.Fatalf("%d pushes", len(svc.got))
	}
	pub, _ := s.PublicKey()
	for _, r := range svc.got {
		h := r.header
		if h.Get("TTL") != "21600" || h.Get("Urgency") != "normal" || h.Get("Topic") != Topic("app:tab-9") ||
			h.Get("Content-Encoding") != "aes128gcm" || h.Get("Content-Type") != "application/octet-stream" {
			t.Fatalf("headers %v", h)
		}
		verifyVAPID(t, h.Get("Authorization"), svc.srv.URL, "mailto:ops@example.org")
		if !strings.HasSuffix(h.Get("Authorization"), "k="+pub) {
			t.Fatal("the key the page subscribed with")
		}
		dev := map[string]device{"/laptop": laptop, "/phone": phone, "/old": old}[r.path]
		plain, err := Decrypt(r.body, dev.key, dev.auth)
		if err != nil {
			t.Fatalf("%s: %v", r.path, err)
		}
		var msg Message
		if err := json.Unmarshal(plain, &msg); err != nil || msg.Body != "Готово — Готово." || msg.TabID != "tab-9" {
			t.Fatalf("%s %v", plain, err)
		}
	}
	left := s.Store.Subscriptions()
	if len(left) != 2 || left[0].Endpoint != laptop.sub["endpoint"] || left[1].DeviceID != "b" {
		t.Fatalf("the gone subscription is dropped, the others kept: %+v", left)
	}
	kept, _ := os.ReadFile(filepath.Join(dir, SubscriptionsFile))
	want, _ := json.MarshalIndent([]any{laptop.sub, phone.sub}, "", "  ")
	var a, b any
	_ = json.Unmarshal(kept, &a)
	_ = json.Unmarshal(want, &b)
	if !strings.HasPrefix(string(kept), "[\n  {\n    \"") || !jsonEqual(a, b) {
		t.Fatalf("%s", kept)
	}

	// Someone looks at the laptop: a question goes there only, urgent.
	svc.got = nil
	s.Store.MarkVisible("a")
	s.Push("tab-9", tabEntry(transcript, func(e *status.Entry) { e.CliState = "needs-input" }), "question")
	if len(svc.got) != 1 || svc.got[0].path != "/laptop" || svc.got[0].header.Get("Urgency") != "high" {
		t.Fatalf("%+v", svc.got)
	}
	// No subscriptions: nothing is read or sent.
	_ = os.WriteFile(filepath.Join(dir, SubscriptionsFile), []byte("[]"), 0o600)
	svc.got = nil
	s.Push("tab-9", tabEntry(transcript, nil), "done")
	if len(svc.got) != 0 {
		t.Fatal("sent without subscriptions")
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestRemoveKeepsTheOtherRecordsAsWritten(t *testing.T) {
	dir := t.TempDir()
	rec := func(n, extra string) string {
		return "  {\n    \"endpoint\": \"https://push.example/" + n + "\",\n    \"keys\": {\n      \"p256dh\": \"P" + n + "\",\n      \"auth\": \"A" + n + "\"\n    }" + extra + "\n  }"
	}
	a, b, c := rec("a", ",\n    \"deviceId\": \"d<1>\",\n    \"mobile\": true"), rec("b", ""), rec("c", ",\n    \"deviceId\": \"d3\"")
	_ = os.WriteFile(filepath.Join(dir, SubscriptionsFile), []byte("[\n"+a+",\n"+b+",\n"+c+"\n]"), 0o600)
	st := &Store{Dir: dir}
	if subs := st.Subscriptions(); len(subs) != 3 || subs[0].Mobile == nil || !*subs[0].Mobile || subs[2].DeviceID != "d3" {
		t.Fatalf("%+v", subs)
	}
	if err := st.Remove("https://push.example/b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, SubscriptionsFile)); string(got) != "[\n"+a+",\n"+c+"\n]" {
		t.Fatalf("%s", got)
	}
	if err := st.Remove("https://push.example/none"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(dir, SubscriptionsFile)); st.Mode().Perm() != 0o600 {
		t.Fatal("mode")
	}
}

// Add and Remove from many goroutines go through one lock: no subscription is lost to a concurrent write.
func TestAddReplacesTheDeviceAndKeepsEveryWrite(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Dir: dir}
	keys := jsvalue.NewObject("p256dh", "P", "auth", "A")
	yes := true
	if err := st.Add("https://push.example/1", keys, "laptop", nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Add("https://push.example/2", keys, "laptop", &yes); err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\n    \"endpoint\": \"https://push.example/2\",\n    \"keys\": {\n      \"p256dh\": \"P\",\n      \"auth\": \"A\"\n    },\n    \"deviceId\": \"laptop\",\n    \"mobile\": true\n  }\n]"
	if got, _ := os.ReadFile(filepath.Join(dir, SubscriptionsFile)); string(got) != want {
		t.Fatalf("%s", got)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = st.Add("https://push.example/n"+strconv.Itoa(i), keys, "", nil)
		}(i)
	}
	wg.Wait()
	if got := st.DeviceEndpoints(); len(got) != 21 {
		t.Fatalf("%d subscriptions", len(got))
	}
	if subs := st.Subscriptions(); len(subs) != 21 || subs[0].DeviceID != "laptop" {
		t.Fatalf("%+v", subs[0])
	}
}
