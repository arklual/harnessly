package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The stopper on a private tmux server of this test only (-L hs-reaper-test): it never sees the machine's sessions.
func TestStopperEndsThePaneTreeAndTheSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	const socket = "hs-reaper-test"
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tm := tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"})
	session := "hs-ws-1-pane-1-tab-reap"
	// The pane's shell runs a "helper" in the background and waits on another: a tree two deep.
	out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", session, "-x", "80", "-y", "24",
		"sh -c 'sleep 600 & sleep 601; :'").CombinedOutput()
	if err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	s := &v2Stopper{v: &v2Live{c: &core{}, hub: wire.NewHub(nil)}, tm: tm, self: map[int]bool{os.Getpid(): true}}
	var pane int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if pane = tm.PanePID(session); pane > 0 && len(proc.Descendants(pane)) >= 2 {
			break
		}
	}
	if !s.Owned(session) {
		t.Fatal("the tab's session is not owned")
	}
	if s.Owned("hs-shell-tab-reap") || s.Owned("other") {
		t.Fatal("a session of no tab is owned")
	}
	sample, ok := s.Sample(session)
	if !ok || sample.Root != pane || len(sample.Procs) < 3 {
		t.Fatalf("sample %+v %v", sample, ok)
	}
	var sleeps []int
	for _, p := range sample.Procs {
		if p.Comm == "sleep" {
			sleeps = append(sleeps, p.PID)
		}
		if p.RSS <= 0 {
			t.Errorf("no memory read for %+v", p)
		}
	}
	if len(sleeps) != 2 || !strings.Contains(strings.Join(sample.Procs[0].Argv, " "), "sh") {
		t.Fatalf("tree %+v", sample.Procs)
	}
	if s.Watched("tab-reap", session) {
		t.Fatal("watched with nobody attached")
	}
	s.self[pane] = true
	if s.Owned(session) {
		t.Fatal("the pane that holds the runtime is owned")
	}
	delete(s.self, pane)

	freed, err := s.Stop(session)
	if err != nil {
		t.Fatal(err)
	}
	if freed <= 0 {
		t.Fatalf("freed %d", freed)
	}
	if tm.HasSession(session) {
		t.Fatal("the session is still there")
	}
	for _, pid := range append(sleeps, pane) {
		deadline := time.Now().Add(3 * time.Second)
		for !ended(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !ended(pid) {
			t.Fatalf("%d still runs", pid)
		}
	}
}

// ended: the process is gone, or a zombie its parent has not reaped yet.
func ended(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(raw)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(fields) == 0 || fields[0] == "Z" || fields[0] == "X"
}
