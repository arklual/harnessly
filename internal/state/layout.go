package state

import (
	"math"
	"sort"
	"strings"
	"sync"

	"harnessly/internal/jsvalue"
)

// Every read-modify-write of a layout holds that workspace's lock; tmux is started and stopped outside it, and the
// layout is read again after, so a slow tmux never holds up another workspace or another write.

const (
	cols, rows = 80, 24
)

func (s *Store) newTab(wsID, paneID, tabID, name string, order float64, cwd string) *jsvalue.Object {
	tab := jsvalue.NewObject("id", tabID, "sessionName", s.cfg.Tmux.SessionName(wsID, paneID, tabID), "name", name, "order", order)
	if cwd != "" {
		tab.Set("cwd", cwd)
	}
	return tab
}

func newPane(paneID string, tab *jsvalue.Object) *jsvalue.Object {
	return jsvalue.NewObject("type", "pane", "id", paneID, "tabs", []any{tab}, "activeTabId", tab.String("id"))
}

// defaultLayout is one pane with one tab, and the tab's tmux session.
func (s *Store) defaultLayout(wsID, cwd, panelType string) (*jsvalue.Object, error) {
	paneID, tabID := "pane-"+s.cfg.NewID(), "tab-"+s.cfg.NewID()
	tab := s.newTab(wsID, paneID, tabID, "", 0, cwd)
	if panelType != "" {
		tab.Set("panelType", panelType)
	}
	if err := s.cfg.Tmux.CreateSession(tab.String("sessionName"), cols, rows, cwd); err != nil {
		return nil, err
	}
	return jsvalue.NewObject("root", newPane(paneID, tab), "activePaneId", paneID, "updatedAt", s.now()), nil
}

// locked runs fn on the workspace's layout under its lock; fn sees nil when there is no layout. A true answer from fn
// stamps and writes the layout.
func (s *Store) locked(wsID string, fn func(layout *jsvalue.Object) bool) (*jsvalue.Object, error) {
	m := s.layoutLock(wsID)
	m.Lock()
	defer m.Unlock()
	layout := s.readLayout(wsID)
	if !fn(layout) || layout == nil {
		return nil, nil
	}
	layout.Set("updatedAt", s.now())
	if err := s.writeLayout(wsID, layout); err != nil {
		return nil, err
	}
	return layout, nil
}

// mutate changes the layout and then brings the workspace's folders in line with its tabs.
func (s *Store) mutate(wsID string, fn func(layout *jsvalue.Object) bool) (*jsvalue.Object, error) {
	layout, err := s.locked(wsID, func(l *jsvalue.Object) bool { return l != nil && fn(l) })
	if layout != nil {
		s.syncWorkspaceDirectories(wsID, layout)
	}
	return layout, err
}

// syncWorkspaceDirectories makes a workspace's folders the distinct folders of its tabs, in tree order.
func (s *Store) syncWorkspaceDirectories(wsID string, layout *jsvalue.Object) {
	var cwds []string
	seen := map[string]bool{}
	for _, t := range allTabs(root(layout)) {
		if c := t.String("cwd"); c != "" && !seen[c] {
			seen[c] = true
			cwds = append(cwds, c)
		}
	}
	if len(cwds) > 0 {
		_ = s.UpdateWorkspaceDirectories(wsID, cwds)
	}
}

func (s *Store) readLocked(wsID string) *jsvalue.Object {
	m := s.layoutLock(wsID)
	m.Lock()
	defer m.Unlock()
	return s.readLayout(wsID)
}

// Layout is a workspace's layout as stored, nil when there is none.
func (s *Store) Layout(wsID string) *jsvalue.Object {
	if !ValidWorkspaceID(wsID) {
		return nil
	}
	return s.readLocked(wsID)
}

// GetLayout is a workspace's layout; a workspace without one gets a default layout and its tmux session first.
func (s *Store) GetLayout(wsID, defaultCwd string) (*jsvalue.Object, error) {
	if !ValidWorkspaceID(wsID) {
		return nil, badRequest("Invalid workspace")
	}
	if existing := s.readLocked(wsID); existing != nil {
		return existing, nil
	}
	created, err := s.defaultLayout(wsID, defaultCwd, "")
	if err != nil {
		return nil, err
	}
	m := s.layoutLock(wsID)
	m.Lock()
	if existing := s.readLayout(wsID); existing != nil {
		m.Unlock()
		s.cfg.Tmux.KillSession(tabsOf(root(created))[0].String("sessionName"))
		return existing, nil
	}
	err = s.writeLayout(wsID, created)
	m.Unlock()
	if err != nil {
		return nil, err
	}
	return created, nil
}

