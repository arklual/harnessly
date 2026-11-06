//go:build unix

package supervise

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func fast(o Options) Options {
	o.BackoffBase, o.BackoffMax, o.Stable, o.Grace = 20*time.Millisecond, 80*time.Millisecond, time.Hour, time.Second
	return o
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBackoffDoublesToTheCap(t *testing.T) {
	var got []time.Duration
	for n := 1; n <= 7; n++ {
		got = append(got, Backoff(n, time.Second, 10*time.Second))
	}
	want := []int{1, 2, 4, 8, 10, 10, 10}
	for i := range want {
		if got[i] != time.Duration(want[i])*time.Second {
			t.Fatalf("step %d: %v, want %vs", i+1, got[i], want[i])
		}
	}
}

func run(t *testing.T, o Options) (cancel func(), done chan struct{}) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { _ = Run(ctx, fast(o)); close(done) }()
	t.Cleanup(func() { stop(); <-done })
	return stop, done
}

func testBoth(t *testing.T, name string, reap bool) {
	t.Run(name, func(t *testing.T) {
		dir := t.TempDir()
		status := filepath.Join(dir, "supervisor.json")
		errs := &lockedBuf{}
		counter := filepath.Join(dir, "n")
		// Ends with status 3 the first two times, then stays up.
		script := `echo x >> ` + counter + `; [ $(wc -l < ` + counter + `) -ge 3 ] && exec sleep 30; exit 3`
		stop, done := run(t, Options{
			Children:   []Child{{Name: "runtime", Path: "sh", Args: []string{"-c", script}}, {Name: "agent", Path: "sleep", Args: []string{"30"}}},
			Stdout:     &lockedBuf{},
			Stderr:     errs,
			StatusFile: status,
			Reap:       &reap,
		})
		waitUntil(t, "two restarts of the runtime", func() bool {
			return strings.Count(errs.String(), "runtime ended") >= 2
		})
		if !strings.Contains(errs.String(), "runtime ended after") || !strings.Contains(errs.String(), "exited with status 3") {
			t.Fatalf("the reason is not said:\n%s", errs.String())
		}
		if strings.Contains(errs.String(), "agent ended") {
			t.Fatalf("the agent was running and must not have been restarted:\n%s", errs.String())
		}
		waitUntil(t, "the status file", func() bool {
			b, err := os.ReadFile(status)
			if err != nil {
				return false
			}
			var s struct {
				Children []ChildStatus `json:"children"`
			}
			if json.Unmarshal(b, &s) != nil || len(s.Children) != 2 {
				return false
			}
			return s.Children[0].Restarts == 2 && s.Children[0].Running && s.Children[1].Running && strings.Contains(s.Children[0].LastEnd, "status 3")
		})
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after the context ended")
		}
	})
}

func TestRestartsWithTheReasonAndNoteInTheStatusFile(t *testing.T) {
	testBoth(t, "waiting", false)
	testBoth(t, "reaping", true)
}

func TestKilledChildIsStartedAgainAndTheSignalIsSaid(t *testing.T) {
	for _, reap := range []bool{false, true} {
		dir := t.TempDir()
		errs := &lockedBuf{}
		pidFile := filepath.Join(dir, "pid")
		o := Options{
			Children: []Child{{Name: "agent", Path: "sh", Args: []string{"-c", `echo $$ >> ` + pidFile + `; exec sleep 30`}}},
			Stdout:   &lockedBuf{}, Stderr: errs, Reap: &reap,
		}
		run(t, o)
		var first string
		waitUntil(t, "the first start", func() bool {
			b, _ := os.ReadFile(pidFile)
			first = strings.TrimSpace(string(b))
			return first != ""
		})
		if err := killPid(first); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "the second start", func() bool {
			b, _ := os.ReadFile(pidFile)
			return strings.Count(string(b), "\n") >= 2
		})
		if !strings.Contains(errs.String(), "killed by signal 9 (killed)") {
			t.Fatalf("reap=%v: the signal is not said:\n%s", reap, errs.String())
		}
	}
}

func TestStopsTheChildrenOnTheContext(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	stop, done := run(t, Options{
		Children: []Child{{Name: "runtime", Path: "sh", Args: []string{"-c", `echo $$ > ` + pidFile + `; exec sleep 30`}}},
		Stdout:   &lockedBuf{}, Stderr: &lockedBuf{},
	})
	var pid string
	waitUntil(t, "the start", func() bool { b, _ := os.ReadFile(pidFile); pid = strings.TrimSpace(string(b)); return pid != "" })
	stop()
	<-done
	if alive(pid) {
		t.Fatalf("child %s is still alive after Run returned", pid)
	}
}

func TestAProgramThatIsNotThereIsRetriedAndSaid(t *testing.T) {
	errs := &lockedBuf{}
	run(t, Options{Children: []Child{{Name: "runtime", Path: "/no/such/program"}}, Stdout: &lockedBuf{}, Stderr: errs})
	waitUntil(t, "two tries", func() bool { return strings.Count(errs.String(), "did not start") >= 2 })
}
