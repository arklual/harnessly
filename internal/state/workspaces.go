package state

import (
	"os"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

const workspaceNamePrefix = "Workspace "

func workspacesOf(data *jsvalue.Object) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, w := range data.Array("workspaces") {
		if o, ok := w.(*jsvalue.Object); ok {
			out = append(out, o)
		}
	}
	return out
}

func setObjects(data *jsvalue.Object, key string, list []*jsvalue.Object) {
	arr := make([]any, len(list))
	for i, o := range list {
		arr[i] = o
	}
	data.Set(key, arr)
}

func groupsOf(data *jsvalue.Object) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, g := range data.Array("groups") {
		if o, ok := g.(*jsvalue.Object); ok {
			out = append(out, o)
		}
	}
	return out
}

// normalizeWorkspaces reads older shapes as the current one: a single folder, an order field, a group that is gone.
// False when the document is not a workspace list at all.
func normalizeWorkspaces(data *jsvalue.Object) bool {
	v, _ := data.Get("workspaces")
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, w := range list {
		ws, ok := w.(*jsvalue.Object)
		if !ok {
			return false
		}
		dirs, _ := ws.Get("directories")
		if legacy, _ := ws.Get("directory"); !jsvalue.Truthy(dirs) && jsvalue.Truthy(legacy) {
			ws.Set("directories", []any{legacy})
			ws.Delete("directory")
		}
		ws.Delete("order")
	}
	if g, _ := data.Get("groups"); g == nil {
		data.Set("groups", []any{})
	} else if _, isArr := g.([]any); !isArr {
		data.Set("groups", []any{})
	}
	valid := map[any]bool{}
	for _, g := range data.Array("groups") {
		o, ok := g.(*jsvalue.Object)
		if !ok {
			if g == nil {
				return false
			}
			continue
		}
		o.Delete("order")
		id, _ := o.Get("id")
		if comparable(id) {
			valid[id] = true
		}
	}
	for _, ws := range workspacesOf(data) {
		gid, _ := ws.Get("groupId")
		if jsvalue.Truthy(gid) && (!comparable(gid) || !valid[gid]) {
			ws.Set("groupId", nil)
		}
	}
	return true
}

func comparable(v any) bool {
	switch v.(type) {
	case []any, *jsvalue.Object:
		return false
	}
	return true
}

// visualOrder stores the workspaces as the sidebar shows them: each group's in group order, then the ungrouped ones.
func visualOrder(data *jsvalue.Object) {
	if g, _ := data.Get("groups"); !jsvalue.Truthy(g) {
		data.Set("groups", []any{})
	}
	groups := groupsOf(data)
	valid := map[string]bool{}
	for _, g := range groups {
		valid[g.String("id")] = true
	}
	byGroup := map[string][]*jsvalue.Object{}
	var ungrouped []*jsvalue.Object
	for _, ws := range workspacesOf(data) {
		gid := ws.String("groupId")
		if gid != "" && valid[gid] {
			byGroup[gid] = append(byGroup[gid], ws)
		} else {
			ungrouped = append(ungrouped, ws)
		}
	}
	var ordered []*jsvalue.Object
	for _, g := range groups {
		ordered = append(ordered, byGroup[g.String("id")]...)
	}
	setObjects(data, "workspaces", append(ordered, ungrouped...))
}

// readWorkspaces is the normalised workspace list, nil when there is none; one that is not a list is copied to `.bak`.
func (s *Store) readWorkspaces() *jsvalue.Object {
	file := s.workspacesFile()
	v, ok := readJSON(file)
	if !ok {
		return nil
	}
	data, ok := v.(*jsvalue.Object)
	if !ok || !normalizeWorkspaces(data) {
		backup(file)
		return nil
	}
	return data
}

func (s *Store) emptyWorkspaces() *jsvalue.Object {
	return jsvalue.NewObject("workspaces", []any{}, "groups", []any{}, "sidebarCollapsed", false, "sidebarWidth", 240, "updatedAt", s.now())
}

func workspacesKey(data *jsvalue.Object) string {
	key := jsvalue.NewObject()
	if v, ok := data.Get("workspaces"); ok {
		key.Set("workspaces", v)
	}
	if v, ok := data.Get("groups"); ok && v != nil {
		key.Set("groups", v)
	} else {
		key.Set("groups", []any{})
	}
	for _, k := range []string{"activeWorkspaceId", "sidebarCollapsed", "sidebarWidth"} {
		if v, ok := data.Get(k); ok {
			key.Set(k, v)
		}
	}
	return string(jsvalue.Stringify(key, ""))
}