// CreatePane starts a session for a pane the page is about to add; the layout is not touched.
func (s *Store) CreatePane(wsID, cwd string) (paneID string, tab *jsvalue.Object, err error) {
	if !ValidWorkspaceID(wsID) {
		return "", nil, badRequest("Invalid workspace")
	}
	paneID = "pane-" + s.cfg.NewID()
	tabID := "tab-" + s.cfg.NewID()
	tab = s.newTab(wsID, paneID, tabID, "", 0, cwd)
	if err := s.cfg.Tmux.CreateSession(tab.String("sessionName"), cols, rows, cwd); err != nil {
		return "", nil, err
	}
	return paneID, tab, nil
}

// KillPaneSessions ends the sessions of a pane the page closed outside any workspace; only the runtime's own session
// names are touched.
func (s *Store) KillPaneSessions(sessions []string) {
	n := s.cfg.Tmux.Names()
	for _, name := range sessions {
		if (n.Prefix != "" && strings.HasPrefix(name, n.Prefix)) || (n.LegacyPrefix != "" && strings.HasPrefix(name, n.LegacyPrefix)) {
			s.cfg.Tmux.KillSession(name)
		}
	}
}

// TabOptions are a new tab's name, folder, panel type and the command typed into its shell.
type TabOptions struct {
	Name, Cwd, PanelType, Command string
}

func maxOrder(tabs []*jsvalue.Object) float64 {
	m := math.Inf(-1)
	for _, t := range tabs {
		n, ok := t.Number("order")
		if !ok {
			return math.NaN()
		}
		m = math.Max(m, n)
	}
	return m
}

// AddTab appends a tab to a pane, starts its session (not for a web browser) and makes it the pane's active tab.
func (s *Store) AddTab(wsID, paneID string, o TabOptions) (*jsvalue.Object, error) {
	gone := notFound("Pane not found")
	if !ValidWorkspaceID(wsID) {
		return nil, gone
	}
	if l := s.readLocked(wsID); l == nil || findPane(root(l), paneID) == nil {
		return nil, gone
	}
	tabID := "tab-" + s.cfg.NewID()
	session := s.cfg.Tmux.SessionName(wsID, paneID, tabID)
	web := o.PanelType == webBrowser
	if !web {
		if err := s.cfg.Tmux.CreateSession(session, cols, rows, o.Cwd); err != nil {
			return nil, err
		}
		if o.Command != "" {
			if err := s.cfg.Tmux.SendKeys(session, o.Command); err != nil {
				return nil, err
			}
		}
	}
	var tab *jsvalue.Object
	layout, err := s.mutate(wsID, func(l *jsvalue.Object) bool {
		pane := findPane(root(l), paneID)
		if pane == nil {
			return false
		}
		tabs := tabsOf(pane)
		next := float64(0)
		if len(tabs) > 0 {
			next = maxOrder(tabs) + 1
		}
		name := jsvalue.TrimJS(o.Name)
		if name == "" {
			name = defaultTabName(o.PanelType)
		}
		tab = s.newTab(wsID, paneID, tabID, name, next, o.Cwd)
		if o.PanelType != "" {
			tab.Set("panelType", o.PanelType)
		}
		pane.Set("tabs", append(pane.Array("tabs"), tab))
		pane.Set("activeTabId", tabID)
		return true
	})
	if err != nil {
		return nil, err
	}
	if layout == nil {
		if !web {
			s.cfg.Tmux.KillSession(session)
		}
		return nil, gone
	}
	return tab, nil
}

// RemoveTab ends the tab's session, then removes the tab; an emptied pane goes too unless it is the last one.
func (s *Store) RemoveTab(wsID, paneID, tabID string) error {
	gone := notFound("Tab not found")
	if !ValidWorkspaceID(wsID) {
		return gone
	}
	l := s.readLocked(wsID)
	tab, _ := findTab(findPane(root(l), paneID), tabID)
	if l == nil || tab == nil {
		return gone
	}
	if p, _ := tab.Get("panelType"); p != webBrowser {
		s.cfg.Tmux.KillSession(tab.String("sessionName"))
	}
	layout, err := s.mutate(wsID, func(l *jsvalue.Object) bool {
		pane := findPane(root(l), paneID)
		if pane == nil {
			return false
		}
		_, idx := findTab(pane, tabID)
		if idx < 0 {
			return false
		}
		tabs := tabsOf(pane)
		setTabs(pane, append(tabs[:idx:idx], tabs[idx+1:]...))
		if active, _ := pane.Get("activeTabId"); active == tabID {
			pane.Set("activeTabId", firstTabIDOrNull(pane))
		}
		if len(tabsOf(pane)) == 0 && len(panes(root(l))) > 1 {
			removePaneWithFocus(l, paneID)
		}
		renumber(pane)
		return true
	})
	if err != nil {
		return err
	}
	if layout == nil {
		return gone
	}
	return nil
}

