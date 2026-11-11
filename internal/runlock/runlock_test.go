package runlock

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "x.lock")
	at := time.UnixMilli(1792400400000)
	l, err := Acquire(Config{File: file, App: "acme", Now: func() time.Time { return at }}, 8022)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	want := "{\n  \"pid\": " + strconv.Itoa(os.Getpid()) + ",\n  \"port\": 8022,\n  \"startedAt\": 1792400400000\n}\n"
	if string(raw) != want {
		t.Fatalf("%q", raw)
	}
	if again, err := Acquire(Config{File: file, App: "acme"}, 9000); err != nil || again == nil {
		t.Fatal("this process holds it already")
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	// A live runtime on its port keeps it.
	holder := `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":4321,"startedAt":1}`
	_ = os.WriteFile(file, []byte(holder), 0o600)
	_, err = Acquire(Config{File: file, App: "acme", Health: func(port int) (string, bool) { return "acme", port == 4321 }}, 8022)
	var held *HeldError
	if !errors.As(err, &held) || held.Port != 4321 {
		t.Fatalf("%v", err)
	}
	l.Release() // not ours any more: left alone
	if raw, _ := os.ReadFile(file); string(raw) != holder {
		t.Fatal("released another runtime's lock")
	}
}

const holdEnv = "RUNLOCK_TEST_HOLD"

// TestHoldHelper is the other process of TestSecondProcess: it takes the lock and holds it until it is killed.
func TestHoldHelper(t *testing.T) {
	file := os.Getenv(holdEnv)
	if file == "" {
		t.Skip("helper of TestSecondProcess")
	}
	if _, err := Acquire(Config{File: file, App: "acme"}, 4321); err != nil {
		os.Stdout.WriteString("failed " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("held\n")
	time.Sleep(time.Minute)
}

// A second process is refused for as long as the first lives, though the first answers nothing on its port; the moment
// the first is killed the lock is free.
func TestSecondProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHoldHelper$")
	cmd.Env = append(os.Environ(), holdEnv+"="+file)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "held\n" {
		t.Fatalf("helper: %q", line)
	}
	deaf := func(int) (string, bool) { return "", false }
	_, err = Acquire(Config{File: file, App: "acme", Health: deaf}, 8022)
	var held *HeldError
	if !errors.As(err, &held) || held.PID != cmd.Process.Pid || held.Port != 4321 {
		t.Fatalf("a live holder that does not answer lost the lock: %v", err)
	}
	if pid, _ := holder(file); pid != cmd.Process.Pid {
		t.Fatalf("the holder file was rewritten: pid %d", pid)
	}

	_ = cmd.Process.Signal(syscall.SIGKILL)
	_ = cmd.Wait()
	l, err := Acquire(Config{File: file, App: "acme", Health: deaf}, 8022)
	if err != nil {
		t.Fatalf("the lock of a killed process was not free: %v", err)
	}
	if pid, port := holder(file); pid != os.Getpid() || port != 8022 {
		t.Fatalf("holder file names %d on %d", pid, port)
	}
	l.Release()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("holder file left by a release")
	}
}

// A file left by a runtime older than the guard: it does not stand in the way once that runtime is gone or deaf, and
// is honoured while that runtime answers.
func TestOldHolderFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.lock")
	deaf := func(int) (string, bool) { return "", false }
	for name, body := range map[string]string{
		"gone":    `{"pid":2147483646,"port":4321,"startedAt":1}`,
		"deaf":    `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":4321,"startedAt":1}`,
		"garbage": "not json",
	} {
		_ = os.WriteFile(file, []byte(body), 0o600)
		l, err := Acquire(Config{File: file, App: "acme", Health: deaf}, 8022)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pid, _ := holder(file); pid != os.Getpid() {
			t.Fatalf("%s: holder file names %d", name, pid)
		}
		l.Release()
	}
	live := `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":4321,"startedAt":1}`
	_ = os.WriteFile(file, []byte(live), 0o600)
	_, err := Acquire(Config{File: file, App: "acme", Health: func(int) (string, bool) { return "acme", true }}, 8022)
	var held *HeldError
	if !errors.As(err, &held) || held.PID != os.Getppid() {
		t.Fatalf("%v", err)
	}
	// The refusal left the guard free for the next try.
	l, err := Acquire(Config{File: file, App: "acme", Health: deaf}, 8022)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}
