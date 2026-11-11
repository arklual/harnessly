package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Delays of a chat's notices, the keeper's own: the Stop and the Notification hooks fire before the transcript has the
// answer or the question, and the agent's last writes land a moment before its Stop.
const (
	chatPushSettle    = 1500 * time.Millisecond
	chatSnapshotDelay = 400 * time.Millisecond
)

// chatNotices does for a chat what the keeper did for its tab once the agent's turn ended or it asked: the web push of
// the finished turn or the question, the snapshot of the folder for the rewind, Codex's rate limits. It hears the
// chat's actor (convo.Deps.Notify), so one turn's end is told once whatever hooks came; the keeper leaves the chats'
// sessions to it (status.Deps.ChatSession).
type chatNotices struct {
	// owns says the tmux session is a chat's; one that is not is still the keeper's to push for.
	owns func(session string) bool
	// record is the chat as its actor stands now.
	record func(id string) (wire.Conversation, bool)
	// entry is the keeper's tab of the session, for the names a push shows; false when it has none.
	entry   func(session string) (string, status.Entry, bool)
	push    func() status.Pusher
	turnEnd func(tabID, cwd string)
	cwd     func(session string) string
	codex   func(transcript string)
	after   func(d time.Duration, f func())
	// mirror puts the keeper's tab of the session where the chat is (status.Manager.MirrorChat).
	mirror func(session string, s status.ChatState)

	// The mirror goes off the actor, in order: the latest state of each session waits here for one drainer.
	mu       sync.Mutex
	pending  map[string]status.ChatState
	order    []string
	draining bool
}

// newChatNotices: rt is set once the chats' runtime is made; a notice before that tells nobody.
func newChatNotices(c *core, rt *atomic.Pointer[convo.Runtime]) *chatNotices {
	return &chatNotices{
		owns: func(s string) bool { r := rt.Load(); return r != nil && r.OwnsSession(s) },
		record: func(id string) (wire.Conversation, bool) {
			if r := rt.Load(); r != nil {
				return r.Record(id)
			}
			return wire.Conversation{}, false
		},
		entry: func(s string) (string, status.Entry, bool) {
			if m := c.manager(); m != nil {
				return m.EntryOfSession(s)
			}
			return "", status.Entry{}, false
		},
		push: func() status.Pusher {
			if c.push == nil {
				return nil
			}
			return c.push
		},
		turnEnd: c.turnEnd,
		cwd: func(s string) string {
			if c.cfg.Tmux == nil {
				return ""
			}
			return machine.SessionDir(c.cfg.Tmux, s)
		},
		codex: func(t string) {
			if d := limits.CodexTranscriptLimits(t, float64(time.Now().UnixMicro())/1e6); d != nil && c.limits != nil {
				_, _ = c.limits.Write("codex", d)
			}
		},
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
		mirror: func(s string, st status.ChatState) {
			if m := c.manager(); m != nil {
				m.MirrorChat(s, st)
			}
		},
	}
}

// notice is called on the chat's actor: everything here goes off it at once.
func (n *chatNotices) notice(x convo.Notice) {
	rec := x.Conv
	if rec.Session == "" || !n.owns(rec.Session) {
		return
	}
	if x.Kind == convo.NoticeState {
		n.queueMirror(rec)
		return
	}
	if x.Kind == convo.NoticeTurnEnded {
		n.after(chatSnapshotDelay, func() { n.turnEnded(rec) })
		return
	}
	if x.Push() {
		n.after(chatPushSettle, func() { n.pushNow(x) })
	}
}

func (n *chatNotices) turnEnded(rec wire.Conversation) {
	cwd := n.cwd(rec.Session)
	if cwd == "" {
		cwd = rec.Cwd
	}
	n.turnEnd(rec.ID, cwd)
	if rec.Provider == wire.ProviderCodex && rec.TranscriptPath != "" && n.codex != nil {
		n.codex(rec.TranscriptPath)
	}
}

// pushNow sends the push if the chat still stands where the notice found it: a turn that went on, or a question
// answered meanwhile, tells nobody.
func (n *chatNotices) pushNow(x convo.Notice) {
	p := n.push()
	if p == nil {
		return
	}
	now, ok := n.record(x.Conv.ID)
	if !ok || now.State.K != x.Conv.State.K || now.State.Since != x.Conv.State.Since {
		return
	}
	tabID, e := n.pushEntry(now, x.Kind)
	kind := "done"
	if x.Kind == convo.NoticeQuestion {
		kind = "question"
	}
	p.Push(tabID, e, kind)
}

// pushEntry is what the push is built from: the keeper's tab of the chat when it has one (its workspace and names),
// with what the chat knows better.
func (n *chatNotices) pushEntry(rec wire.Conversation, k convo.NoticeKind) (string, status.Entry) {
	tabID, e, ok := n.entry(rec.Session)
	if !ok {
		tabID, e = rec.ID, status.Entry{TmuxSession: rec.Session, TabName: rec.Title}
	}
	if rec.TranscriptPath != "" {
		e.JSONLPath = rec.TranscriptPath
	}
	if rec.AgentSessionID != "" {
		e.AgentSessionID = status.Some(rec.AgentSessionID)
	}
	provider := agents.Claude
	if rec.Provider == wire.ProviderCodex {
		provider = agents.Codex
	}
	e.AgentProviderID = status.Some(provider)
	event := status.EventStop
	if k == convo.NoticeFailed {
		event = status.EventStopFailure
	}
	if k != convo.NoticeQuestion {
		e.LastEvent = status.Some(status.LastEvent{Name: event, At: float64(rec.State.Since)})
	}
	return tabID, e
}

// cliStates is the keeper's word for a chat's agent state; a state missing here (starting, unknown) leaves the tab's
// state as it is. The event is the hook the change stands for.
var cliStates = map[wire.AgentStateKind][2]string{
	wire.StateWorking:           {status.Busy, status.EventPromptSubmit},
	wire.StateWaitingQuestion:   {status.NeedsInput, status.EventNotification},
	wire.StateWaitingPermission: {status.NeedsInput, status.EventNotification},
	wire.StateReady:             {status.ReadyForReview, status.EventStop},
	wire.StateIdle:              {status.Idle, ""},
	wire.StateStopped:           {status.Inactive, ""},
	wire.StateGone:              {status.Inactive, ""},
	wire.StateLaunchFailed:      {status.Inactive, ""},
}

func chatStateOf(rec wire.Conversation) status.ChatState {
	cs := cliStates[rec.State.K]
	return status.ChatState{CliState: cs[0], Event: cs[1], Transcript: rec.TranscriptPath, AgentSessionID: rec.AgentSessionID}
}

// queueMirror keeps the chat's latest state for the drainer, which takes the keeper's lock off the actor.
func (n *chatNotices) queueMirror(rec wire.Conversation) {
	if n.mirror == nil {
		return
	}
	n.mu.Lock()
	if n.pending == nil {
		n.pending = map[string]status.ChatState{}
	}
	if _, ok := n.pending[rec.Session]; !ok {
		n.order = append(n.order, rec.Session)
	}
	n.pending[rec.Session] = chatStateOf(rec)
	start := !n.draining
	n.draining = true
	n.mu.Unlock()
	if start {
		n.after(0, n.drain)
	}
}

func (n *chatNotices) drain() {
	for {
		n.mu.Lock()
		if len(n.order) == 0 {
			n.draining = false
			n.mu.Unlock()
			return
		}
		session := n.order[0]
		n.order = n.order[1:]
		st := n.pending[session]
		delete(n.pending, session)
		n.mu.Unlock()
		n.mirror(session, st)
	}
}
