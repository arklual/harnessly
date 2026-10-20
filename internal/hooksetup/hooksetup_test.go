package hooksetup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/codex"
)

const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The neutral names the golden was generated with (testdata/ts-golden.json: the previous runtime's writers run under
// node from copies whose product names were replaced by these).
func testConfig(dir string) Config {
	return Config{
		DataDir:          dir,
		ScriptDataDir:    "$HOME/.acme-ai",
		TokenHeaders:     []string{"x-acme-token", "x-omux-token"},
		MCPServer:        "acme-preview",
		MCPPath:          "/api/cli/preview/mcp",
		Allow:            []string{"mcp__acme-preview", "mcp__oldmux-preview"},
		EditorName:       "acme-prompt-editor",
		EditorTitle:      "Acme prompt editor (TCK-3010)",
		HumanEditorVar:   "ACME_HUMAN_EDITOR",
		LegacyTmuxSocket: "violet",
	}
}

type golden struct {
	Result struct {
		CodexHookInstallFailed bool `json:"codexHookInstallFailed"`
	} `json:"result"`
	Files map[string]struct {
		Content string `json:"content"`
	} `json:"files"`
	PromptEditor struct{ Path, Content string } `json:"promptEditor"`
	EditorEnv    string                         `json:"editorEnv"`
}

func loadGolden(t *testing.T, dir string) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/ts-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "@DATA@", dir)), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func read(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestFilesMatchThePreviousRuntime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	g := loadGolden(t, dir)
	w := New(testConfig(dir))
	old := syscall.Umask(0o077) // the modes must not depend on the umask
	res, err := w.Ensure(8022, token)
	syscall.Umask(old)
	if err != nil || res.CodexHookFailed != g.Result.CodexHookInstallFailed {
		t.Fatalf("%+v %v", res, err)
	}
	for name, f := range g.Files {
		if got := read(t, filepath.Join(dir, name)); got != f.Content {
			t.Errorf("%s differs:\n--- go\n%s\n--- ts\n%s", name, got, f.Content)
		}
	}
	for name, want := range map[string]os.FileMode{PortFile: 0o600, StatusHookFile: 0o755, StatuslineFile: 0o755, CodexHookFile: 0o700, MCPConfigFile: 0o600, HooksFile: 0o600} {
		if got := mode(t, filepath.Join(dir, name)); got != want {
			t.Errorf("%s mode %v, want %v", name, got, want)
		}
	}
	editor, err := w.EnsureEditor()
	if err != nil || editor != g.PromptEditor.Path || read(t, editor) != g.PromptEditor.Content || mode(t, editor) != 0o700 {
		t.Fatalf("editor %q %v:\n%s\n--- ts\n%s", editor, err, read(t, editor), g.PromptEditor.Content)
	}
	if st, err := os.Stat(w.HandoffDir()); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatal("hand-off folder")
	}
	if got := w.EditorEnv(editor); got != g.EditorEnv {
		t.Fatalf("editor env %q, want %q", got, g.EditorEnv)
	}
}

