package heavy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSlotsFollowTheCores(t *testing.T) {
	for cpus, want := range map[int]int{1: 1, 4: 1, 5: 2, 8: 2, 16: 2, 32: 4} {
		if got := Slots(cpus); got != want {
			t.Errorf("Slots(%d) = %d, want %d", cpus, got, want)
		}
	}
}

func TestHeavyCalls(t *testing.T) {
	cases := []struct {
		tool string
		args string
		want bool
	}{
		{"go", "test ./internal/heavy", true},
		{"go", "-C sub build ./...", true},
		{"go", "vet ./...", true},
		{"go", "install ./cmd/x", true},
		{"go", "env GOPATH", false},
		{"go", "list -m all", false},
		{"go", "version", false},
		{"go", "mod tidy", false},
		{"go", "", false},
		{"vitest", "run src/a.test.ts", true},
		{"vitest", "--version", false},
		{"tsc", "--noEmit -p .", true},
		{"tsc", "-v", false},
		{"eslint", "src", true},
		{"playwright", "test e2e/a.spec.ts", true},
		{"playwright", "install chromium", false},
		{"npx", "vitest run", true},
		{"npx", "--yes vitest@2 run", true},
		{"npx", "tsc --noEmit", true},
		{"npx", "-p typescript tsc --noEmit", true},
		{"npx", "playwright test", true},
		{"npx", "@playwright/test test", true},
		{"npx", "playwright install", false},
		{"npx", "eslint .", true},
		{"npx", "prettier --check .", false},
		{"npx", "tsc --version", false},
		{"vitest", "--watch", false},
		{"tsc", "-w -p .", false},
		{"npm", "test", true},
		{"npm", "t", true},
		{"npm", "--prefix web test", true},
		{"npm", "run lint", true},
		{"npm", "run test:unit -- --reporter dot", true},
		{"npm", "run build:web", true},
		{"npm", "run check", true},
		{"npm", "run dev", false},
		{"npm", "run test -- --watch", false},
		{"npm", "run", false},
		{"npm", "install", false},
		{"npm", "ci", false},
		{"npm", "view vitest version", false},
		{"npm", "exec vitest run", true},
		{"npm", "exec -- eslint .", true},
		{"npm", "--version", false},
		{"pnpm", "test", true},
		{"pnpm", "--filter web run build", true},
		{"pnpm", "-C web lint", true},
		{"pnpm", "vitest run", true},
		{"pnpm", "dlx tsc --noEmit", true},
		{"pnpm", "exec playwright test", true},
		{"pnpm", "install", false},
		{"pnpm", "dev", false},
		{"pnpm", "add -D vitest", false},
	}
	for _, c := range cases {
		if got := Heavy(c.tool, strings.Fields(c.args)); got != c.want {
			t.Errorf("Heavy(%s %s) = %v, want %v", c.tool, c.args, got, c.want)
		}
	}
}

// fixture is a PATH with a launcher folder in front of a folder with a fake go that prints what it was given.
type fixture struct {
	launchers, real, slots string
	env                    map[string]string
	stdout, stderr         bytes.Buffer
	execd                  []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{launchers: filepath.Join(root, "launchers"), real: filepath.Join(root, "real"), slots: filepath.Join(root, "slots")}
	for _, d := range []string{f.launchers, f.real} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(f.launchers, "go"), "#!/bin/sh\n# Installed by harnessly ("+Marker+")\nexit 99\n")
	write(filepath.Join(f.real, "go"), "#!/bin/sh\necho \"args=$* slot=$HARNESSLY_HEAVY_SLOT procs=$GOMAXPROCS flags=$GOFLAGS path=$PATH\"\nexit ${FAKE_EXIT:-0}\n")
	f.env = map[string]string{
		"PATH":                 f.launchers + ":" + f.real,
		"HARNESSLY_HEAVY_DIR":  f.slots,
		"HARNESSLY_HEAVY_WAIT": "5s",
	}
	return f
}

func (f *fixture) run(args ...string) int {
	f.stdout.Reset()
	f.stderr.Reset()
	return Run(append([]string{"--skip", f.launchers}, args...), Env{
		Getenv: func(k string) string { return f.env[k] },
		Environ: func() []string {
			var out []string
			for k, v := range f.env {
				out = append(out, k+"="+v)
			}
			return out
		},
		Stdout: &f.stdout, Stderr: &f.stderr, CPUs: 4, Poll: 20 * time.Millisecond,
		Exec: func(path string, argv, env []string) error {
			f.execd = append([]string{path}, argv...)
			return nil
		},
	})
}

