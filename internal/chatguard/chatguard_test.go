package chatguard

import (
	"errors"
	"testing"
)

func TestBashMode(t *testing.T) {
	for text, want := range map[string]bool{
		"!ls":                 true,
		"! rm -rf /":          true,
		"  \n!ls":             true,
		"\u200b!ls":           true, // a zero-width space before it
		" !ls":                true,
		"!":                   true,
		"hello !ls":           false,
		"what is !important?": false,
		"/model opus":         false,
		"":                    false,
		"！ls":                 false, // a full-width mark is no bash mode
		"line one\n!line two": false,
	} {
		if got := BashMode(text); got != want {
			t.Errorf("BashMode(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestGuardDefaults(t *testing.T) {
	var nilGuard *Guard
	if nilGuard.Off() || nilGuard.Func() != nil {
		t.Error("a nil guard is on")
	}
	nilGuard.Set(true) // must not panic
	if (&Guard{}).Off() {
		t.Error("the zero guard is on")
	}
	g := New(true)
	if !g.Off() || !g.Func()() {
		t.Error("New(true) is off")
	}
	g.Set(false)
	if g.Off() {
		t.Error("Set(false)")
	}
}

type writes struct{ paste, line, keys int }

func (w *writes) CaptureStyled() (string, bool) { return "", true }
func (w *writes) Capture() (string, bool)       { return "", true }
func (w *writes) Paste(string) error            { w.paste++; return nil }
func (w *writes) TypeLine(string) error         { w.line++; return nil }
func (w *writes) Keys(...string) error          { w.keys++; return nil }

func TestGuardedPaneRefusesShellsWhileTheTerminalIsOff(t *testing.T) {
	w := &writes{}
	g := New(false)
	foreground := "bash"
	p := Guarded(w, g, func() string { return foreground })

	// On: it is the pane, whatever runs in it.
	if err := p.Paste("x"); err != nil || w.paste != 1 {
		t.Fatalf("terminal on: %v %d", err, w.paste)
	}

	g.Set(true)
	for _, command := range []string{"bash", "zsh", "fish", "sh", "dash", ""} {
		foreground = command
		if err := p.Paste("x"); !errors.Is(err, ErrTerminalOff) {
			t.Errorf("paste into %q: %v", command, err)
		}
		if err := p.TypeLine("x"); !errors.Is(err, ErrTerminalOff) {
			t.Errorf("line into %q: %v", command, err)
		}
		if err := p.Keys("Enter"); !errors.Is(err, ErrTerminalOff) {
			t.Errorf("keys into %q: %v", command, err)
		}
	}
	if w.paste != 1 || w.line != 0 || w.keys != 0 {
		t.Errorf("a refused write reached the pane: %+v", *w)
	}

	foreground = "claude"
	if p.Paste("x") != nil || p.TypeLine("x") != nil || p.Keys("Enter") != nil || w.paste != 2 || w.line != 1 || w.keys != 1 {
		t.Errorf("writes into an agent: %+v", *w)
	}
	// Reading a pane is never refused.
	foreground = "bash"
	if _, ok := p.Capture(); !ok {
		t.Error("capture")
	}
	if Guarded(nil, g, nil) != nil || Guarded(w, nil, nil) != any(w) {
		t.Error("nothing to guard")
	}
}