// writeWorkspaces writes a changed workspace list in visual order; an unchanged one is not written, committed or
// announced.
func (s *Store) writeWorkspaces(data *jsvalue.Object) error {
	visualOrder(data)
	key := workspacesKey(data)
	s.cacheMu.Lock()
	same := s.workspacesKey != nil && *s.workspacesKey == key
	s.cacheMu.Unlock()
	if same {
		return nil
	}
	data.Set("updatedAt", s.now())
	if s.owned() {
		if content, ok := s.commit(workspacesName, base(data), data); ok {
			data.Replace(content)
		} else {
			data.Set("stateDirty", true)
		}
	}
	if err := writeAtomic(s.workspacesFile(), data); err != nil {
		return err
	}
	key = workspacesKey(data)
	s.cacheMu.Lock()
	s.workspacesKey = &key
	s.cacheMu.Unlock()
	s.broadcastWorkspaces()
	return nil
}

// withWorkspaces runs fn on the workspace list under its lock; fn sees nil when there is none.
func (s *Store) withWorkspaces(fn func(data *jsvalue.Object) error) error {
	s.wsLock.Lock()
	defer s.wsLock.Unlock()
	return fn(s.readWorkspaces())
}

func findWorkspace(data *jsvalue.Object, id string) (*jsvalue.Object, int) {
	for i, ws := range workspacesOf(data) {
		if ws.String("id") == id {
			return ws, i
		}
	}
	return nil, -1
}

// Workspaces is GET /api/workspace: the list, groups, active workspace and sidebar.
func (s *Store) Workspaces() *jsvalue.Object {
	data := s.readWorkspaces()
	if data == nil {
		return jsvalue.NewObject("workspaces", []any{}, "groups", []any{}, "sidebarCollapsed", false, "sidebarWidth", 240)
	}
	data = jsvalue.Clone(data).(*jsvalue.Object)
	view := jsvalue.NewObject("workspaces", data.Array("workspaces"))
	if g, ok := data.Get("groups"); ok && g != nil {
		view.Set("groups", g)
	} else {
		view.Set("groups", []any{})
	}
	for _, k := range []string{"activeWorkspaceId", "sidebarCollapsed", "sidebarWidth"} {
		if v, ok := data.Get(k); ok {
			view.Set(k, v)
		}
	}
	return view
}

// ActiveWorkspaceID is the active workspace, or the first one when the active one is gone; "" when there is none.
func (s *Store) ActiveWorkspaceID() string {
	data := s.readWorkspaces()
	if active := data.String("activeWorkspaceId"); active != "" {
		if ws, _ := findWorkspace(data, active); ws != nil {
			return active
		}
	}
	if list := workspacesOf(data); len(list) > 0 {
		return list[0].String("id")
	}
	return ""
}

// Workspace is one workspace, nil when there is no such one.
func (s *Store) Workspace(id string) *jsvalue.Object {
	ws, _ := findWorkspace(s.readWorkspaces(), id)
	return ws
}

func nextWorkspaceName(list []*jsvalue.Object) string {
	max := float64(0)
	for _, ws := range list {
		if name := ws.String("name"); strings.HasPrefix(name, workspaceNamePrefix) {
			if n := jscompat.ParseInt(name[len(workspaceNamePrefix):], 10); n > max {
				max = n
			}
		}
	}
	return workspaceNamePrefix + jscompat.Number(max+1)
}

// CreateWorkspace adds a workspace for a folder with a default layout (its first tab of panelType, if given, and that
// tab's session).
func (s *Store) CreateWorkspace(directory, name, panelType string) (*jsvalue.Object, error) {
	st, err := os.Stat(directory)
	if err != nil {
		return nil, badRequest("Directory does not exist")
	}
	if !st.IsDir() {
		return nil, badRequest("Please enter a directory path, not a file")
	}
	wsID := "ws-" + s.cfg.NewID()
	layout, err := s.defaultLayout(wsID, directory, panelType)
	if err != nil {
		return nil, err
	}
	var workspace *jsvalue.Object
	err = s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			data = s.emptyWorkspaces()
		}
		wsName := jsvalue.TrimJS(name)
		if wsName == "" {
			wsName = nextWorkspaceName(workspacesOf(data))
		}
		m := s.layoutLock(wsID)
		m.Lock()
		err := s.writeLayout(wsID, layout)
		m.Unlock()
		if err != nil {
			return err
		}
		workspace = jsvalue.NewObject("id", wsID, "name", wsName, "directories", []any{directory})
		data.Set("workspaces", append(data.Array("workspaces"), workspace))
		return s.writeWorkspaces(data)
	})
	if err != nil {
		return nil, err
	}
	s.writePrompts(workspace)
	return workspace, nil
}

