package prompts

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/screen"
)

// world is a pane and a transcript on the fake clock; a key may change both.
type world struct {
	clock      *fakeClock
	screen     func(now time.Duration) (string, bool)
	transcript func(now time.Duration) ([]string, bool)
	onKey      func(key string, w *world)
	keys       []string
	start      time.Time
}

func (w *world) elapsed() time.Duration           { return w.clock.Now().Sub(w.start) }
func (w *world) Capture() (string, bool)          { return w.screen(w.elapsed()) }
func (w *world) ReadTranscript() ([]string, bool) { return w.transcript(w.elapsed()) }
func (w *world) SendKeys(keys ...string) error {
	for _, k := range keys {
		w.keys = append(w.keys, k)
		if w.onKey != nil {
			w.onKey(k, w)
		}
	}
	return nil
}

func newWorld(screenText string, lines []string, onKey func(string, *world)) *world {
	c := newFakeClock()
	w := &world{clock: c, start: c.Now(), onKey: onKey}
	w.screen = func(time.Duration) (string, bool) { return screenText, screenText != "" }
	w.transcript = func(time.Duration) ([]string, bool) { return lines, lines != nil }
	return w
}

// takesQueueOnChord: Claude takes the queue when the chord is complete; the screen and the transcript move on.
func takesQueueOnChord(after string, done []string) func(string, *world) {
	return func(key string, w *world) {
		if key != "C-s" {
			return
		}
		w.screen = func(time.Duration) (string, bool) { return after, true }
		w.transcript = func(time.Duration) ([]string, bool) { return done, true }
	}
}

// permissionFrame is a permission prompt shaped like Claude's, a message queued above it.
var permissionFrame = strings.Join([]string{
	"❯ " + bashText,
	"  ctrl+x ctrl+s to send now",
	"",
	" Bash command",
	"",
	"   rm -rf build",
	"   Remove the build directory",
	"",
	" Do you want to proceed?",
	" ❯ 1. Yes",
	"   2. Yes, and don't ask again for rm commands in /work",
	"   3. No",
	"",
	" Esc to cancel · Tab to amend · ctrl+e to explain",
}, "\n")

var (
	sentNow      = SendNowResult{OK: true, Outcome: "sent"}
	deliveredNow = SendNowResult{OK: true, Outcome: "delivered"}
)

func frameOf(over func(*screen.SendNowFrame)) screen.SendNowFrame {
	f := screen.SendNowFrame{InputLine: true, Queued: true}
	if over != nil {
		over(&f)
	}
	return f
}

func waiting(over func(*Probe)) Probe {
	p := Probe{Running: []string{"Bash"}, Waiting: true}
	if over != nil {
		over(&p)
	}
	return p
}

func TestDecideSendNow(t *testing.T) {
	cases := []struct {
		name    string
		frame   screen.SendNowFrame
		probe   Probe
		steered bool
		want    SendNowStep
	}{
		{"bash", frameOf(nil), waiting(nil), false, StepPress},
		{"bash and mcp", frameOf(nil), waiting(func(p *Probe) { p.Running = []string{"Bash", "mcp__slow__slow_wait"} }), false, StepPress},
		{"out of the queue", frameOf(func(f *screen.SendNowFrame) { f.Queued = false }), waiting(func(p *Probe) { p.Waiting = false }), false, StepDelivered},
		{"out of the queue, menu", frameOf(func(f *screen.SendNowFrame) { f.Menu = true }), waiting(func(p *Probe) { p.Waiting = false }), false, StepDelivered},
		{"writing", frameOf(nil), waiting(func(p *Probe) { p.Running = nil }), false, StepWriting},
		{"unknown tool", frameOf(nil), waiting(func(p *Probe) { p.Running = []string{"WebFetch"} }), false, StepTool},
		{"unknown tool next to bash", frameOf(nil), waiting(func(p *Probe) { p.Running = []string{"Bash", "WebFetch"} }), false, StepTool},
		{"menu before draft", frameOf(func(f *screen.SendNowFrame) { f.Menu = true; f.Input = "x" }), waiting(nil), false, StepMenu},
		{"draft", frameOf(func(f *screen.SendNowFrame) { f.Input = "git status" }), waiting(nil), false, StepConflict},
		{"queue not on screen", frameOf(func(f *screen.SendNowFrame) { f.Queued = false }), waiting(nil), false, StepUnseen},
		{"steered, not on screen", frameOf(func(f *screen.SendNowFrame) { f.Queued = false }), waiting(func(p *Probe) { p.Waiting = false }), true, StepPress},
		{"steered, delivered", frameOf(func(f *screen.SendNowFrame) { f.Queued = false }), waiting(func(p *Probe) { p.Waiting = false; p.Delivered = true }), true, StepDelivered},
		{"steered, writing", frameOf(func(f *screen.SendNowFrame) { f.Queued = false }), waiting(func(p *Probe) { p.Waiting = false; p.Running = nil }), true, StepWriting},
	}
	for _, c := range cases {
		if got := DecideSendNow(c.frame, c.probe, c.steered); got != c.want {
			t.Errorf("%s: %s", c.name, got)
		}
	}
}

