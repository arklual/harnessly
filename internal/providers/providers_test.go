package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A provider-shaped key is never written out literally (see .gitleaks.toml), so it is joined from parts.
const fakeKey = "sk-" + "test-0000-abcdef123456"

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "providers")}
}

func TestPresetsAreWithinTheContract(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets() {
		if seen[p.ID] || !wire.ValidProfileID(p.ID) {
			t.Errorf("%s: id", p.ID)
		}
		seen[p.ID] = true
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Scheme != "https" || u.Host != p.Host {
			t.Errorf("%s: address %q host %q", p.ID, p.BaseURL, p.Host)
		}
		if p.Via == wire.ProviderCodex != (p.KeyEnv != "") {
			t.Errorf("%s: only a Codex profile takes its key from a variable", p.ID)
		}
		for _, m := range p.Models {
			if !agents.ModelIDRe.MatchString(m.ID) || m.Window < 100_000 || m.Label == "" {
				t.Errorf("%s: model %+v", p.ID, m)
			}
		}
		for _, id := range []string{p.Small, p.Test} {
			if _, ok := p.ModelOf(id); !ok {
				t.Errorf("%s: %q is none of its models", p.ID, id)
			}
		}
		info := wire.ProfileInfo{ID: p.ID, Label: p.Label, Vendor: p.Vendor, Via: p.Via, Host: p.Host, Models: p.AgentModels()}
		if err := info.Validate(); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		for _, m := range p.AgentModels() {
			if m.Efforts == nil || len(m.Efforts) != 0 || m.Ultracode {
				t.Errorf("%s: %s takes no effort and no ultracode", p.ID, m.ID)
			}
		}
	}
	for _, id := range []string{"kimi", "kimi-code", "glm", "glm-cn", "xai"} {
		if !seen[id] {
			t.Errorf("no preset %s", id)
		}
	}
}