// DeleteWorkspace ends every session of the workspace, removes its layout and takes it off the list.
func (s *Store) DeleteWorkspace(id string) error {
	gone := notFound("Workspace not found")
	if ws := s.Workspace(id); ws == nil || !ValidWorkspaceID(id) {
		return gone
	}
	if layout := s.Layout(id); layout != nil {
		for _, t := range allTabs(root(layout)) {
			s.cfg.Tmux.KillSession(t.String("sessionName"))
		}
	}
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			data = s.emptyWorkspaces()
		}
		_ = s.RemoveLayout(id)
		_, idx := findWorkspace(data, id)
		if idx < 0 {
			return nil
		}
		list := data.Array("workspaces")
		data.Set("workspaces", append(list[:idx:idx], list[idx+1:]...))
		return s.writeWorkspaces(data)
	})
}

// RenameWorkspace names a workspace (the route trims and checks the name).
func (s *Store) RenameWorkspace(id, name string) (*jsvalue.Object, error) {
	var renamed *jsvalue.Object
	err := s.withWorkspaces(func(data *jsvalue.Object) error {
		ws, _ := findWorkspace(data, id)
		if ws == nil {
			return notFound("Workspace not found")
		}
		ws.Set("name", name)
		renamed = ws
		return s.writeWorkspaces(data)
	})
	if err != nil {
		return nil, err
	}
	s.writePrompts(renamed)
	return renamed.Copy(), nil
}

// SetWorkspaceGroup puts a workspace in a group; nil, or a group that does not exist, takes it out of any.
func (s *Store) SetWorkspaceGroup(id string, groupID *string) error {
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		ws, _ := findWorkspace(data, id)
		if ws == nil {
			return notFound("Workspace not found")
		}
		var next any
		if groupID != nil && *groupID != "" && hasGroup(data, *groupID) {
			next = *groupID
		}
		if cur, _ := ws.Get("groupId"); cur == next {
			return nil
		}
		ws.Set("groupId", next)
		return s.writeWorkspaces(data)
	})
}

func hasGroup(data *jsvalue.Object, id string) bool {
	for _, g := range groupsOf(data) {
		if g.String("id") == id {
			return true
		}
	}
	return false
}

// UpdateActive stores the active workspace and the sidebar's state from the body of PATCH /api/workspace/active, each
// field present as given.
func (s *Store) UpdateActive(body *jsvalue.Object) error {
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			data = s.emptyWorkspaces()
		}
		for _, k := range []string{"activeWorkspaceId", "sidebarCollapsed", "sidebarWidth"} {
			if v, ok := body.Get(k); ok {
				data.Set(k, v)
			}
		}
		return s.writeWorkspaces(data)
	})
}

// UpdateWorkspaceDirectories replaces a workspace's folders when they changed.
func (s *Store) UpdateWorkspaceDirectories(id string, directories []string) error {
	var changed *jsvalue.Object
	err := s.withWorkspaces(func(data *jsvalue.Object) error {
		ws, _ := findWorkspace(data, id)
		if ws == nil {
			return nil
		}
		cur, _ := ws.Get("directories")
		next := jsvalue.StringArray(directories)
		if _, has := ws.Get("directories"); has && jsvalue.Equal(cur, next) {
			return nil
		}
		ws.Set("directories", next)
		changed = ws
		return s.writeWorkspaces(data)
	})
	if err == nil && changed != nil {
		s.writePrompts(changed)
	}
	return err
}

// ReorderItem is one workspace of a reorder and, when HasGroup, the group it moves to (nil: none).
type ReorderItem struct {
	ID       string
	GroupID  *string
	HasGroup bool
}

// ReorderWorkspaces sets the workspaces' order (and groups); the items must name every workspace once.
func (s *Store) ReorderWorkspaces(items []ReorderItem) error {
	if len(items) == 0 {
		return badRequest("items array required")
	}
	invalid := badRequest("Invalid order")
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			data = s.emptyWorkspaces()
		}
		list := workspacesOf(data)
		byID := map[string]*jsvalue.Object{}
		for _, ws := range list {
			byID[ws.String("id")] = ws
		}
		seen := map[string]bool{}
		var reordered []*jsvalue.Object
		for _, it := range items {
			ws := byID[it.ID]
			if ws == nil || seen[it.ID] {
				return invalid
			}
			seen[it.ID] = true
			if it.HasGroup {
				var g any
				if it.GroupID != nil && *it.GroupID != "" && hasGroup(data, *it.GroupID) {
					g = *it.GroupID
				}
				ws.Set("groupId", g)
			}
			reordered = append(reordered, ws)
		}
		if len(reordered) != len(data.Array("workspaces")) {
			return invalid
		}
		setObjects(data, "workspaces", reordered)
		return s.writeWorkspaces(data)
	})
}

