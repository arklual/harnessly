package prompts

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestScheduleDeliversAfterTheSettle(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "после хода", nil)
	r.svc.TabSettled("tab-1")
	r.clock.Advance(settleDelay - time.Millisecond)
	if len(r.panes.sent()) != 0 {
		t.Fatal("typed before the prompt box came back")
	}
	r.clock.Advance(time.Millisecond)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:после хода"}) || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("sent %v", got)
	}
	if _, ok := r.svc.EnterClock().Take(testSession); !ok {
		t.Error("the typed line's Enter is not on the clock")
	}
}

func TestScheduleNothingForAnEmptyQueue(t *testing.T) {
	r := newRig(t, "", Tab{})
	r.svc.Schedule("tab-1", 0)
	if len(r.clock.pending()) != 0 {
		t.Fatal("a delivery was scheduled for an empty queue")
	}
}

func TestBusyAgentWaitsForItsTurnToEnd(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	_, _ = r.svc.Queue().Add("tab-1", "потом", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if len(r.panes.sent()) != 0 || len(r.clock.pending()) != 0 {
		t.Fatal("a working agent was typed into, or retried without a transition")
	}
	// The watchdog looks again; the turn ended meanwhile.
	r.svc.Start()
	r.tabs.set("tab-1", func(tab *Tab) { tab.CliState = StateIdle })
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	r.clock.Advance(watchdogEvery)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:потом"}) {
		t.Fatalf("sent %v", got)
	}
	r.svc.Stop()
}

func TestRetriesWhileTheScreenRefuses(t *testing.T) {
	draft := strings.Replace(tui(t, "claude-input-draft.ansi.txt"), "", "", 1)
	r := newRig(t, draft, Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "моё", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	p := r.clock.pending()
	if len(p) != 1 || p[0].at.Sub(r.clock.Now()) != blockedRetry {
		t.Fatalf("a blocked delivery is retried in %v", p)
	}
	// Retries never stop while the message waits (no attempt limit): forty-odd blocked looks later it still retries.
	for i := 0; i < 45; i++ {
		r.clock.Advance(blockedRetry)
	}
	if len(r.clock.pending()) != 1 || len(r.panes.sent()) != 0 {
		t.Fatal("the retries stopped")
	}
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	r.clock.Advance(blockedRetry)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:моё"}) {
		t.Fatalf("sent %v", got)
	}
	// An unreadable screen is a short retry.
	_, _ = r.svc.Queue().Add("tab-1", "ещё", nil)
	r.panes.mu.Lock()
	r.panes.styled = func() (string, bool) { return "", false }
	r.panes.mu.Unlock()
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if p := r.clock.pending(); len(p) != 1 || p[0].at.Sub(r.clock.Now()) != retryDelay {
		t.Fatalf("wait retry %v", p)
	}
}

func TestGoneAgentIsResumedFirst(t *testing.T) {
	r := newRig(t, "agent@host:~$ ", Tab{HasAgentSession: true})
	r.agents.running = false
	_, _ = r.svc.Queue().Add("tab-1", "проснись", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if r.agents.resumed != 1 || len(r.panes.sent()) != 0 {
		t.Fatalf("resumed %d, sent %v", r.agents.resumed, r.panes.sent())
	}
	// Resumed: looked at again 1.5 s later; the launch is under way, so nothing happens until it ends.
	r.tabs.set("tab-1", func(tab *Tab) { tab.Launching = true })
	r.clock.Advance(retryDelay)
	if r.agents.resumed != 1 || len(r.panes.sent()) != 0 {
		t.Fatal("typed or resumed again during the launch")
	}
	r.tabs.set("tab-1", func(tab *Tab) { tab.Launching = false })
	r.agents.running = true
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	r.svc.LaunchEnded("tab-1")
	r.clock.Advance(settleDelay)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:проснись"}) {
		t.Fatalf("sent %v", got)
	}
}

