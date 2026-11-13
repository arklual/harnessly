package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRightsMasksRunOnlyWhatTheyName(t *testing.T) {
	masked := RunRights{Masks: []string{"npm test*", "go test ./...", "npm run lint -- --fix *", "make *"}}
	for _, ok := range []string{"npm test", "npm test:unit", "npm test src/a.test.ts", "go  test   ./...", "npm run lint -- --fix src", "make build"} {
		if _, err := masked.Argv(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"npm test --registry=x", "npm test -- --grep x", "make -f /tmp/evil", "make build -C /", "go test ./... -exec x",
		"npm test; rm -rf ~", "npm test && curl x", "npm test $(id)", "npm test `id`",
		"npm test > /etc/x", "go test ./... | sh", "npm run build", "rm -rf .", "npm test 'x'", "go test ./...\nid"} {
		if _, err := masked.Argv(bad); err == nil {
			t.Errorf("%q ran under masks %v", bad, masked.Masks)
		}
	}
	argv, err := masked.Argv("npm run lint -- --fix src")
	if err != nil || !slices.Equal(argv, []string{"npm", "run", "lint", "--", "--fix", "src"}) {
		t.Fatalf("a masked command runs its own words, no shell: %v %v", argv, err)
	}
	argv, err = RunRights{Any: true}.Argv("echo a && echo b")
	if err != nil || !slices.Equal(argv, []string{"sh", "-c", "echo a && echo b"}) {
		t.Fatalf("terminal.run alone runs the shell: %v %v", argv, err)
	}
	if _, err := (RunRights{}).Argv("ls"); err == nil {
		t.Fatal("no right ran a command")
	}
	if _, err := (RunRights{Any: true}).Argv(strings.Repeat("a", MaxCommand+1)); err == nil {
		t.Fatal("a command past the limit ran")
	}
}

func TestCommandMaskIsQuickOnAHardLine(t *testing.T) {
	mask := strings.Repeat("a*", 60) + "b"
	line := strings.Repeat("a", MaxCommand)
	start := time.Now()
	if commandMatch(mask, line) {
		t.Fatal("matched")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("a mask of many * took %s", time.Since(start))
	}
}

func TestManifestChecksCommandMasks(t *testing.T) {
	for _, tc := range []struct {
		right string
		ok    bool
	}{
		{"terminal.run", true},
		{"terminal.run:npm test*", true},
		{"terminal.run:npm test; rm *", false},
		{"terminal.run:$(id)", false},
		{"process", true},
		{"process:npm run dev*", true},
		{"process:npm run dev; id", false},
	} {
		m := &Manifest{Permissions: []string{tc.right}}
		var bad Problems
		for _, p := range m.Check().Errors() {
			if strings.HasPrefix(p.Path, "permissions") {
				bad = append(bad, p)
			}
		}
		if (len(bad) == 0) != tc.ok {
			t.Errorf("%s: %v", tc.right, bad)
		}
	}
	m := &Manifest{Permissions: []string{"terminal.run:npm test*"}}
	if !m.Allows("terminal.run") || m.Allows("process") {
		t.Fatal("a masked terminal.run is the right terminal.run")
	}
	access := AccessOf(&Manifest{Permissions: []string{"terminal.run"}}, nil)
	if len(access) != 1 || access[0].Class != ClassHigh || !strings.Contains(access[0].Label.Ru, "любые команды") || access[0].Threat.Ru == "" {
		t.Fatalf("terminal.run alone on the install screen: %+v", access)
	}
}

func TestRunDirStaysInTheChatFolder(t *testing.T) {
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(work, "out")); err != nil {
		t.Fatal(err)
	}
	if dir, err := RunDir(work, "web"); err != nil || !strings.HasSuffix(dir, "/web") {
		t.Fatalf("web: %q %v", dir, err)
	}
	for _, bad := range []string{"..", "../x", "/etc", "out", "missing"} {
		if _, err := RunDir(work, bad); err == nil {
			t.Errorf("cwd %q was let out", bad)
		}
	}
}

