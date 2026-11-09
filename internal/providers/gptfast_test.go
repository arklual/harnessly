package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
)

func TestAChatPaneIsReadOffItsHeader(t *testing.T) {
	for _, c := range []struct{ in, pane, socket string }{
		{"harnessly-pane/%12//tmp/tmux-1001/harnessly,1550,0", "%12", "harnessly"},
		{" harnessly-pane/%3/ ", "%3", ""},
		{"harnessly-pane/%7", "%7", ""},
	} {
		pane, socket, ok := chatPane(c.in)
		if !ok || pane != c.pane || socket != c.socket {
			t.Fatalf("%q: %q %q %v", c.in, pane, socket, ok)
		}
	}
	for _, v := range []string{"", "0.167.0", "pane/%1"} {
		if _, _, ok := chatPane(v); ok {
			t.Fatalf("%q read as a pane", v)
		}
	}
}

// The header the launch line sets reads back as the pane and the server it ran in, once sh fills it in.
func TestTheLaunchLineHeaderNamesThePaneAfterSh(t *testing.T) {
	out, err := exec.Command("sh", "-c", "TMUX_PANE=%5 TMUX=/tmp/tmux-1/harnessly,9,0; exec env "+ChatHeaderEnv+` sh -c 'printf %s "$ANTHROPIC_CUSTOM_HEADERS"'`).Output()
	if err != nil {
		t.Fatal(err)
	}
	name, value, _ := strings.Cut(string(out), ": ")
	pane, socket, ok := chatPane(value)
	if name != chatHeader || !ok || pane != "%5" || socket != "harnessly" {
		t.Fatalf("%q", out)
	}
}

func TestTheFastTierIsAskedForOnAModelThatHasIt(t *testing.T) {
	has := func(m string) bool { return m == "gpt-6.1-sol" }
	in := `{"model":"gpt-6.1-sol","input":[{"type":"message","content":"<a&b>"}]}`
	got := string(WithFastTier([]byte(in), has))
	if got != `{"service_tier":"priority",`+in[1:] {
		t.Fatalf("%s", got)
	}
	var tier struct {
		ServiceTier string `json:"service_tier"`
	}
	if err := json.Unmarshal([]byte(got), &tier); err != nil || tier.ServiceTier != "priority" {
		t.Fatalf("%s: %v", got, err)
	}
	// A tier already asked for is replaced, not doubled.
	replaced := string(WithFastTier([]byte(`{"service_tier":"auto","model":"gpt-6.1-sol","x":"<"}`), has))
	if strings.Count(replaced, "service_tier") != 1 || !strings.Contains(replaced, `"priority"`) || !strings.Contains(replaced, `"<"`) {
		t.Fatalf("%s", replaced)
	}
	// Another model, or what is not a JSON object, goes as it came.
	for _, body := range []string{`{"model":"gpt-6-astra","input":[]}`, `not json`, `[1]`} {
		if got := string(WithFastTier([]byte(body), has)); got != body {
			t.Fatalf("%s became %s", body, got)
		}
	}
}

