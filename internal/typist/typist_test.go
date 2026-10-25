package typist

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const claude, codex = wire.ProviderClaude, wire.ProviderCodex

// leaves makes the box empty again once Enter (or a typed line) goes in.
func leaves(t *testing.T, p *fakePane) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p.onSend = func(call string) {
		if call == "keys:Enter" || strings.HasPrefix(call, "line:") {
			p.set(idle)
		}
	}
}

func TestTheScreenDecides(t *testing.T) {
	cases := []struct {
		name, screen string
		steer        bool
		want         Outcome
	}{
		{"idle", "claude-idle-placeholder.ansi.txt", false, Sent},
		{"busy", "claude-busy-spinner.ansi.txt", false, Wait},
		{"busy, steered", "claude-busy-spinner.ansi.txt", true, Sent},
		{"someone's draft", "claude-input-draft.ansi.txt", false, BlockedText},
		{"someone's draft, steered", "claude-input-draft.ansi.txt", true, BlockedText},
		{"a model picker", "claude-switch-model-confirm.txt", true, BlockedMenu},
	}
	for _, c := range cases {
		p := newPane(tui(t, c.screen))
		leaves(t, p)
		res := Type(Message{Text: "hello", Steer: c.steer}, claude, p, newClock())
		if res.Outcome != c.want {
			t.Errorf("%s: %s", c.name, res.Outcome)
		}
		if c.want != Sent && len(p.sent()) != 0 {
			t.Errorf("%s: typed %v", c.name, p.sent())
		}
	}
}

func TestAPermissionMenuNeverTakesKeys(t *testing.T) {
	p := newPane(permissionScreen)
	for _, steer := range []bool{false, true} {
		res := Type(Message{Text: "yes go on", Steer: steer}, claude, p, newClock())
		if res.Outcome != BlockedMenu || res.Before.Menu != MenuPermission || len(p.sent()) != 0 {
			t.Fatalf("%+v %v", res.Before.Menu, p.sent())
		}
	}
	if s := Read(permissionScreen, claude); len(s.Options) != 3 || s.PromptReady() {
		t.Fatalf("%+v", s)
	}
}

