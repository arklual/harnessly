package main

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

type noPanes struct{}

func (noPanes) ReadAllPanes() (map[string]tmux.PaneInfo, bool) {
	return map[string]tmux.PaneInfo{}, true
}
func (noPanes) HasSession(string) bool { return false }

// What the looks keep of a session goes when the conversation says nobody looks at it any more.
func TestForgetSessionDropsWhatTheLooksKept(t *testing.T) {
	m := &v2Tmux{probe: noPanes{}}
	for _, s := range []string{"s1", "s2"} {
		m.Probe(s)
	}
	m.tall.last = map[string]tallCapture{"s1": {}, "s2": {}}
	if len(m.doubt) != 2 {
		t.Fatalf("doubt %d", len(m.doubt))
	}
	m.ForgetScreen("s1")
	if _, kept := m.tall.last["s1"]; kept || len(m.doubt) != 2 {
		t.Fatal("the tall capture alone goes with the terminal")
	}
	m.ForgetSession("s1")
	m.ForgetSession("s2")
	if len(m.doubt) != 0 || len(m.tall.last) != 0 {
		t.Fatalf("left: doubt %d, tall %d", len(m.doubt), len(m.tall.last))
	}
	m.ForgetSession("never-seen")
}
