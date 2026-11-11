package typist

import (
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/wire"
)

var versionedScreen = regexp.MustCompile(`^(claude|codex)-(\d+\.\d+\.\d+)-`)

// Every versioned reference screen belongs to a row of the table of checked versions, and every version a row names
// has screens: a capture of a new version is not a checked version until the table says so, and the table cannot
// promise a version nobody captured.
func TestTheCheckedVersionsAreTheReferenceScreens(t *testing.T) {
	entries, err := os.ReadDir("testdata/tui")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string][]string{}
	for _, e := range entries {
		if m := versionedScreen.FindStringSubmatch(e.Name()); m != nil && !slices.Contains(have[m[1]], m[2]) {
			have[m[1]] = append(have[m[1]], m[2])
		}
	}
	named := map[string][]string{}
	for _, r := range cliversion.Table() {
		named[r.Provider] = append(named[r.Provider], r.Screens...)
	}
	for provider, versions := range have {
		for _, v := range versions {
			if !slices.Contains(named[provider], v) {
				t.Errorf("testdata/tui has screens of %s %s, and no row of cliversion's table names them", provider, v)
			}
		}
	}
	for provider, versions := range named {
		sort.Strings(versions)
		for _, v := range versions {
			if !slices.Contains(have[provider], v) {
				t.Errorf("cliversion's table names screens of %s %s, and testdata/tui has none", provider, v)
			}
		}
	}
}

func TestTheReaderKnowsItsReferenceScreens(t *testing.T) {
	if bad := CheckGoldens(); bad != nil {
		t.Fatalf("the reader misreads its reference screens:\n%s", strings.Join(bad, "\n"))
	}
	// Each one is of a checked version.
	for _, g := range Goldens() {
		if cliversion.Check(string(g.Provider), g.Version) != cliversion.Checked {
			t.Errorf("%s %s is a reference screen of an unchecked version", g.Provider, g.Version)
		}
	}
}

func TestAReaderThatLostTheScreensIsCaught(t *testing.T) {
	blind := func(string, wire.Provider) Screen { return Screen{} }
	if bad := checkGoldens(blind); len(bad) != len(Goldens()) {
		t.Fatalf("a reader that sees nothing failed %d of %d screens", len(bad), len(Goldens()))
	}
}

func cautious(t *testing.T) {
	t.Helper()
	cliversion.Reset()
	t.Cleanup(cliversion.Reset)
	if cliversion.Note(cliversion.Claude, "99.0.0") != cliversion.Unchecked {
		t.Fatal("99.0.0 is a checked version")
	}
}

func TestACheckedVersionTypesAsBefore(t *testing.T) {
	cliversion.Reset()
	t.Cleanup(cliversion.Reset)
	cliversion.Note(cliversion.Claude, cliversion.Table()[0].To)
	question := tui(t, "claude-ask-preview-question.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(question)
	p.onSend = func(call string) {
		switch call {
		case "keys:Down":
			p.set(strings.Replace(question, "  "+chatLabelLine(t, question), "❯ "+chatLabelLine(t, question), 1))
		case "keys:Enter":
			p.set(idle)
		}
	}
	if res := TypeChecked(Message{Text: "neither, do it this way"}, claude, p, newClock()); !res.ChatAbout || res.Outcome != Sent {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

func TestAnUncheckedVersionDoesNotCloseAGuessedQuestion(t *testing.T) {
	cautious(t)
	p := newPane(tui(t, "claude-ask-preview-question.txt"))
	res := TypeChecked(Message{Text: "neither, do it this way"}, claude, p, newClock())
	if res.Outcome != BlockedMenu || res.ChatAbout || res.Typed {
		t.Fatalf("%+v", res)
	}
	if got := p.sent(); len(got) != 0 {
		t.Fatalf("keys went into a screen read on an unchecked version: %v", got)
	}
}

func TestAnUncheckedVersionDoesNotEscapeAGuessedAside(t *testing.T) {
	cautious(t)
	p := newPane(tui(t, "claude-2.1.312-login-method.ansi.txt"))
	res := TypeChecked(Message{Text: "hello"}, claude, p, newClock())
	if res.Outcome != BlockedMenu || len(p.sent()) != 0 {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

// The emptied box is not proof that Claude queued the text: the hand-over chord is not pressed on it.
func TestAnUncheckedVersionDoesNotHandOverOnAnEmptiedBox(t *testing.T) {
	cautious(t)
	queued := tui(t, "claude-2.1.296-queued-bash.ansi.txt")
	p := newPane(tui(t, "claude-busy-spinner.ansi.txt"))
	p.onSend = func(call string) {
		if strings.HasPrefix(call, "line:") {
			p.set(queued)
		}
	}
	res := TypeChecked(Message{Text: "also check the logs", Steer: true, HandOver: true}, claude, p, newClock())
	if res.Outcome != Sent || res.HandedOver || slices.Contains(p.sent(), "keys:C-x C-s") {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

func TestAnUncheckedVersionStillTypesIntoAnIdleBox(t *testing.T) {
	cautious(t)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(idle)
	p.onSend = func(call string) {
		if call == "keys:Enter" {
			p.set(idle)
		}
	}
	if res := TypeChecked(Message{Text: "hello"}, claude, p, newClock()); res.Outcome != Sent || !res.Typed {
		t.Fatalf("%+v %v", res, p.sent())
	}
}
