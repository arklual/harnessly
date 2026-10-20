package launch

import (
	"errors"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/status"
)

// InboxTmux is what document delivery asks of tmux.
type InboxTmux interface {
	HasSession(name string) bool
	// SessionsOfPane are the sessions a pane id names (on socket, or on every server when "").
	SessionsOfPane(paneID, socket string) []string
	SessionCwd(name string) string
}

// Inbox types a person's document comments into the conversation the document is bound to: at once when the agent
// is idle, after its turn when busy, into a resumed copy of the session when the tab is gone.
type Inbox struct {
	Starter *Starter
	Tmux    InboxTmux
	// Submit types a text into a Claude pane and submits it.
	Submit func(session, text string) error
	Log    func(level, msg string, kv ...any)
	// Every is how often queued messages are tried (3 s); MaxWait how long one may wait for its tab (6 h).
	Every, MaxWait time.Duration
	Now            func() time.Time

	mu      sync.Mutex
	order   []string
	queues  map[string][]queued
	running bool
}

type queued struct {
	message string
	at      time.Time
}

var (
	pasteStates = map[string]bool{status.Idle: true, status.ReadyForReview: true}
	liveStates  = map[string]bool{status.Idle: true, status.Busy: true, status.ReadyForReview: true, status.NeedsInput: true}
)

func (b *Inbox) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Inbox) log(level, msg string, kv ...any) {
	if b.Log != nil {
		b.Log(level, msg, kv...)
	}
}

func (b *Inbox) sources() []status.AgentSource {
	if k := b.Starter.keeper(); k != nil {
		return k.AgentSources()
	}
	return nil
}

func (b *Inbox) queueLen(tabID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queues[tabID])
}

func (b *Inbox) enqueue(tabID, message string) {
	b.mu.Lock()
	if b.queues == nil {
		b.queues = map[string][]queued{}
	}
	if _, has := b.queues[tabID]; !has {
		b.order = append(b.order, tabID)
	}
	b.queues[tabID] = append(b.queues[tabID], queued{message: message, at: b.now()})
	start := !b.running
	b.running = true
	b.mu.Unlock()
	if start {
		every := b.Every
		if every == 0 {
			every = 3 * time.Second
		}
		go func() {
			for range time.Tick(every) {
				b.Flush()
			}
		}()
	}
}

// Flush tries the head of every tab's queue once: one message per tick, as the paste makes the agent busy.
func (b *Inbox) Flush() {
	maxWait := b.MaxWait
	if maxWait == 0 {
		maxWait = 6 * time.Hour
	}
	b.mu.Lock()
	order := append([]string(nil), b.order...)
	b.mu.Unlock()
	for _, tabID := range order {
		var source *status.AgentSource
		for _, s := range b.sources() {
			if s.TabID == tabID {
				source = &s
				break
			}
		}
		b.mu.Lock()
		q := b.queues[tabID]
		if len(q) == 0 {
			delete(b.queues, tabID)
			for i, id := range b.order {
				if id == tabID {
					b.order = append(b.order[:i], b.order[i+1:]...)
					break
				}
			}
			b.mu.Unlock()
			continue
		}
		head := q[0]
		if source == nil || b.now().Sub(head.at) > maxWait {
			b.queues[tabID] = q[1:]
			b.mu.Unlock()
			reason := "tab is gone"
			if source != nil {
				reason = "waited too long"
			}
			b.log("warn", "doc message dropped: "+reason, "tabId", tabID)
			continue
		}
		if !pasteStates[source.CliState] {
			b.mu.Unlock()
			continue
		}
		b.queues[tabID] = q[1:]
		b.mu.Unlock()
		if err := b.Submit(source.TmuxSession, head.message); err != nil {
			b.log("warn", "doc message paste failed", "tabId", tabID, "err", err.Error())
		}
	}
}

// Delivery is a comment and where its document came from.
type Delivery struct {
	TabID, SessionName, AgentSessionID, Cwd string
	Message                                 string
}

// Deliver types or queues the message, or reopens the conversation; {mode, tabId}.
func (b *Inbox) Deliver(in Delivery) (*jsvalue.Object, error) {
	sources := b.sources()
	var target *status.AgentSource
	for i := range sources {
		if in.TabID != "" && sources[i].TabID == in.TabID {
			target = &sources[i]
			break
		}
	}
	if target == nil {
		for i := range sources {
			if in.AgentSessionID != "" && sources[i].AgentSessionID == in.AgentSessionID {
				target = &sources[i]
				break
			}
		}
	}
	if target != nil && liveStates[target.CliState] && b.Tmux.HasSession(target.TmuxSession) {
		if pasteStates[target.CliState] && b.queueLen(target.TabID) == 0 {
			if err := b.Submit(target.TmuxSession, in.Message); err != nil {
				return nil, err
			}
			return jsvalue.NewObject("mode", "pasted", "tabId", target.TabID), nil
		}
		b.enqueue(target.TabID, in.Message)
		return jsvalue.NewObject("mode", "queued", "tabId", target.TabID), nil
	}
	start := func(cwd string) (Result, error) {
		o := Options{Provider: agents.Claude, Cwd: cwd}
		if in.AgentSessionID != "" {
			o.ResumeSessionID = in.AgentSessionID
		} else {
			msg := in.Message
			o.Prompt = &msg
		}
		return b.Starter.Start(o)
	}
	// The document's folder may lie outside the project root; the root is always allowed.
	started, err := start(in.Cwd)
	if err == nil && !started.OK && in.Cwd != "" {
		started, err = start("")
	}
	if err != nil {
		return nil, err
	}
	if !started.OK {
		return nil, errors.New("could not reopen the conversation: " + string(jsvalue.Stringify(started.Body, "")))
	}
	id := started.ConversationID()
	if in.AgentSessionID != "" {
		b.enqueue(id, in.Message)
	}
	return jsvalue.NewObject("mode", "resumed", "tabId", id), nil
}

// ResolvePane is the tab of this runtime a publishing shell's pane id belongs to. One id may name a pane on each tmux
// server; the one running an agent is the publisher, and socket settles it outright.
func (b *Inbox) ResolvePane(paneID, socket string) *jsvalue.Object {
	sessions := b.Tmux.SessionsOfPane(paneID, socket)
	var source *status.AgentSource
	for _, s := range b.sources() {
		for _, name := range sessions {
			if s.TmuxSession == name {
				source = &s
				break
			}
		}
		if source != nil {
			break
		}
	}
	null := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	sessionName := ""
	if source != nil {
		sessionName = source.TmuxSession
	} else if len(sessions) > 0 {
		sessionName = sessions[0]
	}
	out := jsvalue.NewObject("tabId", nil, "sessionName", null(sessionName), "agentSessionId", nil, "provider", agents.Claude, "cwd", nil)
	if source != nil {
		out.Set("tabId", source.TabID)
		out.Set("agentSessionId", null(source.AgentSessionID))
		if source.Provider != "" {
			out.Set("provider", source.Provider)
		}
	}
	if sessionName != "" {
		out.Set("cwd", null(b.Tmux.SessionCwd(sessionName)))
	}
	return out
}
