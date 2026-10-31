package providers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeJWT is a token of the claims, unsigned: the runtime reads its claims and never checks a signature. It is built
// at run time, so no token-shaped literal is in the tree.
func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(claims) + ".c2ln"
}

// codexAuth writes a Codex auth.json of a ChatGPT sign-in whose access token runs out at exp.
func codexAuth(t *testing.T, path, mode, access string, exp time.Time, refresh bool) {
	t.Helper()
	tokens := map[string]any{
		"id_token": fakeJWT(t, map[string]any{"email": "me@example.com",
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-from-id"}}),
		"access_token": fakeJWT(t, map[string]any{"exp": exp.Unix(), "tok": access,
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-1"}}),
		"account_id": "acct-1",
	}
	if refresh {
		tokens["refresh_token"] = "rt-" + access
	}
	raw, _ := json.Marshal(map[string]any{"auth_mode": mode, "OPENAI_API_KEY": nil, "tokens": tokens, "last_refresh": "2026-10-26T12:00:00Z"})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// claimOf is a claim of a fakeJWT token.
func claimOf(t *testing.T, token, key string) any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a token: %q", token)
	}
	var all map[string]any
	_ = json.Unmarshal(must(base64.RawURLEncoding.DecodeString(parts[1])), &all)
	return all[key]
}

func TestTheCodexSignInIsReadByTheSubscriptionOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	c := &CodexLogin{Path: path, Now: func() time.Time { return now }}
	if _, ok := c.Account(); ok {
		t.Fatal("no file, no sign-in")
	}
	codexAuth(t, path, "chatgpt", "a", now.Add(240*time.Hour), true)
	if acc, ok := c.Account(); !ok || acc != "me@example.com" || c.AccountID() != "acct-1" {
		t.Fatalf("a ChatGPT sign-in: %q %v %q", acc, ok, c.AccountID())
	}
	if tok, err := c.Token(context.Background()); err != nil || claimOf(t, tok, "tok") != "a" {
		t.Fatalf("fresh token: %v", err)
	}
	codexAuth(t, path, "apikey", "a", now.Add(240*time.Hour), true)
	if _, ok := c.Account(); ok {
		t.Fatal("a sign-in by an API key is no subscription")
	}
	raw, _ := json.Marshal(map[string]any{"OPENAI_API_KEY": "sk-" + "x", "tokens": nil})
	_ = os.WriteFile(path, raw, 0o600)
	if _, ok := c.Account(); ok {
		t.Fatal("an API key alone is no subscription")
	}
	codexAuth(t, path, "chatgpt", "a", now.Add(-time.Minute), false)
	if _, ok := c.Account(); ok {
		t.Fatal("run out and nothing to renew it with")
	}
	if got := CodexHome("/home/u"); os.Getenv("CODEX_HOME") == "" && got != "/home/u/.codex" {
		t.Fatalf("home %s", got)
	}
}

func TestTheCodexTokenIsRenewedThroughTheCLINearItsEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	renews := 0
	c := &CodexLogin{Path: path, Now: func() time.Time { return now }, Renew: func(context.Context) error {
		renews++
		codexAuth(t, path, "chatgpt", "new", now.Add(240*time.Hour), true)
		return nil
	}}
	// Ten minutes left: Codex itself would not renew it yet, so neither does the runtime.
	codexAuth(t, path, "chatgpt", "old", now.Add(10*time.Minute), true)
	if tok, err := c.Token(context.Background()); err != nil || claimOf(t, tok, "tok") != "old" || renews != 0 {
		t.Fatalf("not yet: %v %d", err, renews)
	}
	codexAuth(t, path, "chatgpt", "old", now.Add(2*time.Minute), true)
	if tok, err := c.Token(context.Background()); err != nil || claimOf(t, tok, "tok") != "new" || renews != 1 {
		t.Fatalf("renewed: %v %d", err, renews)
	}
	// The CLI fails: a token not run out yet still goes, one that has does not.
	c.Renew = func(context.Context) error { return errors.New("offline") }
	codexAuth(t, path, "chatgpt", "old", now.Add(2*time.Minute), true)
	if tok, err := c.Token(context.Background()); err != nil || claimOf(t, tok, "tok") != "old" {
		t.Fatalf("last resort: %v", err)
	}
	codexAuth(t, path, "chatgpt", "old", now.Add(-time.Minute), true)
	if _, err := c.Token(context.Background()); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("run out: %v", err)
	}
}