// RestartTab starts a tab's session again when it is gone, in its folder or the nearest one that still exists; with
// the folder lost the stored folder follows and the remembered command is dropped instead of run.
func (s *Store) RestartTab(wsID, paneID, tabID, command string) error {
	gone := notFound("Tab not found")
	if !ValidWorkspaceID(wsID) {
		return gone
	}
	l := s.readLocked(wsID)
	tab, _ := findTab(findPane(root(l), paneID), tabID)
	if l == nil || tab == nil {
		return gone
	}
	session := tab.String("sessionName")
	if s.cfg.Tmux.HasSession(session) {
		return nil
	}
	cwd := tab.String("cwd")
	effective := s.cfg.Tmux.ResolveExistingDir(cwd)
	lost := cwd != "" && cwd != effective
	failed := &Error{Status: 500, Message: "Failed to restart session"}
	if err := s.cfg.Tmux.CreateSession(session, cols, rows, effective); err != nil {
		return failed
	}
	if command != "" && !lost {
		if err := s.cfg.Tmux.SendKeys(session, command); err != nil {
			return failed
		}
	}
	if lost {
		_, err := s.locked(wsID, func(l *jsvalue.Object) bool {
			t, _ := findTab(findPane(root(l), paneID), tabID)
			if t == nil {
				return false
			}
			t.Set("cwd", effective)
			t.Delete("lastCommand")
			return true
		})
		if err != nil {
			return failed
		}
	}
	return nil
}

// ReconcileTabCwd corrects the stored folder of a tab whose session ended in a folder that no longer exists.
func (s *Store) ReconcileTabCwd(sessionName string) error {
	wsID, paneID, tabID, ok := s.cfg.Tmux.ParseSessionName(sessionName)
	if !ok || !ValidWorkspaceID(wsID) {
		return nil
	}
	_, err := s.locked(wsID, func(l *jsvalue.Object) bool {
		t, _ := findTab(findPane(root(l), paneID), tabID)
		cwd := t.String("cwd")
		if t == nil || cwd == "" {
			return false
		}
		effective := s.cfg.Tmux.ResolveExistingDir(cwd)
		if effective == cwd {
			return false
		}
		t.Set("cwd", effective)
		t.Delete("lastCommand")
		return true
	})
	return err
}

// FirstPaneTabs are the tabs of the first pane in their order, and its active tab.
func (s *Store) FirstPaneTabs(wsID string) (tabs []*jsvalue.Object, activeTabID any) {
	l := s.Layout(wsID)
	ps := panes(root(l))
	if l == nil || len(ps) == 0 {
		return nil, nil
	}
	tabs = tabsOf(ps[0])
	sort.SliceStable(tabs, func(i, j int) bool {
		a, _ := tabs[i].Number("order")
		b, _ := tabs[j].Number("order")
		return a < b
	})
	active, _ := ps[0].Get("activeTabId")
	return tabs, active
}

// PatchLayout sets the focus, a split's ratio, equal ratios or the diff settings.
func (s *Store) PatchLayout(wsID string, p LayoutPatch) (*jsvalue.Object, error) {
	return s.mutateOr(wsID, "Target not found", func(l *jsvalue.Object) bool { return patchLayoutData(l, p) })
}

// PatchPane sets a pane's active tab.
func (s *Store) PatchPane(wsID, paneID string, activeTabID *string) (*jsvalue.Object, error) {
	return s.mutateOr(wsID, "Target not found", func(l *jsvalue.Object) bool { return patchPaneData(l, paneID, activeTabID) })
}

// ReorderTabs puts a pane's tabs in the given order.
func (s *Store) ReorderTabs(wsID, paneID string, tabIDs []string) (*jsvalue.Object, error) {
	if len(tabIDs) == 0 {
		return nil, badRequest("tabIds array required")
	}
	return s.mutateOr(wsID, "Target not found", func(l *jsvalue.Object) bool { return reorderTabsData(l, paneID, tabIDs) })
}

// MoveTab moves a tab to another pane (or another place in its own) and focuses it there.
func (s *Store) MoveTab(wsID, tabID, fromPaneID, toPaneID string, toIndex float64) (*jsvalue.Object, error) {
	return s.mutateOr(wsID, "Target not found", func(l *jsvalue.Object) bool {
		return moveTabData(l, tabID, fromPaneID, toPaneID, toIndex)
	})
}

func (s *Store) mutateOr(wsID, missing string, fn func(*jsvalue.Object) bool) (*jsvalue.Object, error) {
	if !ValidWorkspaceID(wsID) {
		return nil, notFound(missing)
	}
	l, err := s.mutate(wsID, fn)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, notFound(missing)
	}
	return l, nil
}

