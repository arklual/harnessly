package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// AgentFrame identifies a flattened child session without exposing the shared connection.
// Parent is empty when the frame belongs directly to the tab session.
type AgentFrame struct{ Session, Frame, Parent string }
type agentFrameBinding struct {
	AgentFrame
	tab  string
	conn *cdp.Conn
}

// AgentEvent is a tab-scoped event. Session is empty for the tab itself.
type AgentEvent struct {
	Method, Session string
	Params          json.RawMessage
}
type agentListener struct {
	conv, tab string
	events    chan AgentEvent
}

// AgentEvents observes only an owned tab; callers must unsubscribe when the tab is no longer used.
// A bounded queue deliberately drops excess events so tools cannot stall the CDP reader.
func (i *Instance) AgentEvents(ctx context.Context, conv, id string) (<-chan AgentEvent, func(), error) {
	if _, err := i.AgentCall(ctx, conv, id, "Page.enable", nil); err != nil {
		return nil, nil, err
	}
	l := &agentListener{conv: conv, tab: id, events: make(chan AgentEvent, 1024)}
	i.mu.Lock()
	if i.agentListeners == nil {
		i.agentListeners = make(map[*agentListener]bool)
	}
	i.agentListeners[l] = true
	i.mu.Unlock()
	return l.events, func() { i.mu.Lock(); delete(i.agentListeners, l); i.mu.Unlock() }, nil
}

// AgentFrames enables flattened iframe attachment only for a tab used by an agent.
func (i *Instance) AgentFrames(ctx context.Context, conv, id string) ([]AgentFrame, error) {
	if _, err := i.AgentCall(ctx, conv, id, "Target.setAutoAttach", map[string]any{
		"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true,
	}); err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var out []AgentFrame
	for _, f := range i.agentFrames {
		if f.tab == id && f.conn == i.conn {
			out = append(out, f.AgentFrame)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Frame < out[b].Frame })
	return out, nil
}

// AgentFrameCall enforces the same owner, control and dialog checks as AgentCall,
// plus membership of the child session in that tab's current connection.
func (i *Instance) AgentFrameCall(ctx context.Context, conv, id, session, method string, params any) (json.RawMessage, error) {
	if session == "" {
		return i.AgentCall(ctx, conv, id, method, params)
	}
	if err := i.beginOperation(); err != nil {
		return nil, err
	}
	defer i.tasks.Done()
	if err := i.agentAllowed(conv, id, method); err != nil {
		return nil, err
	}
	i.mu.Lock()
	f, ok := i.agentFrames[session]
	c := i.conn
	i.mu.Unlock()
	if !ok || f.tab != id || f.conn != c {
		return nil, errors.New("frame is no longer available; refresh the snapshot")
	}
	if err := i.agentAllowed(conv, id, method); err != nil {
		return nil, err
	}
	return c.Call(ctx, method, params, session)
}

func (i *Instance) agentEvent(method, session string, raw json.RawMessage) {
	i.mu.Lock()
	defer i.mu.Unlock()
	tab, child := "", session
	for id, p := range i.pages {
		if p.binding.session == session && p.binding.conn == i.conn {
			tab, child = id, ""
			break
		}
	}
	if f, ok := i.agentFrames[session]; ok && f.conn == i.conn {
		tab = f.tab
	}
	if method == "Target.attachedToTarget" && tab != "" {
		var e struct {
			Session string `json:"sessionId"`
			Target  target `json:"targetInfo"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Target.Type == "iframe" {
			if i.agentFrames == nil {
				i.agentFrames = make(map[string]agentFrameBinding)
			}
			i.agentFrames[e.Session] = agentFrameBinding{AgentFrame{e.Session, e.Target.ID, child}, tab, i.conn}
		}
	}
	if method == "Target.detachedFromTarget" {
		var e struct {
			Session string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &e) == nil {
			delete(i.agentFrames, e.Session)
		}
	}
	p := i.pages[tab]
	if p == nil {
		return
	}
	// Child dialogs block the whole tab, so the ordinary agent guard must see them too.
	if child != "" && method == "Page.javascriptDialogOpening" {
		var d Dialog
		if json.Unmarshal(raw, &d) == nil {
			p.Dialog = &d
			i.notifyLocked()
		}
	}
	if child != "" && method == "Page.javascriptDialogClosed" {
		p.Dialog = nil
		i.notifyLocked()
	}
	for l := range i.agentListeners {
		if l.tab == tab && l.conv == p.Conversation {
			select {
			case l.events <- AgentEvent{method, child, raw}:
			default:
			}
		}
	}
}

// AgentCheck allows tools to validate metadata operations without sending a command
// into a renderer that may already be blocked by a modal dialog.
func (i *Instance) AgentCheck(conv, id, method string) error { return i.agentAllowed(conv, id, method) }
