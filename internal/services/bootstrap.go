package services

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Bootstrap answers the page's start data from this machine's files: the person, whether tmux and git are usable,
// the workspaces, the settings without their secrets, the quick prompts and the sidebar items. Creating the first
// workspace of an empty machine is the layout store's work (CreateFirst).
type Bootstrap struct {
	DataDir string
	// ConfigFile is the runtime's settings file.
	ConfigFile string
	// HostLocked: the deployment fixed the listening address (the previous runtime's HOST).
	HostLocked bool
	// BindLocal: the previous runtime listens on loopback only.
	BindLocal bool
	// Owner is the person a request's session names (its token's sub), nil without one.
	Owner func(*http.Request) any
	// Tools reports whether tmux (a compatible one) and git are installed; it may be slow.
	Tools func() bool
	// CreateFirst makes the machine's first workspace when it has none, before the answer is read; nil leaves an empty
	// machine empty.
	CreateFirst func() error
	Now         func() time.Time

	mu      sync.Mutex
	toolsOK *bool
	toolsAt time.Time
	refresh bool
}

const toolsRevalidate = 30 * time.Second

var (
	builtinPrompts = []*jsvalue.Object{
		obj("id", "builtin-commit", "name", "Commit", "prompt", "/commit-commands:commit", "enabled", true),
		obj("id", "builtin-simplify", "name", "Simplify", "prompt", "/simplify", "enabled", true),
	}
	builtinItems = []*jsvalue.Object{
		obj("id", "builtin-notes", "name", "Notes", "icon", "FileText", "url", "/reports", "enabled", true),
		obj("id", "builtin-stats", "name", "Stats", "icon", "BarChart3", "url", "/stats", "enabled", true),
	}
)

// readJSONFile is JSON.parse of a file read as UTF-8; ok is false when it cannot be read or parsed.
func readJSONFile(path string) (any, bool, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	v, err := jsvalue.Parse([]byte(files.DecodeUTF8(raw)))
	return v, true, err == nil
}

// normalizeWorkspaces reads old shapes of the workspaces file as the current one; false where the previous runtime
// threw on it.
func normalizeWorkspaces(data any) bool {
	list, ok := opt(data, "workspaces").([]any)
	if _, isString := opt(data, "workspaces").(string); !ok && !(isString && isRecord(data)) {
		return false // for…of over something that is not iterable
	}
	for _, ws := range list {
		if nullish(ws) {
			return false
		}
		o, isObj := ws.(*jsvalue.Object)
		if !isObj {
			continue
		}
		if !truthy(opt(o, "directories")) && truthy(opt(o, "directory")) {
			o.Set("directories", []any{opt(o, "directory")})
			o.Delete("directory")
		}
		o.Delete("order")
	}
	d := data.(*jsvalue.Object)
	groups, isList := opt(d, "groups").([]any)
	if !isList {
		groups = []any{}
		d.Set("groups", groups)
	}
	valid := map[string]bool{}
	for _, g := range groups {
		if nullish(g) {
			return false
		}
		if o, ok := g.(*jsvalue.Object); ok {
			o.Delete("order")
		}
		valid[str(opt(g, "id"))] = true
	}
	for _, ws := range list {
		if o, ok := ws.(*jsvalue.Object); ok {
			if id := opt(o, "groupId"); truthy(id) && !valid[str(id)] {
				o.Set("groupId", nil)
			}
		}
	}
	return true
}

func (b *Bootstrap) workspacesView() *jsvalue.Object {
	path := filepath.Join(b.DataDir, "workspaces.json")
	data, read, parsed := readJSONFile(path)
	if read && (!parsed || !normalizeWorkspaces(data)) {
		if raw, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(strings.TrimSuffix(path, ".json")+".json.bak", raw, 0o644)
		}
		data = nil
	}
	if _, ok := opt(data, "workspaces").([]any); !read || !ok {
		return obj("workspaces", []any{}, "groups", []any{}, "sidebarCollapsed", false, "sidebarWidth", 220)
	}
	d := jsvalue.Clone(data).(*jsvalue.Object)
	normalizeWorkspaces(d)
	groups := opt(d, "groups")
	if nullish(groups) {
		groups = []any{}
	}
	return obj("workspaces", opt(d, "workspaces"), "groups", groups, "activeWorkspaceId", opt(d, "activeWorkspaceId"),
		"sidebarCollapsed", opt(d, "sidebarCollapsed"), "sidebarWidth", opt(d, "sidebarWidth"))
}

// HasWorkspaces: the machine has a workspace already, so the answer needs no new one.
func (b *Bootstrap) HasWorkspaces() bool {
	list, _ := opt(b.workspacesView(), "workspaces").([]any)
	return len(list) > 0
}

// rest is {...v} without some keys, as object rest destructuring makes it.
func rest(v any, without ...string) *jsvalue.Object {
	out := obj()
	switch x := v.(type) {
	case *jsvalue.Object:
		out = x.Copy()
	case []any:
		for i, e := range x {
			out.Set(itoa(i), e)
		}
	case string:
		for i, u := range []rune(x) {
			out.Set(itoa(i), string(u))
		}
	}
	for _, k := range without {
		out.Delete(k)
	}
	return out
}

