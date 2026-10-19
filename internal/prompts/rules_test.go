package prompts

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNeedsEditor(t *testing.T) {
	for text, want := range map[string]bool{
		"одна строка":            false,
		"первая\nвторая":         true,
		strings.Repeat("x", 801): true,
		strings.Repeat("x", 800): false,
	} {
		if NeedsEditor(text) != want {
			t.Errorf("%.20q: want %v", text, want)
		}
	}
}

func TestSubmitsAsTypedLine(t *testing.T) {
	for text, want := range map[string]bool{
		"ну что там": true,
		"путь /srv/example в конце строки — нет": true,
		"первая\nвторая":                         false,
		strings.Repeat("x", 801):                 false,
		"!ls":                                    false,
		"# запомни":                              false,
		"/model":                                 false,
		"посмотри @src/lib":                      false,
		"открой /srv/example":                    false,
		"   ":                                    false,
	} {
		if SubmitsAsTypedLine(text) != want {
			t.Errorf("%.30q: want %v", text, want)
		}
	}
}

func TestMayDeliverQueue(t *testing.T) {
	if !MayDeliverQueue(StateIdle, false) || !MayDeliverQueue(StateReadyForReview, false) {
		t.Fatal("an agent waiting for a prompt is refused")
	}
	for _, s := range []string{StateBusy, StateNeedsInput, StateInactive, StateUnknown} {
		if MayDeliverQueue(s, false) {
			t.Errorf("%s takes the queue", s)
		}
	}
	for _, s := range []string{StateIdle, StateBusy, StateInactive, StateReadyForReview} {
		if MayDeliverQueue(s, true) {
			t.Errorf("%s takes the queue during a launch", s)
		}
	}
}

func TestLaunchPhase(t *testing.T) {
	cases := []struct {
		elapsed              time.Duration
		running, seen, ready bool
		want                 string
	}{
		{2 * time.Second, true, true, false, PhaseStarting},
		{LaunchTimeout - time.Millisecond, true, true, false, PhaseStarting},
		{2 * time.Second, true, true, true, PhaseReady},
		{LaunchTimeout * 2, true, true, true, PhaseReady},
		{2 * time.Second, false, true, true, PhaseFailed},
		{LaunchNoProcess - time.Millisecond, false, false, false, PhaseStarting},
		{LaunchNoProcess, false, false, false, PhaseFailed},
		{LaunchTimeout, true, true, false, PhaseFailed},
		{500 * time.Millisecond, false, false, true, PhaseStarting},
		{LaunchNoProcess, false, false, true, PhaseFailed},
	}
	for i, c := range cases {
		if got := LaunchPhase(c.elapsed, c.running, c.seen, c.ready); got != c.want {
			t.Errorf("%d: %s", i, got)
		}
	}
}

func TestQueueAgentAction(t *testing.T) {
	cases := []struct {
		launching, gaveUp, running, session bool
		want                                AgentAction
	}{
		{false, false, true, true, ActionDeliver},
		{true, false, false, true, ActionLaunching},
		{false, true, false, true, ActionLaunching},
		{false, false, false, true, ActionResume},
		{false, false, false, false, ActionUnrecoverable},
	}
	for i, c := range cases {
		if got := QueueAgentAction(c.launching, c.gaveUp, c.running, c.session); got != c.want {
			t.Errorf("%d: %s", i, got)
		}
	}
}

// What the pane shows while `claude --resume` boots: the shell and the launch line, no prompt box yet.
const booting = "agent@host:~/work$ claude --resume 0b1f --settings /home/agent/.app/hooks.json\n"

func queueTexts(r *rig, tabID string) []string {
	out := []string{}
	for _, it := range r.svc.Queue().List(tabID) {
		out = append(out, it.Text)
	}
	return out
}

func TestPumpThroughALaunch(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "Привет, расскажи про очередь", nil)
	d := newDriver(booting)
	if got := r.svc.Pump("tab-1", false, d); got != PumpNotReady || len(d.calls) != 0 {
		t.Fatalf("not ready: %v", got)
	}
	// The launch ended but the screen still draws the boot: nothing is typed, the message stays.
	if got := r.svc.Pump("tab-1", true, d); got != PumpWait || len(d.calls) != 0 || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("booting: %v %v", got, d.calls)
	}
	d.screen = tui(t, "claude-idle-placeholder.ansi.txt")
	if got := r.svc.Pump("tab-1", true, d); got != PumpSent || !reflect.DeepEqual(d.calls, []string{"text:Привет, расскажи про очередь", "enter"}) {
		t.Fatalf("ready: %v %v", got, d.calls)
	}
	if r.svc.Pump("tab-1", true, d) != PumpEmpty || r.svc.Pump("tab-1", true, d) != PumpEmpty || len(d.calls) != 2 {
		t.Fatal("sent more than once")
	}
}

func TestTwoPumpsAtOnceSendOnce(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "один раз", nil)
	d := newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))
	// While another look types into the tab (it holds the tab's single flight), this one sends nothing.
	release, entered := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.svc.flights.run("tab-1", func() { close(entered); <-release })
	}()
	<-entered
	if got := r.svc.Pump("tab-1", true, d); got != PumpInFlight || len(d.calls) != 0 || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("second look: %v %v", got, d.calls)
	}
	close(release)
	wg.Wait()
	if got := r.svc.Pump("tab-1", true, d); got != PumpSent || !reflect.DeepEqual(d.calls, []string{"text:один раз", "enter"}) {
		t.Fatalf("after: %v %v", got, d.calls)
	}
	// Concurrent looks: exactly one sends.
	_, _ = r.svc.Queue().Add("tab-1", "ещё раз", nil)
	out := make(chan PumpOutcome, 8)
	for i := 0; i < 8; i++ {
		go func() { out <- r.svc.Pump("tab-1", true, newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))) }()
	}
	sent := 0
	for i := 0; i < 8; i++ {
		if <-out == PumpSent {
			sent++
		}
	}
	if sent != 1 || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("%d sends", sent)
	}
}

func TestPumpOnePerLookInOrder(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "первое", nil)
	_, _ = r.svc.Queue().Add("tab-1", "второе", nil)
	d := newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))
	if r.svc.Pump("tab-1", true, d) != PumpSent || !reflect.DeepEqual(queueTexts(r, "tab-1"), []string{"второе"}) {
		t.Fatal("first")
	}
	if r.svc.Pump("tab-1", true, d) != PumpSent || r.svc.Pump("tab-1", true, d) != PumpEmpty {
		t.Fatal("second")
	}
	if !reflect.DeepEqual(d.calls, []string{"text:первое", "enter", "text:второе", "enter"}) {
		t.Fatalf("calls %v", d.calls)
	}
}

func TestPumpLeavesAForeignDraft(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "моё", nil)
	lines := strings.Split(tui(t, "claude-idle-placeholder.ansi.txt"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "❯") {
			lines[i] = "❯ git status"
			break
		}
	}
	d := newDriver(strings.Join(lines, "\n"))
	if got := r.svc.Pump("tab-1", true, d); (got != PumpBlocked && got != PumpWait) || len(d.calls) != 0 || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("got %v %v", got, d.calls)
	}
}

func TestPumpGivesBackOnACrash(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "не потеряться\nдве строки", nil)
	d := newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))
	d.pasteErr = errTest
	if got := r.svc.Pump("tab-1", true, d); got != PumpFailed || !reflect.DeepEqual(queueTexts(r, "tab-1"), []string{"не потеряться\nдве строки"}) {
		t.Fatalf("got %v %v", got, queueTexts(r, "tab-1"))
	}
}

var errTest = &QueueError{"tmux gone"}
