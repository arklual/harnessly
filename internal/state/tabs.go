package state

import (
	"path"
	"regexp"
	"strings"

	"harnessly/internal/jsvalue"
)

// The tab fields the status keeper writes, found by the tab's session name. When the cluster owns the state such a
// change goes to it as the fields it set and removed, so it never overwrites what the cluster changed meanwhile; the
// disk gets it at once, marked dirty when the cluster could not take it.

func (s *Store) mutateTab(sessionName string, fn func(tab *jsvalue.Object) bool) error {
	wsID, _, _, ok := s.cfg.Tmux.ParseSessionName(sessionName)
	if !ok || !ValidWorkspaceID(wsID) {
		return nil
	}
	m := s.layoutLock(wsID)
	m.Lock()
	defer m.Unlock()
	layout := s.readLayout(wsID)
	tab := tabBySession(layout, sessionName)
	if layout == nil || tab == nil {
		return nil
	}
	before := jsvalue.Clone(tab).(*jsvalue.Object)
	if !fn(tab) {
		return nil
	}
	layout.Set("updatedAt", s.now())
	return s.writeTabChange(wsID, layout, sessionName, before, tab)
}

func (s *Store) writeTabChange(wsID string, layout *jsvalue.Object, sessionName string, before, after *jsvalue.Object) error {
	if !s.owned() {
		return s.writeLayout(wsID, layout)
	}
	if set, unset := tabStateDiff(before, after); set != nil && !s.patch(layoutName(wsID), sessionName, set, unset) {
		layout.Set("stateDirty", true)
	}
	return s.writeLayoutLocal(wsID, layout)
}

// Provider names the agent CLI whose conversation a tab holds.
type Provider string

const (
	Claude Provider = "claude"
	Codex  Provider = "codex"
)

// The conversation fields: Claude's are also kept under the older top-level keys.
var legacyKey = map[string]string{"sessionId": "claudeSessionId", "jsonlPath": "claudeJsonlPath", "summary": "claudeSummary"}

func nullable(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return nil
}

func (p Provider) state(tab *jsvalue.Object) *jsvalue.Object {
	st := tab.Object("agentState")
	if st != nil {
		if id, _ := st.Get("providerId"); id == string(p) {
			return st
		}
	}
	return nil
}

func (p Provider) ensureState(tab *jsvalue.Object) *jsvalue.Object {
	if st := p.state(tab); st != nil {
		return st
	}
	seeded := jsvalue.NewObject("providerId", string(p), "sessionId", nil, "jsonlPath", nil, "summary", nil)
	if p == Claude {
		for _, f := range []string{"sessionId", "jsonlPath", "summary"} {
			v, _ := tab.Get(legacyKey[f])
			seeded.Set(f, nullable(v))
		}
	}
	tab.Set("agentState", seeded)
	return seeded
}

// read is the stored value of a conversation field; defined is false for a Claude field its agentState lacks, which
// differs from every value, null included.
func (p Provider) read(tab *jsvalue.Object, field string) (v any, defined bool) {
	st := p.state(tab)
	if p == Codex {
		if st == nil {
			return nil, true
		}
		v, _ = st.Get(field)
		return v, true
	}
	if st != nil {
		v, has := st.Get(field)
		if !has {
			return nil, false
		}
		if v != nil {
			return v, true
		}
	}
	v, _ = tab.Get(legacyKey[field])
	return v, true
}

func (p Provider) write(tab *jsvalue.Object, field string, value any) {
	p.ensureState(tab).Set(field, value)
	if p == Claude {
		tab.Set(legacyKey[field], value)
	}
}

var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// readField reads a field; a Claude tab that lost its id but kept its transcript path still knows its session, the
// transcript's file name.
func (p Provider) readField(tab *jsvalue.Object, field string) (any, bool) {
	v, defined := p.read(tab, field)
	if field != "sessionId" || p != Claude || v != nil {
		return v, defined
	}
	jsonl, _ := p.read(tab, "jsonlPath")
	if s, _ := jsonl.(string); s != "" {
		if id := strings.TrimSuffix(path.Base(s), ".jsonl"); uuidRe.MatchString(id) {
			return id, true
		}
	}
	return nil, true
}

