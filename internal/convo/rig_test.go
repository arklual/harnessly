package convo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The rig plays the machine around one runtime: a fake tmux pane whose screen the test sets, a Claude transcript
// file the test appends to, a fake clock moved by hand, and a publisher that checks every event on its channel.
// The runtime runs inline: no goroutine, no sleep.

func tui(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "typist", "testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const idleLine = "\x1b[39m❯\u00a0"

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
func (c *fakeClock) After(time.Duration) <-chan time.Time { return nil }

type published struct {
	ch  wire.Channel
	seq int64
	e   wire.Event
}

type fakePub struct {
	t    *testing.T
	mu   sync.Mutex
	seqs map[wire.Channel]int64
	all  []published
}

func (p *fakePub) Publish(ch wire.Channel, e wire.Event) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := e.Validate(); err != nil {
		p.t.Errorf("%s on %s: %v", e.EventType(), ch, err)
	}
	_, conv := ch.ConvID()
	switch {
	case slices.Contains(wire.ConvsEventTypes, e.EventType()) && ch != wire.ChannelConvs,
		slices.Contains(wire.ConvEventTypes, e.EventType()) && !conv:
		p.t.Errorf("%s on channel %s", e.EventType(), ch)
	}
	p.seqs[ch]++
	p.all = append(p.all, published{ch, p.seqs[ch], e})
	return p.seqs[ch]
}

type fakePane struct {
	screen string
	calls  []string
	fail   string
	// onSend plays the agent: what it does with a key or a paste.
	onSend func(call string)
}

func (p *fakePane) CaptureStyled() (string, bool) { return p.screen, true }
func (p *fakePane) Capture() (string, bool)       { return screen.StripANSI(p.screen), true }
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

type fakeMachine struct {
	panes  map[string]*fakePane
	probes map[string]Probe
}

func (m *fakeMachine) Pane(session string) typist.Pane {
	if p := m.panes[session]; p != nil {
		return p
	}
	return nil
}
func (m *fakeMachine) Probe(session string) Probe { return m.probes[session] }

type fakeLauncher struct {
	launched []LaunchRequest
	resumed  []string
}

func (l *fakeLauncher) Launch(req LaunchRequest) error {
	l.launched = append(l.launched, req)
	return nil
}
func (l *fakeLauncher) Resume(c wire.Conversation) error {
	l.resumed = append(l.resumed, c.ID)
	return nil
}

type rig struct {
	t        *testing.T
	clock    *fakeClock
	pub      *fakePub
	machine  *fakeMachine
	launcher *fakeLauncher
	rt       *Runtime
	dir      string
	path     string
	pane     *fakePane
	n        int
	id       string
	// deps, when set, changes what the runtime is made with (and made again with on a restart).
	deps func(*Deps)
}

const convID = "tab-one"

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, dir: dir, path: filepath.Join(dir, "t.jsonl"), id: convID,
		clock: &fakeClock{now: time.UnixMilli(1_800_000_000_000)}, pub: &fakePub{t: t, seqs: map[wire.Channel]int64{}},
		pane: &fakePane{screen: tui(t, "claude-idle-placeholder.ansi.txt")}, launcher: &fakeLauncher{}}
	r.machine = &fakeMachine{panes: map[string]*fakePane{"s1": r.pane},
		probes: map[string]Probe{"s1": {Session: true, Foreground: true}}}
	if err := os.WriteFile(r.path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.rt = r.runtime()
	return r
}

func (r *rig) runtime() *Runtime {
	d := Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits}
	if r.deps != nil {
		r.deps(&d)
	}
	return NewInline(d)
}

// open imports the tab the rig plays and lets the probes settle it.
func (r *rig) open() {
	r.t.Helper()
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderClaude, Cwd: "/w",
		TranscriptPath: r.path}}, nil)
}

func (r *rig) uuid() string {
	r.n++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", r.n)
}

func (r *rig) line(format string, args ...any) string {
	r.t.Helper()
	fh, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	defer fh.Close()
	u := r.uuid()
	ts := r.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	fmt.Fprintf(fh, `{"uuid":%q,"timestamp":%q,`+format+"}\n", append([]any{u, ts}, args...)...)
	return u
}

func (r *rig) user(text string) string {
	return r.line(`"type":"user","message":{"role":"user","content":%q}`, text)
}

func (r *rig) say(text string) string {
	return r.line(`"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%q}]}`, text)
}

func (r *rig) tool(id, command string) string {
	return r.line(`"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Bash","input":{"command":%q}}]}`, id, command)
}

func (r *rig) result(id, text string) string {
	return r.line(`"type":"user","message":{"role":"user","content":[{"tool_use_id":%q,"type":"tool_result","content":%q,"is_error":false}]}`, id, text)
}

func (r *rig) ask(id string) string {
	return r.line(`"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"AskUserQuestion","input":{"questions":[{"question":"Which one?","header":"Pick","multiSelect":false,"options":[{"label":"Red","description":"warm"},{"label":"Blue","description":"cold"}]}]}}]}`, id)
}

