package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"harnessly/internal/jsvalue"
)

func (s *Store) file(name string) string { return filepath.Join(s.DataDir, name) }

// --- keyboard shortcuts --------------------------------------------------------------------------------------------------

// sanitizeKeybindings is the overrides as the page gets them: string or null values of whatever the file's overrides
// hold (an array's under its indices).
func sanitizeKeybindings(raw any) *jsvalue.Object {
	overrides := jsvalue.NewObject()
	o, _ := raw.(*jsvalue.Object)
	src, _ := o.Get("overrides")
	all := jsvalue.NewObject()
	switch src.(type) {
	case *jsvalue.Object, []any:
		Spread(all, src)
	}
	for _, k := range all.Keys() {
		v, _ := all.Get(k)
		if _, ok := v.(string); ok || v == nil {
			overrides.Set(k, v)
		}
	}
	return jsvalue.NewObject("overrides", overrides)
}

// Keybindings is readKeybindings: the file sanitized, or no overrides.
func (s *Store) Keybindings() *jsvalue.Object {
	v, _ := readJSON(s.file("keybindings.json"))
	return sanitizeKeybindings(v)
}

func (s *Store) writeKeybindings(data *jsvalue.Object) error {
	return writeInPlace(s.file("keybindings.json"), jsvalue.Stringify(sanitizeKeybindings(data), "  "), 0o600)
}

// SetKeybinding sets one action's key (null: no key) and returns the file written.
func (s *Store) SetKeybinding(id string, key any) (*jsvalue.Object, error) {
	defer s.lockFile("keybindings.json")()
	data := s.Keybindings()
	overrides := data.Object("overrides").Copy()
	overrides.Set(id, key)
	data = jsvalue.NewObject("overrides", overrides)
	return data, s.writeKeybindings(data)
}

// ResetKeybinding drops one action's override.
func (s *Store) ResetKeybinding(id string) (*jsvalue.Object, error) {
	defer s.lockFile("keybindings.json")()
	overrides := s.Keybindings().Object("overrides").Copy()
	overrides.Delete(id)
	data := jsvalue.NewObject("overrides", overrides)
	return data, s.writeKeybindings(data)
}

// ResetAllKeybindings drops every override.
func (s *Store) ResetAllKeybindings() (*jsvalue.Object, error) {
	defer s.lockFile("keybindings.json")()
	return jsvalue.NewObject("overrides", jsvalue.NewObject()), s.writeKeybindings(jsvalue.NewObject("overrides", jsvalue.NewObject()))
}

// --- quick prompts and sidebar items ------------------------------------------------------------------------------------

// errViewFailed: building the view threw in the previous runtime (a field of the wrong kind); it then showed defaults.
var errViewFailed = errors.New("settings: the file cannot be shown")

// builtinView is buildQuickPromptsData / buildSidebarItemsData: the built-in entries with their enabled flag, copies of
// the custom ones and the order as the file has them.
func builtinView(file any, builtins []*jsvalue.Object) (*jsvalue.Object, error) {
	field := func(k string) (any, bool) {
		o, ok := file.(*jsvalue.Object)
		if !ok {
			return nil, false
		}
		v, ok := o.Get(k)
		return v, ok && v != nil
	}
	disabled := map[string]bool{}
	if v, ok := field("disabledBuiltinIds"); ok {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					disabled[s] = true
				}
			}
		case string: // a Set of a string holds its characters
			for _, r := range x {
				disabled[string(r)] = true
			}
		case *jsvalue.Object, float64, bool:
			return nil, errViewFailed
		}
	}
	list := make([]any, 0, len(builtins))
	for _, b := range builtins {
		c := b.Copy()
		c.Set("enabled", !disabled[b.String("id")])
		list = append(list, c)
	}
	custom := []any{}
	if v, ok := field("custom"); ok {
		arr, isArr := v.([]any)
		if !isArr {
			return nil, errViewFailed
		}
		for _, e := range arr {
			c := jsvalue.NewObject()
			Spread(c, e)
			custom = append(custom, c)
		}
	}
	var order any = []any{}
	if v, ok := field("order"); ok {
		order = v
	}
	return jsvalue.NewObject("builtins", list, "custom", custom, "order", order), nil
}