// AgentState is a conversation update; a nil field is left alone, a pointer to nil stores null.
type AgentState struct {
	SessionID, JsonlPath, Summary, LastUserMessage *any
}

// Value is a field of AgentState: a string, or nil for null.
func Value(v any) *any { return &v }

// UpdateTabAgentState stores what changed of a tab's conversation and its last prompt.
func (s *Store) UpdateTabAgentState(sessionName string, p Provider, st AgentState) error {
	return s.mutateTab(sessionName, func(tab *jsvalue.Object) bool {
		changed := false
		for _, f := range []struct {
			field string
			v     *any
		}{{"sessionId", st.SessionID}, {"jsonlPath", st.JsonlPath}, {"summary", st.Summary}} {
			if f.v == nil {
				continue
			}
			if cur, defined := p.readField(tab, f.field); !defined || !jsvalue.SameValue(cur, *f.v) {
				p.write(tab, f.field, *f.v)
				changed = true
			}
		}
		if st.LastUserMessage != nil {
			cur, has := tab.Get("lastUserMessage")
			if !has || !jsvalue.SameValue(cur, *st.LastUserMessage) {
				tab.Set("lastUserMessage", *st.LastUserMessage)
				changed = true
			}
		}
		return changed
	})
}

// UpdateTabAgentSessionID stores a tab's conversation id (nil clears it).
func (s *Store) UpdateTabAgentSessionID(sessionName string, p Provider, sessionID any) error {
	return s.UpdateTabAgentState(sessionName, p, AgentState{SessionID: &sessionID})
}

// UpdateTabAgentSummary stores a tab's conversation summary.
func (s *Store) UpdateTabAgentSummary(sessionName string, p Provider, summary any) error {
	return s.UpdateTabAgentState(sessionName, p, AgentState{Summary: &summary})
}

// StoreRecoveredAgentSession stores a session found on disk. The stored id itself is compared: a Claude tab reads its
// id back from the transcript path, so a tab whose id is null would look up to date.
func (s *Store) StoreRecoveredAgentSession(sessionName string, p Provider, sessionID, jsonlPath string) error {
	return s.mutateTab(sessionName, func(tab *jsvalue.Object) bool {
		var stored any
		if st := tab.Object("agentState"); st != nil {
			stored, _ = st.Get("sessionId")
		}
		if stored == nil {
			stored, _ = tab.Get("claudeSessionId")
		}
		cur, _ := p.read(tab, "jsonlPath")
		if stored == sessionID && cur == jsonlPath {
			return false
		}
		p.write(tab, "sessionId", sessionID)
		p.write(tab, "jsonlPath", jsonlPath)
		return true
	})
}

// UpdateTabLastUserMessage stores the tab's last prompt (nil stores null).
func (s *Store) UpdateTabLastUserMessage(sessionName string, message any) error {
	return s.UpdateTabAgentState(sessionName, "", AgentState{LastUserMessage: &message})
}

// UpdateTabCliStatus persists the tab's agent state, when it was dismissed (nil: absent) and its last hook event
// (stored as null when absent).
func (s *Store) UpdateTabCliStatus(sessionName string, cliState string, dismissedAt *any, lastEventName any) error {
	return s.mutateTab(sessionName, func(tab *jsvalue.Object) bool {
		curState, _ := tab.Get("cliState")
		curDismissed, hasDismissed := tab.Get("dismissedAt")
		curEvent, _ := tab.Get("lastEventName")
		sameDismissed := (dismissedAt == nil && !hasDismissed) || (dismissedAt != nil && hasDismissed && jsvalue.SameValue(curDismissed, *dismissedAt))
		if curState == cliState && sameDismissed && jsvalue.SameValue(curEvent, lastEventName) {
			return false
		}
		tab.Set("cliState", cliState)
		if dismissedAt == nil {
			tab.Delete("dismissedAt")
		} else {
			tab.Set("dismissedAt", *dismissedAt)
		}
		tab.Set("lastEventName", lastEventName)
		return true
	})
}

