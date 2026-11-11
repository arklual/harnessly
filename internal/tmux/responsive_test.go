package tmux

import (
	"errors"
	"testing"
)

// A server that does not answer in time is slow, and keys are not typed into it; one that answers — even to say the
// session is gone — is not.
func TestResponsiveTellsASlowServer(t *testing.T) {
	f := newFake()
	f.servers["main"] = [][2]string{{"nw-a", "%1"}}
	tm := f.tmux(Config{Names: fakeNames})
	if err := tm.Responsive("nw-a"); err != nil {
		t.Fatalf("a server that answers: %v", err)
	}
	if err := tm.Responsive("nw-gone"); err != nil {
		t.Fatalf("a session that is gone is not a slow server: %v", err)
	}
	f.silent["main"] = true
	var slow *SlowError
	if err := tm.Responsive("nw-a"); !errors.As(err, &slow) || slow.Command != "display-message" || slow.After < ProbeTimeout {
		t.Fatalf("a server that does not answer: %v", err)
	}
}
