package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/voice/engine"
)

func fakeProbe(t *testing.T) (machineProbe, string) {
	home := t.TempDir()
	files := map[string]bool{}
	ver := "3.4"
	return machineProbe{
		GOOS: "linux", Home: home, DataDir: filepath.Join(home, ".harnessly"), BinDir: filepath.Join(home, ".local", "bin"),
		User: "someone", Shell: "/bin/bash",
		Tools: func() preflight.Result {
			return preflight.Result{Tmux: preflight.Tmux{Tool: preflight.Tool{Installed: true, Version: &ver}, Compatible: true},
				Git: preflight.Tool{Installed: true}, Claude: preflight.Claude{Installed: true}}
		},
		ShellPath:   func() string { return "/usr/bin:/bin" },
		HasDecoder:  func() bool { return true },
		Free:        func(string) (uint64, bool) { return 50 * gib, true },
		Reach:       func(context.Context, string) error { return nil },
		GitIdentity: func() gitPerson { return gitPerson{Name: "A", Email: "a@b.c"} },
		LookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Exists: func(p string) bool {
			if files[p] {
				return true
			}
			_, err := os.Stat(p)
			return err == nil
		},
		Invalidate: func() {},
	}, home
}

func checksByID(cs []machineCheck) map[string]machineCheck {
	m := map[string]machineCheck{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

func TestMachineChecksSayWhatIsMissingAndHow(t *testing.T) {
	p, home := fakeProbe(t)
	p.Tools = func() preflight.Result { return preflight.Result{} }
	p.HasDecoder = func() bool { return false }
	p.GitIdentity = func() gitPerson { return gitPerson{} }
	_ = os.MkdirAll(filepath.Join(home, ".harnessly"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".harnessly", "install-manifest"), nil, 0o644)

	got := checksByID(p.run(context.Background()))
	for _, id := range []string{"tmux", "git", "agent_cli", "ffmpeg", "linger", "local_bin_path"} {
		c, ok := got[id]
		if !ok || c.OK || c.FixCommand == "" {
			t.Errorf("%s: %+v", id, c)
		}
	}
	if got["tmux"].FixCommand != "sudo apt-get install -y -q --no-install-recommends tmux" || got["linger"].FixCommand != "sudo loginctl enable-linger someone" {
		t.Errorf("commands: %q %q", got["tmux"].FixCommand, got["linger"].FixCommand)
	}
	if _, ok := got["git_identity"]; ok {
		t.Error("git's identity is not asked about without git")
	}
	if !got["local_bin_path"].Fixable || got["tmux"].Fixable || got["linger"].Fixable {
		t.Error("only the PATH line is the runtime's to fix here")
	}
}

func TestMachineChecksAllInOrder(t *testing.T) {
	p, home := fakeProbe(t)
	p.ShellPath = func() string { return filepath.Join(home, ".local", "bin") + ":/usr/bin" }
	for _, c := range p.run(context.Background()) {
		if !c.OK {
			t.Errorf("%s: %+v", c.ID, c)
		}
	}
}

func TestMachineCheckOldTmuxAndDisk(t *testing.T) {
	p, _ := fakeProbe(t)
	old := "2.6"
	p.Tools = func() preflight.Result {
		return preflight.Result{Tmux: preflight.Tmux{Tool: preflight.Tool{Installed: true, Version: &old}}, Git: preflight.Tool{Installed: true},
			Codex: preflight.Codex{Installed: true}}
	}
	p.Free = func(string) (uint64, bool) { return 200 * mib, true }
	p.GOOS = "darwin"
	got := checksByID(p.run(context.Background()))
	if c := got["tmux"]; c.OK || c.Detail != "tmux 2.6" || c.FixCommand != "brew install tmux" {
		t.Errorf("tmux: %+v", c)
	}
	if c := got["disk"]; c.OK || !strings.Contains(c.Detail, "200 MB") {
		t.Errorf("disk: %+v", c)
	}
	if !got["agent_cli"].OK {
		t.Error("Codex alone is an agent")
	}
}

func TestMachineCheckVoiceDownload(t *testing.T) {
	p, home := fakeProbe(t)
	eng := engine.New(engine.Config{Dir: filepath.Join(home, "voice"), Bin: "/bin/true", Model: "tiny"})
	p.Voice = func() *engine.Engine { return eng }
	p.Reach = func(_ context.Context, url string) error {
		if strings.Contains(url, "huggingface.co") {
			return errors.New("connection refused")
		}
		return nil
	}
	got := checksByID(p.run(context.Background()))
	c := got["voice_download"]
	if c.OK || !strings.Contains(c.Detail, "huggingface.co: connection refused") || !strings.Contains(c.FixCommand, "ggml-tiny-q5_1.bin") {
		t.Fatalf("download: %+v", c)
	}
	if !got["voice_engine"].OK {
		t.Fatalf("engine: %+v", got["voice_engine"])
	}
	// With the person's own server nothing is downloaded and whisper is not asked about.
	eng.SetRemote(&engine.Remote{URL: "https://example.invalid/v1", Model: "whisper-1"})
	got = checksByID(p.run(context.Background()))
	if _, ok := got["voice_download"]; ok {
		t.Fatal("a server of one's own needs no download")
	}
}

func TestFixPathIsAppendedOnce(t *testing.T) {
	p, home := fakeProbe(t)
	p.Shell = "/usr/bin/zsh"
	for range 2 {
		if err := p.fix(context.Background(), "local_bin_path"); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil || strings.Count(string(raw), ".local/bin") != 2 { // the comment and the line, once
		t.Fatalf("%q %v", raw, err)
	}
	if p.pathFixCommand() != `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc` {
		t.Fatal(p.pathFixCommand())
	}
}

func TestFixOnlyKnownChecks(t *testing.T) {
	p, _ := fakeProbe(t)
	for _, id := range []string{"", "tmux", "linger", "ffmpeg", "rm -rf /", "cli"} {
		if err := p.fix(context.Background(), id); !errors.Is(err, errNoFix) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestMachineCheckRoutes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("RUNTIME_VOICE_ENGINE", "off")
	d := deps{Home: home, DataDir: filepath.Join(home, ".harnessly"), Tools: preflight.New(home),
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Session") == "1" }}
	routes := machineCheckRoutes(d, nil)
	call := func(method, path, body string, session bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, platformAPI+path, strings.NewReader(body))
		if session {
			r.Header.Set("X-Session", "1")
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return w
			}
		}
		t.Fatalf("no route for %s", path)
		return nil
	}
	if w := call(http.MethodGet, "/machine/checks", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
	if w := call(http.MethodPost, "/machine/checks/fix", `{"id":"tmux"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("a check without a fix: %d %s", w.Code, w.Body)
	}
	if w := call(http.MethodPost, "/machine/checks/fix", `{"id":"local_bin_path"}`, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("fix without a session: %d", w.Code)
	}
	if w := call(http.MethodGet, "/machine/checks", "", true); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"tmux"`) {
		t.Fatalf("checks: %d %s", w.Code, w.Body)
	}
}

// The wall around the agents of chats with apps: no check on a machine without apps, the package or the kernel's
// setting as the fix where bwrap cannot make it.
func TestMachineCheckAppsBoundary(t *testing.T) {
	p, _ := fakeProbe(t)
	sysctls := map[string]string{}
	p.ReadFile = func(name string) ([]byte, error) {
		if v, ok := sysctls[name]; ok {
			return []byte(v + "\n"), nil
		}
		return nil, os.ErrNotExist
	}
	var applies bool
	var wallErr error
	p.AppsBoundary = func() (bool, error) { return applies, wallErr }
	if _, ok := checksByID(p.run(context.Background()))["apps_boundary"]; ok {
		t.Fatal("no apps, no check")
	}
	applies = true
	if c := checksByID(p.run(context.Background()))["apps_boundary"]; !c.OK || c.FixCommand != "" {
		t.Fatalf("%+v", c)
	}
	for _, tc := range []struct {
		err    error
		sysctl map[string]string
		want   string
	}{
		{apps.ErrNoBwrap, nil, "sudo apt-get install -y -q --no-install-recommends bubblewrap"},
		{errors.New("bwrap: setting up uid map: Permission denied"), map[string]string{"/proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1"}, "sudo apparmor_parser -r /etc/apparmor.d/bwrap-userns-restrict"},
		{errors.New("bwrap: No permissions to create new namespace"), map[string]string{"/proc/sys/user/max_user_namespaces": "0"}, "echo user.max_user_namespaces=15000 | sudo tee /etc/sysctl.d/60-harnessly-userns.conf && sudo sysctl --system"},
		{errors.New("bwrap: something else"), nil, ""},
	} {
		wallErr, sysctls = tc.err, tc.sysctl
		c := checksByID(p.run(context.Background()))["apps_boundary"]
		if c.OK || c.Detail != tc.err.Error() || c.FixCommand != tc.want {
			t.Fatalf("%v: %+v", tc.err, c)
		}
	}
}

func TestAgentBrowserMachineCheck(t *testing.T) {
	t.Setenv("RUNTIME_BROWSER", "off")
	check := checkAgentBrowser(t.Context(), t.TempDir())
	if check.ID != "agent_browser" || !check.OK || !strings.Contains(check.Detail, "Disabled") {
		t.Fatalf("%+v", check)
	}
	t.Setenv("RUNTIME_BROWSER", "")
	t.Setenv("RUNTIME_BROWSER_CDP", "http://127.0.0.1:1")
	check = checkAgentBrowser(t.Context(), t.TempDir())
	if !check.OK || !strings.Contains(check.Detail, "externally") {
		t.Fatalf("%+v", check)
	}
	t.Setenv("RUNTIME_BROWSER_CDP", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUNTIME_BROWSER_CHROME", "")
	t.Setenv("RUNTIME_BROWSER_DOWNLOAD", "off")
	// A machine with no Chrome: the test does not depend on what the machine it runs on has (a snap chromium is found
	// by its absolute path whatever PATH says).
	was := agentBrowserConfig
	agentBrowserConfig = func(data string) provision.Config {
		cfg := was(data)
		cfg.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
		return cfg
	}
	t.Cleanup(func() { agentBrowserConfig = was })
	check = checkAgentBrowser(t.Context(), t.TempDir())
	if check.OK || !strings.Contains(check.Detail, "DOWNLOAD=off") {
		t.Fatalf("%+v", check)
	}
}

// Whoever opened the page is who commits on the machine: with git's identity missing, the check offers to take the
// name and the email of the sign-in, and only then.
func TestGitIdentityIsTakenFromTheSignIn(t *testing.T) {
	p, _ := fakeProbe(t)
	author := gitPerson{}
	var gotName, gotEmail string
	p.GitIdentity = func() gitPerson { return author }
	p.SetGitIdentity = func(name, e string) error {
		gotName, gotEmail, author = name, e, gitPerson{Name: name, Email: e}
		return nil
	}

	anon := checksByID(p.run(context.Background()))["git_identity"]
	if anon.OK || anon.Fixable || anon.FixCommand == "" {
		t.Fatalf("nobody signed in: only the command is offered: %+v", anon)
	}
	if err := p.fix(context.Background(), "git_identity"); !errors.Is(err, errNoFix) {
		t.Fatalf("no email to write: %v", err)
	}

	ctx := withPerson(context.Background(), "Ada Lovelace", "ada@example.com")
	if c := checksByID(p.run(ctx))["git_identity"]; c.OK || !c.Fixable {
		t.Fatalf("the sign-in has an email: %+v", c)
	}
	if err := p.fix(ctx, "git_identity"); err != nil {
		t.Fatal(err)
	}
	if gotName != "Ada Lovelace" || gotEmail != "ada@example.com" {
		t.Fatalf("written: %q %q", gotName, gotEmail)
	}
	if c := checksByID(p.run(ctx))["git_identity"]; !c.OK || c.Fixable {
		t.Fatalf("git knows the person now: %+v", c)
	}
	// An identity the person set by hand is never overwritten.
	gotEmail = ""
	if err := p.fix(ctx, "git_identity"); !errors.Is(err, errNoFix) || gotEmail != "" {
		t.Fatalf("existing identity: %v %q", err, gotEmail)
	}
}

func TestSetGitIdentityWritesTheGlobalConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	if err := setGitIdentity(os.Getenv("PATH"))("Ada Lovelace", "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := gitIdentity(os.Getenv("PATH")).Email; got != "ada@example.com" {
		t.Fatalf("email: %q", got)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if !strings.Contains(string(raw), "Ada Lovelace") {
		t.Fatalf("name: %s", raw)
	}
}

func TestLaunchEnvUndeclared(t *testing.T) {
	env := func(m map[string]string) (func(string) string, func(string) (string, bool)) {
		return func(k string) string { return m[k] }, func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	for name, c := range map[string]struct {
		env  map[string]string
		want bool
	}{
		"no hub":            {map[string]string{}, false},
		"a hub's, not set":  {map[string]string{"RUNTIME_HUB_URL": "https://hub.example"}, true},
		"a hub's, none":     {map[string]string{"RUNTIME_HUB_URL": "https://hub.example", extproviders.ReservedPrefixesEnv: ""}, false},
		"a hub's, declared": {map[string]string{"RUNTIME_HUB_URL": "https://hub.example", extproviders.ReservedPrefixesEnv: "ACME_"}, false},
	} {
		get, look := env(c.env)
		if got := launchEnvUndeclared(get, look); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	p, _ := fakeProbe(t)
	p.LaunchEnvUndeclared = func() bool { return true }
	found := false
	for _, c := range p.run(context.Background()) {
		found = found || (c.ID == "launch_env_reserved" && !c.OK)
	}
	if !found {
		t.Error("the check is not in the list")
	}
}

// harnessly doctor preview on a standalone runtime in path mode with no public origins answers a report with the mode
// and an empty list, not a nil-function panic that the CLI saw as EOF (GitHub #126).
func TestPreviewCheckRouteWithoutOrigins(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "cli-token"), []byte("person"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIME_PREVIEW_MODE", "path")
	t.Setenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS", "")
	t.Setenv("RUNTIME_PREVIEW_DOMAIN", "")
	t.Setenv("RUNTIME_VOICE_ENGINE", "off")
	saved := standaloneSlots
	standaloneSlots = &previewSlots{}
	t.Cleanup(func() { standaloneSlots = saved })
	d := deps{Home: home, DataDir: home, Tools: preflight.New(home),
		CLI:       &gate.CLIToken{DataDir: home, Headers: []string{"x-cli-token"}},
		SessionOK: func(*http.Request) bool { return false }}
	call := func(routes []gate.Route) previewCheck {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/cli/preview/check", nil)
		r.Header.Set("x-cli-token", "person")
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				break
			}
		}
		var out previewCheck
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return out
	}
	routes := machineCheckRoutes(d, nil)
	if got := call(routes); got.Mode != "path" || got.Origins == nil || len(got.Origins) != 0 || got.Source != "" {
		t.Fatalf("%+v", got)
	}
	// The same with no callback at all, as a probe built without one has it.
	probe := newMachineProbe(d, nil)
	probe.PreviewOrigins = nil
	if probe.previewCheck(context.Background()) != nil {
		t.Fatal("no origins, yet a preview_dns check")
	}
}

func TestGitIdentityRepairPreservesEachCustomField(t *testing.T) {
	person := gitPerson{Name: "Verified Name", Email: "person@example.org"}
	for _, tt := range []struct{ have, want gitPerson }{
		{gitPerson{Name: "Chosen Name"}, gitPerson{Email: person.Email}},
		{gitPerson{Email: "custom@example.org"}, gitPerson{Name: person.Name}},
	} {
		p, _ := fakeProbe(t)
		p.GitIdentity = func() gitPerson { return tt.have }
		var wrote gitPerson
		p.SetGitIdentity = func(n, e string) error { wrote = gitPerson{Name: n, Email: e}; return nil }
		ctx := withPerson(t.Context(), person.Name, person.Email)
		if c := checksByID(p.run(ctx))["git_identity"]; c.OK || !c.Fixable {
			t.Fatalf("incomplete author: %+v", c)
		}
		if err := p.fix(ctx, "git_identity"); err != nil || wrote != tt.want {
			t.Fatalf("patch %+v, want %+v (%v)", wrote, tt.want, err)
		}
	}
}

func TestGitIdentityReadsXDGDefaultsAndPersonalOverrides(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := filepath.Join(xdg, "git")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(dir, "config"):      "[include]\n path = defaults\n",
		filepath.Join(dir, "defaults"):    "[user]\n name = Listed name\n email = user@example.org\n",
		filepath.Join(home, ".gitconfig"): "[user]\n name = Chosen name\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := gitIdentity(os.Getenv("PATH")); got.Name != "Chosen name" || got.Email != "user@example.org" {
		t.Fatalf("read %+v", got)
	}
}