func TestTheProxyConfigListensOnLoopbackWithItsKeysAndModels(t *testing.T) {
	y := string(ProxyConfig{Port: 41234, AuthDir: "/data/p/auth", ClientKey: "client-key", RelayKey: "relay-key",
		RelayBase: "http://127.0.0.1:7000" + GPTRelayPath}.YAML())
	for _, want := range []string{
		"host: \"127.0.0.1\"\n", "port: 41234\n", "auth-dir: \"/data/p/auth\"\n", "api-keys:\n  - \"client-key\"\n",
		"  secret-key: \"\"\n", "  disable-control-panel: true\n", "disable-image-generation: true\n",
		"  - api-key: \"relay-key\"\n    base-url: \"http://127.0.0.1:7000/api/cli/gpt/v1\"\n    proxy-url: \"direct\"\n",
		"      - name: \"gpt-6.1-sol\"\n        alias: \"gpt-6.1-sol\"\n",
		"          levels: [\"low\", \"medium\", \"high\", \"xhigh\", \"max\"]\n",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("no %q in\n%s", want, y)
		}
	}
	p, _ := Find("gpt")
	if n := strings.Count(y, "      - name: "); n != len(p.Models) {
		t.Errorf("%d models, want %d", n, len(p.Models))
	}
	if strings.Contains(y, "refresh") || strings.Contains(y, "access_token") {
		t.Error("the proxy is given a sign-in")
	}
}