func TestAKeyIsKeptInAFileOfTheOwnerAndShownByItsTail(t *testing.T) {
	s := newStore(t)
	k, err := s.Set("kimi", fakeKey)
	if err != nil || !k.Configured || *k.Tail != "…3456" || k.Validate() != nil {
		t.Fatalf("set: %+v %v", k, err)
	}
	if fi, err := os.Stat(s.KeyFile("kimi")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", fi, err)
	}
	if fi, err := os.Stat(s.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("folder: %v %v", fi, err)
	}
	if raw, _ := os.ReadFile(s.KeyFile("kimi")); string(raw) != fakeKey {
		t.Fatalf("the file holds %q: no newline, no more", raw)
	}
	if left, _ := filepath.Glob(filepath.Join(s.Dir, "*.tmp")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
	// Another key replaces it; another profile's is its own.
	if k, _ := s.Set("kimi", "sk-test-1111-zzzzzzzz7890"); *k.Tail != "…7890" {
		t.Fatalf("replace: %+v", k)
	}
	if s.Configured("glm") {
		t.Fatal("the profiles keep their keys apart")
	}
	if got := s.State("kimi"); !got.Configured || *got.Tail != "…7890" {
		t.Fatalf("state: %+v", got)
	}
	if k, err := s.Remove("kimi"); err != nil || k.Configured || k.Tail != nil || s.Configured("kimi") {
		t.Fatalf("remove: %+v %v", k, err)
	}
	if _, err := s.Remove("kimi"); err != nil {
		t.Fatalf("remove again: %v", err)
	}
	for _, id := range []string{"gemini", "../kimi"} {
		if _, err := s.Set(id, fakeKey); err != wire.ErrProfileUnknown {
			t.Errorf("%s: %v", id, err)
		}
	}
	if _, err := s.Set("kimi", "short"); err == nil {
		t.Fatal("a short key is no key")
	}
	if _, err := s.Key("kimi"); err != ErrNoKey {
		t.Fatalf("key after remove: %v", err)
	}
}

func TestTheSettingsFileHoldsNoKeyAndLeavesNoAnthropicTokenToTheVendor(t *testing.T) {
	s := newStore(t)
	hooks := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(hooks, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"x"}]}]},"env":{"KEEP":"1","ANTHROPIC_BASE_URL":"https://proxy.example"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("kimi", fakeKey); err != nil {
		t.Fatal(err)
	}
	path, err := s.SettingsFile("kimi", "kimi-k3.1-pro", hooks)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != s.Dir || filepath.Base(path) != "kimi.kimi-k3-1-pro.settings.json" {
		t.Fatalf("path %s", path)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), fakeKey) || strings.Contains(string(raw), "3456") {
		t.Fatal("the settings file holds the key")
	}
	var got struct {
		Hooks        map[string]any    `json:"hooks"`
		Env          map[string]string `json:"env"`
		APIKeyHelper string            `json:"apiKeyHelper"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Hooks["Stop"] == nil {
		t.Fatal("the hooks of harnessly are lost")
	}
	if got.APIKeyHelper != "cat '"+s.KeyFile("kimi")+"'" {
		t.Fatalf("apiKeyHelper %q", got.APIKeyHelper)
	}
	env := got.Env
	for k, want := range map[string]string{
		"ANTHROPIC_BASE_URL":                       "https://api.moonshot.ai/anthropic",
		"ANTHROPIC_MODEL":                          "kimi-k3.1-pro",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "kimi-k3.1-pro",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "kimi-k3.1-pro",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "kimi-k2.7-code",
		"CLAUDE_CODE_SUBAGENT_MODEL":               "kimi-k3.1-pro",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		// No credential of Anthropic's reaches the vendor: the two variables that win over the helper are empty, and a
		// `claude` started inside the chat finds a token that is no one's.
		"ANTHROPIC_AUTH_TOKEN":    "",
		"ANTHROPIC_API_KEY":       "",
		"CLAUDE_CODE_OAUTH_TOKEN": OAuthStub,
		"KEEP":                    "1",
	} {
		if v, ok := env[k]; !ok || v != want {
			t.Errorf("env %s = %q (present %v), want %q", k, v, ok, want)
		}
	}
	// A profile owns the address: the hooks file's own is overridden.
	if env["ANTHROPIC_BASE_URL"] == "https://proxy.example" {
		t.Fatal("the profile's address lost to the hooks file's")
	}

	// The window is the model's, GLM asks for its long timeout, and the file is the same until something changes.
	small, err := s.SettingsFile("kimi", "kimi-k2.7-code", hooks)
	if err != nil || small == path {
		t.Fatalf("a model of its own: %s %v", small, err)
	}
	raw, _ = os.ReadFile(small)
	if !strings.Contains(string(raw), `"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "262144"`) {
		t.Fatalf("window of the small model: %s", raw)
	}
	glm, err := s.SettingsFile("glm", "glm-5.3", hooks)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ = os.ReadFile(glm); !strings.Contains(string(raw), `"API_TIMEOUT_MS": "3000000"`) {
		t.Fatalf("glm: %s", raw)
	}
	before, _ := os.Stat(path)
	time.Sleep(5 * time.Millisecond)
	if again, err := s.SettingsFile("kimi", "kimi-k3.1-pro", hooks); err != nil || again != path {
		t.Fatalf("again: %s %v", again, err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged file is not written again")
	}

	for name, call := range map[string]func() error{
		"unknown profile": func() error { _, err := s.SettingsFile("gemini", "x", hooks); return err },
		"codex profile":   func() error { _, err := s.SettingsFile("xai", "grok-build-0.1", hooks); return err },
		"unknown model":   func() error { _, err := s.SettingsFile("kimi", "claude-opus-5-5", hooks); return err },
		"no hooks file":   func() error { _, err := s.SettingsFile("kimi", "kimi-k3.1-pro", hooks+".none"); return err },
	} {
		if call() == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestAKeyThatAnAgentPrintedLeavesMasked(t *testing.T) {
	s := newStore(t)
	key := `sk-te"st<0000>&abcdef123456`
	if _, err := s.Set("glm", key); err != nil {
		t.Fatal(err)
	}
	frame, err := wire.Marshal(map[string]any{"text": "cat says " + key + " and env says " + key, "n": 1})
	if err != nil {
		t.Fatal(err)
	}
	// Before the redactor is set the frame is the marshaller's own.
	if !strings.Contains(string(frame), "\\u003c0000") {
		t.Fatalf("plain frame: %s", frame)
	}
	wire.SetRedactor(s.Redact)
	t.Cleanup(func() { wire.SetRedactor(nil) })
	frame, _ = wire.Marshal(map[string]any{"text": "cat says " + key + " and env says " + key, "n": 1})
	if strings.Contains(string(frame), "abcdef") || strings.Contains(string(frame), "0000") {
		t.Fatalf("the key is on the wire: %s", frame)
	}
	var back struct{ Text string }
	if err := json.Unmarshal(frame, &back); err != nil || back.Text != "cat says •••3456 and env says •••3456" {
		t.Fatalf("masked: %q %v", back.Text, err)
	}
	// Raw bytes, as a page of a different encoder would carry it, are masked too.
	if got := string(s.Redact([]byte("x " + key))); got != "x •••3456" {
		t.Fatalf("raw: %q", got)
	}
	// A key saved later is masked at once; one removed is not masked any more.
	if _, err := s.Set("kimi", "sk-test-1111-zzzzzzzz7890"); err != nil {
		t.Fatal(err)
	}
	if got := string(s.Redact([]byte("sk-test-1111-zzzzzzzz7890"))); got != "•••7890" {
		t.Fatalf("new key: %q", got)
	}
	_, _ = s.Remove("kimi")
	if got := string(s.Redact([]byte("sk-test-1111-zzzzzzzz7890"))); got != "sk-test-1111-zzzzzzzz7890" {
		t.Fatalf("removed key: %q", got)
	}
	plain := []byte(`{"text":"nothing secret"}`)
	if got := s.Redact(plain); &got[0] != &plain[0] {
		t.Fatal("a frame with nothing to hide is returned as it is")
	}
}

// fakeVendor records what the test request carried.
type fakeVendor struct {
	mu      sync.Mutex
	status  int
	delay   time.Duration
	redirTo string
	path    string
	header  http.Header
	body    map[string]any
	calls   int
}

func (f *fakeVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls++
	f.path, f.header = r.URL.Path, r.Header.Clone()
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &f.body)
	status, delay, redir := f.status, f.delay, f.redirTo
	f.mu.Unlock()
	time.Sleep(delay)
	if redir != "" {
		http.Redirect(w, r, redir, http.StatusTemporaryRedirect)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + fakeKey + `"}`))
}

