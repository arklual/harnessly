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
)

// Caps is what a page reads.
type Caps struct {
	Delegation bool
	Workflows  bool
}

// CapsOf reads the caps a page named in its request's address; an unknown one is ignored.
func CapsOf(r *http.Request) Caps {
	var names []string
	for _, v := range r.URL.Query()["caps"] {
		names = append(names, strings.Split(v, ",")...)
	}
	return Caps{Delegation: slices.Contains(names, string(CapDelegation)), Workflows: slices.Contains(names, string(CapWorkflows))}
}

// AllCaps reads every optional part.
var AllCaps = Caps{Delegation: true, Workflows: true}

// olderCaps is every Caps short of AllCaps that a page may have: the forms an event can go out in besides its own.
// Each cap came with a build of the page that read the ones before it.
var olderCaps = []Caps{{}, {Delegation: true}}

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
	return e, changed
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
	return f
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
