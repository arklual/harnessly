package preflight

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func script(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVersionAndTmux(t *testing.T) {
	for in, want := range map[string]string{"tmux 3.4\n": "3.4", "git version 2.43.0": "2.43.0", "tmux next-3.5a": "3.5", "2.1.302 (Claude Code)": "2.1.302"} {
		if v := Version(in); v == nil || *v != want {
			t.Errorf("%q: %v", in, v)
		}
	}
	if Version("no version") != nil {
		t.Fatal("none")
	}
	v := func(s string) *string { return &s }
	for tool, want := range map[*Tool]bool{
		{Installed: true, Version: v("3.4")}: true, {Installed: true, Version: v("2.8.1")}: false, {Installed: true, Version: v("2.9")}: true,
		{Installed: true, Uncertain: true}: true, {Installed: true}: false, {}: false, {Installed: true, Version: v("10.0.1")}: true,
	} {
		if got := tmuxCompatible(*tool); got != want {
			t.Errorf("%+v: %v", *tool, got)
		}
	}
}

func TestRun(t *testing.T) {
	bin := t.TempDir()
	script(t, bin, "ok", `echo "hi $FOO"`)
	script(t, bin, "bad", `exit 3`)
	script(t, bin, "slow", `sleep 5`)
	if err := os.WriteFile(filepath.Join(bin, "noexec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run("ok", []string{"FOO=bar", "PATH=/usr/bin:/bin"}, bin); err != nil || out != "hi bar\n" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := run("bad", nil, bin); err == nil || errors.Is(err, errTimedOut) {
		t.Fatalf("exit code: %v", err)
	}
	if _, err := run("noexec", nil, bin); err == nil {
		t.Fatal("not executable")
	}
	if _, err := run("missing", nil, bin); err == nil {
		t.Fatal("missing")
	}
	saved := cmdTimeout
	cmdTimeout = 200 * time.Millisecond
	defer func() { cmdTimeout = saved }()
	if _, err := run("slow", []string{"PATH=/usr/bin:/bin"}, bin); !errors.Is(err, errTimedOut) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestRuntime(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	shell := filepath.Join(t.TempDir(), "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\necho junk-from-rc >&2\nprintf ' %s \\n' \""+bin+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, bin, "tmux", `echo "tmux 3.4"`)
	script(t, bin, "git", `exit 1`)
	script(t, bin, "codex", `if [ "$1" = login ]; then echo "Not logged in"; exit 1; fi; echo "codex-cli 0.147.0"`)
	script(t, bin, "which", `echo "  /opt/codex/bin/codex "`)
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, filepath.Join(home, ".local", "bin"), "claude", `echo 1.0.0`)
	c := &Checker{Home: home, Env: []string{"PATH=/nowhere", "HOME=" + home}, Shell: shell}
	r := c.Runtime()
	if !r.Tmux.Installed || r.Tmux.Version == nil || *r.Tmux.Version != "3.4" || !r.Tmux.Compatible {
		t.Fatalf("tmux %+v", r.Tmux)
	}
	if r.Git.Installed || r.Git.Version != nil {
		t.Fatalf("git %+v", r.Git)
	}
	// claude is not on the login PATH, but in ~/.local/bin; no ~/.claude yet.
	if r.Claude.Installed || r.Claude.BinaryPath == nil || *r.Claude.BinaryPath != filepath.Join(home, ".local", "bin") || r.Claude.LoggedIn {
		t.Fatalf("claude %+v", r.Claude)
	}
	if !r.Codex.Installed || *r.Codex.Version != "0.147.0" || *r.Codex.BinaryPath != "/opt/codex/bin/codex" {
		t.Fatalf("codex %+v", r.Codex)
	}
	if r.Codex.LoggedIn {
		t.Fatalf("codex is not signed in: %+v", r.Codex)
	}
	// Kept for half a minute.
	script(t, bin, "git", `echo "git version 2.43.0"`)
	if r2 := c.Runtime(); r2.Git.Installed {
		t.Fatal("not cached")
	}
	c.at = time.Now().Add(-time.Minute)
	if r3 := c.Runtime(); !r3.Git.Installed {
		t.Fatal("not refreshed")
	}
}

func TestShellPathFallsBack(t *testing.T) {
	c := &Checker{Env: []string{"PATH=/fallback"}, Shell: "/nonexistent/shell"}
	if got := c.shellPath(); got != "/fallback" {
		t.Fatal(got)
	}
}

func TestCodexLogin(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	script(t, bin, "which", `echo /opt/codex/bin/codex`)
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"signed in":  {`if [ "$1" = login ]; then echo "Logged in using ChatGPT"; exit 0; fi; echo "codex-cli 0.152.0"`, true},
		"signed out": {`if [ "$1" = login ]; then echo "Not logged in"; exit 1; fi; echo "codex-cli 0.152.0"`, false},
		"timed out":  {`if [ "$1" = login ]; then sleep 5; fi; echo "codex-cli 0.152.0"`, true},
	} {
		script(t, bin, "codex", tc.body)
		saved := cmdTimeout
		cmdTimeout = 500 * time.Millisecond
		c := &Checker{Home: home, Env: []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home}}
		got := c.probeCodex(c.getenv("PATH"))
		cmdTimeout = saved
		if !got.Installed || got.LoggedIn != tc.want {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A ~/.claude folder is there as soon as the CLI has started once, signed in or not: only the CLI knows.
func TestClaudeLogin(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"signed in":  {`if [ "$1" = auth ]; then echo '{"loggedIn": true}'; exit 0; fi; echo "2.1.0 (Claude Code)"`, true},
		"signed out": {`if [ "$1" = auth ]; then echo '{"loggedIn": false}'; exit 1; fi; echo "2.1.0 (Claude Code)"`, false},
		"timed out":  {`if [ "$1" = auth ]; then sleep 5; fi; echo "2.1.0 (Claude Code)"`, true},
	} {
		script(t, bin, "claude", tc.body)
		saved := cmdTimeout
		cmdTimeout = 500 * time.Millisecond
		c := &Checker{Home: home, Env: []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home}}
		got := c.claudeStatus(c.getenv("PATH"))
		cmdTimeout = saved
		if !got.Installed || got.LoggedIn != tc.want {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A home that worked before the CLI was asked keeps its agent: its key or endpoint reaches the chat, not the probe.
func TestClaudeLoginKeepsAConfiguredHome(t *testing.T) {
	bin := t.TempDir()
	script(t, bin, "claude", `if [ "$1" = auth ]; then exit 1; fi; echo "2.1.0 (Claude Code)"`)
	for name, tc := range map[string]struct {
		file, body string
		want       bool
	}{
		"credentials":  {".credentials.json", `{}`, true},
		"own endpoint": {"settings.json", `{"env":{"ANTHROPIC_BASE_URL":"https://example.com"}}`, true},
		"plain":        {"settings.json", `{"theme":"dark"}`, false},
	} {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude", tc.file), []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c := &Checker{Home: home, Env: []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home}}
		if got := c.claudeProbe(c.getenv("PATH")); !got.Installed || got.LoggedIn != tc.want {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A managed machine keeps the sign-in in its own folder: the probe asks the CLI with it and counts its credentials.
func TestClaudeLoginInTheMachinesAuthFolder(t *testing.T) {
	home, bin, auth := t.TempDir(), t.TempDir(), t.TempDir()
	script(t, bin, "claude", `if [ "$1" = auth ]; then [ -n "$CLAUDE_SECURESTORAGE_CONFIG_DIR" ] && exit 0; exit 1; fi; echo "2.1.0 (Claude Code)"`)
	c := &Checker{Home: home, Env: []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home}, AuthDir: auth}
	if got := c.claudeProbe(c.getenv("PATH")); !got.LoggedIn {
		t.Errorf("asked with the auth folder: %+v", got)
	}
	script(t, bin, "claude", `if [ "$1" = auth ]; then exit 1; fi; echo "2.1.0 (Claude Code)"`)
	if got := c.claudeProbe(c.getenv("PATH")); got.LoggedIn {
		t.Errorf("an empty auth folder signs nobody in: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(auth, ".credentials.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.claudeProbe(c.getenv("PATH")); !got.LoggedIn {
		t.Errorf("credentials in the auth folder: %+v", got)
	}
}

func TestAuthDirOfTheRuntimeConfig(t *testing.T) {
	file := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(file, []byte(`{"claude_binary":"/opt/claude","auth_directory":"/run/auth/me"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := authDirOf(file); got != "/run/auth/me" {
		t.Errorf("authDirOf = %q", got)
	}
	if got := authDirOf(""); got != "" {
		t.Errorf("no config: %q", got)
	}
}