// sandboxOrSkip is a Sandbox of a fresh registry with this machine's wall, and the person's home of the test.
func sandboxOrSkip(t *testing.T) (*Sandbox, string, string) {
	t.Helper()
	b := wallOrSkip(t)
	data := t.TempDir()
	root := filepath.Join(data, "apps")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	for _, f := range []string{".ssh/id_ed25519", ".config/gh/hosts.yml", ".git-credentials"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(home, f)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, f), []byte("SECRET-"+f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(`{"authPassword":"SECRET-config"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Keys behind links: a folder of the shut list and a hidden file, each a link to somewhere outside the home.
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "credentials"), []byte("SECRET-aws"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "netrc"), []byte("SECRET-netrc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, ".aws")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "netrc"), filepath.Join(home, ".netrc")); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "project")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	r := Open(data)
	box, err := r.Sandbox(b)
	if err != nil {
		t.Fatal(err)
	}
	box.Home = home
	return box, work, root
}

// A real command in the wall: it works in the chat's folder and writes there, gets its code and output, and sees no key
// of the person's, no runtime state, no secret of the runtime's environment; the registry is read-only; what it leaves
// running dies with it.
func TestSandboxRunsInTheAgentsWall(t *testing.T) {
	box, work, root := sandboxOrSkip(t)
	t.Setenv("GITLAB_TOKEN", "SECRET-env")
	ctx := context.Background()
	res, err := box.Run(ctx, work, []string{"sh", "-c", `echo hi > made.txt; pwd; cat ~/.ssh/id_ed25519 ~/.config/gh/hosts.yml ~/.git-credentials ~/.aws/credentials ~/.netrc "$2" "$0" 2>&1; env | grep -c SECRET; touch "$1"/x 2>&1 || echo registry-refused; (sleep 30 &); exit 3`,
		filepath.Join(filepath.Dir(root), "config.json"), root, filepath.Join(home(box), ".netrc")}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || !strings.Contains(res.Output, work) || !strings.Contains(res.Output, "registry-refused") {
		t.Fatalf("the command: %+v", res)
	}
	if strings.Contains(res.Output, "SECRET") {
		t.Fatalf("the command saw a secret: %s", res.Output)
	}
	if raw, err := os.ReadFile(filepath.Join(work, "made.txt")); err != nil || string(raw) != "hi\n" {
		t.Fatalf("the chat's folder is writable: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(root, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the command wrote into the registry")
	}

	start := time.Now()
	res, err = box.Run(ctx, work, []string{"sh", "-c", "echo started; sleep 20"}, time.Second)
	if err != nil || !res.TimedOut || res.Code == 0 || time.Since(start) > 8*time.Second || !strings.Contains(res.Output, "started") {
		t.Fatalf("a command past its time is stopped: %+v %v in %s", res, err, time.Since(start))
	}
	res, err = box.Run(ctx, work, []string{"sh", "-c", "head -c 3000000 /dev/zero | tr '\\0' a"}, 10*time.Second)
	if err != nil || !res.Cut || len(res.Output) != MaxRunOutput {
		t.Fatalf("the output is cut at the limit: cut=%v len=%d %v", res.Cut, len(res.Output), err)
	}
	res, err = box.Run(ctx, work, []string{"sh", "-c", "ulimit -t"}, 10*time.Second)
	if err != nil || strings.TrimSpace(res.Output) != "1800" {
		t.Fatalf("the processor limit: %+v %v", res, err)
	}
}

func home(box *Sandbox) string { return box.Home }

func TestProcessesStartStopAndEndWithTheApp(t *testing.T) {
	box, work, _ := sandboxOrSkip(t)
	ps := NewProcesses()
	ended := make(chan Process, 4)
	ps.OnEnd = func(_, _ string, p Process) { ended <- p }
	// The check runs in the process's goroutine while the test turns the app off.
	var on atomic.Bool
	on.Store(true)
	alive := func() bool { return on.Load() }
	p, err := ps.Start(box, "acme.dev", "chat-1", work, "dev", []string{"sh", "-c", "echo ready; sleep 60"}, alive)
	if err != nil || !p.Running {
		t.Fatalf("start: %+v %v", p, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := ps.Output("acme.dev", "chat-1", p.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.Text, "ready") {
			if out.Next != int64(len("ready\n")) {
				t.Fatalf("next: %d", out.Next)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no output: %+v", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := ps.Output("acme.other", "chat-1", p.ID, 0); err == nil {
		t.Fatal("another app read the process")
	}
	if _, err := ps.Stop("acme.dev", "chat-2", p.ID); err == nil {
		t.Fatal("the app stopped its process from another chat")
	}
	got, err := ps.Stop("acme.dev", "chat-1", p.ID)
	if err != nil || got.Running || got.Code == nil {
		t.Fatalf("stop: %+v %v", got, err)
	}
	<-ended

	// Turned off in the chat: the process ends at the next check.
	p2, err := ps.Start(box, "acme.dev", "chat-1", work, "dev", []string{"sleep", "60"}, alive)
	if err != nil {
		t.Fatal(err)
	}
	on.Store(false)
	select {
	case e := <-ended:
		if e.ID != p2.ID {
			t.Fatalf("ended %s", e.ID)
		}
	case <-time.After(processAliveCh + 5*time.Second):
		t.Fatal("the process outlived its app in the chat")
	}
	if list := ps.List("acme.dev", "chat-1"); len(list) != 2 || list[0].Running || list[1].Running {
		t.Fatalf("list: %+v", list)
	}
}