func TestTheGPTRelayAsksForTheFastTierOfAChatInFastMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	codexAuth(t, path, "chatgpt", "a", time.Now().Add(240*time.Hour), true)
	var got *http.Request
	var body []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		_, _ = io.WriteString(w, "{}")
	}))
	defer up.Close()
	fast := map[string]bool{"%1": true}
	var asked []string
	g := &GPTProxy{Dir: t.TempDir(), Login: &CodexLogin{Path: path},
		FastTier: func(m string) bool { return m == "gpt-6.1-sol" },
		FastPane: func(pane, socket string) bool { asked = append(asked, pane+"@"+socket); return fast[pane] }}
	rl := g.Relay()
	rl.Upstream = up.URL + "/backend-api/codex"
	srv := httptest.NewServer(rl)
	defer srv.Close()
	post := func(pathRest, marker, sent string) string {
		t.Helper()
		got, body = nil, nil
		req, _ := http.NewRequest("POST", srv.URL+GPTRelayPath+pathRest, strings.NewReader(sent))
		req.Header.Set("Authorization", "Bearer "+g.Secret())
		if marker != "" {
			req.Header.Set("Version", marker)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if got == nil {
			t.Fatal("nothing went up")
		}
		return string(body)
	}
	sent := `{"model":"gpt-6.1-sol","input":[]}`

	if b := post("/responses", "harnessly-pane/%1//tmp/tmux-1/harnessly,1,0", sent); b != `{"service_tier":"priority",`+sent[1:] {
		t.Fatalf("a chat in fast mode: %s", b)
	}
	if got.Header.Get("Version") != "" {
		t.Fatalf("the pane header went up: %v", got.Header)
	}
	if len(asked) != 1 || asked[0] != "%1@harnessly" {
		t.Fatalf("asked %v", asked)
	}
	// A chat not in fast mode, a model without the tier, another path: as they came, the pane header still taken off.
	if b := post("/responses", "harnessly-pane/%2/", sent); b != sent || got.Header.Get("Version") != "" {
		t.Fatalf("a chat not in fast mode: %s %v", b, got.Header)
	}
	if b := post("/responses", "harnessly-pane/%1/", `{"model":"gpt-6-astra"}`); b != `{"model":"gpt-6-astra"}` {
		t.Fatalf("a model without the tier: %s", b)
	}
	if b := post("/responses/compact", "harnessly-pane/%1/", sent); b != sent {
		t.Fatalf("compact: %s", b)
	}
	// A Version that is not the pane header is the client's own and goes up.
	if b := post("/responses", "0.167.0", sent); b != sent || got.Header.Get("Version") != "0.167.0" {
		t.Fatalf("a client version: %s %v", b, got.Header)
	}
}

func TestTheGPTProfileOffersFastModeOnTheModelsWithTheTier(t *testing.T) {
	g := &GPTProxy{FastTier: func(m string) bool { return m == "gpt-6.1-sol" }}
	store := &Store{Dir: t.TempDir(), Switches: map[string]Switch{"gpt": g}}
	p, _ := Find("gpt")
	fast := map[string]bool{}
	for _, m := range store.AgentModels(p) {
		fast[m.ID] = m.Fast
	}
	if !fast["gpt-6.1-sol"] || fast["gpt-6-astra"] {
		t.Fatalf("%v", fast)
	}
	if m, ok := (Launcher{Store: store}).Model("gpt", "gpt-6.1-sol"); !ok || !m.Fast {
		t.Fatalf("Model: %+v", m)
	}
	list, err := (&Source{Store: store}).Profiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range list.Profiles {
		for _, m := range info.Models {
			if m.Fast != (info.ID == "gpt" && m.ID == "gpt-6.1-sol") {
				t.Fatalf("%s %s fast=%v", info.ID, m.ID, m.Fast)
			}
		}
	}
	if (Launcher{Store: store}).ClaudeLineEnv("gpt") != ChatHeaderEnv || (Launcher{Store: store}).ClaudeLineEnv("auto") != "" {
		t.Fatal("the pane header is the gpt chat's only")
	}
}

// A GPT chat's launch and resume lines say its pane, and fast mode puts nothing of Claude Code's own on them.
func TestAGPTChatLineCarriesThePaneHeader(t *testing.T) {
	st := newStore(t)
	g, _ := newGPT(t)
	st.Switches = map[string]Switch{"gpt": g}
	if err := g.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	c := claudeCfg(t, st, true)
	model := "gpt-6.1-sol"
	l := &agents.Launch{Model: &model, Profile: "gpt", Fast: true}
	line, err := c.LaunchCommand("ws", l, nil)
	if err != nil {
		t.Fatal(err)
	}
	resume, err := c.ResumeCommand("11111111-2222-4333-8444-555555555555", "ws", l)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []string{line, resume} {
		if !strings.Contains(got, claude.LaunchEnv+" "+ChatHeaderEnv+" ") || strings.Contains(got, "fast") {
			t.Fatalf("%q", got)
		}
	}
	if _, err := st.Set("glm", fakeKey); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("glm")
	glm, err := c.LaunchCommand("ws", choose(p.Models[0].ID, "glm"), nil)
	if err != nil || strings.Contains(glm, "ANTHROPIC_CUSTOM_HEADERS") {
		t.Fatalf("glm: %q %v", glm, err)
	}
}
