package state

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Init prepares the stores at start and after a tmux reset: the workspace list (migrated from the oldest single-layout
// files when there is none), a default layout for a workspace without one, and each layout checked against tmux:
// a missing agent tab's session is started again and a session of the workspace that no tab names becomes a tab.
func (s *Store) Init() error {
	if err := s.InitLayouts(); err != nil {
		return err
	}
	return s.RestoreSessions()
}

// InitLayouts is Init's first half, the files alone: the workspace list is there, made or migrated.
func (s *Store) InitLayouts() error {
	if err := os.MkdirAll(filepath.Join(s.cfg.DataDir, "workspaces"), 0o777); err != nil {
		return err
	}
	s.wsLock.Lock()
	data := s.readWorkspaces()
	if data == nil {
		if exists(filepath.Join(s.cfg.DataDir, "layout.json")) {
			data = s.migrateLayout()
		} else if exists(filepath.Join(s.cfg.DataDir, "tabs.json")) {
			data = s.migrateTabs()
		}
	}
	if data == nil {
		data = s.emptyWorkspaces()
		if err := s.writeWorkspaces(data); err != nil {
			s.wsLock.Unlock()
			return err
		}
	}
	s.wsLock.Unlock()
	return nil
}

// RestoreSessions is Init's second half: each layout checked against tmux. After a reboot it starts every agent tab's
// session again, which takes seconds for a hundred tabs; the runtime's start does the rest of its work first.
func (s *Store) RestoreSessions() error {
	s.wsLock.Lock()
	data := s.readWorkspaces()
	s.wsLock.Unlock()
	list := workspacesOf(data)
	if len(list) == 0 {
		return nil
	}
	sessions := s.cfg.Tmux.ListSessions()
	for _, ws := range list {
		if err := s.checkWorkspace(ws, sessions); err != nil {
			agent.Log("error", "workspace tmux consistency check failed", agent.F{K: "workspace", V: ws.String("id")}, agent.F{K: "error", V: err.Error()})
		}
	}
	return nil
}

// sessionWorkers is how many tmux sessions Init starts at once.
const sessionWorkers = 8

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isAgentPanel(t *jsvalue.Object) bool {
	p, _ := t.Get("panelType")
	return p == "claude-code" || p == "codex-cli"
}

func (s *Store) checkWorkspace(ws *jsvalue.Object, sessions []string) error {
	id := ws.String("id")
	if !ValidWorkspaceID(id) {
		return nil
	}
	firstDir := ""
	if dirs := ws.Array("directories"); len(dirs) > 0 {
		firstDir, _ = dirs[0].(string)
	}
	layout := s.Layout(id)
	if layout == nil {
		created, err := s.defaultLayout(id, firstDir, "")
		if err != nil {
			return err
		}
		m := s.layoutLock(id)
		m.Lock()
		defer m.Unlock()
		return s.writeLayout(id, created)
	}

	names := s.cfg.Tmux.Names()
	var prefixes []string
	for _, p := range []string{names.Prefix, names.LegacyPrefix} {
		if p != "" {
			prefixes = append(prefixes, p+id+"-")
		}
	}
	relevantOf := func(l *jsvalue.Object) []string {
		named := map[string]bool{}
		for _, t := range allTabs(root(l)) {
			named[t.String("sessionName")] = true
		}
		var out []string
		for _, sess := range sessions {
			if named[sess] || hasAnyPrefix(sess, prefixes) {
				out = append(out, sess)
			}
		}
		return out
	}
	present := map[string]bool{}
	for _, sess := range relevantOf(layout) {
		present[sess] = true
	}
	type missing struct{ name, cwd string }
	var gone []missing
	for _, t := range allTabs(root(layout)) {
		if p, _ := t.Get("panelType"); p == webBrowser {
			continue
		}
		if !present[t.String("sessionName")] && isAgentPanel(t) {
			cwd := t.String("cwd")
			if cwd == "" {
				cwd = firstDir
			}
			gone = append(gone, missing{t.String("sessionName"), cwd})
		}
	}
	// After a reboot every tab's session is gone: a hundred of them one by one held the start for seconds. The first
	// starts the server, the rest go sessionWorkers at a time.
	restarted := len(gone) > 0
	if restarted {
		if err := s.cfg.Tmux.CreateSession(gone[0].name, cols, rows, gone[0].cwd); err != nil {
			return err
		}
		errs := make([]error, len(gone))
		sem := make(chan struct{}, sessionWorkers)
		var wg sync.WaitGroup
		for i, m := range gone[1:] {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				errs[i+1] = s.cfg.Tmux.CreateSession(m.name, cols, rows, m.cwd)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}

	_, err := s.locked(id, func(l *jsvalue.Object) bool {
		if l == nil {
			return false
		}
		inLayout := map[string]bool{}
		for _, t := range allTabs(root(l)) {
			if p, _ := t.Get("panelType"); p != webBrowser {
				inLayout[t.String("sessionName")] = true
			}
		}
		var orphans []string
		for _, sess := range relevantOf(l) {
			if !inLayout[sess] {
				orphans = append(orphans, sess)
			}
		}
		if len(orphans) > 0 {
			if ps := panes(root(l)); len(ps) > 0 {
				first := ps[0]
				tabs := tabsOf(first)
				order := float64(-1)
				if len(tabs) > 0 {
					order = maxOrder(tabs)
				}
				arr := first.Array("tabs")
				for _, sess := range orphans {
					order++
					arr = append(arr, jsvalue.NewObject("id", "tab-"+s.cfg.NewID(), "sessionName", sess, "name", "", "order", order))
				}
				first.Set("tabs", arr)
				if active, _ := first.Get("activeTabId"); !jsvalue.Truthy(active) && len(arr) > 0 {
					first.Set("activeTabId", tabsOf(first)[0].String("id"))
				}
			}
		}
		if !restarted && len(orphans) == 0 {
			return false
		}
		if active, _ := l.Get("activePaneId"); jsvalue.Truthy(active) && findPane(root(l), l.String("activePaneId")) == nil {
			if ps := panes(root(l)); len(ps) > 0 {
				l.Set("activePaneId", ps[0].String("id"))
			} else {
				l.Set("activePaneId", nil)
			}
		}
		return true
	})
	return err
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// migrateLayout makes the oldest single layout (`layout.json` at the top) the workspace "default" in the home folder.
func (s *Store) migrateLayout() *jsvalue.Object {
	v, _ := readJSON(filepath.Join(s.cfg.DataDir, "layout.json"))
	legacy, _ := v.(*jsvalue.Object)
	if legacy == nil {
		return nil
	}
	const wsID = "ws-default"
	m := s.layoutLock(wsID)
	m.Lock()
	err := s.writeLayout(wsID, legacy)
	m.Unlock()
	if err != nil {
		return nil
	}
	updated := legacy.String("updatedAt")
	if updated == "" {
		updated = s.now()
	}
	data := jsvalue.NewObject(
		"workspaces", []any{jsvalue.NewObject("id", wsID, "name", "default", "directories", []any{s.cfg.Home})},
		"sidebarCollapsed", false, "sidebarWidth", 240, "updatedAt", updated)
	if s.writeWorkspaces(data) != nil {
		return nil
	}
	agent.Log("info", "single layout migrated to the workspace 'default'")
	return data
}

// migrateTabs turns the even older tab list (`tabs.json`) into a single layout, then migrates that.
func (s *Store) migrateTabs() *jsvalue.Object {
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "tabs.json"))
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	old, _ := v.(*jsvalue.Object)
	if err != nil || old == nil || len(old.Array("tabs")) == 0 {
		return nil
	}
	paneID := "pane-" + s.cfg.NewID()
	active, ok := old.Get("activeTabId")
	if !ok {
		active = nil
	}
	layout := jsvalue.NewObject(
		"root", jsvalue.NewObject("type", "pane", "id", paneID, "tabs", old.Array("tabs"), "activeTabId", active),
		"activePaneId", paneID, "updatedAt", s.now())
	if writeAtomic(filepath.Join(s.cfg.DataDir, "layout.json"), layout) != nil {
		return nil
	}
	return s.migrateLayout()
}