func (f *fixture) holdSlot(t *testing.T, n int) *os.File {
	t.Helper()
	root, err := openDir(f.slots)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := openShared(root, "slot"+string(rune('0'+n)), os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return lock
}

func TestALightCallRunsTheRealToolAtOnce(t *testing.T) {
	f := newFixture(t)
	if code := f.run("go", "env", "GOPATH"); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	if len(f.execd) == 0 || f.execd[0] != filepath.Join(f.real, "go") || strings.Join(f.execd[1:], " ") != "go env GOPATH" {
		t.Fatalf("exec %v, want the real go", f.execd)
	}
	if _, err := os.Stat(f.slots); err == nil {
		t.Error("a light call made the slots' folder")
	}
}

func TestAHeavyCallTakesASlotAndRunsGently(t *testing.T) {
	f := newFixture(t)
	if code := f.run("go", "test", "./x"); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	out := f.stdout.String()
	for _, want := range []string{"args=test ./x", "slot=1", "procs=2", "flags=-p=2", "path=" + f.real + "\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("the tool saw %q, want %q in it", out, want)
		}
	}
	if f.execd != nil {
		t.Error("a heavy call replaced the process instead of holding the slot")
	}
	st, err := ReadStats(f.slots, time.Now().Add(-time.Hour))
	if err != nil || st.Ran != 1 || st.ByTool["go"] != 1 {
		t.Errorf("stats %+v %v, want one go that ran", st, err)
	}
	if info, err := os.Stat(f.slots); err != nil || info.Mode().Perm() != 0o777 || info.Mode()&os.ModeSticky == 0 {
		t.Errorf("slots' folder mode %v, want sticky and open to every user", info.Mode())
	}
}

func TestTheToolsExitCodeComesBack(t *testing.T) {
	f := newFixture(t)
	f.env["FAKE_EXIT"] = "3"
	if code := f.run("go", "vet", "./..."); code != 3 {
		t.Fatalf("exit %d, want the tool's 3", code)
	}
}

func TestTheCallersOwnParallelismStays(t *testing.T) {
	f := newFixture(t)
	f.env["GOFLAGS"] = "-mod=mod -p=1"
	f.run("go", "build", "./...")
	if !strings.Contains(f.stdout.String(), "flags=-mod=mod -p=1 ") {
		t.Errorf("GOFLAGS rewritten: %s", f.stdout.String())
	}
}

func TestABusyMachineRefusesInWordsAnAgentActsOn(t *testing.T) {
	f := newFixture(t)
	f.env["HARNESSLY_HEAVY_WAIT"] = "100ms"
	lock := f.holdSlot(t, 1)
	defer lock.Close()
	if code := f.run("go", "test", "./..."); code != ExitBusy {
		t.Fatalf("exit %d, want %d", code, ExitBusy)
	}
	msg := f.stderr.String()
	for _, want := range []string{"waiting up to", "the machine is busy", "people's chats", "did not run", "Do not retry it now",
		"go on without this check", "CI", "(exit 75)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if f.stdout.Len() != 0 {
		t.Error("the tool ran on a busy machine")
	}
	st, _ := ReadStats(f.slots, time.Now().Add(-time.Hour))
	if st.Refused != 1 || st.Ran != 0 {
		t.Errorf("stats %+v, want one refused", st)
	}
}

func TestAWaitingCallRunsWhenASlotFreesUp(t *testing.T) {
	f := newFixture(t)
	lock := f.holdSlot(t, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		lock.Close()
	}()
	if code := f.run("go", "test", "./..."); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "slot=1") {
		t.Errorf("tool output %q", f.stdout.String())
	}
	st, _ := ReadStats(f.slots, time.Now().Add(-time.Hour))
	if st.Waited != 1 || st.WaitMaxMs < 100 {
		t.Errorf("stats %+v, want one call that waited", st)
	}
}

