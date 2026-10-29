package typist

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const hotelPath = "/home/u/.harnessly/v2/uploads/up_2BF4KAWQ4REIRK36.png"

const hotelText = "Вот смотри, семь отелей здесь непонятно где они здесь. Вот, не видно. У нас, помнишь, был прикол, " +
	"что мы их веером раскрывали и можно было их проскроллить. Типа, видно было, какие, где эмэры.\n\nСлушай, ещё вот я " +
	"выбрал отель, да, нажал."

// The case of 29.10 (tab-as454dx7m): under the rows of our message the box holds the path of its picture Claude read
// late. With the message's end in the box, that is the message itself: only its Enter is pressed. Cut short (the end
// is not there), it is cleared, path and all, and typed whole — never Entered half.
func TestOurTextWithOurLatePathIsEnteredOrTypedWhole(t *testing.T) {
	m := Message{Text: hotelText, Images: []string{hotelPath}}
	whole := newPane(withInput(t, "Типа, видно было, какие, где эмэры.\n\nСлушай, ещё вот я выбрал отель, да, нажал. "+hotelPath))
	leaves(t, whole)
	if plan := Decide(m, Read(whole.screen, claude), claude); plan.Decision != DecideSend || !plan.OwnText {
		t.Fatalf("%+v", plan)
	}
	if res := Type(m, claude, whole, newClock()); res.Outcome != Sent || !reflect.DeepEqual(whole.sent(), []string{"keys:Enter"}) {
		t.Fatalf("%s %q", res.Outcome, whole.sent())
	}

	rows := []string{"и можно было их проскроллить. Типа, видно было, какие, где эмэры.", "", hotelPath}
	cut := newPane(withInput(t, strings.Join(rows, "\n")))
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	four := tui(t, "claude-2.1.310-four-pictures-in-box.ansi.txt")
	cut.onSend = func(call string) {
		switch {
		case call == "keys:C-u" && len(rows) > 1:
			rows = rows[:len(rows)-1]
			cut.set(withInput(t, strings.Join(rows, "\n")))
		case call == "keys:C-u", call == "keys:Enter":
			cut.set(idle)
		case strings.HasPrefix(call, "paste:/"):
			cut.set(four)
		}
	}
	res := Type(m, claude, cut, newClock())
	want := []string{"keys:C-u", "keys:C-u", "keys:C-u", "paste:" + hotelPath, "paste: " + hotelText, "keys:Enter"}
	if res.Outcome != Sent || !reflect.DeepEqual(cut.sent(), want) {
		t.Fatalf("%s %q", res.Outcome, cut.sent())
	}
}

// What is left of an earlier message of ours (and its path) blocks the next one no more: it is cleared, then the
// message goes. A person's draft is still theirs.
func TestWhatIsLeftOfAnEarlierMessageIsCleared(t *testing.T) {
	rows := []string{"и можно было их проскроллить. Типа, видно было, какие, где эмэры.", "", hotelPath}
	p := newPane(withInput(t, strings.Join(rows, "\n")))
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p.onSend = func(call string) {
		switch {
		case call == "keys:C-u" && len(rows) > 1:
			rows = rows[:len(rows)-1]
			p.set(withInput(t, strings.Join(rows, "\n")))
		case call == "keys:C-u":
			p.set(idle)
		case call == "line:следующее", call == "keys:Enter":
			p.set(idle)
		}
	}
	m := Message{Text: "следующее", Typed: []string{hotelText}, Leftovers: []string{hotelPath}}
	res := Type(m, claude, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"keys:C-u", "keys:C-u", "keys:C-u", "line:следующее"}) {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}
	draft := newPane(withInput(t, "мой черновик"))
	if res := Type(m, claude, draft, newClock()); res.Outcome != BlockedText || len(draft.sent()) != 0 {
		t.Fatalf("%s %q", res.Outcome, draft.sent())
	}
}

// «Очистить поле и отправить»: the person's word wipes someone's text, then the message goes; without it, nothing is
// pressed.
func TestWipeClearsSomeonesTextOnTheWord(t *testing.T) {
	p := newPane(withInput(t, "чужой текст\nещё строка"))
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	rows := 2
	p.onSend = func(call string) {
		switch call {
		case "keys:C-u":
			rows--
			if rows == 1 {
				p.set(withInput(t, "чужой текст"))
			} else {
				p.set(idle)
			}
		case "line:привет", "keys:Enter":
			p.set(idle)
		}
	}
	res := Type(Message{Text: "привет", Wipe: true}, claude, p, newClock())
	if res.Outcome != Sent || !res.Wiped || !reflect.DeepEqual(p.sent(), []string{"keys:C-u", "keys:C-u", "line:привет"}) {
		t.Fatalf("%+v %q", res.Outcome, p.sent())
	}
}

// The case of 29.10 («Дизайн сайта hiddenjem»): text and six pictures; Claude read the paste of their paths so long that
// the wait gave up, the paths went in again and the Enter took the box half read — a message of «…/up_FQ5I….pn» alone.
// Now no Enter goes while Claude reads a paste, nor while the box stops short of the message's end.
func TestNoEnterWhileThePasteIsReadOrCutShort(t *testing.T) {
	pasting := tui(t, "claude-2.1.310-pasting-pictures.ansi.txt")
	var imgs []string
	for _, id := range []string{"FQ5I5U64QSREFY7F", "7U2GTAMEOMWOHD3N", "VS5I6ZJ7WL7IDGZZ", "3U253NUDBIPYO65V", "A", "B"} {
		imgs = append(imgs, "/home/u/.harnessly/v2/uploads/up_"+id+".png")
	}
	m := Message{Text: "посмотри дизайн сайта", Images: imgs}

	// Claude reads the paste for good: nothing more is pasted and no Enter is pressed.
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		if strings.HasPrefix(call, "paste:") {
			p.set(pasting)
		}
	}
	res := Type(m, claude, p, newClock())
	if res.Outcome != Failed || len(p.sent()) != 1 || !strings.HasPrefix(p.sent()[0], "paste:") {
		t.Fatalf("%s %q", res.Outcome, p.sent())
	}

	// The box shows the paths cut short («….pn»), no references: the text goes in, no Enter after it.
	cut := strings.Join(imgs[:1], "")
	cut = cut[:len(cut)-1]
	p = newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	p.onSend = func(call string) {
		if strings.HasPrefix(call, "paste:/") {
			p.set(withInput(t, cut))
		}
	}
	clock := newClock()
	res = Type(m, claude, p, clock)
	for _, call := range p.sent() {
		if call == "keys:Enter" {
			t.Fatalf("an Enter on a box cut short: %q", p.sent())
		}
	}
	if res.Outcome != Failed || clock.slept > 2*time.Minute {
		t.Fatalf("%s %q %v", res.Outcome, p.sent(), clock.slept)
	}
}

func TestWithoutPathsTakesOurPathsOut(t *testing.T) {
	s := Read(withInput(t, "текст\n"+EscapePathForPrompt("/u/a b.png")+" /u/c.png"), claude)
	got := withoutPaths(s, []string{"/u/a b.png", "/u/c.png"})
	if strings.TrimSpace(got.Box) != "текст" || got.Input != "текст" {
		t.Fatalf("%q %q", got.Box, got.Input)
	}
	if only := withoutPaths(Read(withInput(t, "/u/c.png"), claude), []string{"/u/c.png"}); only.Input != "" {
		t.Fatalf("%+v", only)
	}
}