func TestAnUnreadableScreenWaits(t *testing.T) {
	p := newPane("")
	p.readable = false
	if res := Type(Message{Text: "x"}, claude, p, newClock()); res.Outcome != Wait || len(p.sent()) != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestAShortLineIsTypedWithItsEnter(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	leaves(t, p)
	res := Type(Message{Text: "hello there"}, claude, p, newClock())
	if res.Outcome != Sent || !res.Typed || !reflect.DeepEqual(p.sent(), []string{"line:hello there"}) {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

func TestALongTextIsPastedThenEntered(t *testing.T) {
	long := "first line\n" + strings.Repeat("y", 900)
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	leaves(t, p)
	res := Type(Message{Text: long}, claude, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"paste:" + long, "keys:Enter"}) {
		t.Fatalf("%+v %v", res, p.sent())
	}
	for _, s := range []string{"/compact", "!ls", "see @src/", "#note"} {
		if SubmitsAsTypedLine(s) {
			t.Errorf("%q typed as a line", s)
		}
	}
}

// A lost Enter: the text stays in the box, Enter is pressed again 700 ms after the last one, three Enters in all; then
// stuck. The retry finds the text as its own and presses only Enter.
func TestALostEnterIsPressedAgainThenTheRetryEntersOnly(t *testing.T) {
	typed := withInput(t, "hello there")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(idle)
	p.onSend = func(call string) {
		if call == "keys:C-u" {
			p.set(idle)
			return
		}
		p.set(typed)
	}
	clock := newClock()
	res := Type(Message{Text: "hello there"}, claude, p, clock)
	// Stuck after three Enters: the text is cleared out of the box (ctrl+u, never ctrl+c), not left for the next one.
	if res.Outcome != Stuck || !reflect.DeepEqual(p.sent(), []string{"line:hello there", "keys:Enter", "keys:Enter", "keys:C-u"}) {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
	if clock.slept > confirmLimit+confirmPoll+clearStep {
		t.Fatalf("waited %v", clock.slept)
	}
	// A box found holding the message (a restart, an Enter that did not take): only its Enter is pressed.
	p.set(typed)
	p.calls = nil
	leaves(t, p)
	res = Type(Message{Text: "hello there"}, claude, p, newClock())
	if res.Outcome != Sent || !res.Own || !reflect.DeepEqual(p.sent(), []string{"keys:Enter"}) {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

// The retry reads the whole box: a message the box wrapped onto three lines is still ours, not someone's draft.
func TestTheRetryReadsTheWholeBox(t *testing.T) {
	msg := strings.Repeat("слово ", 30) + "конец"
	wrapped := withInput(t, msg[:60]+"\n"+msg[60:130]+"\n"+msg[130:])
	p := newPane(wrapped)
	leaves(t, p)
	res := Type(Message{Text: msg}, claude, p, newClock())
	if res.Outcome != Sent || !res.Own || !reflect.DeepEqual(p.sent(), []string{"keys:Enter"}) {
		t.Fatalf("%+v %v box=%q", res.Outcome, p.sent(), res.Before.Box)
	}
}

func TestSomeoneElsesTextIsNotOurs(t *testing.T) {
	p := newPane(withInput(t, "a different draft of the person"))
	res := Type(Message{Text: "hello there"}, claude, p, newClock())
	if res.Outcome != BlockedText || res.Own || len(p.sent()) != 0 {
		t.Fatalf("%+v %v", res, p.sent())
	}
}

// The live screen of a tab whose fifth picture reference the box wrapped in the middle: "[Image" ends the first line,
// "#104]" starts the second.
const wrappedRefs = "[Image #100] [Image #101] [Image #102] [Image #103] [Image\n  #104]"

func wrappedRefScreens(t *testing.T) (before, after string) {
	t.Helper()
	after = tui(t, "claude-pictures-wrapped-mid-ref.ansi.txt")
	if !strings.Contains(after, wrappedRefs) {
		t.Fatal("fixture has no wrapped references")
	}
	return strings.Replace(after, wrappedRefs, "", 1), after
}

var fivePictures = []string{"/u/a.png", "/u/b.png", "/u/c.png", "/u/d.png", "/u/e.png"}

func TestAReferenceWrappedMidwayCounts(t *testing.T) {
	before, after := wrappedRefScreens(t)
	plainBefore, plainAfter := strip(before), strip(after)
	if got := CountImageRefs(plainAfter); got != 5 {
		t.Errorf("counted %d", got)
	}
	if got := NewImageRefs(plainBefore, plainAfter); got != 5 {
		t.Errorf("new %d", got)
	}
	// Only references new on the screen count: an older one of the conversation above is not this paste's.
	if got := NewImageRefs("[Image #1] old", "[Image #1] old\n[Image #1]"); got != 1 {
		t.Errorf("a number shown again counted %d", got)
	}
}

// Five pictures go in and the box wraps the last reference: the try confirms them and types the text, it does not
// time out and leave the queue blocked behind its own pictures.
func TestFivePicturesAndALongTextGoInOneTry(t *testing.T) {
	before, after := wrappedRefScreens(t)
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	p := newPane(before)
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/u/"):
			p.set(after)
		case call == "keys:Enter":
			p.set(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	res := Type(Message{Text: long, Images: fivePictures}, claude, p, newClock())
	want := []string{"paste:/u/a.png /u/b.png /u/c.png /u/d.png /u/e.png", "paste: " + long, "keys:Enter"}
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), want) {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}
}

// After a restart, or a try that failed after its pictures, the box holds this message's pictures: they are its own,
// not someone's text, and only the text and its Enter go — no picture twice.
func TestOwnPicturesInTheBoxAreFinishedNotRetyped(t *testing.T) {
	_, after := wrappedRefScreens(t)
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	p := newPane(after)
	p.onSend = func(call string) {
		if call == "keys:Enter" {
			p.set(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	res := Type(Message{Text: long, Images: fivePictures}, claude, p, newClock())
	if res.Outcome != Sent || !res.Own || !reflect.DeepEqual(p.sent(), []string{"paste: " + long, "keys:Enter"}) {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}
}

func TestOnlyTheMissingPicturesArePasted(t *testing.T) {
	two := withInput(t, "[Image #1] [Image #2]")
	all := withInput(t, "[Image #1] [Image #2] [Image #3]")
	p := newPane(two)
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/u/"):
			p.set(all)
		case call == "keys:Enter":
			p.set(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	res := Type(Message{Text: "look", Images: []string{"/u/1.png", "/u/2.png", "/u/3.png"}}, claude, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"paste:/u/3.png", "paste: look", "keys:Enter"}) {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}
}

func TestMorePicturesThanTheMessageHasAreSomeoneElses(t *testing.T) {
	p := newPane(withInput(t, "[Image #1] [Image #2] [Image #3]"))
	res := Type(Message{Text: "look", Images: []string{"/u/1.png"}}, claude, p, newClock())
	if res.Outcome != BlockedText || len(p.sent()) != 0 {
		t.Fatalf("%s %v", res.Outcome, p.sent())
	}
}

// Claude never shows the picture as [Image #1] (a loaded machine, a big file): the message goes with the path as text,
// one Enter for path and text, and counts as sent once that line left the box. A box that lost the path gets it again.
func TestAPictureThatDoesNotShowUpGoesAsItsPath(t *testing.T) {
	for _, c := range []struct {
		name  string
		after string // the box once the path was pasted
		want  []string
	}{
		{"kept as text", "/u/1.png", []string{"paste:/u/1.png", "paste: look", "keys:Enter"}},
		{"lost", "", []string{"paste:/u/1.png", "paste:/u/1.png", "paste: look", "keys:Enter"}},
	} {
		idle := tui(t, "claude-idle-placeholder.ansi.txt")
		p := newPane(idle)
		pasted := 0
		p.onSend = func(call string) {
			switch call {
			case "paste:/u/1.png":
				pasted++
				if pasted == 1 && c.after == "" {
					return
				}
				p.set(withInput(t, "/u/1.png"))
			case "paste: look":
				p.set(withInput(t, "/u/1.png look"))
			case "keys:Enter":
				p.set(idle)
			}
		}
		clock := newClock()
		res := Type(Message{Text: "look", Images: []string{"/u/1.png"}}, claude, p, clock)
		if res.Outcome != Sent || !res.Typed || !res.AsPath {
			t.Fatalf("%s: %+v", c.name, res)
		}
		if clock.slept < imageConfirmBase {
			t.Fatalf("%s: gave the picture %v", c.name, clock.slept)
		}
		if sent := p.sent(); !reflect.DeepEqual(sent, c.want) {
			t.Fatalf("%s: %q", c.name, sent)
		}
	}
}

// The path and text that stay in the box after their Enters are stuck, and taken out of it: the retry types them whole.
func TestAPathAndTextThatStayInTheBoxAreStuck(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(idle)
	p.onSend = func(call string) {
		switch call {
		case "paste:/u/1.png":
			p.set(withInput(t, "/u/1.png"))
		case "paste: look":
			p.set(withInput(t, "/u/1.png look"))
		case "keys:C-u":
			p.set(idle)
		}
	}
	res := Type(Message{Text: "look", Images: []string{"/u/1.png"}}, claude, p, newClock())
	if res.Outcome != Stuck || !res.AsPath {
		t.Fatalf("%+v", res)
	}
	if v := Look(p, claude); !blank(v.Input) {
		t.Fatalf("the box keeps %q", v.Input)
	}
}

// Every key that does not go in fails the try; the caller holds the lock and lets it go by defer, and nothing
// counts as sent.
func TestEveryTmuxFailureFailsTheTry(t *testing.T) {
	cases := []struct{ name, fail, text string }{
		{"typed line", "line:", "hello"},
		{"paste", "paste:", "two\nlines"},
		{"enter", "keys:Enter", "two\nlines"},
	}
	for _, c := range cases {
		p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
		p.fail[c.fail] = errTmux
		unlock := Lock("pane-fail")
		res := func() Result {
			defer unlock()
			return Type(Message{Text: c.text}, claude, p, newClock())
		}()
		if res.Outcome != Failed || res.Err != errTmux {
			t.Errorf("%s: %+v", c.name, res)
		}
		if again, ok := TryLock("pane-fail"); !ok {
			t.Errorf("%s: the lock is held", c.name)
		} else {
			again()
		}
	}
}

func TestATextWrittenIntoAQuestionIsChattedAbout(t *testing.T) {
	question := tui(t, "claude-ask-preview-question.txt")
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(question)
	downs := 0
	p.onSend = func(call string) {
		switch call {
		case "keys:Down":
			downs++
			p.set(strings.Replace(question, "  "+chatLabelLine(t, question), "❯ "+chatLabelLine(t, question), 1))
		case "keys:Enter":
			p.set(idle)
		}
	}
	res := Type(Message{Text: "neither, do it this way"}, claude, p, newClock())
	if !res.ChatAbout || res.Outcome != Sent {
		t.Fatalf("%+v %v", res, p.sent())
	}
	if got := p.sent(); got[len(got)-1] != "line:neither, do it this way" {
		t.Fatalf("%v", got)
	}
}

// chatLabelLine is the «Chat about this» row of a question fixture, without its indent.
func chatLabelLine(t *testing.T, s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "Chat about this") {
			return strings.TrimLeft(l, " ")
		}
	}
	t.Fatal("no Chat about this row")
	return ""
}

func TestCodexIsPastedAndWatched(t *testing.T) {
	p := newPane(tui(t, "codex-idle-placeholder.ansi.txt"))
	idle := tui(t, "codex-idle-placeholder.ansi.txt")
	p.onSend = func(call string) {
		if strings.HasPrefix(call, "paste:") {
			p.set(strings.Replace(idle, "›", "› hello codex", 1))
		}
		if call == "keys:Enter" {
			p.set(idle)
		}
	}
	res := Type(Message{Text: "hello codex"}, codex, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"paste:hello codex", "keys:Enter"}) {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
	if res := Type(Message{Text: "please review the diff"}, codex, newPane(tui(t, "codex-input-draft.ansi.txt")), newClock()); res.Outcome != BlockedText {
		t.Fatalf("codex draft: %s", res.Outcome)
	}
}

// A message steered into a Bash call is handed over by Claude's chord once its queue shows.
func TestASteerIntoBashIsHandedOver(t *testing.T) {
	queued := tui(t, "claude-2.1.296-queued-bash.ansi.txt")
	p := newPane(tui(t, "claude-busy-spinner.ansi.txt"))
	p.onSend = func(call string) {
		if strings.HasPrefix(call, "line:") {
			p.set(queued)
		}
	}
	res := Type(Message{Text: "also check the logs", Steer: true, HandOver: true}, claude, p, newClock())
	if res.Outcome != Sent || !res.HandedOver || p.sent()[len(p.sent())-1] != "keys:C-x C-s" {
		t.Fatalf("%+v %v", res, p.sent())
	}
	p.calls = nil
	res = Type(Message{Text: "and the metrics", Steer: true}, claude, p, newClock())
	if res.HandedOver {
		t.Fatal("handed over without a tool to hand over")
	}
}

// the tool may end while the text is typed; the chord then would cut the model's answer short, so it is
// asked for again right before the keys.
func TestTheChordWaitsForTheToolToStillRun(t *testing.T) {
	queued := tui(t, "claude-2.1.296-queued-bash.ansi.txt")
	run := func(still bool) []string {
		p := newPane(tui(t, "claude-busy-spinner.ansi.txt"))
		p.onSend = func(call string) {
			if strings.HasPrefix(call, "line:") {
				p.set(queued)
			}
		}
		res := Type(Message{Text: "also check the logs", Steer: true, HandOver: true, HandOverOK: func() bool { return still }}, claude, p, newClock())
		if res.Outcome != Sent || res.HandedOver != still {
			t.Fatalf("still=%v: %+v", still, res)
		}
		return p.sent()
	}
	if sent := run(true); sent[len(sent)-1] != "keys:C-x C-s" {
		t.Fatalf("the chord was not pressed: %v", sent)
	}
	if sent := run(false); slices.Contains(sent, "keys:C-x C-s") {
		t.Fatalf("the chord cut a model that writes: %v", sent)
	}
}

func TestTheLockIsOnePerPane(t *testing.T) {
	unlock := Lock("pane-a")
	if _, ok := TryLock("pane-a"); ok {
		t.Fatal("a held lock taken twice")
	}
	other, ok := TryLock("pane-b")
	if !ok {
		t.Fatal("another pane's lock is held")
	}
	other()
	done := make(chan struct{})
	go func() {
		u := Lock("pane-a")
		u()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("the lock did not wait")
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	unlock()
	<-done
}

func strip(s string) string { p := newPane(s); out, _ := p.Capture(); return out }

// Security review: Pick looks at the screen under the lock and presses nothing on a menu other than the
// one decided on.
func TestPickPressesOnlyTheMenuDecidedOn(t *testing.T) {
	want := PickWant{N: 1, Label: "Yes", Command: "rm -rf build"}
	always := PickWant{N: 2, Label: "Yes, and don't ask again for rm commands in /home/coder/app", Command: "rm -rf build"}
	other := strings.NewReplacer("rm -rf build", "git push --force").Replace(permissionScreen)
	otherOptions := strings.NewReplacer("rm commands", "git commands").Replace(permissionScreen)
	cases := []struct {
		name   string
		screen string
		want   PickWant
		keys   bool
	}{
		{"the menu decided on", permissionScreen, want, true},
		{"its second option", permissionScreen, always, true},
		{"another tool call, the same options", other, want, false},
		{"the same call, other options", otherOptions, always, false},
		{"a wrong label", permissionScreen, PickWant{N: 1, Label: "No", Command: "rm -rf build"}, false},
		{"a number the menu lacks", permissionScreen, PickWant{N: 7, Label: "Yes"}, false},
		{"a plan menu for a tool decision", planScreen, want, false},
		{"no menu", tui(t, "claude-idle-placeholder.ansi.txt"), want, false},
	}
	for _, c := range cases {
		p := newPane(c.screen)
		p.onSend = func(call string) { p.set(tui(t, "claude-idle-placeholder.ansi.txt")) }
		err := Pick(p, newClock(), claude, c.want)
		switch {
		case c.keys && err != nil, !c.keys && !errors.Is(err, ErrMenuChanged):
			t.Errorf("%s: %v", c.name, err)
		}
		if got := len(p.sent()) > 0; got != c.keys {
			t.Errorf("%s: keys %v", c.name, p.sent())
		}
	}
}

// A plan's menu is decided as a plan's.
func TestPickOnAPlanMenu(t *testing.T) {
	s := Read(planScreen, claude)
	if s.Menu != MenuPlan || len(s.Options) == 0 {
		t.Fatalf("fixture: %+v", s)
	}
	label := OptionLabel(s.Options[0])
	p := newPane(planScreen)
	p.onSend = func(string) { p.set(tui(t, "claude-idle-placeholder.ansi.txt")) }
	if err := Pick(p, newClock(), claude, PickWant{N: 1, Label: label, Plan: true}); err != nil || len(p.sent()) != 1 {
		t.Fatalf("%v %v", err, p.sent())
	}
	if err := Pick(newPane(planScreen), newClock(), claude, PickWant{N: 1, Label: label}); !errors.Is(err, ErrMenuChanged) {
		t.Fatalf("a plan menu taken for a permission: %v", err)
	}
}

func TestLiveReplyIsTheLastTextBullet(t *testing.T) {
	plain := "● Bash(ls)\n  ⎿ a b\n\n● I looked at the folder and\n  found two files.\n\n✻ Working… (esc to interrupt)\n"
	if got := liveReply(plain); got != "I looked at the folder and found two files." {
		t.Fatalf("the reply %q", got)
	}
	if got := liveReply("● Bash(ls)\n  ⎿ a b\n✻ Working…\n"); got != "" {
		t.Fatalf("a tool call is not a reply: %q", got)
	}
	long := "● " + strings.Repeat("слово ", 60)
	if got := liveReply(long); len([]rune(got)) != liveReplyMax {
		t.Fatalf("a long reply is cut to %d characters, it is %d", liveReplyMax, len([]rune(got)))
	}
}

// Claude 2.1.310 draws a call it asks about as «Read(/path)» while its summary is «Read /path»: the call is taken
// only from the dialog's styled layout with a title known for the tool, whole, and with nothing on the screen that
// could be a second dialog; anything else is left to the person in the terminal (found on the live stand).
func TestPickFindsTheCallInItsDrawnForm(t *testing.T) {
	live := tui(t, "claude-2.1.310-read-outside-dirs.ansi.txt")
	const drawn = "Read(/home/coder/shot.png)"
	const solid = "\x1b[38;5;153m" + "────────────────────────────────────────" + "\n"
	const dashed = "\x1b[38;5;239m" + "╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌" + "\n"
	const title = "\x1b[39m \x1b[1m\x1b[38;5;153mRead outside the working directories\x1b[0m\n"
	with := func(pairs ...string) string { return strings.NewReplacer(pairs...).Replace(live) }
	fake := solid + title + dashed + "\x1b[39m Read(/home/coder/shot.png)\n" + dashed
	label := "Yes, but ask again next time"
	long := "/home/coder/projects/harnessly/web/app/next/features/designly/frame.png"
	cases := []struct {
		name    string
		screen  string
		command string
		keys    bool
	}{
		{"the call drawn whole", live, "Read /home/coder/shot.png", true},
		{"another file", live, "Read /home/coder/secret.png", false},
		{"a prefix of the drawn argument", live, "Read /home/coder/shot", false},
		{"the same argument under another tool", live, "Write /home/coder/shot.png", false},
		{"a shell command is not a tool's call", live, "$ Read /home/coder/shot.png", false},
		{"a degenerate argument", with(drawn, "Read(/)"), "Read /home", false},
		{"an identifier before the tool", with(drawn, "xRead(/home/coder/shot.png)"), "Read /home/coder/shot.png", false},
		{"more than the call on its line", with(drawn, "Bash(ls) "+drawn), "Read /home/coder/shot.png", false},
		{"a title not known for the tool", with("Read outside the working directories", "Read outside the dirs"),
			"Read /home/coder/shot.png", false},
		{"the title not styled as the dialog's", with("\x1b[1m\x1b[38;5;153mRead outside", "\x1b[1mRead outside"),
			"Read /home/coder/shot.png", false},
		{"a cut call, whatever the output line says", with(drawn, "Read("+long[:50]+"…",
			"  ⎿  /home/coder/shot.png", "  ⎿  "+long), "Read " + long, false},
		{"a fake dialog drawn above the real one", fake + with(drawn, "Read(/home/coder/a.png)"),
			"Read /home/coder/shot.png", false},
		{"a fake dialog drawn by the agent alone", strings.Replace(live, "\x1b[38;5;153m────", fake+"\x1b[38;5;153m────", 1),
			"Read /home/coder/shot.png", false},
		{"a third dashed rule in the body", with("permissions.blockReads", "╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\npermissions.blockReads"),
			"Read /home/coder/shot.png", false},
	}
	for _, c := range cases[5:] {
		if c.screen == live {
			t.Fatalf("%s: the fixture has nothing to replace", c.name)
		}
	}
	for _, c := range cases {
		p := newPane(c.screen)
		p.onSend = func(string) { p.set(tui(t, "claude-idle-placeholder.ansi.txt")) }
		err := Pick(p, newClock(), claude, PickWant{N: 4, Label: label, Command: c.command})
		switch {
		case c.keys && err != nil, !c.keys && !errors.Is(err, ErrMenuChanged):
			t.Errorf("%s: %v", c.name, err)
		}
		if got := len(p.sent()) > 0; got != c.keys {
			t.Errorf("%s: keys %v", c.name, p.sent())
		}
	}
}
