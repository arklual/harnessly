package wire

import (
	"net/http"
	"slices"
	"strings"
)

// Cap is an optional part of the frames that a page says it reads. The page decodes strictly and an open tab keeps
// its build after a new runtime ships, so the runtime sends a newer field or value only to a page that asked for it.
// A page asks in the socket's address (?caps=delegation,...), not in hello: a runtime older than the page refuses any
// field of hello it does not know, and ignores a query it does not read. Its HTTP calls that carry such a part name
// the caps the same way.
type Cap string

const (
	// CapDelegation: the page reads Conversation.ParentID and RuntimeSettings.Delegation (README 63).
	CapDelegation Cap = "delegation"
	// CapWorkflows: the page reads the workflow kind of a background task, its run (BackgroundTask.Workflow) and
	// BgWait.Workflows (README 52).
	CapWorkflows Cap = "workflows"
	// CapProfiles: the page reads the backend profiles (README 64): MachineAgent.Profiles and Model.Profile.
	CapProfiles Cap = "profiles"
	// CapMsgStatus: the page reads a message queued stalled (QueuedStalled) and lists it in the queue (README 65).
	CapMsgStatus Cap = "msgstatus"
)

// Caps is what a page reads.
type Caps struct {
	Delegation bool
	Workflows  bool
	Profiles   bool
	MsgStatus  bool
}

// CapsOf reads the caps a page named in its request's address; an unknown one is ignored.
func CapsOf(r *http.Request) Caps {
	var names []string
	for _, v := range r.URL.Query()["caps"] {
		names = append(names, strings.Split(v, ",")...)
	}
	return Caps{Delegation: slices.Contains(names, string(CapDelegation)), Workflows: slices.Contains(names, string(CapWorkflows)),
		Profiles: slices.Contains(names, string(CapProfiles)), MsgStatus: slices.Contains(names, string(CapMsgStatus))}
}

// AllCaps reads every optional part.
var AllCaps = Caps{Delegation: true, Workflows: true, Profiles: true, MsgStatus: true}

// olderCaps is every Caps short of AllCaps that a page may have: the forms an event can go out in besides its own.
// Each cap came with a build of the page that read the ones before it.
var olderCaps = []Caps{{}, {Delegation: true}, {Delegation: true, Workflows: true}, {Delegation: true, Workflows: true, Profiles: true}}

// eventFor is e as a page with caps gets it, and whether it differs from e.
func eventFor(e Event, caps Caps) (Event, bool) {
	changed := false
	if !caps.Delegation {
		if up, ok := e.(ConversationUpsertEv); ok && up.Conv.ParentID != nil {
			up.Conv.ParentID = nil
			e, changed = up, true
		}
	}
	if !caps.Workflows {
		if old, ok := withoutWorkflows(e); ok {
			e, changed = old, true
		}
	}
	if !caps.Profiles {
		if old, ok := withoutProfiles(e); ok {
			e, changed = old, true
		}
	}
	if !caps.MsgStatus {
		if old, ok := withoutStall(e); ok {
			e, changed = old, true
		}
	}
	return e, changed
}

// stalledAsFailed is the state a page without CapMsgStatus holds a stalled message in: the unconfirmed failure it
// got before there was a stall, which its Retry and Delete answer the same way. A stalled state keeps the moment of the
// stall in At (not on the wire for queued).
func stalledAsFailed(s MessageState) (MessageState, bool) {
	if s.K != MsgQueued || s.Reason != QueuedStalled {
		return s, false
	}
	return MessageState{K: MsgFailed, Code: MsgFailedUnconfirmed, Retryable: true, At: s.At}, true
}

// messageWithoutStall is m as a page without CapMsgStatus reads it.
func messageWithoutStall(m Message) Message {
	if st, ok := stalledAsFailed(m.State); ok {
		m.State = st
	}
	return m
}

// queueWithoutStall is q without its stalled items; q's own slice is not changed.
func queueWithoutStall(q Queue) (Queue, bool) {
	if len(q.Stalled) == 0 {
		return q, false
	}
	q.Items = slices.DeleteFunc(slices.Clone(q.Items), func(id string) bool { return slices.Contains(q.Stalled, id) })
	return q, true
}

// withoutStall is e as a page that does not read a stall gets it, and whether it differs from e: a stalled message is
// failed{unconfirmed} there (same rev), and the queue does not list it.
func withoutStall(e Event) (Event, bool) {
	switch e := e.(type) {
	case PromptStateEv:
		if st, ok := stalledAsFailed(e.State); ok {
			e.State = st
			return e, true
		}
	case MessageUpsertEv:
		if _, ok := stalledAsFailed(e.Message.State); ok {
			return MessageUpsertEv{Message: messageWithoutStall(e.Message)}, true
		}
	case QueueChangedEv:
		if q, ok := queueWithoutStall(e.Queue); ok {
			return QueueChangedEv{Queue: q}, true
		}
	}
	return e, false
}

// withoutProfiles is e as a page that does not read profiles gets it, and whether it differs from e: the profile of a
// conversation's model and the profiles a machine offers are left out (such a page draws a Kimi chat as a Claude one
// on a model it does not know, and offers no profile to start).
func withoutProfiles(e Event) (Event, bool) {
	switch e := e.(type) {
	case MachineStatusEv:
		if agents, changed := agentsWithoutProfiles(e.Agents); changed {
			e.Agents = agents
			return e, true
		}
	case ConversationUpsertEv:
		if conv, changed := convWithoutProfile(e.Conv); changed {
			return ConversationUpsertEv{Conv: conv}, true
		}
	}
	return e, false
}