func TestResumeOutcomes(t *testing.T) {
	r := newRig(t, "agent@host:~$ ", Tab{HasAgentSession: true})
	r.agents.running = false
	_, _ = r.svc.Queue().Add("tab-1", "x", nil)

	r.agents.resume = "busy-shell"
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if p := r.clock.pending(); len(p) != 1 || p[0].at.Sub(r.clock.Now()) != blockedRetry {
		t.Fatalf("a shell still starting is not retried in 5 s: %v", p)
	}
	r.agents.resume = ResumeNoSession
	r.clock.Advance(blockedRetry)
	if !reflect.DeepEqual(r.agents.gaveUp, []string{"tab-1"}) || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("gave up %v", r.agents.gaveUp)
	}
	// No session at all: the launch is given up at once, the message stays queued.
	r2 := newRig(t, "agent@host:~$ ", Tab{})
	r2.agents.running = false
	_, _ = r2.svc.Queue().Add("tab-1", "x", nil)
	r2.svc.Schedule("tab-1", 0)
	r2.clock.Advance(0)
	if !reflect.DeepEqual(r2.agents.gaveUp, []string{"tab-1"}) || r2.agents.recovered != 1 || r2.agents.resumed != 0 {
		t.Fatalf("unrecoverable: %+v", r2.agents)
	}
	// A launch that gave up is left alone.
	r2.tabs.set("tab-1", func(tab *Tab) { tab.LaunchFailed = true })
	r2.svc.Schedule("tab-1", 0)
	r2.clock.Advance(0)
	if len(r2.agents.gaveUp) != 1 {
		t.Fatal("given up twice")
	}
}

func TestTabRemovedDropsItsQueue(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	_, _ = r.svc.Queue().Add("tab-1", "x", nil)
	r.svc.TabSettled("tab-1")
	r.svc.TabRemoved("tab-1")
	if len(r.clock.pending()) != 0 || len(r.svc.Queue().All()) != 0 {
		t.Fatal("the closed tab keeps its queue or its delivery")
	}
	last := r.bc.msgs[len(r.bc.msgs)-1].(QueueUpdate)
	if last.Type != "prompt-queue:update" || last.TabID != "tab-1" || len(last.Items) != 0 {
		t.Errorf("pages told %+v", last)
	}
}

func TestStartDeliversWhatWaitedThroughARestart(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock()
	q := NewQueue(dir+"/queue.json", clock)
	_, _ = q.Add("tab-1", "до перезапуска", nil)
	q.Flush()

	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	r.svc = New(Config{Panes: r.panes, Tabs: r.tabs, Agents: r.agents, Events: r.events, Broadcast: r.bc, Clock: r.clock, QueueFile: dir + "/queue.json",
		Procs: Procs{Children: func(int) []int { return nil }, Args: func(int) (string, bool) { return "", false }, Environ: func(int) ([]string, bool) { return nil, false }}})
	if sync := r.svc.QueueSync(); sync.Type != "prompt-queue:sync" || len(sync.Queues["tab-1"]) != 1 {
		t.Fatalf("sync %+v", sync)
	}
	r.svc.Start()
	r.clock.Advance(settleDelay)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:до перезапуска"}) {
		t.Fatalf("sent %v", got)
	}
	r.svc.Stop()
}

func TestTakeNewPrompt(t *testing.T) {
	// A working agent gets the message in its running turn now.
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return nil, false }
	it, _ := r.svc.Queue().Add("tab-1", "и проверь логи", nil)
	if got := r.svc.TakeNewPrompt("tab-1", it.ID); got != DeliverySteered || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("busy: %s", got)
	}
	if got := r.panes.sent(); len(got) == 0 || got[0] != "line:и проверь логи" {
		t.Fatalf("sent %v", got)
	}
	// A draft in the box keeps it queued.
	r.panes.setScreen(tui(t, "claude-input-draft.ansi.txt"))
	it, _ = r.svc.Queue().Add("tab-1", "после черновика", nil)
	if got := r.svc.TakeNewPrompt("tab-1", it.ID); got != DeliveryQueued || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("draft: %s", got)
	}
	// An agent that is not working takes it from the queue at once.
	r2 := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	it, _ = r2.svc.Queue().Add("tab-1", "сразу", nil)
	if got := r2.svc.TakeNewPrompt("tab-1", it.ID); got != DeliveryQueued {
		t.Fatalf("idle: %s", got)
	}
	r2.clock.Advance(0)
	if got := r2.panes.sent(); !reflect.DeepEqual(got, []string{"line:сразу"}) {
		t.Fatalf("idle sent %v", got)
	}
}