func TestTheConnectionIsTestedWithTheSavedKeyAgainstTheVendor(t *testing.T) {
	s := newStore(t)
	vendor := &fakeVendor{status: 200}
	srv := httptest.NewServer(vendor)
	defer srv.Close()
	src := &Source{Store: s, BaseURL: func(p Preset) string { return srv.URL + "/" + p.ID + "/" }}
	ctx := context.Background()

	got, err := src.TestProfile(ctx, "kimi")
	if err != nil || got.OK || got.Reason == nil || *got.Reason != wire.TestNoKey || got.Validate() != nil || vendor.calls != 0 {
		t.Fatalf("without a key: %+v %v (calls %d)", got, err, vendor.calls)
	}
	if _, err := src.TestProfile(ctx, "gemini"); err != wire.ErrProfileUnknown {
		t.Fatalf("unknown: %v", err)
	}
	if k, err := src.SetProfileKey(ctx, "kimi", fakeKey); err != nil || !k.Configured {
		t.Fatalf("save: %+v %v", k, err)
	}
	got, err = src.TestProfile(ctx, "kimi")
	if err != nil || !got.OK || got.Reason != nil || *got.Status != 200 || got.Validate() != nil {
		t.Fatalf("pass: %+v %v", got, err)
	}
	if vendor.path != "/kimi/v1/messages" || vendor.header.Get("Authorization") != "Bearer "+fakeKey || vendor.header.Get("x-api-key") != fakeKey ||
		vendor.header.Get("anthropic-version") == "" || vendor.body["model"] != "kimi-k2.7-code" || vendor.body["max_tokens"] != float64(16) {
		t.Fatalf("request: %s %v %v", vendor.path, vendor.header, vendor.body)
	}
	out, _ := json.Marshal(got)
	if strings.Contains(string(out), "abcdef") {
		t.Fatalf("the answer holds the vendor's body or the key: %s", out)
	}

	for status, reason := range map[int]wire.ProfileTestReason{401: wire.TestUnauthorized, 403: wire.TestForbidden, 429: wire.TestRateLimited, 500: wire.TestRejected, 400: wire.TestRejected} {
		vendor.mu.Lock()
		vendor.status = status
		vendor.mu.Unlock()
		got, err := src.TestProfile(ctx, "kimi")
		if err != nil || got.OK || got.Reason == nil || *got.Reason != reason || got.Status == nil || *got.Status != status || got.Validate() != nil {
			t.Errorf("%d: %+v %v", status, got, err)
		}
	}

	// Grok goes to the Responses API with a bearer only.
	vendor.status = 200
	if _, err := src.SetProfileKey(ctx, "xai", fakeKey); err != nil {
		t.Fatal(err)
	}
	if got, _ := src.TestProfile(ctx, "xai"); !got.OK || vendor.path != "/xai/responses" || vendor.header.Get("x-api-key") != "" ||
		vendor.header.Get("Authorization") != "Bearer "+fakeKey || vendor.body["model"] != "grok-build-0.1" || vendor.body["input"] == nil {
		t.Fatalf("xai: %+v %s %v %v", got, vendor.path, vendor.header, vendor.body)
	}

	// A redirect is not followed: it would carry the key to another host.
	other := &fakeVendor{status: 200}
	otherSrv := httptest.NewServer(other)
	defer otherSrv.Close()
	vendor.mu.Lock()
	vendor.redirTo = otherSrv.URL + "/stolen"
	vendor.mu.Unlock()
	got, _ = src.TestProfile(ctx, "kimi")
	if got.OK || other.calls != 0 || got.Reason == nil || *got.Reason != wire.TestRejected {
		t.Fatalf("redirect: %+v (calls %d)", got, other.calls)
	}
}