func TestSendQueuedNow(t *testing.T) {
	bash := transcript(t, "claude-2.1.296-send-now-bash.jsonl")
	stream := transcript(t, "claude-2.1.296-send-now-stream.jsonl")
	two := transcript(t, "claude-2.1.296-send-now-two-queued.jsonl")
	mcp := transcript(t, "claude-2.1.296-send-now-mcp.jsonl")
	hook := transcript(t, "claude-2.1.296-send-now-hook.jsonl")
	bashQueued := tui(t, "claude-2.1.296-queued-bash.ansi.txt")
	bashPending := tui(t, "claude-2.1.296-queued-bash-chord-pending.ansi.txt")
	bashAfter := tui(t, "claude-2.1.296-queued-bash-after-send-now.ansi.txt")
	chord := []string{"C-x", "C-s"}

	run := func(w *world, text string) SendNowResult { return SendQueuedNow(text, w, w.clock, 0) }
	expect := func(name string, w *world, got, want SendNowResult, keys []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
		if keys == nil {
			keys = []string{}
		}
		if w.keys == nil {
			w.keys = []string{}
		}
		if !reflect.DeepEqual(w.keys, keys) {
			t.Errorf("%s: keys %v", name, w.keys)
		}
	}

	w := newWorld(bashQueued, bash[:3], takesQueueOnChord(bashAfter, bash[:5]))
	expect("bash", w, run(w, bashText), sentNow, chord)

	var pressedAt time.Duration = -1
	w = newWorld("", bash[:3], nil)
	w.screen = func(now time.Duration) (string, bool) {
		switch {
		case pressedAt < 0:
			return bashQueued, true
		case now-pressedAt < time.Second:
			return bashPending, true
		}
		return bashAfter, true
	}
	w.onKey = func(key string, w *world) {
		if key == "C-s" {
			pressedAt = w.elapsed()
		}
	}
	expect("queue stays a second", w, run(w, bashText), sentNow, chord)

	w = newWorld(tui(t, "claude-2.1.296-queued-post-tool-hook.ansi.txt"), hook[:3], takesQueueOnChord(bashAfter, hook[:5]))
	expect("post-tool hooks", w, run(w, "hook queued: say Q-HOOK"), sentNow, chord)

	w = newWorld(tui(t, "claude-2.1.296-queued-mcp.ansi.txt"), mcp[:5], takesQueueOnChord(bashAfter, mcp[:6]))
	expect("mcp", w, run(w, "mcp queued: say Q-MCP"), sentNow, chord)

	for _, text := range []string{"first queued: say Q-ONE", "second queued: say Q-TWO"} {
		w = newWorld(tui(t, "claude-2.1.296-queued-two.ansi.txt"), two[:4], takesQueueOnChord(tui(t, "claude-2.1.296-queued-two-after-send-now.ansi.txt"), two[:7]))
		expect("two "+text, w, run(w, text), sentNow, chord)
	}

	w = newWorld(tui(t, "claude-2.1.296-queued-stream.ansi.txt"), stream[:2], nil)
	expect("writing", w, run(w, "Then also say QUEUED-MSG-3 right after STORY-END"), SendNowResult{Status: 409, Message: msgWriting, Waits: true}, nil)

	webFetch := []string{bash[0], call("toolu_x", "WebFetch"), bash[2]}
	w = newWorld(bashQueued, webFetch, nil)
	got := run(w, bashText)
	if got.OK || got.Status != 409 || !got.Waits || !strings.Contains(got.Message, "«WebFetch»") || len(w.keys) != 0 {
		t.Errorf("tool: %+v %v", got, w.keys)
	}

	w = newWorld(bashAfter, bash[:5], nil)
	expect("already delivered", w, run(w, bashText), deliveredNow, nil)

	reads := 0
	w = newWorld("", nil, nil)
	w.screen = func(time.Duration) (string, bool) {
		if reads > 1 {
			return bashAfter, true
		}
		return bashQueued, true
	}
	w.transcript = func(time.Duration) ([]string, bool) {
		reads++
		if reads > 1 {
			return bash[:5], true
		}
		return bash[:3], true
	}
	expect("tool ended between the reads", w, run(w, bashText), deliveredNow, nil)

	w = newWorld(tui(t, "claude-2.1.296-queued-draft.ansi.txt"), bash[:3], nil)
	expect("draft", w, run(w, bashText), SendNowResult{Status: 409, Message: msgConflict}, nil)

	w = newWorld(permissionFrame, bash[:3], nil)
	expect("permission prompt", w, run(w, bashText), SendNowResult{Status: 409, Message: msgMenu}, nil)

	w = newWorld(bashAfter, bash[:3], nil)
	expect("never on screen", w, run(w, bashText), SendNowResult{Status: 409, Message: msgUnseen}, nil)
	if w.elapsed() < 1500*time.Millisecond {
		t.Errorf("gave up after %v", w.elapsed())
	}

	w = newWorld("", bash[:3], takesQueueOnChord(bashAfter, bash[:5]))
	w.screen = func(now time.Duration) (string, bool) {
		if now < 300*time.Millisecond {
			return bashAfter, true
		}
		return bashQueued, true
	}
	expect("shows up a moment later", w, run(w, bashText), sentNow, chord)

	w = newWorld(bashQueued, bash[:3], nil)
	expect("queue stays after the chord", w, run(w, bashText), SendNowResult{Status: 500, Message: msgNotAccepted}, chord)
	if e := w.elapsed(); e < 5*time.Second || e >= 5500*time.Millisecond {
		t.Errorf("gave up after %v", e)
	}

	w = newWorld(bashQueued, bash[:3], func(key string, w *world) {
		if key == "C-s" {
			w.screen = func(time.Duration) (string, bool) { return permissionFrame, true }
		}
	})
	expect("menu after the chord", w, run(w, bashText), SendNowResult{Status: 409, Message: msgMenu}, chord)

	w = newWorld("", bash[:3], nil)
	expect("no screen", w, run(w, bashText), SendNowResult{Status: 500, Message: msgNoScreen}, nil)
	w = newWorld(bashQueued, nil, nil)
	expect("no transcript", w, run(w, bashText), SendNowResult{Status: 500, Message: msgNoTranscript}, nil)

	for i, w := range []*world{
		newWorld(bashQueued, bash[:3], nil),
		newWorld(tui(t, "claude-2.1.296-queued-stream.ansi.txt"), stream[:2], nil),
		newWorld(tui(t, "claude-2.1.296-queued-draft.ansi.txt"), bash[:3], nil),
		newWorld(permissionFrame, bash[:3], nil),
		newWorld(bashAfter, bash[:3], nil),
		newWorld("", nil, nil),
	} {
		run(w, bashText)
		for _, k := range w.keys {
			if k != "C-x" && k != "C-s" {
				t.Errorf("world %d pressed %s", i, k)
			}
		}
	}
}

