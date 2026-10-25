package outbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func tui(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "typist", "testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const idleLine = "\x1b[39m❯ "

func withInput(t *testing.T, text string) string {
	t.Helper()
	return strings.Replace(tui(t, "claude-idle-placeholder.ansi.txt"), idleLine+"\n", idleLine+text+"\n", 1)
}

const permissionScreen = `● Bash(rm -rf build)

 Bash command

   rm -rf build

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /home/coder/app
   3. No, and tell Claude what to do differently (esc)
`

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type fakePane struct {
	screen string
	calls  []string
	onSend func(call string)
	fail   string
	// view, when set, is what a look sees instead of screen: a TUI that draws late.
	view func() string
}

func (p *fakePane) shown() string {
	if p.view != nil {
		return p.view()
	}
	return p.screen
}

func (p *fakePane) CaptureStyled() (string, bool) { return p.shown(), true }
func (p *fakePane) Capture() (string, bool)       { return screen.StripANSI(p.shown()), true }
func (p *fakePane) send(call string) error {
	if p.fail != "" && strings.HasPrefix(call, p.fail) {
		return fmt.Errorf("tmux: %s failed", call)
	}
	p.calls = append(p.calls, call)
	if p.onSend != nil {
		p.onSend(call)
	}
	return nil
}
func (p *fakePane) Paste(text string) error    { return p.send("paste:" + text) }
func (p *fakePane) TypeLine(text string) error { return p.send("line:" + text) }
func (p *fakePane) Keys(keys ...string) error  { return p.send("keys:" + strings.Join(keys, " ")) }

type fakeLog struct{ lines []string }

func (l *fakeLog) Info(msg string, kv ...any) { l.lines = append(l.lines, msg+" "+fmt.Sprint(kv...)) }
func (l *fakeLog) Warn(msg string, kv ...any) { l.lines = append(l.lines, msg+" "+fmt.Sprint(kv...)) }

// rig plays the conversation's actor around one box: it looks at the fake pane before every pump, runs a job at
// once through the real typist on the fake clock, and keeps every event the box made.
type rig struct {
	t      *testing.T
	box    *Box
	clock  *fakeClock
	pane   *fakePane
	agent  Agent
	size   int64
	events []wire.Event
	log    *fakeLog
	jobs   int
}

func newRig(t *testing.T, screenText string, state wire.AgentStateKind) *rig {
	log := &fakeLog{}
	r := &rig{t: t, log: log, clock: &fakeClock{now: time.UnixMilli(1_800_000_000_000)}, pane: &fakePane{screen: screenText},
		agent: Agent{State: wire.AgentState{K: state, Since: 1}, Provider: wire.ProviderClaude, Foreground: true}}
	r.box = New("tab-1", wire.DefaultLimits, log)
	return r
}

// leaves empties the box on the agent's side once Enter or a typed line goes in.
func (r *rig) leaves() {
	idle := tui(r.t, "claude-idle-placeholder.ansi.txt")
	r.pane.onSend = func(call string) {
		if call == "keys:Enter" || strings.HasPrefix(call, "line:") {
			r.pane.screen = idle
		}
	}
}

func (r *rig) record(ch Change) {
	r.t.Helper()
	for _, e := range ch.Events {
		if err := e.Validate(); err != nil {
			r.t.Fatalf("%s: %v", e.EventType(), err)
		}
	}
	r.events = append(r.events, ch.Events...)
}

func (r *rig) send(id, text string, mode wire.MessageMode) wire.RefuseCode {
	r.t.Helper()
	ch, code := r.box.Send(Draft{ClientID: id, Text: text, Mode: mode, Kind: wire.KindPrompt}, r.agent.State, r.clock.Now())
	r.record(ch)
	return code
}

func (r *rig) pump() {
	r.t.Helper()
	look := typist.Look(r.pane, r.agent.Provider)
	r.agent.Screen = &look
	job, ch := r.box.Pump(r.agent, r.size, r.clock.Now())
	r.record(ch)
	if job == nil {
		return
	}
	r.jobs++
	res := typist.Type(job.Message, r.agent.Provider, r.pane, r.clock)
	r.record(r.box.Typed(job.ClientID, res, r.clock.Now()))
}

func (r *rig) advance(d time.Duration) {
	r.clock.Sleep(d)
	r.pump()
}

func (r *rig) state(id string) wire.MessageState {
	m, ok := r.box.Message(id)
	if !ok {
		r.t.Fatalf("no message %s", id)
	}
	return m.State
}

// trail is the states one message went through, as its prompt_state events said.
func (r *rig) trail(id string) string {
	var ks []string
	for _, e := range r.events {
		if ps, ok := e.(wire.PromptStateEv); ok && ps.ClientID == id {
			k := string(ps.State.K)
			switch ps.State.K {
			case wire.MsgQueued:
				k += "{" + string(ps.State.Reason) + "}"
			case wire.MsgFailed:
				k += "{" + string(ps.State.Code) + "}"
			}
			if len(ks) == 0 || ks[len(ks)-1] != k {
				ks = append(ks, k)
			}
		}
	}
	return strings.Join(ks, " → ")
}

func (r *rig) typed() []string { return r.pane.calls }