// convWithoutProfile is c without the profile of any model it names; c's own slices are not changed.
func convWithoutProfile(c Conversation) (Conversation, bool) {
	changed := false
	if m, ok := modelWithoutProfile(c.Model); ok {
		c.Model, changed = m, true
	}
	if o := c.Origin; o != nil {
		if m, ok := modelWithoutProfile(o.FromModel); ok {
			cp := *o
			cp.FromModel = m
			c.Origin, changed = &cp, true
		}
	}
	if slices.ContainsFunc(c.ModelLog, func(s ModelSwitch) bool { return s.To.Profile != "" || (s.From != nil && s.From.Profile != "") }) {
		log := slices.Clone(c.ModelLog)
		for i := range log {
			log[i].From, _ = modelWithoutProfile(log[i].From)
			log[i].To.Profile = ""
		}
		c.ModelLog, changed = log, true
	}
	return c, changed
}

// withoutWorkflows is e as a page that does not read workflows gets it, and whether it differs from e. Such a page
// never learned of a workflow: its upsert is a removal of an id it does not hold (the seq stays without a gap), and
// a conversation no longer counts the workflows it waits for.
func withoutWorkflows(e Event) (Event, bool) {
	switch e := e.(type) {
	case BackgroundUpsertEv:
		if e.Task.Kind == BgWorkflow {
			return BackgroundRemovedEv{ID: e.Task.ID}, true
		}
	case ConversationUpsertEv:
		if conv, changed := convWithoutWorkflows(e.Conv); changed {
			return ConversationUpsertEv{Conv: conv}, true
		}
	}
	return e, false
}

func convWithoutWorkflows(c Conversation) (Conversation, bool) {
	if c.BgWait == nil || c.BgWait.Workflows == 0 {
		return c, false
	}
	w := *c.BgWait
	c.BgAgents -= w.Workflows
	w.Workflows = 0
	c.BgWait = &w
	if w.Total() == 0 {
		c.BgWait = nil
	}
	return c, true
}

// snapshotFor is f as a page with caps gets it; f itself is never changed.
func snapshotFor(f SnapshotFrame, caps Caps) SnapshotFrame {
	if !caps.Delegation && f.Convs != nil && slices.ContainsFunc(f.Convs.Conversations, delegated) {
		cs := *f.Convs
		cs.Conversations = slices.Clone(cs.Conversations)
		for i := range cs.Conversations {
			cs.Conversations[i].ParentID = nil
		}
		f.Convs = &cs
	}
	if !caps.Workflows {
		if f.Convs != nil {
			cs := *f.Convs
			cs.Conversations = slices.Clone(cs.Conversations)
			for i := range cs.Conversations {
				cs.Conversations[i], _ = convWithoutWorkflows(cs.Conversations[i])
			}
			f.Convs = &cs
		}
		if f.Conv != nil && slices.ContainsFunc(f.Conv.Background, isWorkflowTask) {
			cv := *f.Conv
			cv.Background = slices.DeleteFunc(slices.Clone(cv.Background), isWorkflowTask)
			f.Conv = &cv
		}
	}
	if !caps.Profiles {
		if f.Convs != nil && slices.ContainsFunc(f.Convs.Conversations, func(c Conversation) bool { _, changed := convWithoutProfile(c); return changed }) {
			cs := *f.Convs
			cs.Conversations = slices.Clone(cs.Conversations)
			for i := range cs.Conversations {
				cs.Conversations[i], _ = convWithoutProfile(cs.Conversations[i])
			}
			f.Convs = &cs
		}
		if f.Machine != nil {
			if agents, changed := agentsWithoutProfiles(f.Machine.Machine.Agents); changed {
				m := *f.Machine
				m.Machine.Agents = agents
				f.Machine = &m
			}
		}
	}
	if !caps.MsgStatus && f.Conv != nil {
		q, cut := queueWithoutStall(f.Conv.Queue)
		stalled := slices.ContainsFunc(f.Conv.Messages, func(m Message) bool { _, ok := stalledAsFailed(m.State); return ok })
		if cut || stalled {
			cv := *f.Conv
			cv.Queue = q
			cv.Messages = slices.Clone(cv.Messages)
			for i := range cv.Messages {
				cv.Messages[i] = messageWithoutStall(cv.Messages[i])
			}
			f.Conv = &cv
		}
	}
	return f
}

// MachineFor is m as a page with caps gets it; m itself is never changed.
func MachineFor(m Machine, caps Caps) Machine {
	if !caps.Profiles {
		m.Agents, _ = agentsWithoutProfiles(m.Agents)
	}
	return m
}

func isWorkflowTask(t BackgroundTask) bool { return t.Kind == BgWorkflow }

func delegated(c Conversation) bool { return c.ParentID != nil }

// settingsFor is s as a page with caps reads it.
func settingsFor(s RuntimeSettings, caps Caps) RuntimeSettings {
	if !caps.Delegation {
		s.Delegation = nil
	}
	return s
}