func TestSendNowOnePerTab(t *testing.T) {
	bash := transcript(t, "claude-2.1.296-send-now-bash.jsonl")
	r := newRig(t, tui(t, "claude-2.1.296-queued-bash.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return bash[:3], true }
	r.panes.onSend = func(call string) {
		if call == "seq:C-x C-s" {
			r.panes.setScreen(tui(t, "claude-2.1.296-queued-bash-after-send-now.ansi.txt"))
		}
	}
	// A second send for the tab while one runs is refused; another tab runs.
	release, entered := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.svc.flights.run("tab-1", func() { close(entered); <-release })
	}()
	<-entered
	if got, found := r.svc.SendNow("tab-1", bashText); !found || !reflect.DeepEqual(got, SendNowResult{Status: 409, Message: msgInFlight}) {
		t.Fatalf("second send: %+v", got)
	}
	close(release)
	wg.Wait()

	if got, _ := r.svc.SendNow("tab-1", bashText); !reflect.DeepEqual(got, sentNow) {
		t.Fatalf("send: %+v", got)
	}
	// The chord types nothing: a second press for the same text within 30 s is delivered without keys.
	before := len(r.panes.sent())
	if got, _ := r.svc.SendNow("tab-1", bashText); !reflect.DeepEqual(got, deliveredNow) || len(r.panes.sent()) != before {
		t.Fatalf("second press: %+v", got)
	}
	if _, found := r.svc.SendNow("tab-x", bashText); found {
		t.Fatal("an unknown tab was found")
	}
}

func TestSendNowFailureFreesItsID(t *testing.T) {
	r := newRig(t, tui(t, "claude-2.1.296-queued-stream.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	stream := transcript(t, "claude-2.1.296-send-now-stream.jsonl")
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return stream[:2], true }
	text := "Then also say QUEUED-MSG-3 right after STORY-END"
	for i := 0; i < 2; i++ {
		if got, _ := r.svc.SendNow("tab-1", text); got.Status != 409 || !got.Waits {
			t.Fatalf("try %d: %+v", i, got)
		}
	}
}