func emptyBuiltinFile() *jsvalue.Object {
	return jsvalue.NewObject("custom", []any{}, "disabledBuiltinIds", []any{}, "order", []any{})
}

var builtinPrompts = []*jsvalue.Object{
	jsvalue.NewObject("id", "builtin-commit", "name", "Commit", "prompt", "/commit-commands:commit", "enabled", true),
	jsvalue.NewObject("id", "builtin-simplify", "name", "Simplify", "prompt", "/simplify", "enabled", true),
}

var builtinSidebarItems = []*jsvalue.Object{
	jsvalue.NewObject("id", "builtin-notes", "name", "Notes", "icon", "FileText", "url", "/reports", "enabled", true),
	jsvalue.NewObject("id", "builtin-stats", "name", "Stats", "icon", "BarChart3", "url", "/stats", "enabled", true),
}

// migrateFlatPrompts is migrateFromFlatArray: the oldest quick prompts file, a flat list.
func migrateFlatPrompts(arr []any) (*jsvalue.Object, error) {
	builtin := map[string]bool{}
	for _, b := range builtinPrompts {
		builtin[b.String("id")] = true
	}
	isBuiltin := func(p any) (bool, error) {
		o, ok := p.(*jsvalue.Object)
		if !ok {
			if p == nil {
				return false, errViewFailed // null.id threw
			}
			return false, nil
		}
		id, _ := o.Get("id")
		s, isStr := id.(string)
		return isStr && builtin[s], nil
	}
	disabled, custom := []any{}, []any{}
	for _, p := range arr {
		b, err := isBuiltin(p)
		if err != nil {
			return nil, err
		}
		if b {
			enabled, _ := p.(*jsvalue.Object).Get("enabled")
			if !jsvalue.Truthy(enabled) {
				id, _ := p.(*jsvalue.Object).Get("id")
				disabled = append(disabled, id)
			}
		}
	}
	for _, p := range arr {
		if b, _ := isBuiltin(p); !b {
			custom = append(custom, p)
		}
	}
	return jsvalue.NewObject("custom", custom, "disabledBuiltinIds", disabled, "order", []any{}), nil
}

// QuickPrompts is readQuickPrompts: the file's view; a flat list is migrated and written back first; defaults when the
// file is missing or cannot be shown.
func (s *Store) QuickPrompts() *jsvalue.Object {
	defaults, _ := builtinView(emptyBuiltinFile(), builtinPrompts)
	v, ok := readJSON(s.file("quick-prompts.json"))
	if !ok {
		return defaults
	}
	if arr, isArr := v.([]any); isArr {
		migrated, err := migrateFlatPrompts(arr)
		if err != nil {
			return defaults
		}
		if err := s.WriteQuickPrompts(migrated); err != nil {
			return defaults
		}
		v = migrated
	}
	if v == nil {
		return defaults // null.disabledBuiltinIds threw
	}
	view, err := builtinView(v, builtinPrompts)
	if err != nil {
		return defaults
	}
	return view
}

// WriteQuickPrompts writes what the page sent, as it sent it.
func (s *Store) WriteQuickPrompts(body any) error {
	defer s.lockFile("quick-prompts.json")()
	return writeInPlace(s.file("quick-prompts.json"), jsvalue.Stringify(body, "  "), 0o600)
}

// SidebarItems is readSidebarItems: the file's view, or defaults.
func (s *Store) SidebarItems() *jsvalue.Object {
	defaults, _ := builtinView(emptyBuiltinFile(), builtinSidebarItems)
	v, ok := readJSON(s.file("sidebar-items.json"))
	if !ok || v == nil {
		return defaults
	}
	view, err := builtinView(v, builtinSidebarItems)
	if err != nil {
		return defaults
	}
	return view
}

