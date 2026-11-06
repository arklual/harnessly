package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

type nodeRef struct {
	backend int
	session string
	frame   string
	epoch   uint64
}
type tabState struct {
	mask             func(string) string
	register         func(string)
	sealed           []sealedField
	mu               sync.Mutex
	refs             map[string]nodeRef
	next             uint64
	epochs           map[string]uint64
	parents          map[string]string
	snapshot         []snapshotLine
	interactive      bool
	passwords        []string
	console, network []string
	requests         map[string]string
	pending          map[string]bool
	loading          map[string]bool
	last             time.Time
	cancel           context.CancelFunc
	unsubscribe      func()
}

// Close stops all event consumers without stopping the persistent browser hosts.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, st := range s.tabs {
		st.cancel()
		st.unsubscribe()
	}
	s.tabs = nil
}
func (s *Service) state(ctx context.Context, i *agentbrowser.Instance, conv, id string) (*tabState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("browser tools are closed")
	}
	key := i.Profile().ID + "/" + conv + "/" + id
	if st := s.tabs[key]; st != nil {
		return st, nil
	}
	events, unsubscribe, err := i.AgentEvents(ctx, conv, id)
	if err != nil {
		return nil, err
	}
	changes, stopChanges := i.Changes()
	stopEvents := unsubscribe
	unsubscribe = func() { stopEvents(); stopChanges() }
	life, cancel := context.WithCancel(context.Background())
	st := &tabState{mask: i.Redact, register: i.RememberSecret, refs: map[string]nodeRef{},
		requests: map[string]string{}, pending: map[string]bool{}, loading: map[string]bool{},
		last: time.Now(), cancel: cancel, unsubscribe: unsubscribe}
	if s.tabs == nil {
		s.tabs = map[string]*tabState{}
	}
	s.tabs[key] = st
	go func() {
		for {
			select {
			case <-life.Done():
				return
			case <-changes:
				state, _ := i.State()
				exists := false
				for _, tab := range i.Tabs(conv) {
					if tab.ID == id {
						exists = true
						break
					}
				}
				if !exists || state != "up" {
					s.forget(i, conv, id)
					return
				}
			case e := <-events:
				st.event(e)
			}
		}
	}()
	for _, method := range []string{"Runtime.enable", "Network.enable", "DOM.enable"} {
		if _, err = i.AgentCall(ctx, conv, id, method, nil); err != nil {
			delete(s.tabs, key)
			cancel()
			unsubscribe()
			return nil, err
		}
	}
	return st, nil
}
func ringPush(entries []string, s string) []string {
	if len(entries) == 500 {
		copy(entries, entries[1:])
		entries = entries[:499]
	}
	return append(entries, clip(s, 4096))
}
func (st *tabState) event(e agentbrowser.AgentEvent) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var v map[string]any
	if json.Unmarshal(e.Params, &v) != nil {
		return
	}
	switch e.Method {
	case "Page.frameNavigated":
		frame, _ := v["frame"].(map[string]any)
		id, _ := frame["id"].(string)
		parent, _ := frame["parentId"].(string)
		if st.parents == nil {
			st.parents = map[string]string{}
		}
		if parent != "" {
			st.parents[id] = parent
		}
		st.invalidateFrame(id, e.Session == "" && parent == "")
		st.last = time.Now()
	case "Page.frameDetached":
		id, _ := v["frameId"].(string)
		st.invalidateFrame(id, false)
	case "Page.frameStartedLoading", "Page.frameStoppedLoading":
		frame, _ := v["frameId"].(string)
		if st.loading == nil {
			st.loading = map[string]bool{}
		}
		if e.Method == "Page.frameStartedLoading" {
			st.loading[e.Session+"/"+frame] = true
		} else {
			delete(st.loading, e.Session+"/"+frame)
		}
		st.last = time.Now()
	case "Page.loadEventFired":
		st.last = time.Now()
	case "Runtime.consoleAPICalled":
		// Remote object previews can contain arbitrary DOM properties; retain only scalar arguments.
		values := []any{}
		if args, ok := v["args"].([]any); ok {
			for _, arg := range args {
				if obj, ok := arg.(map[string]any); ok {
					values = append(values, obj["value"])
				}
			}
		}
		st.console = ringPush(st.console, st.redactLocked(jsonText(map[string]any{"type": v["type"], "args": values})))
	case "Runtime.exceptionThrown":
		st.console = ringPush(st.console, "JavaScript exception (use chrome_evaluate to inspect explicitly)")
	case "Network.requestWillBeSent", "Network.responseReceived":
		request, _ := v["requestId"].(string)
		key := e.Session + "/" + request
		st.last = time.Now()
		if e.Method == "Network.requestWillBeSent" {
			st.pending[key] = true
		}
		st.requests[request] = e.Session
		if len(st.requests) > 500 {
			st.requests = map[string]string{request: e.Session}
		}
		part, _ := v["request"].(map[string]any)
		if part == nil {
			part, _ = v["response"].(map[string]any)
		}
		entry := map[string]any{"request_id": request, "event": e.Method}
		for _, k := range []string{"url", "method", "status", "mimeType"} {
			if val, ok := part[k]; ok {
				entry[k] = val
			}
		}
		headers := map[string]any{}
		if h, ok := part["headers"].(map[string]any); ok {
			for k, val := range h {
				switch strings.ToLower(k) {
				case "authorization", "cookie", "set-cookie":
					headers[k] = "[redacted]"
				default:
					headers[k] = val
				}
			}
		}
		entry["headers"] = headers
		st.network = ringPush(st.network, st.redactLocked(jsonText(entry)))
	case "Network.loadingFinished", "Network.loadingFailed":
		request, _ := v["requestId"].(string)
		delete(st.pending, e.Session+"/"+request)
		st.last = time.Now()
	}
}
func (st *tabState) redactLocked(s string) string {
	if st.mask != nil {
		return st.mask(s)
	}
	return secrets.Mask(s, st.passwords)
}