// preview-mcp-config: rewritten when the port changes, narrowed when left wide, left alone when unchanged.
func TestWriteRules(t *testing.T) {
	dir := t.TempDir()
	w := New(testConfig(dir))
	if _, err := w.Ensure(8022, token); err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks       map[string]any      `json:"hooks"`
		Permissions map[string][]string `json:"permissions"`
	}
	_ = json.Unmarshal([]byte(read(t, w.HooksPath())), &settings)
	if !slices.Equal(settings.Permissions["allow"], []string{"mcp__acme-preview", "mcp__oldmux-preview"}) || len(settings.Hooks) != 7 {
		t.Fatalf("%+v", settings)
	}
	past := time.Now().Add(-time.Hour)
	for _, f := range []string{w.MCPConfigPath(), w.HooksPath(), w.StatusHookPath()} {
		_ = os.Chtimes(f, past, past)
	}
	_ = os.Chmod(w.MCPConfigPath(), 0o644)
	_ = os.Chmod(w.StatusHookPath(), 0o700)
	if _, err := w.Ensure(8022, token); err != nil {
		t.Fatal(err)
	}
	if mode(t, w.MCPConfigPath()) != 0o600 {
		t.Fatal("a wide token file is narrowed even when its content is right")
	}
	for _, f := range []string{w.MCPConfigPath(), w.HooksPath(), w.StatusHookPath()} {
		if st, _ := os.Stat(f); !st.ModTime().Equal(past) {
			t.Errorf("%s rewritten though unchanged", f)
		}
	}
	if mode(t, w.StatusHookPath()) != 0o700 {
		t.Fatal("an unchanged script keeps its mode")
	}
	_ = os.WriteFile(w.StatusHookPath(), []byte("old"), 0o600)
	if _, err := w.Ensure(8023, token); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, w.MCPConfigPath()), "http://127.0.0.1:8023/api/cli/preview/mcp") || read(t, w.PortPath()) != "8023" {
		t.Fatal("the port change reaches the MCP config and the port file")
	}
	if mode(t, w.StatusHookPath()) != 0o755 || read(t, w.StatusHookPath()) == "old" {
		t.Fatal("a changed script is rewritten and gets its mode after the write")
	}
	if err := w.RemovePortFile(); err != nil || exists(w.PortPath()) {
		t.Fatal("port file removed")
	}
	if err := w.RemovePortFile(); err != nil {
		t.Fatal("removing twice is fine")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestCodexHookFailureDoesNotStopTheStart(t *testing.T) {
	dir := t.TempDir()
	w := New(testConfig(dir))
	if err := os.MkdirAll(filepath.Join(w.CodexHookPath(), "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := w.Ensure(8022, token)
	if err != nil || !res.CodexHookFailed || !exists(w.HooksPath()) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestEnsureToken(t *testing.T) {
	w := New(testConfig(filepath.Join(t.TempDir(), "d")))
	tok, err := w.EnsureToken()
	if err != nil || len(tok) != 64 || mode(t, w.TokenPath()) != 0o600 {
		t.Fatalf("%q %v", tok, err)
	}
	again, _ := w.EnsureToken()
	if again != tok {
		t.Fatal("the token is kept")
	}
	_ = os.WriteFile(w.TokenPath(), []byte("abc\n"), 0o600)
	if got, _ := w.EnsureToken(); got != "abc" {
		t.Fatal("an existing token is read trimmed")
	}
}

func TestEnsureEditorOncePerProcess(t *testing.T) {
	w := New(testConfig(t.TempDir()))
	p, err := w.EnsureEditor()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("changed"), 0o700)
	if again, _ := w.EnsureEditor(); again != p || read(t, p) != "changed" {
		t.Fatal("written once per process")
	}
	broken := New(testConfig(filepath.Join(t.TempDir(), "f")))
	_ = os.WriteFile(filepath.Join(filepath.Dir(broken.cfg.DataDir), "f"), nil, 0o600)
	if _, err := broken.EnsureEditor(); err == nil {
		t.Fatal("a folder that cannot be made is an error")
	}
}

func TestCodexLauncher(t *testing.T) {
	w := New(testConfig(t.TempDir()))
	if _, err := w.CodexLauncher(); err == nil {
		t.Fatal("not configured")
	}
	cfg := testConfig(w.cfg.DataDir)
	cfg.Codex = &codex.LauncherScriptConfig{DataDirs: []string{".acme-ai"}, PortFile: PortFile, TokenFile: TokenFile, LaunchArgsPath: "/api/codex/launch-args", TokenHeaders: cfg.TokenHeaders, TokenEnv: "ACME_PREVIEW_TOKEN"}
	w = New(cfg)
	p, err := w.CodexLauncher()
	if err != nil || p != w.LauncherPath() || mode(t, p) != 0o700 || !strings.Contains(read(t, p), "/api/codex/launch-args") {
		t.Fatalf("%q %v", p, err)
	}
}

// prompt-editor: the script contract, run for real.
func TestEditorScriptContract(t *testing.T) {
	dir := t.TempDir()
	w := New(testConfig(dir))
	script, err := w.EnsureEditor()
	if err != nil {
		t.Fatal(err)
	}
	handoff := w.HandoffDir()
	buffer := filepath.Join(dir, "prompt.md")
	run := func(env ...string) {
		t.Helper()
		cmd := exec.Command(script, buffer)
		cmd.Env = append(os.Environ(), append([]string{"TMUX=", "TMUX_PANE=", "ACME_HUMAN_EDITOR="}, env...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	left := func() []string {
		e, _ := os.ReadDir(handoff)
		var names []string
		for _, x := range e {
			names = append(names, x.Name())
		}
		return names
	}
	_ = os.WriteFile(buffer, []byte("[Image #1]"), 0o600)
	_ = os.WriteFile(filepath.Join(handoff, "7.txt"), []byte(" первая строка\nвторая"), 0o600)
	run("TMUX_PANE=%7")
	if read(t, buffer) != "[Image #1] первая строка\nвторая" || len(left()) != 0 {
		t.Fatalf("appended once: %q %v", read(t, buffer), left())
	}
	// A pane of the current server is told from the same pane id on the old one.
	_ = os.WriteFile(buffer, nil, 0o600)
	_ = os.WriteFile(filepath.Join(handoff, "7.txt"), []byte("вкладка на старом"), 0o600)
	_ = os.WriteFile(filepath.Join(handoff, "main-7.txt"), []byte("вкладка на новом"), 0o600)
	run("TMUX=/tmp/tmux-1000/main,123,0", "TMUX_PANE=%7")
	if read(t, buffer) != "вкладка на новом" {
		t.Fatalf("%q", read(t, buffer))
	}
	run("TMUX=/tmp/tmux-1000/violet,99,0", "TMUX_PANE=%7")
	if read(t, buffer) != "вкладка на новомвкладка на старом" || len(left()) != 0 {
		t.Fatalf("%q %v", read(t, buffer), left())
	}
	// Nothing waits for this pane: the person's editor opens.
	_ = os.WriteFile(buffer, nil, 0o600)
	_ = os.WriteFile(filepath.Join(handoff, "8.txt"), []byte("чужая вкладка"), 0o600)
	human := filepath.Join(dir, "human-editor")
	_ = os.WriteFile(human, []byte("#!/bin/sh\necho \"редактор человека\" > \"$1\"\n"), 0o700)
	run("TMUX_PANE=%7", "ACME_HUMAN_EDITOR="+human)
	if read(t, buffer) != "редактор человека\n" || !slices.Equal(left(), []string{"8.txt"}) {
		t.Fatalf("%q %v", read(t, buffer), left())
	}
	// The person's editor is read by the shell before VISUAL is replaced.
	cmd := exec.Command("bash", "-c", w.EditorEnv(script)+" env")
	cmd.Env = append(os.Environ(), "VISUAL=code --wait")
	out, _ := cmd.Output()
	for _, want := range []string{"ACME_HUMAN_EDITOR=code --wait", "VISUAL=" + script, "EDITOR=" + script} {
		if !strings.Contains(string(out), want+"\n") {
			t.Errorf("env lacks %q", want)
		}
	}
}

// status-hook-script: returns at once and still posts {event, session, notificationType, at}.
func TestStatusHookScriptPosts(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not here")
	}
	home := t.TempDir()
	data := filepath.Join(home, ".acme-ai")
	cfg := testConfig(data)
	w := New(cfg)
	got := make(chan string, 1)
	srv := newRecorder(t, got)
	if _, err := w.Ensure(srv.port, token); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(w.TokenPath(), []byte(token), 0o600)
	bin := filepath.Join(home, "bin")
	_ = os.MkdirAll(bin, 0o700)
	_ = os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\necho tab-session\n"), 0o755)
	before := time.Now().UnixMilli()
	cmd := exec.Command("sh", w.StatusHookPath(), "notification")
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`)
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the hook must return at once")
	}
	var body struct {
		Event, Session, NotificationType string
		At                               int64
	}
	select {
	case raw := <-got:
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatalf("%q", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing posted")
	}
	if body.Event != "notification" || body.Session != "tab-session" || body.NotificationType != "permission_prompt" || body.At < before-1000 || body.At > time.Now().UnixMilli() {
		t.Fatalf("%+v", body)
	}
	if srv.headers.Get("x-acme-token") != token || srv.headers.Get("x-omux-token") != token {
		t.Fatalf("headers %v", srv.headers)
	}
}
