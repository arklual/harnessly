package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const workspacesName = "workspaces.json"

func (s *Store) layoutDir(wsID string) string {
	return filepath.Join(s.cfg.DataDir, "workspaces", wsID)
}
func (s *Store) layoutFile(wsID string) string {
	return filepath.Join(s.layoutDir(wsID), "layout.json")
}
func (s *Store) workspacesFile() string { return filepath.Join(s.cfg.DataDir, workspacesName) }

func layoutName(wsID string) string { return "workspaces/" + wsID + "/layout.json" }

// readJSON reads a state file; ok is false when it is absent, and when it does not parse (a copy then goes to `.bak`).
func readJSON(file string) (v any, ok bool) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	v, err = jsvalue.Parse(raw)
	if err != nil {
		backup(file)
		return nil, false
	}
	return v, true
}

func backup(file string) {
	raw, err := os.ReadFile(file)
	st, serr := os.Stat(file)
	if err != nil || serr != nil {
		return
	}
	bak := strings.TrimSuffix(file, ".json") + ".json.bak"
	if os.WriteFile(bak, raw, st.Mode().Perm()) == nil {
		_ = os.Chmod(bak, st.Mode().Perm())
	}
}

// writeAtomic writes data as JSON.stringify(data, null, 2) with no newline, mode 0600, through a temp file of its own
// so two writers never share one.
func writeAtomic(file string, data *jsvalue.Object) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%s.tmp", file, os.Getpid(), nanoid(8))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(jsvalue.Stringify(data, "  "))
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// readLayout is a workspace's layout, nil when there is none (or it does not parse, or is not an object).
func (s *Store) readLayout(wsID string) *jsvalue.Object {
	v, _ := readJSON(s.layoutFile(wsID))
	o, _ := v.(*jsvalue.Object)
	return o
}

func layoutKey(data *jsvalue.Object) string {
	key := jsvalue.NewObject()
	for _, k := range []string{"root", "activePaneId", "diffSettings"} {
		if v, ok := data.Get(k); ok {
			key.Set(k, v)
		}
	}
	return string(jsvalue.Stringify(key, ""))
}

// writeLayoutLocal puts the layout on disk and tells the reconciler and the pages; the tab-change path writes this way
// every time, with no content check.
func (s *Store) writeLayoutLocal(wsID string, data *jsvalue.Object) error {
	if err := writeAtomic(s.layoutFile(wsID), data); err != nil {
		return err
	}
	s.announceLayout(wsID, data)
	return nil
}

func (s *Store) announceLayout(wsID string, data *jsvalue.Object) {
	if s.cfg.Reconciler != nil {
		var ids []string
		for _, t := range allTabs(root(data)) {
			ids = append(ids, t.String("id"))
		}
		s.cfg.Reconciler.ReconcileWorkspaceTabs(wsID, ids)
	}
	s.broadcastLayout(wsID)
}

// writeLayout writes a changed layout. An unchanged one (same tree, focus and diff settings as the last write) is not
// written, committed or announced. When the cluster owns the state the change is committed there first and its answer
// is what is written; when it cannot be committed the file is marked dirty for the cluster to take later.
func (s *Store) writeLayout(wsID string, data *jsvalue.Object) error {
	s.cacheMu.Lock()
	same := s.layoutCache[wsID] == layoutKey(data)
	s.cacheMu.Unlock()
	if same {
		return nil
	}
	if s.owned() {
		if content, ok := s.commit(layoutName(wsID), base(data), data); ok {
			data.Replace(content)
		} else {
			data.Set("stateDirty", true)
		}
	}
	if err := s.writeLayoutLocal(wsID, data); err != nil {
		return err
	}
	s.cacheMu.Lock()
	s.layoutCache[wsID] = layoutKey(data)
	s.cacheMu.Unlock()
	return nil
}

func base(data *jsvalue.Object) any {
	if v, ok := data.Get("stateVersion"); ok && v != nil {
		return v
	}
	return float64(0)
}

func (s *Store) clearLayoutCache(wsID string) {
	s.cacheMu.Lock()
	delete(s.layoutCache, wsID)
	s.cacheMu.Unlock()
}

// RemoveLayout deletes a workspace's layout folder (in the cluster first when it owns the state) and tells the
// reconciler and the pages.
func (s *Store) RemoveLayout(wsID string) error {
	if !ValidWorkspaceID(wsID) {
		return notFound("Workspace not found")
	}
	m := s.layoutLock(wsID)
	m.Lock()
	defer m.Unlock()
	return s.removeLayout(wsID)
}

func (s *Store) removeLayout(wsID string) error {
	if s.owned() {
		s.commit(layoutName(wsID), float64(0), nil)
	}
	err := os.RemoveAll(s.layoutDir(wsID))
	s.clearLayoutCache(wsID)
	if s.cfg.Reconciler != nil {
		s.cfg.Reconciler.RemoveWorkspaceTabs(wsID)
	}
	s.broadcastLayout(wsID)
	return err
}

// LayoutChangedByCluster: the cluster wrote this layout; the next write starts from it, and the reconciler and the
// pages follow it.
func (s *Store) LayoutChangedByCluster(wsID string) {
	if !ValidWorkspaceID(wsID) {
		return
	}
	s.clearLayoutCache(wsID)
	if data := s.readLayout(wsID); data != nil {
		s.announceLayout(wsID, data)
	}
}

// WorkspacesChangedByCluster: the cluster wrote the workspace list.
func (s *Store) WorkspacesChangedByCluster() {
	s.cacheMu.Lock()
	s.workspacesKey = nil
	s.cacheMu.Unlock()
	s.broadcastWorkspaces()
}

var layoutNameRe = regexp.MustCompile(`^workspaces/([\w-]+)/layout\.json$`)

// StateChanged takes the agent's notice that the cluster wrote a state file (the state-changed route).
func (s *Store) StateChanged(name string) error {
	if name == workspacesName {
		s.WorkspacesChangedByCluster()
		return nil
	}
	if m := layoutNameRe.FindStringSubmatch(name); m != nil {
		s.LayoutChangedByCluster(m[1])
		return nil
	}
	return badRequest("unknown state file")
}