func itoa(n int) string { return jscompat.Number(float64(n)) }

func (b *Bootstrap) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Bootstrap) config() *jsvalue.Object {
	data, _, parsed := readJSONFile(b.ConfigFile)
	if !parsed || data == nil {
		iso, _ := jscompat.ISOString(float64(b.now().UnixMilli()))
		data = obj("updatedAt", iso)
	}
	out := rest(data, "authPassword", "authSecret")
	out.Set("hasAuthPassword", truthy(opt(data, "authPassword")))
	out.Set("hostEnvLocked", b.HostLocked)
	out.Set("bindHostIsLocal", b.BindLocal)
	return out
}

// spreadAll is list.map(c => ({...c})); false where the previous runtime threw.
func spreadAll(v any) ([]any, bool) {
	if nullish(v) {
		return []any{}, true
	}
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]any, len(list))
	for i, e := range list {
		out[i] = rest(e)
	}
	return out, true
}

// listView builds the quick prompts or sidebar items from their file: the built-ins (each on unless disabled), the
// person's own, their order; a file that cannot be read gives the built-ins alone.
func listView(file any, builtins []*jsvalue.Object) *jsvalue.Object {
	empty := func() *jsvalue.Object {
		b := make([]any, len(builtins))
		for i, x := range builtins {
			b[i] = x.Copy()
		}
		return obj("builtins", b, "custom", []any{}, "order", []any{})
	}
	if file == nil {
		return empty()
	}
	disabled := map[string]bool{}
	switch d := opt(file, "disabledBuiltinIds").(type) {
	case []any:
		for _, x := range d {
			if s, ok := x.(string); ok {
				disabled[s] = true
			}
		}
	case string:
		for _, r := range d {
			disabled[string(r)] = true
		}
	case nil:
	default:
		if !isUndefined(d) {
			return empty()
		}
	}
	custom, ok := spreadAll(opt(file, "custom"))
	if !ok {
		return empty()
	}
	order := opt(file, "order")
	if nullish(order) {
		order = []any{}
	}
	b := make([]any, len(builtins))
	for i, x := range builtins {
		c := x.Copy()
		c.Set("enabled", !disabled[str(opt(x, "id"))])
		b[i] = c
	}
	return obj("builtins", b, "custom", custom, "order", order)
}

// quickPrompts reads a legacy flat list as the file it migrates to.
func (b *Bootstrap) quickPrompts() *jsvalue.Object {
	data, _, parsed := readJSONFile(filepath.Join(b.DataDir, "quick-prompts.json"))
	if !parsed {
		return listView(nil, builtinPrompts)
	}
	if list, ok := data.([]any); ok {
		builtin := map[string]bool{}
		for _, x := range builtinPrompts {
			builtin[str(opt(x, "id"))] = true
		}
		var disabled, custom []any
		for _, p := range list {
			id := opt(p, "id")
			if s, ok := id.(string); ok && builtin[s] {
				if nullish(p) {
					return listView(nil, builtinPrompts)
				}
				if !truthy(opt(p, "enabled")) {
					disabled = append(disabled, s)
				}
				continue
			}
			if nullish(p) {
				return listView(nil, builtinPrompts)
			}
			custom = append(custom, p)
		}
		data = obj("custom", append([]any{}, custom...), "disabledBuiltinIds", append([]any{}, disabled...), "order", []any{})
	}
	return listView(data, builtinPrompts)
}

func (b *Bootstrap) sidebarItems() *jsvalue.Object {
	data, _, parsed := readJSONFile(filepath.Join(b.DataDir, "sidebar-items.json"))
	if !parsed {
		return listView(nil, builtinItems)
	}
	return listView(data, builtinItems)
}

// toolsReady is kept once known good and checked again in the background past half a minute, as the previous runtime
// did: tmux and git do not come and go between page loads.
func (b *Bootstrap) toolsReady() bool {
	b.mu.Lock()
	if b.toolsOK != nil && *b.toolsOK {
		ok := *b.toolsOK
		if time.Since(b.toolsAt) > toolsRevalidate && !b.refresh {
			b.refresh = true
			go func() {
				v := b.Tools()
				b.mu.Lock()
				b.toolsOK, b.toolsAt, b.refresh = &v, time.Now(), false
				b.mu.Unlock()
			}()
		}
		b.mu.Unlock()
		return ok
	}
	b.mu.Unlock()
	v := b.Tools()
	b.mu.Lock()
	b.toolsOK, b.toolsAt = &v, time.Now()
	b.mu.Unlock()
	return v
}

// Serve is GET /bootstrap.
func (b *Bootstrap) Serve(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeEmpty(w, 405)
		return
	}
	if !b.HasWorkspaces() && b.CreateFirst != nil {
		if err := b.CreateFirst(); err != nil {
			writeText(w, 500, "Internal Server Error")
			return
		}
	}
	var owner any = "Workspace"
	if o := b.Owner(r); truthy(o) {
		owner = o
	}
	ready := b.toolsReady()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, 200, obj("owner", owner, "preflightOk", ready, "initialWorkspace", b.workspacesView(), "initialConfig", b.config(),
		"initialQuickPrompts", b.quickPrompts(), "initialSidebarItems", b.sidebarItems()))
}