// WriteSidebarItems writes what the page sent, as it sent it.
func (s *Store) WriteSidebarItems(body any) error {
	defer s.lockFile("sidebar-items.json")()
	return writeInPlace(s.file("sidebar-items.json"), jsvalue.Stringify(body, "  "), 0o666)
}

// --- sent messages ------------------------------------------------------------------------------------------------------

// historyFile is a workspace's message history; false when the id leads out of the workspaces folder.
func (s *Store) historyFile(wsID string) (string, bool) {
	root := filepath.Join(s.DataDir, "workspaces")
	dir := filepath.Join(root, wsID)
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(dir, "message-history.json"), true
}

// ValidWorkspace says whether a workspace id names a folder inside the workspaces folder.
func (s *Store) ValidWorkspace(wsID string) bool {
	_, ok := s.historyFile(wsID)
	return ok
}

// readHistory is the file's entries when they are a list; none otherwise.
func readHistory(file string) []any {
	v, _ := readJSON(file)
	o, _ := v.(*jsvalue.Object)
	if arr := o.Array("entries"); arr != nil {
		return arr
	}
	return []any{}
}

// MessageHistory is a workspace's sent messages as the file holds them.
func (s *Store) MessageHistory(wsID string) []any {
	file, ok := s.historyFile(wsID)
	if !ok {
		return []any{}
	}
	return readHistory(file)
}

// maxHistory is how many sent messages a workspace keeps.
const maxHistory = 500

func (s *Store) lockWorkspace(wsID string) func() {
	s.history.Lock()
	if s.wsLocks == nil {
		s.wsLocks = map[string]*wsLock{}
	}
	l := s.wsLocks[wsID]
	if l == nil {
		l = &wsLock{}
		s.wsLocks[wsID] = l
	}
	l.users++
	s.history.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		s.history.Lock()
		if l.users--; l.users == 0 {
			delete(s.wsLocks, wsID)
		}
		s.history.Unlock()
	}
}

// errBadEntry: an entry of the file is null, which the previous runtime's filter threw on.
var errBadEntry = errors.New("settings: a null entry in the message history")

func field(e any, k string) (any, error) {
	if e == nil {
		return nil, errBadEntry
	}
	o, ok := e.(*jsvalue.Object)
	if !ok {
		return jsvalue.Undefined, nil
	}
	v, has := o.Get(k)
	if !has {
		return jsvalue.Undefined, nil
	}
	return v, nil
}

// AddMessage puts a sent message first (the same text sent before moves up) and returns its entry.
func (s *Store) AddMessage(wsID, message string) (*jsvalue.Object, error) {
	defer s.lockWorkspace(wsID)()
	file, _ := s.historyFile(wsID)
	entries := readHistory(file)
	entry := jsvalue.NewObject("id", s.newID(), "message", message, "sentAt", s.isoNow())
	next := []any{entry}
	for _, e := range entries {
		v, err := field(e, "message")
		if err != nil {
			return nil, err
		}
		if v != message {
			next = append(next, e)
		}
	}
	if len(next) > maxHistory {
		next = next[:maxHistory]
	}
	return entry, writeHistory(file, next)
}

// DeleteMessage drops the entries with that id (an id of another kind than the entries' matches none).
func (s *Store) DeleteMessage(wsID string, id any) error {
	defer s.lockWorkspace(wsID)()
	file, _ := s.historyFile(wsID)
	next := []any{}
	for _, e := range readHistory(file) {
		v, err := field(e, "id")
		if err != nil {
			return err
		}
		if !strictEqual(v, id) {
			next = append(next, e)
		}
	}
	return writeHistory(file, next)
}

// strictEqual is ===: primitives by value and kind; an object or array equals none read apart from it.
func strictEqual(a, b any) bool {
	switch a.(type) {
	case nil:
		return b == nil
	case string, bool, float64:
		return a == b
	}
	return false
}

func writeHistory(file string, entries []any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	return writeAtomic(file, jsvalue.Stringify(jsvalue.NewObject("entries", entries), "  "), 0o600)
}
