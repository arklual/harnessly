package apps

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// readUntil reads the terminal until its output holds want, or fails after a while.
func readUntil(t *testing.T, term *Terminal, out *bytes.Buffer, want string) {
	t.Helper()
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in %q", want, out.String())
		}
		_ = term.Pty.SetReadDeadline(time.Now().Add(time.Second))
		n, _ := term.Pty.Read(buf)
		out.Write(buf[:n])
	}
}

// A shell on a terminal of its own in the wall: it answers what is typed, is a terminal (colors, size), sees no key of
// the person's nor a secret of the runtime's environment, and ends with everything it started.
func TestTerminalIsAShellInTheWall(t *testing.T) {
	box, work, _ := sandboxOrSkip(t)
	t.Setenv("GITLAB_TOKEN", "SECRET-env")
	term, err := box.Shell(work, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	var out bytes.Buffer
	if _, err := term.Pty.Write([]byte("echo $((6*7))-$TERM; stty size; pwd; cat ~/.ssh/id_ed25519 ~/.git-credentials 2>&1; env | grep -c SECRET\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, &out, "42-xterm-256color")
	readUntil(t, term, &out, "30 100")
	readUntil(t, term, &out, work)
	if strings.Contains(out.String(), "SECRET-") {
		t.Fatalf("the terminal saw a secret: %q", out.String())
	}
	if err := term.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Pty.Write([]byte("stty size; (sleep 60 &); exit 5\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, &out, "40 120")
	select {
	case <-term.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the shell did not end")
	}
	if term.Code() != 5 {
		t.Fatalf("code %d", term.Code())
	}
	if _, err := box.Shell(work, 0, 10); err == nil {
		t.Fatal("a terminal of no size started")
	}
}

func TestTerminalSlotsHoldTheLimits(t *testing.T) {
	var l TerminalSlots
	var rel []func()
	for range MaxAppTerminals {
		r, err := l.Take("a", "c1")
		if err != nil {
			t.Fatal(err)
		}
		rel = append(rel, r)
	}
	if _, err := l.Take("a", "c1"); err == nil {
		t.Fatal("an app opened more terminals than it may")
	}
	if _, err := l.Take("a", "c2"); err != nil {
		t.Fatal("another chat has its own")
	}
	rel[0]()
	rel[0]()
	if _, err := l.Take("a", "c1"); err != nil {
		t.Fatal("a closed terminal gives its place back")
	}
	if _, err := l.Take("a", "c1"); err == nil {
		t.Fatal("release counted twice")
	}
}