// The metadata keys a merge may set.
var metaKeys = []string{"launch", "mode"}

// MergeMeta merges a patch into a tab's metadata: each patch key with a value replaces the stored one, keys outside the
// known ones are ignored and stored keys the patch does not name stay. changed is false when nothing differs.
func MergeMeta(current, patch *jsvalue.Object) (next *jsvalue.Object, changed bool) {
	next = jsvalue.NewObject()
	if current != nil {
		next.Assign(current)
	}
	for _, k := range metaKeys {
		v, ok := patch.Get(k)
		if !ok {
			continue
		}
		cur, has := next.Get(k)
		if has && metaSame(cur, v) {
			continue
		}
		next.Set(k, v)
		changed = true
	}
	return next, changed
}

// UpdateTabMeta merges a patch into the platform metadata of the tab running sessionName and answers the stored
// metadata; nil when that tab does not exist.
func (s *Store) UpdateTabMeta(sessionName string, patch *jsvalue.Object) (*jsvalue.Object, error) {
	var stored *jsvalue.Object
	err := s.mutateTab(sessionName, func(tab *jsvalue.Object) bool {
		next, changed := MergeMeta(tab.Object(s.cfg.MetaKey), patch)
		stored = next
		if !changed {
			return false
		}
		tab.Set(s.cfg.MetaKey, next)
		return true
	})
	return stored, err
}

// metaSame is equality for metadata values: plain objects by their keys and values, anything else by identity.
func metaSame(a, b any) bool {
	x, ok1 := a.(*jsvalue.Object)
	y, ok2 := b.(*jsvalue.Object)
	if !ok1 || !ok2 {
		switch a.(type) {
		case []any, *jsvalue.Object:
			return false
		}
		return a == b
	}
	if x.Len() != y.Len() {
		return false
	}
	for _, k := range x.Keys() {
		xv, _ := x.Get(k)
		yv, has := y.Get(k)
		if !has || !metaSame(xv, yv) {
			return false
		}
	}
	return true
}

// SessionID is the conversation id a tab of this provider holds, nil when none.
func (p Provider) SessionID(tab *jsvalue.Object) any {
	v, _ := p.readField(tab, "sessionId")
	return v
}

// ProviderOfPanel is the provider of a tab's panel type, "" for a panel that runs no agent.
func ProviderOfPanel(panelType any) Provider {
	switch panelType {
	case "claude-code":
		return Claude
	case "codex-cli":
		return Codex
	}
	return ""
}

// MetaKey is the tab field holding the platform's metadata.
func (s *Store) MetaKey() string { return s.cfg.MetaKey }

// WriteSessionID stores a conversation id in a tab object the caller holds (not on disk).
func (p Provider) WriteSessionID(tab *jsvalue.Object, sessionID any) {
	p.write(tab, "sessionId", sessionID)
}

// TabLaunch is the launch choice a tab's metadata remembers: an object of a nullable-string model and effort, nil
// for none (or a malformed one).
func (s *Store) TabLaunch(tab *jsvalue.Object) *jsvalue.Object {
	l := tab.Object(s.cfg.MetaKey).Object("launch")
	if l == nil {
		return nil
	}
	m, _ := l.Get("model")
	e, _ := l.Get("effort")
	_, ms := m.(string)
	_, es := e.(string)
	if !l.Has("model") || !l.Has("effort") || (m != nil && !ms) || (e != nil && !es) {
		return nil
	}
	return jsvalue.NewObject("model", m, "effort", e)
}
