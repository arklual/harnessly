//go:build linux

package proc

import (
	"bufio"
	"math"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func start(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func waitFor[T any](t *testing.T, read func() T, done func(T) bool) T {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		v := read()
		if done(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not reached; last %v", v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pgrepChildren(pid int) []int {
	out, _ := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		n, _ := strconv.Atoi(f)
		pids = append(pids, n)
	}
	slices.Sort(pids)
	return pids
}

// process-proc: lists the same children pgrep does.
func TestChildrenEqualPgrep(t *testing.T) {
	sh := start(t, "sh", "-c", "sleep 30 & sleep 30 & wait")
	kids := waitFor(t, func() []int { return ChildPIDs(sh.Process.Pid) }, func(p []int) bool { return len(p) == 2 })
	if !slices.Equal(kids, pgrepChildren(sh.Process.Pid)) {
		t.Fatalf("%v vs pgrep %v", kids, pgrepChildren(sh.Process.Pid))
	}
	if got := ChildrenFromTable(sh.Process.Pid); !slices.Equal(got, kids) {
		t.Fatalf("from the table: %v", got)
	}
	if got := Descendants(sh.Process.Pid); !slices.Equal(got, kids) {
		t.Fatalf("descendants %v", got)
	}
}

// process-proc: finds a child started from a worker thread, which only that thread lists.
func TestChildOfWorkerThread(t *testing.T) {
	type res struct {
		pid int
		tid int
		err error
	}
	got := make(chan res)
	stop := make(chan struct{})
	var spawn func()
	spawn = func() {
		runtime.LockOSThread()
		tid := syscall.Gettid()
		if tid == os.Getpid() {
			go spawn() // the main thread: try from another one; this thread stays locked and is dropped
			return
		}
		cmd := exec.Command("sleep", "30")
		err := cmd.Start()
		if err != nil {
			got <- res{err: err}
			return
		}
		got <- res{pid: cmd.Process.Pid, tid: tid}
		<-stop
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	go spawn()
	r := <-got
	defer close(stop)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !slices.Contains(ChildPIDs(os.Getpid()), r.pid) {
		t.Fatalf("child %d of thread %d not listed: %v", r.pid, r.tid, ChildPIDs(os.Getpid()))
	}
	raw, _ := os.ReadFile("/proc/" + strconv.Itoa(os.Getpid()) + "/task/" + strconv.Itoa(os.Getpid()) + "/children")
	if slices.Contains(parsePids(string(raw)), r.pid) {
		t.Fatalf("the main thread lists %d too; the test proves nothing", r.pid)
	}
}

// process-proc: returns no children for a process that is gone.
func TestGoneProcess(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got := ChildPIDs(cmd.Process.Pid); got == nil || len(got) != 0 {
		t.Fatalf("%v", got)
	}
	if Running(cmd.Process.Pid) || !Running(os.Getpid()) || Running(0) {
		t.Fatal("running")
	}
	if Running(1) != true {
		t.Fatal("init belongs to root: EPERM counts as running")
	}
}

// process-proc: reads the command line the way ps prints it.
func TestCmdlineLikePs(t *testing.T) {
	sh := start(t, "sh", "-c", "sleep 31; echo done")
	waitFor(t, func() string { a, _ := Cmdline(sh.Process.Pid); return a }, func(a string) bool { return strings.HasPrefix(a, "sh ") })
	ps, _ := exec.Command("ps", "-p", strconv.Itoa(sh.Process.Pid), "-o", "args=").Output()
	if got, _ := Cmdline(sh.Process.Pid); got != strings.TrimSpace(string(ps)) {
		t.Fatalf("%q vs ps %q", got, ps)
	}
	if argv, _ := Argv(sh.Process.Pid); !slices.Equal(argv, []string{"sh", "-c", "sleep 31; echo done"}) {
		t.Fatalf("argv %q", argv)
	}
	if _, ok := Cmdline(1<<22 + 1); ok {
		t.Fatal("a missing process has no command line")
	}
	if Cwd(sh.Process.Pid) == "" {
		t.Fatal("cwd")
	}
}

func TestEnvironAndStartTime(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.Env = append(os.Environ(), "VISUAL=/data/bin/editor")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// Until exec the child has the parent's environment.
	waitFor(t, func() string { a, _ := Cmdline(cmd.Process.Pid); return a }, func(a string) bool { return a == "sleep 30" })
	env, _ := Environ(cmd.Process.Pid)
	if !HasEnvEntry(env, "VISUAL=/old/editor", "VISUAL=/data/bin/editor") || HasEnvEntry(env, "VISUAL=/data/bin") {
		t.Fatal("environ marker")
	}
	st, ok := StartTime(cmd.Process.Pid)
	if !ok || math.Abs(time.Since(st).Seconds()) > 3 {
		t.Fatalf("start %v %v", st, ok)
	}
	if p, ok := Parent(cmd.Process.Pid); !ok || p != os.Getpid() {
		t.Fatalf("parent %d", p)
	}
}

func TestNewestChild(t *testing.T) {
	sh := start(t, "sh", "-c", "sleep 30 & sleep 0.2; sleep 31 & wait")
	waitFor(t, func() string { a, _ := Cmdline(NewestChild(sh.Process.Pid)); return a }, func(a string) bool { return a == "sleep 31" })
}

func TestListeningPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if got := ListeningPorts([]int{os.Getpid()}); !slices.Contains(got, port) {
		t.Fatalf("own port %d not in %v", port, got)
	}
	if got := ListeningPorts(nil); len(got) != 0 {
		t.Fatal("no processes, no ports")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not here")
	}
	// A server two levels below a shell, as a dev server below a pane's shell.
	sh := exec.Command("sh", "-c", `python3 -c "import socket,time
s=socket.socket(); s.bind(('127.0.0.1',0)); s.listen(); print(s.getsockname()[1], flush=True); time.sleep(30)"; true`)
	sh.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, _ := sh.StdoutPipe()
	if err := sh.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-sh.Process.Pid, syscall.SIGKILL); _ = sh.Process.Kill(); _ = sh.Wait() }()
	line, _ := bufio.NewReader(out).ReadString('\n')
	childPort, _ := strconv.Atoi(strings.TrimSpace(line))
	got := TreeListeningPorts(sh.Process.Pid)
	if !slices.Equal(got, []int{childPort}) {
		t.Fatalf("tree ports %v, want [%d]", got, childPort)
	}
}

