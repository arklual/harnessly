package launch

import (
	"sync"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

var listedPanels = map[string]bool{"claude-code": true, "codex-cli": true, "terminal": true}

const listConcurrency = 8

// conversationMeta is the well-formed part of a tab's platform metadata: the launch and the mode, each on its own.
func conversationMeta(raw any) *jsvalue.Object {
	out := jsvalue.NewObject()
	o, ok := raw.(*jsvalue.Object)
	if !ok {
		return out
	}
	if l := o.Object("launch"); l != nil {
		m, hasM := l.Get("model")
		e, hasE := l.Get("effort")
		_, ms := m.(string)
		_, es := e.(string)
		if hasM && hasE && (m == nil || ms) && (e == nil || es) {
			out.Set("launch", jsvalue.NewObject("model", m, "effort", e))
		}
	}
	if mode, _ := o.Get("mode"); mode == "automation" {
		out.Set("mode", mode)
	}
	return out
}

func providerOfTab(panelType any, name string) string {
	switch panelType {
	case "claude-code":
		return "claude"
	case "codex-cli":
		return "codex"
	}
	if name == "Grok" {
		return "grok"
	}
	return "terminal"
}

// orJS is a || b over JSON values.
func orJS(vs ...any) any {
	for _, v := range vs[:len(vs)-1] {
		if jscompat.Truthy(v) {
			return v
		}
	}
	return vs[len(vs)-1]
}

// Conversations lists the agent and terminal conversations of every workspace: titles from the transcripts, ordered by
// the person's last prompt or, without one, the tmux session's birth. created reads a session's start (epoch s, 0 when
// unknown).
func (s *Starter) Conversations(h *Headlines, created func(session string) int64) *jsvalue.Object {
	view := s.cfg.Store.Workspaces()
	workspaces, _ := view.Get("workspaces")
	type item struct{ ws, tab *jsvalue.Object }
	var listed []item
	for _, wt := range s.cfg.Store.WorkspaceTabs() {
		pt, has := wt.Tab.Get("panelType")
		key := "terminal"
		if has && pt != nil {
			key = jscompat.String(pt)
		}
		if listedPanels[key] {
			listed = append(listed, item{wt.Workspace, wt.Tab})
		}
	}
	out := make([]any, len(listed))
	sem := make(chan struct{}, listConcurrency)
	var wg sync.WaitGroup
	for i, it := range listed {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			out[i] = s.conversation(it.ws, it.tab, h, created)
		}()
	}
	wg.Wait()
	return jsvalue.NewObject("conversations", out, "workspaces", workspaces)
}

func (s *Starter) conversation(ws, tab *jsvalue.Object, h *Headlines, created func(string) int64) *jsvalue.Object {
	state := tab.Object("agentState")
	var jsonl any
	if state != nil {
		jsonl, _ = state.Get("jsonlPath")
	}
	if jsonl == nil {
		jsonl, _ = tab.Get("claudeJsonlPath")
	}
	var head Headline
	if p, ok := jsonl.(string); ok && p != "" {
		head = h.Read(p)
	}
	activity := 0.0
	if head.LastPromptAt != nil {
		activity = *head.LastPromptAt
	} else if c := created(tab.String("sessionName")); c != 0 {
		activity = float64(c) * 1000
	}
	pt, _ := tab.Get("panelType")
	var aiTitle any
	if pt == "claude-code" && head.Title != nil {
		aiTitle = *head.Title
	}
	var summary any
	if state != nil {
		summary, _ = state.Get("summary")
	}
	last, _ := tab.Get("lastUserMessage")
	title, _ := tab.Get("title")
	name, _ := tab.Get("name")
	wsID, _ := ws.Get("id")
	wsName, _ := ws.Get("name")
	id, _ := tab.Get("id")
	c := jsvalue.NewObject("id", id, "title", orJS(aiTitle, summary, last, title, name),
		"provider", providerOfTab(pt, tab.String("name")), "workspaceId", wsID, "workspaceName", wsName, "updatedAt", activity)
	if jscompat.Truthy(jsonl) {
		c.Set("jsonlPath", jsonl)
	}
	meta, _ := tab.Get(s.cfg.Store.MetaKey())
	c.Assign(conversationMeta(meta))
	return c
}