func TestSteerErrors(t *testing.T) {
	r := newRig(t, tui(t, "claude-input-draft.ansi.txt"), Tab{CliState: StateBusy})
	if got := r.svc.Steer("tab-x", "q"); got.Status != 404 || got.Message != msgNoTab.String() {
		t.Errorf("unknown tab: %+v", got)
	}
	if got := r.svc.Steer("tab-1", "q-none"); got.Status != 409 || got.Message != "Сообщения уже нет в очереди" {
		t.Errorf("gone: %+v", got)
	}
	it, _ := r.svc.Queue().Add("tab-1", "x", nil)
	if got := r.svc.Steer("tab-1", it.ID); got.Status != 409 || got.Message != steerErrors[OutcomeBlocked].String() || len(queueTexts(r, "tab-1")) != 1 {
		t.Errorf("blocked: %+v", got)
	}
	r.panes.mu.Lock()
	r.panes.styled = func() (string, bool) { return "", false }
	r.panes.mu.Unlock()
	if got := r.svc.Steer("tab-1", it.ID); got.Status != 409 || got.Message != steerErrors[OutcomeWait].String() {
		t.Errorf("wait: %+v", got)
	}
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	pic, _ := r.svc.Queue().Add("tab-1", "картинка", []string{"/uploads/a.png"})
	if got := r.svc.Steer("tab-1", pic.ID); got.Status != 500 || got.Message != steerErrors[OutcomeFailed].String() {
		t.Errorf("failed pictures: %+v", got)
	}
}

func TestSteerHandsOverARunningCall(t *testing.T) {
	bash := transcript(t, "claude-2.1.296-send-now-bash.jsonl")
	r := newRig(t, tui(t, "claude-2.1.296-queued-bash.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	// The steered line is in the box's queue; the chord hands the call over and the transcript gets the prompt.
	lines := append([]string{}, bash[:3]...)
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return lines, true }
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	r.panes.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			r.panes.setScreen(tui(t, "claude-2.1.296-queued-bash.ansi.txt"))
		case call == "seq:C-x C-s":
			lines = append(lines, record(map[string]any{"type": "user", "timestamp": r.clock.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": "steer me"}}))
		}
	}
	it, _ := r.svc.Queue().Add("tab-1", "steer me", nil)
	if got := r.svc.Steer("tab-1", it.ID); !got.OK {
		t.Fatalf("steer: %+v", got)
	}
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:steer me", "seq:C-x C-s"}) {
		t.Fatalf("sent %v", got)
	}
}

func TestTypePrompt(t *testing.T) {
	r := newRig(t, "❯ ", Tab{})
	steps := NewSteps(r.clock)
	via, err := r.svc.TypePrompt("tab-1", "первая\nвторая", true, "m1", steps)
	if err != nil || via != ViaPaste {
		t.Fatalf("via %s %v", via, err)
	}
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"paste:первая\nвторая", "keys:Enter", "keys:Enter"}) {
		t.Fatalf("sent %v", got)
	}
	if !reflect.DeepEqual(steps.Names(), []string{"pasted", "enter"}) {
		t.Errorf("steps %v", steps.Names())
	}
	if via, _ := r.svc.TypePrompt("tab-1", "первая\nвторая", true, "m1", nil); via != ViaDuplicate {
		t.Errorf("the same id typed twice: %s", via)
	}
	if via, _ := r.svc.TypePrompt("tab-1", "без отправки", false, "", nil); via != ViaPaste || r.panes.sent()[3] != "paste:без отправки" || len(r.panes.sent()) != 4 {
		t.Errorf("unsubmitted: %s %v", via, r.panes.sent())
	}
	if _, err := r.svc.TypePrompt("tab-x", "x", true, "", nil); err != ErrNoTab {
		t.Errorf("unknown tab: %v", err)
	}
	// Typing that failed frees the id, so the page's retry goes.
	r.panes.failOn = "paste:"
	if _, err := r.svc.TypePrompt("tab-1", "сбой", true, "m2", nil); err == nil {
		t.Fatal("no error")
	}
	r.panes.failOn = ""
	if via, err := r.svc.TypePrompt("tab-1", "сбой", true, "m2", nil); err != nil || via != ViaPaste {
		t.Errorf("retry: %s %v", via, err)
	}
}