// CreateGroup adds a group; a blank name becomes "Group <n>".
func (s *Store) CreateGroup(name string) (*jsvalue.Object, error) {
	var group *jsvalue.Object
	err := s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			data = s.emptyWorkspaces()
		}
		if g, _ := data.Get("groups"); !jsvalue.Truthy(g) {
			data.Set("groups", []any{})
		}
		trimmed := jsvalue.TrimJS(name)
		if trimmed == "" {
			trimmed = "Group " + jscompat.Number(float64(len(data.Array("groups"))+1))
		}
		group = jsvalue.NewObject("id", "grp-"+s.cfg.NewID(), "name", trimmed, "collapsed", false)
		data.Set("groups", append(data.Array("groups"), group))
		return s.writeWorkspaces(data)
	})
	return group, err
}

func findGroup(data *jsvalue.Object, id string) (*jsvalue.Object, int) {
	for i, g := range groupsOf(data) {
		if g.String("id") == id {
			return g, i
		}
	}
	return nil, -1
}

// RenameGroup names a group; a blank name changes nothing.
func (s *Store) RenameGroup(id, name string) (*jsvalue.Object, error) {
	var out *jsvalue.Object
	err := s.withWorkspaces(func(data *jsvalue.Object) error {
		g, _ := findGroup(data, id)
		if g == nil {
			return notFound("Group not found")
		}
		trimmed := jsvalue.TrimJS(name)
		if trimmed == "" {
			out = g
			return nil
		}
		g.Set("name", trimmed)
		out = g.Copy()
		return s.writeWorkspaces(data)
	})
	return out, err
}

// SetGroupCollapsed folds or unfolds a group in the sidebar.
func (s *Store) SetGroupCollapsed(id string, collapsed bool) error {
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		g, _ := findGroup(data, id)
		if g == nil {
			return notFound("Group not found")
		}
		if cur, _ := g.Get("collapsed"); cur == collapsed {
			return nil
		}
		g.Set("collapsed", collapsed)
		return s.writeWorkspaces(data)
	})
}

// UngroupGroup removes a group; its workspaces stay, outside any group.
func (s *Store) UngroupGroup(id string) error {
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		_, idx := findGroup(data, id)
		if idx < 0 {
			return notFound("Group not found")
		}
		for _, ws := range workspacesOf(data) {
			if g, _ := ws.Get("groupId"); g == id {
				ws.Set("groupId", nil)
			}
		}
		groups := data.Array("groups")
		data.Set("groups", append(groups[:idx:idx], groups[idx+1:]...))
		return s.writeWorkspaces(data)
	})
}

// ReorderGroups sets the groups' order; the ids must name every group once.
func (s *Store) ReorderGroups(ids []string) error {
	invalid := badRequest("Invalid order")
	return s.withWorkspaces(func(data *jsvalue.Object) error {
		if data == nil {
			return invalid
		}
		if g, _ := data.Get("groups"); !jsvalue.Truthy(g) {
			data.Set("groups", []any{})
		}
		byID := map[string]*jsvalue.Object{}
		for _, g := range groupsOf(data) {
			byID[g.String("id")] = g
		}
		seen := map[string]bool{}
		var reordered []*jsvalue.Object
		for _, id := range ids {
			g := byID[id]
			if g == nil || seen[id] {
				return invalid
			}
			seen[id] = true
			reordered = append(reordered, g)
		}
		if len(reordered) != len(data.Array("groups")) {
			return invalid
		}
		setObjects(data, "groups", reordered)
		return s.writeWorkspaces(data)
	})
}

// WorkspaceTab is a tab with the workspace it belongs to.
type WorkspaceTab struct {
	Workspace *jsvalue.Object
	Tab       *jsvalue.Object
}

// WorkspaceTabs is every tab of every workspace, in list and tree order (read without locks, as a lookup).
func (s *Store) WorkspaceTabs() []WorkspaceTab {
	var out []WorkspaceTab
	for _, ws := range workspacesOf(s.readWorkspaces()) {
		id := ws.String("id")
		if !ValidWorkspaceID(id) {
			continue
		}
		l := s.readLayout(id)
		for _, t := range allTabs(root(l)) {
			out = append(out, WorkspaceTab{Workspace: ws, Tab: t})
		}
	}
	return out
}

// Panes are the panes of a layout in tree order.
func Panes(layout *jsvalue.Object) []*jsvalue.Object { return panes(root(layout)) }

// Tabs are a pane's tabs.
func Tabs(pane *jsvalue.Object) []*jsvalue.Object { return tabsOf(pane) }