func TestTheGPTRelayPutsTheChatGPTTokenAndAccountOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	codexAuth(t, path, "chatgpt", "a", time.Now().Add(240*time.Hour), true)
	login := &CodexLogin{Path: path}
	want, _ := login.Token(context.Background())
	var got *http.Request
	var body []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer up.Close()
	g := &GPTProxy{Dir: t.TempDir(), Login: login}
	rl := g.Relay()
	rl.Upstream = up.URL + "/backend-api/codex"
	srv := httptest.NewServer(rl)
	defer srv.Close()

	// Namespaced tools are what the xAI rewrite flattens; the ChatGPT backend takes them, so they go as they are.
	sent := `{"model":"gpt-6-luna","stream":true,"store":false,"instructions":"","tools":[{"type":"namespace","name":"mcp","tools":[]}],"input":[]}`
	post := func(secret string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+GPTRelayPath+"/responses", strings.NewReader(sent))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Originator", "codex-tui")
		req.Header.Set("Session_id", "s-1")
		req.Header.Set("Chatgpt-Account-Id", "someone-else")
		req.Header.Set("X-Other", "dropped")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := post("not-the-secret"); res.StatusCode != http.StatusForbidden || got != nil {
		t.Fatalf("a stranger: %d", res.StatusCode)
	}
	res := post(g.Secret())
	out, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(out), "response.completed") {
		t.Fatalf("answer %d %s", res.StatusCode, out)
	}
	if got.URL.Path != "/backend-api/codex/responses" || got.Header.Get("Authorization") != "Bearer "+want ||
		got.Header.Get("Chatgpt-Account-Id") != "acct-1" || got.Header.Get("Originator") != "codex-tui" ||
		got.Header.Get("Session_id") != "s-1" || got.Header.Get("X-Other") != "" {
		t.Fatalf("upstream %s %v", got.URL.Path, got.Header)
	}
	if string(body) != sent {
		t.Fatalf("the body changed: %s", body)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// TestHelperFakeProxy is the proxy of the switch tests, run as a child of the test binary: it reads the port and the
// client key of the config it is given and lists models to that key until it is stopped.
func TestHelperFakeProxy(t *testing.T) {
	cfg := os.Getenv("HRN_FAKE_PROXY_CONFIG")
	if cfg == "" {
		t.Skip("the child of a switch test")
	}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		os.Exit(3)
	}
	port := regexp.MustCompile(`(?m)^port: (\d+)$`).FindSubmatch(raw)
	key := regexp.MustCompile(`(?m)^api-keys:\n  - "([^"]+)"$`).FindSubmatch(raw)
	if port == nil || key == nil {
		os.Exit(4)
	}
	err = http.ListenAndServe("127.0.0.1:"+string(port[1]), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+string(key[1]) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	_ = err
	os.Exit(5)
}

func fakeProxyCommand(bin, config string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperFakeProxy$")
	cmd.Env = append(os.Environ(), "HRN_FAKE_PROXY_CONFIG="+config)
	return cmd
}

// newGPT is a switch over a signed-in Codex, whose proxy is the fake one.
func newGPT(t *testing.T) (*GPTProxy, string) {
	t.Helper()
	dir := t.TempDir()
	auth := filepath.Join(dir, "auth.json")
	codexAuth(t, auth, "chatgpt", "a", time.Now().Add(240*time.Hour), true)
	installed := 0
	g := &GPTProxy{Dir: filepath.Join(dir, "providers", "gpt-proxy"), OnFile: filepath.Join(dir, "providers", "gpt.on"),
		Login: &CodexLogin{Path: auth}, RelayBase: "http://127.0.0.1:1" + GPTRelayPath, Command: fakeProxyCommand,
		Install: func(_ context.Context, d string) (string, error) {
			installed++
			bin := ProxyBin(d)
			return bin, writePrivate(bin, []byte("#!/bin/sh\n"))
		}}
	t.Cleanup(g.Close)
	return g, auth
}

func TestTheGPTSwitchStartsTheProxyAndIsKept(t *testing.T) {
	g, _ := newGPT(t)
	if g.On() || g.Ready() {
		t.Fatal("off at first")
	}
	if _, _, ok := g.Endpoint(); ok {
		t.Fatal("an endpoint while off")
	}
	if err := g.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	base, keyFile, ok := g.Endpoint()
	if !g.On() || !g.Ready() || !ok || !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("on: %v %v %s %v", g.On(), g.Ready(), base, ok)
	}
	key, _ := os.ReadFile(keyFile)
	if fi, _ := os.Stat(g.Dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", fi.Mode())
	}
	for _, f := range []string{keyFile, g.configFile(), g.OnFile} {
		if fi, err := os.Stat(f); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v", f, err)
		}
	}
	port, _ := g.savedPort()
	if !proxyAnswers(context.Background(), port, string(key)) {
		t.Fatal("the proxy does not answer")
	}

	// Another runtime on the same folder finds the switch on, and the proxy again on the same port and key.
	g.Close()
	if !portFree(port) {
		t.Fatal("the proxy outlived Close")
	}
	g2 := &GPTProxy{Dir: g.Dir, OnFile: g.OnFile, Login: g.Login, RelayBase: g.RelayBase, Command: fakeProxyCommand, Install: g.Install}
	t.Cleanup(g2.Close)
	if !g2.On() {
		t.Fatal("the switch was not kept")
	}
	if err := g2.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if base2, keyFile2, _ := g2.Endpoint(); base2 != base || keyFile2 != keyFile {
		t.Fatalf("moved: %s %s", base2, keyFile2)
	}
	if err := g2.SetOn(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if g2.On() || g2.Ready() || !portFree(port) {
		t.Fatal("off leaves the proxy or the switch")
	}
}

func TestTheGPTSwitchRefusesWithItsReason(t *testing.T) {
	reason := func(err error) wire.ProfileSwitchReason {
		var se *wire.ProfileSwitchError
		if !errors.As(err, &se) {
			t.Fatalf("not a switch error: %v", err)
		}
		return se.Reason
	}
	g, auth := newGPT(t)
	codexAuth(t, auth, "apikey", "a", time.Now().Add(time.Hour), true)
	if r := reason(g.SetOn(context.Background(), true)); r != wire.SwitchNoLogin || g.On() {
		t.Fatalf("no login: %s", r)
	}
	codexAuth(t, auth, "chatgpt", "a", time.Now().Add(240*time.Hour), true)
	g.Install = func(context.Context, string) (string, error) { return "", errors.New("offline") }
	if r := reason(g.SetOn(context.Background(), true)); r != wire.SwitchDownload || g.On() {
		t.Fatalf("download: %s", r)
	}
	g.Install = func(_ context.Context, d string) (string, error) { return "/bin/false", nil }
	g.Command = func(bin, _ string) *exec.Cmd { return exec.Command(bin) }
	if r := reason(g.SetOn(context.Background(), true)); r != wire.SwitchStart || g.On() {
		t.Fatalf("start: %s", r)
	}
	if raw, _ := os.ReadFile(g.LogFile()); raw == nil {
		t.Log("the proxy said nothing")
	}
}

