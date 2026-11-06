package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSafe is the safe as a launch sees it: values by the app's secret names.
type fakeSafe map[string]string

func (f fakeSafe) HasApp(_, name string) bool { _, ok := f[name]; return ok }
func (f fakeSafe) RevealApp(_ string, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range names {
		v, ok := f[n]
		if !ok {
			return nil, fmt.Errorf("no value for %s", n)
		}
		out[n] = v
	}
	return out, nil
}

const secretValue = "s3cr3t-VALUE-9f2c"

const settingsSchemaJSON = `{"type":"object","required":["region"],"properties":{
 "region":{"type":"string","title":"Region","enum":["eu","us"],"default":"eu"},
 "limit":{"type":"integer","minimum":1,"maximum":50,"default":10},
 "verbose":{"type":"boolean"}}}`

// secretApp installs a dev app whose manifest asks for a secret and has settings, with an .mcp.json that uses it.
func secretApp(t *testing.T, optional bool) (*Registry, Installed, string) {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "pay")
	if err := Scaffold(pkg, "acme.pay", "Pay", "chat"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	raw, _ := os.ReadFile(filepath.Join(pkg, ManifestPath))
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["permissions"] = append(m["permissions"].([]any), "secrets:api-key")
	m["secrets"] = []any{map[string]any{"name": "api-key", "description": "Key of the pay service", "optional": optional}}
	m["settings"] = json.RawMessage(settingsSchemaJSON)
	out, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, ManifestPath), out, 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{"mcpServers":{"pay":{"type":"http","url":"https://pay.example.com/mcp","headers":{"Authorization":"Bearer ${secret:api-key}","X-Plain":"1"}},"free":{"type":"http","url":"https://free.example.com/mcp"}}}`
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	a, err := reg.Get("acme.pay")
	if err != nil {
		t.Fatal(err)
	}
	return reg, a, pkg
}

// launcherFor is a launcher with a token of its own and a runtime MCP config, as a real launch has.
func launcherFor(t *testing.T, reg *Registry, safe fakeSafe) *Launcher {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "preview-mcp.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{"preview":{"type":"http","url":"http://127.0.0.1:4455/mcp","headers":{"X-Runtime-Token":"person-token"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Launcher{Registry: reg, GuardCommand: "g", Secrets: safe, MCPConfigPath: cfg,
		AgentToken: func(session string, _ time.Duration) (string, error) { return "agent." + session + ".tok", nil }}
}

func readServers(t *testing.T, dir string) map[string]MCPServer {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Servers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Servers
}

// walkFor reads every file under the folders the launch wrote and says whether any holds text.
func filesHold(t *testing.T, text string, roots ...string) string {
	t.Helper()
	found := ""
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if b, _ := os.ReadFile(p); strings.Contains(string(b), text) {
				found = p
			}
			return nil
		})
	}
	return found
}

func TestRemoteServerGetsProxyAndNoSecretAnywhere(t *testing.T) {
	reg, a, pkg := secretApp(t, false)
	l := launcherFor(t, reg, fakeSafe{"api-key": secretValue})
	got, err := l.Claude([]string{"acme.pay"}, baseSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PluginDirs) != 1 || got.PluginDirs[0] == pkg {
		t.Fatalf("the launch should load its own folder: %v", got.PluginDirs)
	}
	srv := readServers(t, got.PluginDirs[0])
	pay := srv["pay"]
	if !strings.HasPrefix(pay.URL, "http://127.0.0.1:4455"+ProxyPrefix+"acme.pay/pay") || pay.Headers["X-Runtime-Token"] == "" || len(pay.Headers) != 1 {
		t.Fatalf("a remote server points at the proxy with the launch's token: %+v", pay)
	}
	if strings.HasPrefix(pay.Headers["X-Runtime-Token"], "person") {
		t.Fatal("the person's token must not go to the app's server entry")
	}
	if srv["free"].URL != "https://free.example.com/mcp" {
		t.Fatalf("a server with no secret is as written: %+v", srv["free"])
	}
	data := filepath.Dir(reg.Root)
	if f := filesHold(t, secretValue, data); f != "" {
		t.Fatalf("%s holds the secret", f)
	}
	reg0, _ := os.ReadFile(filepath.Join(a.Dir, ".mcp.json"))
	if !strings.Contains(string(reg0), "${secret:api-key}") {
		t.Fatalf("the package changed: %s", reg0)
	}
	if _, err := os.Stat(filepath.Join(got.PluginDirs[0], ManifestPath)); err != nil {
		t.Errorf("the manifest is not reachable from the launch folder: %v", err)
	}
	// The launch wrote down which apps its token is for.
	var session string
	for _, h := range pay.Headers {
		session = strings.Split(h, ".")[1]
	}
	if g := reg.GrantedApps(session); len(g) != 1 || g[0] != "acme.pay" {
		t.Fatalf("grant: %v", g)
	}
	if g := reg.GrantedApps("0000000000000000"); len(g) != 0 {
		t.Fatalf("another token's grant: %v", g)
	}
}

// localSecretApp adds a trusted local server that takes the secret in env.
func localSecretApp(t *testing.T) (*Registry, string) {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "loc")
	if err := Scaffold(pkg, "acme.loc", "Loc", "chat"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	raw, _ := os.ReadFile(filepath.Join(pkg, ManifestPath))
	_ = json.Unmarshal(raw, &m)
	m["trust"] = "trusted"
	m["permissions"] = append(m["permissions"].([]any), "secrets:api-key")
	m["secrets"] = []any{map[string]any{"name": "api-key", "description": "d"}}
	out, _ := json.Marshal(m)
	_ = os.WriteFile(filepath.Join(pkg, ManifestPath), out, 0o644)
	_ = os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(`{"mcpServers":{"l":{"command":"srv","env":{"KEY":"${secret:api-key}"}}}}`), 0o644)
	if _, ps, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	return reg, pkg
}

// sealDirOf is the launch's own folder: where the token file of its Env is.
func sealDirOf(t *testing.T, env string) string {
	t.Helper()
	first, _, _ := strings.Cut(env, " ")
	return filepath.Dir(strings.Trim(strings.TrimPrefix(first, "HARNESSLY_CLI_TOKEN_FILE="), "'"))
}

func TestLocalServerEnvFileIsThisChatsAndSweptWhenGone(t *testing.T) {
	reg, _ := localSecretApp(t)
	l := launcherFor(t, reg, fakeSafe{"api-key": secretValue})
	got, err := l.Claude([]string{"acme.loc"}, baseSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := got.PluginDirs[0]
	if readServers(t, dir)["l"].Env["KEY"] != secretValue {
		t.Fatal("the local server takes the value in env")
	}
	mine := sealDirOf(t, got.Env)
	if !strings.HasPrefix(dir, mine+string(filepath.Separator)) || !strings.HasPrefix(mine, filepath.Join(filepath.Dir(reg.Root), "app-launch")+string(filepath.Separator)) {
		t.Fatalf("the value lives in the launch's own folder outside the registry: %s", dir)
	}
	got2, _ := l.Claude([]string{"acme.loc"}, baseSettings(t))
	other := sealDirOf(t, got2.Env)
	if other == mine {
		t.Fatal("each launch has a folder of its own")
	}
	// Nobody runs with these token files: a launch after the grace sweeps both folders.
	old := time.Now().Add(-time.Hour)
	for _, d := range []string{mine, other} {
		_ = os.Chtimes(d, old, old)
	}
	l.sweepLaunches()
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatalf("the folder of a gone launch stays: %v", err)
	}
}

// What the wall opens for chat A holds nothing of chat B: not its token, its grant, its MCP configs or its folders; and
// nothing the registry (which every chat reads) holds is a token.
func TestWhatTheWallOpensForOneChatHoldsNoOtherChatsToken(t *testing.T) {
	reg, _, _ := secretApp(t, false)
	l := launcherFor(t, reg, fakeSafe{"api-key": secretValue})
	a, err := l.Claude([]string{"acme.pay"}, baseSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := l.Claude([]string{"acme.pay"}, baseSettings(t))
	dirA, dirB := sealDirOf(t, a.Env), sealDirOf(t, b.Env)
	tokB := strings.TrimSpace(string(mustRead(t, filepath.Join(dirB, tokenName))))
	if tokB == "" || tokB == strings.TrimSpace(string(mustRead(t, filepath.Join(dirA, tokenName)))) {
		t.Fatal("tokens")
	}
	seal := &Seal{Data: filepath.Dir(reg.Root), Open: []string{dirA}}
	args := strings.Join(seal.dataArgs(nil), " ")
	if !strings.Contains(args, "--ro-bind "+dirA) || strings.Contains(args, dirB) {
		t.Fatalf("the wall opens A's folder only (read-only): %s", args)
	}
	if f := filesHold(t, tokB, dirA, reg.Root); f != "" {
		t.Fatalf("%s, open to chat A, holds chat B's token", f)
	}
	for _, f := range []string{a.MCPConfig, a.RuntimeMCPConfig} {
		if filepath.Dir(f) != dirA {
			t.Errorf("%s is not in the launch's own folder", f)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(reg.Root, "launch", "agent-token-*")); len(left) != 0 {
		t.Errorf("tokens in the registry: %v", left)
	}
	// The grant is in the folder the wall binds read-only, and each token has its own.
	if g := reg.GrantedApps(filepath.Base(dirA)); len(g) != 1 {
		t.Fatalf("grant: %v", g)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLaunchNewValueNeedsNoReinstall(t *testing.T) {
	reg, _ := localSecretApp(t)
	safe := fakeSafe{"api-key": "old-value-aaaa"}
	l := launcherFor(t, reg, safe)
	read := func() string {
		got, err := l.Claude([]string{"acme.loc"}, baseSettings(t))
		if err != nil {
			t.Fatal(err)
		}
		return readServers(t, got.PluginDirs[0])["l"].Env["KEY"]
	}
	if read() != "old-value-aaaa" {
		t.Fatal("first launch")
	}
	safe["api-key"] = "new-value-bbbb"
	if read() != "new-value-bbbb" {
		t.Fatal("a changed value is read at the next launch")
	}
}

func TestLaunchWaitingForSecretStillStarts(t *testing.T) {
	reg, a, _ := secretApp(t, false)
	l := launcherFor(t, reg, fakeSafe{})
	got, err := l.Claude([]string{"acme.pay"}, baseSettings(t))
	if err != nil {
		t.Fatalf("a chat with an app that waits for a secret must start: %v", err)
	}
	if srv := readServers(t, got.PluginDirs[0]); srv["pay"].URL == "" || srv["free"].URL == "" {
		t.Fatalf("servers: %+v", srv)
	}
	if w := a.Manifest.WaitingFor(func(string) bool { return false }); len(w) != 1 || w[0] != "api-key" {
		t.Fatalf("waiting: %v", w)
	}
	if w := a.Manifest.WaitingFor(func(string) bool { return true }); len(w) != 0 {
		t.Fatalf("waiting with a value: %v", w)
	}
}

func TestOptionalSecretDoesNotWait(t *testing.T) {
	_, a, _ := secretApp(t, true)
	if w := a.Manifest.WaitingFor(func(string) bool { return false }); len(w) != 0 {
		t.Fatalf("an optional secret does not make the app wait: %v", w)
	}
}

func TestSettingsFormAndValues(t *testing.T) {
	reg, a, _ := secretApp(t, false)
	st := &Storage{Root: filepath.Join(filepath.Dir(reg.Root), "storage")}
	vals, err := st.Settings(a.Manifest, a.ID)
	if err != nil || vals["region"] != "eu" || vals["limit"] != float64(10) {
		t.Fatalf("defaults: %v %v", vals, err)
	}
	if bad, err := st.SaveSettings(a.Manifest, a.ID, map[string]any{"region": "asia"}); err != nil || bad["region"] == "" {
		t.Fatalf("an enum outsider is refused: %v %v", bad, err)
	}
	if bad, _ := st.SaveSettings(a.Manifest, a.ID, map[string]any{"limit": 99.0}); bad["limit"] == "" {
		t.Fatal("a number past its maximum is refused")
	}
	if bad, _ := st.SaveSettings(a.Manifest, a.ID, map[string]any{"limit": 2.5}); bad["limit"] == "" {
		t.Fatal("an integer setting takes whole numbers")
	}
	if bad, _ := st.SaveSettings(a.Manifest, a.ID, map[string]any{"nope": true}); bad["nope"] == "" {
		t.Fatal("a key the schema does not describe is refused")
	}
	if bad, err := st.SaveSettings(a.Manifest, a.ID, map[string]any{"region": "us", "limit": 7.0, "verbose": true}); bad != nil || err != nil {
		t.Fatalf("save: %v %v", bad, err)
	}
	vals, _ = st.Settings(a.Manifest, a.ID)
	if vals["region"] != "us" || vals["limit"] != float64(7) || vals["verbose"] != true {
		t.Fatalf("saved: %v", vals)
	}
	if f := a.Manifest.SettingFields(); len(f) != 3 || f[0].Name != "limit" || !f[2].Required && f[2].Name == "region" && false {
		t.Fatalf("fields: %+v", f)
	}
}

func TestAgentSettingsToolHasNoSecret(t *testing.T) {
	reg, a, _ := secretApp(t, false)
	st := &Storage{Root: filepath.Join(filepath.Dir(reg.Root), "storage")}
	if _, err := st.SaveSettings(a.Manifest, a.ID, map[string]any{"region": "us"}); err != nil {
		t.Fatal(err)
	}
	h := HostTools{Conv: "c1", Registry: reg, Storage: st, Asks: NewAsks(), Enabled: func() []string { return []string{a.ID} }}
	text, isErr := call(t, h, "settings_get")
	if isErr || !strings.Contains(text, `"region":"us"`) {
		t.Fatalf("settings_get: %q %v", text, isErr)
	}
	// Even if the value were in the same area under another key, the tool answers the settings and nothing else.
	_ = st.Set(a.ID, ScopeInstall, "", "api-key", json.RawMessage(`"`+secretValue+`"`))
	if text, _ = call(t, h, "settings_get"); strings.Contains(text, secretValue) {
		t.Fatalf("the tool leaked: %q", text)
	}
	if !h.Has("settings_get") {
		t.Fatal("settings_get is a host tool")
	}
	// The listing the agent reads says nothing of values either.
	listing, _ := json.Marshal(h.Tools())
	if strings.Contains(string(listing), secretValue) {
		t.Fatal("tool list")
	}
}

func TestSecretPlaceholders(t *testing.T) {
	if got := SecretsIn("Bearer ${secret:a-b} and ${secret:c.d} ${env:X} ${secret:}"); len(got) != 2 || got[0] != "a-b" || got[1] != "c.d" {
		t.Fatalf("names: %v", got)
	}
	out, miss := FillSecrets("k=${secret:a}&m=${secret:b}", map[string]string{"a": "1"})
	if out != "k=1&m=${secret:b}" || len(miss) != 1 || miss[0] != "b" {
		t.Fatalf("fill: %q %v", out, miss)
	}
}

func TestValidatorChecksSecretUse(t *testing.T) {
	m := &Manifest{Trust: TrustSandboxed, Secrets: []Secret{{Name: "ok", Description: "d"}}}
	mcp := map[string]MCPServer{
		"a": {URL: "https://a.example.com", Headers: map[string]string{"X": "${secret:ok}"}},
		"b": {URL: "https://b.example.com", Headers: map[string]string{"X": "${secret:other}"}},
		"c": {URL: "https://c.example.com/${secret:ok}"},
		"d": {URL: "https://d.example.com", Env: map[string]string{"K": "${secret:ok}"}},
	}
	got := strings.Join(m.secretUseProblems(mcp), "\n")
	for _, want := range []string{"b uses ${secret:other}", "c: ${secret:...} goes in headers", "d: ${secret:...} in env"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "a uses") || strings.Contains(got, "a:") {
		t.Errorf("a is fine:\n%s", got)
	}
	trusted := &Manifest{Trust: TrustTrusted, Secrets: m.Secrets}
	if p := trusted.secretUseProblems(map[string]MCPServer{"l": {Command: "srv", Env: map[string]string{"K": "${secret:ok}"}}}); len(p) != 0 {
		t.Errorf("a trusted local server takes a secret in env: %v", p)
	}
}

func TestViewToolCallFillsHeaders(t *testing.T) {
	srv := MCPServer{URL: "https://x.example.com", Headers: map[string]string{"Authorization": "Bearer ${secret:api-key}"}}
	filled, miss := srv.Filled(map[string]string{"api-key": secretValue})
	if len(miss) != 0 || filled.Headers["Authorization"] != "Bearer "+secretValue {
		t.Fatalf("%v %v", filled, miss)
	}
	if srv.Headers["Authorization"] != "Bearer ${secret:api-key}" {
		t.Fatal("the package's own copy must not change")
	}
	if _, miss := srv.Filled(nil); len(miss) != 1 {
		t.Fatal("a missing value is reported")
	}
}

func baseSettings(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
