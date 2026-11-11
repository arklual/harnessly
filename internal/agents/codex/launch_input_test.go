package codex

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inputConfig(t *testing.T, socket string) LaunchConfig {
	t.Helper()
	dir := t.TempDir()
	return LaunchConfig{Hooks: &HookConfig{Script: "/data/codex-hook.sh", UserConfig: filepath.Join(dir, "none.toml")}, MCP: testMCP,
		LauncherScript: filepath.Join(dir, "codex-launcher.js"), LauncherContent: []byte("x"),
		InputSocket: func(conv string) string {
			if conv != "tab-new" {
				t.Errorf("socket asked for %q", conv)
			}
			return socket
		}}
}

// Only a fresh chat that names its conversation starts beside an app-server: its arguments come with the shell words
// that start the server, and its launcher line tells the conversation. Everything else starts as ever.
func TestAppServerStartIsOnlyForAFreshChat(t *testing.T) {
	c := inputConfig(t, "/data/codex-input/tab-new.sock")
	fresh := choice("gpt-5.4", "")
	fresh.Input = "tab-new"
	start, _, err := c.RuntimeStart("", "", fresh)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`S='/data/codex-input/tab-new.sock';`, `command codex app-server --listen "unix://$S" -c 'hooks.SessionStart=`,
		`-c 'check_for_update_on_startup=false'`, `command codex --remote "unix://$S" "$@"`} {
		if !strings.Contains(start.Prefix, want) {
			t.Fatalf("prefix lacks %q:\n%s", want, start.Prefix)
		}
	}
	if strings.Contains(start.Prefix, "gpt-5.4") || strings.Contains(start.Prefix, "bypass-hook-trust") {
		t.Fatalf("the server takes only the configuration pairs:\n%s", start.Prefix)
	}
	plain, _, _ := LaunchConfig{Hooks: c.Hooks, MCP: testMCP}.RuntimeStart("", "", choice("gpt-5.4", ""))
	if strings.Join(start.Args, "\x00") != strings.Join(plain.Args, "\x00") {
		t.Fatalf("codex's own arguments changed: %q", start.Args)
	}
	if line, err := c.LauncherLine("ws", "", fresh); err != nil || !strings.HasSuffix(line, " --model 'gpt-5.4' --conversation 'tab-new'") {
		t.Fatalf("%q %v", line, err)
	}

	resumed, _, _ := c.RuntimeStart("", session, fresh)
	forked, _, _ := c.ForkStart("", session, fresh)
	unnamed, _, _ := c.RuntimeStart("", "", choice("gpt-5.4", ""))
	none, _, _ := c.RuntimeStart("", "", nil)
	if resumed.Prefix != "" || forked.Prefix != "" || unnamed.Prefix != "" || none.Prefix != "" {
		t.Fatalf("prefixes %q %q %q %q", resumed.Prefix, forked.Prefix, unnamed.Prefix, none.Prefix)
	}
	if line, _ := c.LauncherLine("ws", session, fresh); strings.Contains(line, "--conversation") {
		t.Fatalf("a resumed chat names no conversation: %q", line)
	}
	// A machine whose Codex has no app-server gives no socket.
	c.InputSocket = func(string) string { return "" }
	if off, _, _ := c.RuntimeStart("", "", fresh); off.Prefix != "" {
		t.Fatalf("prefix %q", off.Prefix)
	}
	if line, _ := c.LauncherLine("ws", "", fresh); strings.Contains(line, "--conversation") {
		t.Fatalf("%q", line)
	}
	if inputPrefix("/s", []string{"-c", strings.Repeat("x", maxInputPrefix)}) != "" {
		t.Fatal("words too long to pass are not passed")
	}
}

// TestHelperBindSocket is no test: the fake codex of TestAppServerStartFallsBack runs it to listen on a socket the way
// `codex app-server` does.
func TestHelperBindSocket(t *testing.T) {
	path := os.Getenv("HELPER_SOCKET")
	if path == "" {
		return
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		os.Exit(1)
	}
	defer l.Close()
	time.Sleep(30 * time.Second)
}

// The shell words as the launcher runs them, with a codex that plays each case: the server comes up and the terminal
// attaches to it; the server does not come up, or the attached terminal leaves at once with an error, and codex starts
// as ever. Each time the server is ended and its socket removed.
func TestAppServerStartFallsBack(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	fake := `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
if [ "$1" = app-server ]; then
  [ "$FAKE_SERVER" = up ] || exit 1
  exec env HELPER_SOCKET="${3#unix://}" "$FAKE_HELPER" -test.run=TestHelperBindSocket
fi
if [ "$1" = --remote ] && [ "$FAKE_REMOTE" = fails ]; then exit 2; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"-c", `model="it's"`, "--yolo", "first prompt"}
	run := func(server, remote string) []string {
		t.Helper()
		socket, log := filepath.Join(dir, "in", "s.sock"), filepath.Join(dir, server+remote+".log")
		// As the launcher script runs it: the prefix, then codex with its arguments.
		cmd := exec.Command("sh", append([]string{"-c", inputPrefix(socket, args) + ` codex "$@"`, "codex"}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_LOG="+log,
			"FAKE_SERVER="+server, "FAKE_REMOTE="+remote, "FAKE_HELPER="+os.Args[0])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s/%s: %v\n%s", server, remote, err, out)
		}
		if _, err := os.Stat(socket); err == nil {
			t.Fatalf("%s/%s: the socket is left behind", server, remote)
		}
		raw, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	socket := filepath.Join(dir, "in", "s.sock")
	serverLine := "app-server --listen unix://" + socket + ` -c model="it's"`
	plain := `-c model="it's" --yolo first prompt`
	attached := "--remote unix://" + socket + " " + plain
	for _, c := range []struct {
		server, remote string
		want           []string
	}{
		{"up", "ok", []string{serverLine, attached}},
		{"down", "ok", []string{serverLine, plain}},
		{"up", "fails", []string{serverLine, attached, plain}},
	} {
		if got := run(c.server, c.remote); strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Fatalf("%s/%s ran\n%s\nwant\n%s", c.server, c.remote, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
		}
	}
}