func TestAGPTChatGoesToTheProxyWithTheProxysKey(t *testing.T) {
	st := newStore(t)
	if st.Ready("gpt") {
		t.Fatal("ready without a switch")
	}
	if _, err := st.Set("gpt", fakeKey); !errors.Is(err, wire.ErrProfileUnknown) {
		t.Fatalf("a switched profile took a key: %v", err)
	}
	hooks := filepath.Join(t.TempDir(), "hooks.json")
	_ = os.WriteFile(hooks, []byte(`{}`), 0o600)
	g, _ := newGPT(t)
	st.Switches = map[string]Switch{"gpt": g}
	if _, err := st.SettingsFile("gpt", "gpt-6-luna", hooks); !errors.Is(err, ErrSwitchOff) {
		t.Fatalf("settings while off: %v", err)
	}
	if err := g.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !st.Ready("gpt") {
		t.Fatal("not ready while on")
	}
	path, err := st.SettingsFile("gpt", "gpt-6.1-sol", hooks)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Env          map[string]string `json:"env"`
		APIKeyHelper string            `json:"apiKeyHelper"`
	}
	_ = json.Unmarshal(must(os.ReadFile(path)), &got)
	base, keyFile, _ := g.Endpoint()
	if got.Env["ANTHROPIC_BASE_URL"] != base || got.APIKeyHelper != "cat '"+keyFile+"'" ||
		got.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-6-luna" || got.Env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "475000" {
		t.Fatalf("settings %+v", got)
	}
	src := &Source{Store: st}
	list, _ := src.Profiles(context.Background())
	for _, p := range list.Profiles {
		if (p.ID == "gpt") != (p.Switch != nil) {
			t.Errorf("%s: switch %+v", p.ID, p.Switch)
		}
		if p.ID == "gpt" && (!p.Switch.On || p.Key.Configured || p.Login == nil && st.Logins["gpt"] != nil) {
			t.Errorf("gpt: %+v", p)
		}
	}
	if key, err := st.Credential(context.Background(), "gpt"); err != nil || key != strings.TrimSpace(string(must(os.ReadFile(keyFile)))) {
		t.Fatalf("the test goes with the proxy's key: %v", err)
	}
	info, err := src.SwitchProfile(context.Background(), "gpt", false)
	if err != nil || info.Switch == nil || info.Switch.On || st.Ready("gpt") {
		t.Fatalf("off: %v %+v", err, info)
	}
	if _, err := src.SwitchProfile(context.Background(), "kimi", true); !errors.Is(err, wire.ErrProfileUnknown) {
		t.Fatalf("kimi has no switch: %v", err)
	}
}

func TestTheProxyArchiveIsTakenOnlyWithItsDigest(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{"README.md": "readme", proxyBinName: "#!/bin/sh\necho proxy\n"} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(data))
	}
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) }))
	defer srv.Close()
	dir := t.TempDir()
	bin := ProxyBin(dir)
	_ = os.MkdirAll(filepath.Dir(bin), 0o700)
	if err := fetchProxy(context.Background(), srv.Client(), srv.URL, strings.Repeat("0", 64), bin); err == nil {
		t.Fatal("a wrong digest passed")
	}
	if _, err := os.Stat(bin); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a wrong archive left a binary")
	}
	if err := fetchProxy(context.Background(), srv.Client(), srv.URL, hex.EncodeToString(sum[:]), bin); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(bin); err != nil || fi.Mode().Perm() != 0o700 || string(must(os.ReadFile(bin))) != "#!/bin/sh\necho proxy\n" {
		t.Fatalf("binary: %v", err)
	}
	if got, err := InstallProxy(context.Background(), dir, nil); err != nil || got != bin {
		t.Fatalf("an installed binary is taken as it is: %s %v", got, err)
	}
	for k, a := range proxyAssets {
		if len(a.sha256) != 64 || !strings.Contains(a.name, ProxyVersion) {
			t.Errorf("%s: %+v", k, a)
		}
	}
}