func TestTypePromptCodex(t *testing.T) {
	idle := tui(t, "codex-idle-placeholder.ansi.txt")
	r := newRig(t, idle, Tab{Provider: ProviderCodex})
	if _, err := r.svc.TypePrompt("tab-1", "x", false, "", nil); err != ErrCodexNeedsSubmit {
		t.Fatalf("unsubmitted Codex: %v", err)
	}
	r.panes.onSend = func(call string) {
		if strings.HasPrefix(call, "paste:") {
			r.panes.setScreen("› Also add the word DELTA.\n  tab to queue message")
		}
		if call == "keys:Enter" {
			r.panes.setScreen(idle)
		}
	}
	steps := NewSteps(r.clock)
	if via, err := r.svc.TypePrompt("tab-1", "Also add the word DELTA.", true, "c1", steps); err != nil || via != ViaCodex {
		t.Fatalf("via %s %v", via, err)
	}
	if !reflect.DeepEqual(steps.Names(), []string{"enter"}) {
		t.Errorf("steps %v", steps.Names())
	}
	r.panes.onSend = nil
	r.panes.setScreen("› Stuck text here\n  tab to queue message")
	if via, _ := r.svc.TypePrompt("tab-1", "Stuck text here", true, "c2", nil); via != ViaFailed {
		t.Errorf("kept in the box: %s", via)
	}
}

func TestInterrupt(t *testing.T) {
	r := newRig(t, "", Tab{})
	if !r.svc.Interrupt("tab-1") || !reflect.DeepEqual(r.panes.sent(), []string{"keys:Escape"}) {
		t.Fatalf("sent %v", r.panes.sent())
	}
	if ev := r.events.events(); !reflect.DeepEqual(ev, []string{"interrupted:tab-1:" + testSession}) {
		t.Errorf("events %v", ev)
	}
	if r.svc.Interrupt("tab-x") {
		t.Error("an unknown tab was interrupted")
	}
	r.panes.failOn = "keys:"
	if r.svc.Interrupt("tab-1") {
		t.Error("a key tmux did not take counts")
	}
}

func TestMarkNeedsInputFromScreen(t *testing.T) {
	menu := tui(t, "claude-switch-model-confirm.txt")
	r := newRig(t, menu, Tab{CliState: StateBusy})
	r.events.menuMoves = true
	if !r.svc.MarkNeedsInputFromScreen("tab-1") {
		t.Fatal("not moved")
	}
	r.panes.detail = tmux.PaneDetail{Width: 59, Height: 40}
	if r.svc.MarkNeedsInputFromScreen("tab-1") {
		t.Error("a narrow pane was read")
	}
	r.panes.detail = tmux.PaneDetail{Width: 120, Height: 40}
	r.tabs.set("tab-1", func(tab *Tab) { tab.CliState = StateNeedsInput })
	if r.svc.MarkNeedsInputFromScreen("tab-1") {
		t.Error("a tab already waiting moved")
	}
	r.tabs.set("tab-1", func(tab *Tab) { tab.CliState = StateIdle })
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	if r.svc.MarkNeedsInputFromScreen("tab-1") {
		t.Error("moved without a menu")
	}
	if n := len(r.events.events()); n != 1 {
		t.Errorf("%d transitions asked", n)
	}
}

func TestStepsKeepTheirOrder(t *testing.T) {
	c := newFakeClock()
	s := NewSteps(c)
	c.Advance(5 * time.Millisecond)
	s.Mark("taken")
	c.Advance(10 * time.Millisecond)
	s.Mark("enter")
	s.Mark("taken")
	raw, _ := s.MarshalJSON()
	if string(raw) != `{"taken":15,"enter":15}` {
		t.Errorf("got %s", raw)
	}
}
