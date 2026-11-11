package heavy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlyCallsThatWriteNothingBackGoRemote(t *testing.T) {
	for _, c := range []struct {
		args string
		want bool
	}{
		{"test ./...", true},
		{"test -run=TestX -count 1 -race -v ./internal/x", true},
		{"test -json -cover ./x", true},
		{"vet ./...", true},
		{"test -c ./x", false},
		{"test -o x.test ./x", false},
		{"test -coverprofile=c.out ./x", false},
		{"test -fuzz=FuzzX ./x", false},
		{"test ./x -update", false},
		{"test ./x -args -foo", false},
		{"vet -vettool=/bin/x ./...", false},
		{"build ./...", true},
		{"build ./internal/a ./cmd/b", true},
		{"build -tags x -o /dev/null ./cmd/b", true},
		{"build -o=/dev/null ./cmd/b", true},
		{"build ./cmd/b", false},
		{"build", false},
		{"build -tags x ./cmd/b", false},
		{"build -o bin/b ./cmd/b ./cmd/c", false},
		{"build -o bin/ ./...", false},
		{"build -toolexec=x ./...", false},
		{"build -C sub ./...", false},
		{"install ./cmd/x", false},
	} {
		if got := remoteFits("go", strings.Fields(c.args)); got != c.want {
			t.Errorf("go %s: remote %v, want %v", c.args, got, c.want)
		}
	}
	if remoteFits("vitest", []string{"run"}) {
		t.Error("vitest went remote")
	}
}

// remoteFixture is a fixture with a git tree, a remote root, and ssh and rsync that act on this machine.
func remoteFixture(t *testing.T) (f *fixture, tree, remote string) {
	f = newFixture(t)
	root := t.TempDir()
	tree, remote = filepath.Join(root, "tree"), filepath.Join(root, "remote")
	for _, d := range []string{filepath.Join(tree, ".git"), filepath.Join(tree, "pkg"), filepath.Join(tree, "node_modules", "x")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string, mode os.FileMode) {
		if err := os.WriteFile(p, []byte(s), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(tree, "pkg", "x.go"), "package pkg\n", 0o644)
	// rsync SRC/ HOST:DEST/ and back copies by tar, without .git and node_modules; ssh HOST CMD runs CMD here.
	write(filepath.Join(f.real, "rsync"), `#!/bin/sh
PATH=$PATH:/usr/bin:/bin
[ -n "$FAKE_RSYNC_FAIL" ] && { echo "rsync: connection unexpectedly closed" >&2; exit 12; }
for a; do s=$d; d=$a; done
case $s in *:*) [ -n "$FAKE_RSYNC_FAIL_BACK" ] && { echo "rsync: connection unexpectedly closed" >&2; exit 12; } ;; esac
s=${s#*:} d=${d#*:}
[ -n "$HOME" ] && cd "$HOME"
mkdir -p "$d" && (cd "$s" && tar cf - --exclude=./.git --exclude=node_modules .) | (cd "$d" && tar xf -)
`, 0o755)
	write(filepath.Join(f.real, "ssh"), `#!/bin/sh
PATH=$PATH:/usr/bin:/bin
[ -n "$FAKE_SSH_FAIL" ] && { echo "ssh: connect to host b port 22: Connection refused" >&2; exit 255; }
for a; do c=$a; done
[ -n "$HOME" ] && cd "$HOME"
exec sh -c "$c"
`, 0o755)
	f.env[envRemote] = "build"
	f.env[envRemoteRoot] = remote
	f.env["GOFLAGS"] = "-mod=mod"
	f.wd = filepath.Join(tree, "pkg")
	return f, tree, remote
}

func TestAGoTestRunsOnTheRemoteMachine(t *testing.T) {
	f, tree, remote := remoteFixture(t)
	if code := f.run("go", "test", "-count=1", "./..."); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	out := f.stdout.String()
	for _, want := range []string{"dir=" + filepath.Join(remote, tree, "pkg") + " ", "args=test -count=1 ./...", "slot= ", "procs= ", "flags=-mod=mod "} {
		if !strings.Contains(out, want) {
			t.Errorf("the remote tool saw %q, want %q in it", out, want)
		}
	}
	if _, err := os.Stat(filepath.Join(remote, tree, "pkg", "x.go")); err != nil {
		t.Errorf("the tree was not copied: %v", err)
	}
	if f.stderr.Len() != 0 {
		t.Errorf("stderr %q, want nothing", f.stderr.String())
	}
	st, err := ReadStats(f.slots, time.Now().Add(-time.Hour))
	if err != nil || st.Remote != 1 || st.Ran != 0 || st.ByTool["go"] != 1 {
		t.Errorf("stats %+v %v, want one go on the remote", st, err)
	}
}

func TestTheRemoteExitCodeComesBack(t *testing.T) {
	f, _, _ := remoteFixture(t)
	f.env["FAKE_EXIT"] = "3"
	if code := f.run("go", "vet", "./..."); code != 3 {
		t.Fatalf("exit %d, want the remote tool's 3: %s", code, f.stderr.String())
	}
}

func TestAFailedConnectionRunsTheCallHere(t *testing.T) {
	for _, fail := range []string{"FAKE_SSH_FAIL", "FAKE_RSYNC_FAIL"} {
		f, _, _ := remoteFixture(t)
		f.env[fail] = "1"
		if code := f.run("go", "test", "./x"); code != 0 {
			t.Fatalf("%s: exit %d: %s", fail, code, f.stderr.String())
		}
		if !strings.Contains(f.stdout.String(), "slot=1") {
			t.Errorf("%s: stdout %q, want the call run here in a slot", fail, f.stdout.String())
		}
		if !strings.Contains(f.stderr.String(), "did not start on build") {
			t.Errorf("%s: stderr %q, want one line saying why", fail, f.stderr.String())
		}
	}
}

func TestCallsThatWriteFilesAndCallsOutsideATreeStayHere(t *testing.T) {
	f, _, _ := remoteFixture(t)
	if code := f.run("go", "test", "-c", "./x"); code != 0 || !strings.Contains(f.stdout.String(), "slot=1") {
		t.Errorf("go test -c: exit %d, stdout %q, want it run here", code, f.stdout.String())
	}
	f.wd = t.TempDir()
	if code := f.run("go", "test", "./x"); code != 0 || !strings.Contains(f.stdout.String(), "slot=1") || f.stderr.Len() != 0 {
		t.Errorf("outside a tree: exit %d, stdout %q, stderr %q, want it run here quietly", code, f.stdout.String(), f.stderr.String())
	}
}
