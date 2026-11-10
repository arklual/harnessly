package apps

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Builtin apps: apps that come with the host, in its own bundle, and run in the host's page instead of a
// frame. They are written as any app is, with a manifest, and get no right the list of the surface does not give any
// app: what is theirs alone is where they run. Sinopia is the first: builtin/sinopia/harnessly/app.json.
//
// A builtin app with a screen of its own may declare points: the places other apps contribute to (`points`), and the
// targets of older manifests that name its screen (`aliases`, such as sinopia.<kind>). A record of the registry of
// contributions (contributions.go) that goes into a point carries the point's owner; the reader of the point's field
// of a manifest is the owner's code (pointReaders). Its agent tools are in its manifest too (`backend.tools`): the
// host's tools server offers them, and the owner's code answers them in the runtime's process (builtinCalls).

//go:embed builtin/*/harnessly/app.json
var builtinFS embed.FS

// Delivery of a builtin app; an installed app is a package and names none.
const DeliveryBuiltin = "builtin"

// PointDecl is a point an app declares: a place other apps put contributions in.
type PointDecl struct {
	// Name is <the app's short name>.<what>, such as sinopia.panel.
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Field is where a manifest declares contributions to the point (sinopia.panels[]).
	Field string `json:"field"`
}

// AliasDecl is a target of the manifests that names the app's screen: a family (sinopia.<kind>) or a wildcard
// (sinopia.*). Contributions of such a manifest go into the app's points, scoped by what follows the prefix.
type AliasDecl struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
}

// BackendTool is a tool of a builtin app's backend for the agent of a chat: the host's tools server offers it as it is
// written and the app's code in the runtime answers it.
type BackendTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// Call is the method of the app's code the tool runs (for Sinopia, a Document API method).
	Call string `json:"call"`
}

// builtinCall answers one backend tool of a builtin app for the host tools of a chat.
type builtinCall func(t HostTools, tool BackendTool, args map[string]any) (any, error)

var (
	// pointReaders read a manifest's contributions to a point, by the point's name; the point's owner provides them.
	pointReaders = map[string]func(m *Manifest) []Contribution{}
	// builtinCalls answer the backend tools of a builtin app, by its id.
	builtinCalls  = map[string]builtinCall{}
	pointNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}\.[a-z][a-z0-9-]{0,39}$`)
	aliasNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}\.(<[a-z]+>|\*)$`)
	backendToolRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// builtins are read once, when first asked: the surface's tables (surface.go) are made in an init of their own.
var builtins = sync.OnceValue(func() []*Manifest {
	var out []*Manifest
	names, err := builtinFS.ReadDir("builtin")
	if err != nil {
		panic(err)
	}
	for _, n := range names {
		raw, err := builtinFS.ReadFile(path.Join("builtin", n.Name(), ManifestPath))
		if err != nil {
			panic(err)
		}
		m, err := parseBuiltin(raw)
		if err != nil {
			panic(fmt.Sprintf("apps: builtin %s: %v", n.Name(), err))
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
})

// parseBuiltin reads a builtin app's manifest: the fields of any manifest plus the ones only the host's own apps have.
// It is checked here, not with Manifest.Check: a builtin has no package and no views of its own.
func parseBuiltin(raw []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	switch {
	case !idRe.MatchString(m.ID):
		return nil, fmt.Errorf("id %q", m.ID)
	case m.Delivery != DeliveryBuiltin:
		return nil, fmt.Errorf("delivery is %q, not builtin", m.Delivery)
	case strings.TrimSpace(m.Name) == "" || !semverRe.MatchString(m.Version):
		return nil, fmt.Errorf("name and version are required")
	}
	short := m.ID[strings.LastIndex(m.ID, ".")+1:]
	for _, p := range m.Permissions {
		if !permissions[p] {
			return nil, fmt.Errorf("%q is not a right of the surface", p)
		}
	}
	seen := map[string]bool{}
	for _, p := range m.Points {
		if !pointNameRe.MatchString(p.Name) || !strings.HasPrefix(p.Name, short+".") || p.Field == "" || seen[p.Name] {
			return nil, fmt.Errorf("point %q: a name of %s.<what>, once, with a field", p.Name, short)
		}
		seen[p.Name] = true
	}
	for _, a := range m.Aliases {
		if !aliasNameRe.MatchString(a.Name) || !strings.HasPrefix(a.Name, short+".") {
			return nil, fmt.Errorf("alias %q: %s.<family> or %s.*", a.Name, short, short)
		}
	}
	if m.Backend != nil {
		if m.Backend.URL != "" {
			return nil, fmt.Errorf("a builtin's backend runs in the runtime, it has no url")
		}
		tools := map[string]bool{}
		for _, t := range m.Backend.Tools {
			if !backendToolRe.MatchString(t.Name) || tools[t.Name] || t.Call == "" || !json.Valid(t.InputSchema) {
				return nil, fmt.Errorf("backend tool %q: a name, once, a call and an input schema", t.Name)
			}
			tools[t.Name] = true
		}
	}
	return &m, nil
}

// Builtins are the host's own apps, by id.
func Builtins() []*Manifest { return builtins() }

// IsBuiltin says whether an id is a builtin app's: no package takes it.
func IsBuiltin(id string) bool { return builtinOf(id) != nil }

func builtinOf(id string) *Manifest {
	for _, m := range builtins() {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// Point is a declared point with its owner.
type Point struct {
	PointDecl
	Owner string `json:"owner"`
}

// AppPoints are the points the builtin apps declare, in the order of the apps and their manifests.
func AppPoints() []Point {
	var out []Point
	for _, m := range builtins() {
		for _, p := range m.Points {
			out = append(out, Point{PointDecl: p, Owner: m.ID})
		}
	}
	return out
}

// aliasOwner is the builtin app whose screen a target names through one of its aliases (sinopia.slides,
// sinopia.acme.board, sinopia.*), and the scope: what follows the prefix ("*" for the wildcard). ok is false for a
// target of the host's own.
func aliasOwner(target string) (owner, scope string, ok bool) {
	for _, m := range builtins() {
		for _, a := range m.Aliases {
			prefix := a.Name[:strings.LastIndex(a.Name, ".")+1]
			rest, has := strings.CutPrefix(target, prefix)
			if !has || rest == "" {
				continue
			}
			if rest == "*" {
				if strings.HasSuffix(a.Name, ".*") {
					return m.ID, "*", true
				}
				continue
			}
			if !strings.HasSuffix(a.Name, ".*") {
				return m.ID, rest, true
			}
		}
	}
	return "", "", false
}

// builtinTool is a backend tool of a builtin app by its name, with the app.
func builtinTool(name string) (*Manifest, BackendTool, bool) {
	for _, m := range builtins() {
		if m.Backend == nil {
			continue
		}
		for _, t := range m.Backend.Tools {
			if t.Name == name {
				return m, t, true
			}
		}
	}
	return nil, BackendTool{}, false
}

// plain turns what the MCP server parsed (jsvalue objects) into plain maps and lists.
func plain(v any) any {
	switch x := v.(type) {
	case *jsvalue.Object:
		m := map[string]any{}
		for _, k := range x.Keys() {
			c, _ := x.Get(k)
			m[k] = plain(c)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, c := range x {
			out[i] = plain(c)
		}
		return out
	}
	return v
}