func TestATestThatDoesNotAnswerIsTimeoutOrUnreachable(t *testing.T) {
	s := newStore(t)
	vendor := &fakeVendor{status: 200, delay: 300 * time.Millisecond}
	srv := httptest.NewServer(vendor)
	src := &Source{Store: s, Client: &http.Client{Timeout: 50 * time.Millisecond}, BaseURL: func(Preset) string { return srv.URL }}
	if _, err := src.SetProfileKey(context.Background(), "glm", fakeKey); err != nil {
		t.Fatal(err)
	}
	got, err := src.TestProfile(context.Background(), "glm")
	if err != nil || got.OK || *got.Reason != wire.TestTimeout || got.Status != nil || got.Validate() != nil {
		t.Fatalf("slow: %+v %v", got, err)
	}
	srv.CloseClientConnections()
	srv.Close()
	got, _ = src.TestProfile(context.Background(), "glm")
	if got.OK || *got.Reason != wire.TestUnreachable {
		t.Fatalf("closed: %+v", got)
	}
}

func TestTheProfilesAreListedWithTheirKeyStateOnly(t *testing.T) {
	s := newStore(t)
	src := &Source{Store: s}
	if _, err := src.SetProfileKey(context.Background(), "glm", fakeKey); err != nil {
		t.Fatal(err)
	}
	list, err := src.Profiles(context.Background())
	if err != nil || list.Validate() != nil || len(list.Profiles) != len(Presets()) {
		t.Fatalf("list: %+v %v %v", list, err, list.Validate())
	}
	for _, p := range list.Profiles {
		if (p.ID == "glm") != p.Key.Configured {
			t.Errorf("%s: %+v", p.ID, p.Key)
		}
	}
	out, _ := wire.Marshal(list)
	if strings.Contains(string(out), "abcdef") {
		t.Fatalf("the list holds the key: %s", out)
	}
}

// Through the wire's own pages: a key goes in once and nothing that comes out holds it.
func TestTheKeyGoesInOnceAndNothingComesOut(t *testing.T) {
	s := newStore(t)
	wire.SetRedactor(s.Redact)
	t.Cleanup(func() { wire.SetRedactor(nil) })
	srv := httptest.NewServer(&wire.Server{Profiles: &Source{Store: s}, Limits: wire.DefaultLimits})
	defer srv.Close()
	do := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	code, body := do("PUT", "/api/v2/providers/glm/key", `{"key":"`+fakeKey+`"}`)
	if code != 200 || body != `{"configured":true,"tail":"…3456"}` {
		t.Fatalf("put: %d %s", code, body)
	}
	for _, path := range []string{"/api/v2/providers"} {
		if code, body := do("GET", path, ""); code != 200 || strings.Contains(body, "abcdef") || !strings.Contains(body, `"tail":"…3456"`) {
			t.Fatalf("get %s: %d %s", path, code, body)
		}
	}
	if code, body := do("DELETE", "/api/v2/providers/glm/key", ""); code != 200 || body != `{"configured":false,"tail":null}` {
		t.Fatalf("delete: %d %s", code, body)
	}
	if _, err := os.Stat(s.KeyFile("glm")); !os.IsNotExist(err) {
		t.Fatalf("the file stays: %v", err)
	}
}