// agent-sleep: memory pressure reads the available share from /proc/meminfo.
func TestMemAvailableRatio(t *testing.T) {
	r, ok := ParseMemAvailableRatio("MemTotal:       24000000 kB\nMemFree:          400000 kB\nMemAvailable:    2400000 kB\n")
	if !ok || math.Abs(r-0.1) > 1e-9 {
		t.Fatalf("%v %v", r, ok)
	}
	if _, ok := ParseMemAvailableRatio("garbage"); ok {
		t.Fatal("garbage")
	}
	if r, ok := MemAvailableRatio(); !ok || r <= 0 || r > 1 {
		t.Fatalf("machine ratio %v %v", r, ok)
	}
}

func TestProcsFallbacks(t *testing.T) {
	var p Procs
	if p.ChildrenOf(1) != nil || p.CwdOf(1) != "" {
		t.Fatal("an empty table reads nothing")
	}
	if _, ok := p.ArgsOf(1); ok {
		t.Fatal("args")
	}
	p.Args = func(int) (string, bool) { return "node /x/claude.js --a", true }
	if argv, _ := p.ArgvOf(1); !slices.Equal(argv, []string{"node", "/x/claude.js", "--a"}) {
		t.Fatalf("%q", argv)
	}
	p.Children = func(pid int) []int { return map[int][]int{1: {2, 3}, 2: {4}}[pid] }
	if got := p.WithGrandchildren([]int{2, 3}); !slices.Equal(got, []int{2, 3, 4}) {
		t.Fatalf("%v", got)
	}
}
