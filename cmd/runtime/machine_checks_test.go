package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
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
		ShellPath:  func() string { return "/usr/bin:/bin" },
		HasDecoder: func() bool { return true },
		Free:       func(string) (uint64, bool) { return 50 * gib, true },
		Reach:      func(context.Context, string) error { return nil },
		GitEmail:   func() string { return "a@b.c" },
		LookPath:   func(name string) (string, error) { return "/usr/bin/" + name, nil },
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
	p.GitEmail = func() string { return "" }
	_ = os.MkdirAll(filepath.Join(home, ".harnessly"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".harnessly", "install-manifest"), nil, 0o644)

	got := checksByID(p.run(context.Background()))
	for _, id := range []string{"tmux", "git", "agent_cli", "ffmpeg", "linger", "local_bin_path"} {
		c, ok := got[id]
		if !ok || c.OK || c.FixCommand == "" {
			t.Errorf("%s: %+v", id, c)
		}
	}
	if got["tmux"].FixCommand != "sudo apt-get install -y tmux" || got["linger"].FixCommand != "sudo loginctl enable-linger someone" {
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
		{apps.ErrNoBwrap, nil, "sudo apt-get install -y bubblewrap"},
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
	check = checkAgentBrowser(t.Context(), t.TempDir())
	if check.OK || !strings.Contains(check.Detail, "DOWNLOAD=off") {
		t.Fatalf("%+v", check)
	}
}