func TestTheSecondSlotIsUsedOnABiggerMachine(t *testing.T) {
	f := newFixture(t)
	lock := f.holdSlot(t, 1)
	defer lock.Close()
	f.env["HARNESSLY_HEAVY_SLOTS"] = "2"
	if code := f.run("go", "test", "./..."); code != 0 || !strings.Contains(f.stdout.String(), "slot=2") {
		t.Fatalf("exit %d, output %q", code, f.stdout.String())
	}
}

func TestACallInsideAHeldSlotDoesNotWaitForItself(t *testing.T) {
	f := newFixture(t)
	f.env["HARNESSLY_HEAVY_WAIT"] = "0s"
	lock := f.holdSlot(t, 1)
	defer lock.Close()
	f.env["HARNESSLY_HEAVY_SLOT"] = "1"
	if code := f.run("go", "build", "./..."); code != 0 || f.execd == nil {
		t.Fatalf("exit %d, exec %v: a nested call must run at once", code, f.execd)
	}
}

func TestOffRunsEverythingAtOnce(t *testing.T) {
	f := newFixture(t)
	f.env["HARNESSLY_HEAVY"] = "off"
	if code := f.run("go", "test", "./..."); code != 0 || f.execd == nil {
		t.Fatalf("exit %d, exec %v", code, f.execd)
	}
}

func TestAMissingToolSaysSo(t *testing.T) {
	f := newFixture(t)
	if code := f.run("tsc", "--noEmit"); code != 127 || !strings.Contains(f.stderr.String(), "tsc is not installed") {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
}

func TestALauncherElsewhereOnPathIsNotTheRealTool(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "go"), []byte("#!/bin/sh\n# "+Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.env["PATH"] = other + ":" + f.env["PATH"]
	f.run("go", "env")
	if len(f.execd) == 0 || f.execd[0] != filepath.Join(f.real, "go") {
		t.Fatalf("exec %v, want the real go", f.execd)
	}
}

func TestStatsCommand(t *testing.T) {
	f := newFixture(t)
	f.run("go", "test", "./...")
	f.run("stats", "--since", "1h")
	if out := f.stdout.String(); !strings.Contains(out, "ran at once  1") || !strings.Contains(out, "refused      0") {
		t.Errorf("stats said %q", out)
	}
	f.run("stats", "--json")
	if !strings.Contains(f.stdout.String(), `"ran": 1`) {
		t.Errorf("json %q", f.stdout.String())
	}
}

func TestALinkInTheSharedFolderIsNeverFollowed(t *testing.T) {
	f := newFixture(t)
	if root, err := openDir(f.slots); err != nil {
		t.Fatal(err)
	} else {
		root.Close()
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(f.slots, "events")); err != nil {
		t.Fatal(err)
	}
	if code := f.run("go", "test", "./..."); code != 0 {
		t.Fatalf("exit %d: %s", code, f.stderr.String())
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("the link's target was written: %q", got)
	}
}

func TestALinkedFolderIsNotUsedForSlots(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	_ = os.MkdirAll(real, 0o755)
	link := filepath.Join(root, "link")
	_ = os.Symlink(real, link)
	if root, err := openDir(link); err == nil {
		root.Close()
		t.Error("a link was taken for the slots' folder")
	}
}

func TestAHardLinkOrALinkInsideTheFolderIsNotUsed(t *testing.T) {
	f := newFixture(t)
	root, err := openDir(f.slots)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	victim := filepath.Join(f.slots, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(victim, filepath.Join(f.slots, "events")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("victim", filepath.Join(f.slots, "slot1")); err != nil {
		t.Fatal(err)
	}
	if _, err := openShared(root, "events", os.O_WRONLY|os.O_APPEND); err == nil {
		t.Error("a hard link was opened")
	}
	if _, err := openShared(root, "slot1", os.O_RDONLY); err == nil {
		t.Error("a link inside the folder was followed")
	}
	f.env["HARNESSLY_HEAVY_SLOTS"] = "1"
	f.run("go", "test", "./...")
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("the linked file was written: %q", got)
	}
}

func TestANewFolderIsOpenToAllAndSticky(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "slots")
	root, err := openDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if st, err := os.Lstat(dir); err != nil || st.Mode().Perm() != 0o777 || st.Mode()&os.ModeSticky == 0 {
		t.Errorf("mode %v %v", st.Mode(), err)
	}
}