// PatchTab sets the page's fields of a tab. A folder is taken from the session itself when it can be read, and a
// title is made from the session's program and folder, whatever the page sent.
func (s *Store) PatchTab(wsID, paneID, tabID string, p TabPatch) (*jsvalue.Object, error) {
	if !ValidWorkspaceID(wsID) {
		return nil, notFound("Tab not found")
	}
	_, wantCwd := p["cwd"]
	_, wantTitle := p["title"]
	if wantCwd || wantTitle {
		patched := TabPatch{}
		for k, v := range p {
			patched[k] = v
		}
		if l := s.readLocked(wsID); l != nil {
			if t, _ := findTab(findPane(root(l), paneID), tabID); t != nil && t.String("sessionName") != "" {
				session := t.String("sessionName")
				panelType, ok := p["panelType"]
				if !ok || panelType == nil {
					panelType, _ = t.Get("panelType")
				}
				liveCwd := ""
				if wantCwd {
					liveCwd = s.cfg.Tmux.SessionCwd(session)
					if liveCwd != "" {
						patched["cwd"] = liveCwd
					}
				}
				if wantTitle {
					cmd := s.cfg.Tmux.PaneCurrentCommand(session)
					cwd := liveCwd
					if !wantCwd {
						cwd = s.cfg.Tmux.SessionCwd(session)
					}
					if cmd != "" && cwd != "" {
						patched["title"] = formatTabTitle(cmd+"|"+cwd, panelType)
					}
				}
			}
		}
		p = patched
	}
	return s.mutateOr(wsID, "Tab not found", func(l *jsvalue.Object) bool { return patchTabViewData(l, paneID, tabID, p) })
}

// SplitPane splits a pane in two: the new pane (with a new tab and its session, none for a web browser) goes after it,
// takes the focus, and the ratios stay equal if they were.
func (s *Store) SplitPane(wsID, sourcePaneID, orientation, cwd, panelType string) (*jsvalue.Object, error) {
	if !ValidWorkspaceID(wsID) {
		return nil, notFound("Target pane not found")
	}
	web := panelType == webBrowser
	paneID, tabID := "pane-"+s.cfg.NewID(), "tab-"+s.cfg.NewID()
	tab := s.newTab(wsID, paneID, tabID, defaultTabName(panelType), 0, cwd)
	if !web {
		if err := s.cfg.Tmux.CreateSession(tab.String("sessionName"), cols, rows, cwd); err != nil {
			return nil, &Error{Status: 500, Message: "Failed to create pane"}
		}
	}
	if panelType != "" {
		tab.Set("panelType", panelType)
	}
	pane := newPane(paneID, tab)
	layout, err := s.mutate(wsID, func(l *jsvalue.Object) bool {
		existing := findPane(root(l), sourcePaneID)
		if existing == nil {
			return false
		}
		wasEqualized := isEqualized(root(l))
		if cwd != "" {
			active, _ := existing.Get("activeTabId")
			for _, t := range tabsOf(existing) {
				if id, _ := t.Get("id"); id == active {
					t.Set("cwd", cwd)
					break
				}
			}
		}
		split := jsvalue.NewObject("type", "split", "orientation", orientation, "ratio", 50, "children", []any{existing.Copy(), pane})
		l.Set("root", replacePane(root(l), sourcePaneID, split))
		if wasEqualized {
			l.Set("root", equalizeNode(root(l)))
		}
		l.Set("activePaneId", paneID)
		return true
	})
	if layout == nil && !web {
		s.cfg.Tmux.KillSession(tab.String("sessionName"))
	}
	if err != nil {
		return nil, &Error{Status: 500, Message: "Failed to create pane"}
	}
	if layout == nil {
		return nil, notFound("Target pane not found")
	}
	return layout, nil
}

// ClosePane removes a pane (never the last one); focus goes to its neighbour and its sessions end after the write.
func (s *Store) ClosePane(wsID, paneID string) (*jsvalue.Object, error) {
	gone := notFound("Pane not found")
	if !ValidWorkspaceID(wsID) {
		return nil, gone
	}
	var sessions []string
	layout, err := s.mutate(wsID, func(l *jsvalue.Object) bool {
		pane := findPane(root(l), paneID)
		if pane == nil || len(panes(root(l))) <= 1 {
			return false
		}
		for _, t := range tabsOf(pane) {
			if p, _ := t.Get("panelType"); p != webBrowser {
				sessions = append(sessions, t.String("sessionName"))
			}
		}
		wasEqualized := isEqualized(root(l))
		removePaneWithFocus(l, paneID)
		if wasEqualized {
			l.Set("root", equalizeNode(root(l)))
		}
		return true
	})
	if layout != nil {
		var wg sync.WaitGroup
		for _, name := range sessions {
			wg.Add(1)
			go func() { defer wg.Done(); s.cfg.Tmux.KillSession(name) }()
		}
		wg.Wait()
	}
	if err != nil {
		return nil, err
	}
	if layout == nil {
		return nil, gone
	}
	return layout, nil
}