func (r *rig) turnEnd() string {
	return r.line(`"type":"system","subtype":"stop_hook_summary"`)
}

// compactBoundary and compactSummary are the lines Claude writes when an auto-compaction is done, shaped as in its
// transcripts (2.1.282–2.1.310): the boundary has no parentUuid, the summary is a user line the person never wrote.
func (r *rig) compactBoundary(durationMs int) string {
	return r.line(`"parentUuid":null,"logicalParentUuid":"00000000-0000-4000-8000-000000000001","isSidechain":false,"type":"system","subtype":"compact_boundary","content":"Conversation compacted","level":"info","compactMetadata":{"trigger":"auto","preTokens":604330,"postTokens":38516,"durationMs":%d}`, durationMs)
}

const compactSummaryText = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation."

func (r *rig) compactSummary() string {
	return r.line(`"type":"user","isSidechain":false,"isVisibleInTranscriptOnly":true,"isCompactSummary":true,"turnOrigin":"human","message":{"role":"user","content":%q}`, compactSummaryText)
}

// compactingScreen is Claude 2.1.310 compacting in a 40-column pane: the footer has no "esc to interrupt".
func (r *rig) compactingScreen() {
	r.pane.screen = tui(r.t, "claude-2.1.310-compacting-narrow.ansi.txt")
}

func (r *rig) interruptLine() string {
	return r.line(`"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}`)
}

// tick moves the clock and gives the runtime its look and its clock turn.
func (r *rig) tick(d time.Duration) {
	r.clock.Sleep(d)
	r.rt.Advance()
}

func (r *rig) hook(kind string) {
	r.t.Helper()
	if err := r.rt.Hook(r.id, Hook{Kind: kind}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) do(cmd wire.Command) wire.CmdResult {
	r.t.Helper()
	return r.rt.Do(context.Background(), "dev-1", cmd)
}

func (r *rig) send(id, text string, mode wire.MessageMode) wire.CmdResult {
	r.t.Helper()
	return r.do(wire.SendCmd{OpID: id, ClientID: id, ConversationID: r.id, Text: text, Mode: mode, Kind: wire.KindPrompt,
		AttachmentIDs: []string{}})
}

func (r *rig) snap() wire.ConvSnapshot {
	r.t.Helper()
	s, err := r.rt.Conversation(context.Background(), r.id)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *rig) state() wire.AgentState {
	r.t.Helper()
	cs, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range cs.Conversations {
		if c.ID == r.id {
			return c.State
		}
	}
	r.t.Fatalf("no conversation %s", r.id)
	return wire.AgentState{}
}

func (r *rig) message(id string) wire.Message {
	r.t.Helper()
	for _, m := range r.snap().Messages {
		if m.ClientID == id {
			return m
		}
	}
	r.t.Fatalf("no message %s", id)
	return wire.Message{}
}

// trail is the states one message went through, as its prompt_state events said.
func (r *rig) trail(id string) string {
	var ks []string
	for _, p := range r.pub.all {
		ps, ok := p.e.(wire.PromptStateEv)
		if !ok || ps.ClientID != id {
			continue
		}
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
	return strings.Join(ks, " → ")
}

func (r *rig) events(t wire.EventType) []published {
	var out []published
	for _, p := range r.pub.all {
		if p.e.EventType() == t {
			out = append(out, p)
		}
	}
	return out
}

// agentTakes plays Claude taking a typed message: a short line or a paste and its Enter leave the box, the screen
// turns busy and the transcript gets the user line.
func (r *rig) agentTakes() {
	busy := tui(r.t, "claude-busy-spinner.ansi.txt")
	pasted := ""
	r.pane.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			r.user(strings.TrimPrefix(call, "line:"))
			r.pane.screen = busy
		case strings.HasPrefix(call, "paste:"):
			pasted = strings.TrimPrefix(call, "paste:")
			r.pane.screen = withInput(r.t, pasted)
		case call == "keys:Enter" && pasted != "":
			r.user(pasted)
			pasted = ""
			r.pane.screen = busy
		}
	}
}

// settle lets the runtime look until the agent's state is k, at most a minute of fake time.
func (r *rig) settle(k wire.AgentStateKind) {
	r.t.Helper()
	for range 60 {
		if r.state().K == k {
			return
		}
		r.tick(time.Second)
	}
	r.t.Fatalf("state %s, not %s", r.state().K, k)
}

func (r *rig) idle() {
	r.pane.screen = tui(r.t, "claude-idle-placeholder.ansi.txt")
}

// done: Claude's summary under its finished turn, the box empty.
func (r *rig) done() {
	r.pane.screen = tui(r.t, "claude-2.1.310-turn-done.ansi.txt")
}

func (r *rig) busy() {
	r.pane.screen = tui(r.t, "claude-busy-spinner.ansi.txt")
}

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