func (st *tabState) redact(s string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.redactLocked(s)
}
func (st *tabState) remember(p string) {
	if p == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, old := range st.passwords {
		if old == p {
			return
		}
	}
	st.passwords = append(st.passwords, p)
	if st.register != nil {
		st.register(p)
	}
}

func (t Tools) recording(ctx context.Context, i *agentbrowser.Instance, st *tabState, name string, a arguments) (*jsvalue.Object, error) {
	if name == "chrome_network" && a.RequestID != "" {
		st.mu.Lock()
		session, ok := st.requests[a.RequestID]
		st.mu.Unlock()
		if !ok {
			return nil, errors.New("Unknown or expired request_id")
		}
		raw, err := i.AgentFrameCall(ctx, t.Caller, a.Tab, session, "Network.getResponseBody", map[string]any{"requestId": a.RequestID})
		if err != nil {
			return nil, err
		}
		return textResult(st.redact(string(raw))), nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	entries := st.console
	if name == "chrome_network" {
		entries = st.network
	}
	out := st.redactLocked(strings.Join(entries, "\n"))
	if a.Clear {
		if name == "chrome_network" {
			st.network = nil
		} else {
			st.console = nil
		}
	}
	return textResult(out), nil
}
func (t Tools) settle(ctx context.Context, i *agentbrowser.Instance, st *tabState, id string, before []agentbrowser.Tab) string {
	limit := time.NewTimer(3 * time.Second)
	defer limit.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	for {
		tabs := i.Tabs(t.Caller)
		for _, tab := range tabs {
			if tab.ID == id && tab.Dialog != nil {
				return t.errorText(i, id, agentbrowser.ErrDialogOpen)
			}
		}
		st.mu.Lock()
		quiet := len(st.pending) == 0 && len(st.loading) == 0 && time.Since(st.last) >= 500*time.Millisecond
		st.mu.Unlock()
		if quiet && time.Since(start) >= 500*time.Millisecond {
			break
		}
		select {
		case <-ctx.Done():
			return "Action dispatched; waiting ended at deadline."
		case <-limit.C:
			return t.changes(i, id, before) + "\nPage still busy after 3 seconds."
		case <-tick.C:
		}
	}
	return t.changes(i, id, before)
}
func (t Tools) changes(i *agentbrowser.Instance, id string, before []agentbrowser.Tab) string {
	seen := map[string]bool{}
	for _, tab := range before {
		seen[tab.ID] = true
	}
	var lines []string
	for _, tab := range i.Tabs(t.Caller) {
		if tab.ID == id {
			lines = append(lines, fmt.Sprintf("Tab %s URL: %s", id, tab.URL))
		}
		if before != nil && !seen[tab.ID] {
			if i.SetAgentTab(t.Caller, tab.ID) == nil {
				lines = append(lines, "New working tab: "+tab.ID+" URL: "+tab.URL)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Service) forget(i *agentbrowser.Instance, conv, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := i.Profile().ID + "/" + conv + "/" + id
	if st := s.tabs[key]; st != nil {
		st.cancel()
		st.unsubscribe()
		delete(s.tabs, key)
	}
}

// DOM.documentUpdated and executionContextsCleared lack frame identity. A real navigation
// carries frameNavigated; detached DOM nodes are also rejected by resolve's isConnected check.
func (st *tabState) invalidateFrame(id string, main bool) {
	if st.epochs == nil {
		st.epochs = map[string]uint64{}
	}
	affected := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for child, parent := range st.parents {
			if (main || affected[parent]) && !affected[child] {
				affected[child] = true
				changed = true
			}
		}
	}
	if main {
		for frame := range st.epochs {
			affected[frame] = true
		}
	}
	for frame := range affected {
		st.epochs[frame]++
	}
	for ref, node := range st.refs {
		if main || affected[node.frame] {
			delete(st.refs, ref)
		}
	}
	// A child navigation must not interrupt pagination of the unchanged main document.
	// Its cached rows are skipped by snapshot; only a top-level navigation discards the whole snapshot.
	if main {
		st.snapshot = nil
	}
}
