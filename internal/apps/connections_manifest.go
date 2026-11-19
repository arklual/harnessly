package apps

import (
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// The person's connections an app's views use through hl.connections. The manifest names what the app needs
// of them and how far it goes; the person agrees to each connection for each app (connection_grants.go); a view reaches
// only what is both declared and agreed to.
//
//	"connections": [
//	  {"id": "yandex-tracker", "access": "read", "reason": "Shows your open issues"},
//	  {"category": "mail", "access": "read"}
//	]
//
// id is a catalog entry; category is a group of the catalog (mail, tasks, …) and stands for every connection of the
// person in it, each agreed to apart. access is read (only calls the server marks read-only, and GET requests: the
// default) or all (also what changes something, each time after the person's yes).

// The two levels of access to a connection.
const (
	AccessRead = "read"
	AccessAll  = "all"
)

// MaxConnectionNeeds bounds the list: an app that wants everything is asked about piece by piece.
const MaxConnectionNeeds = 16

// ConnectionNeed is one connection (or group of them) the app asks for.
type ConnectionNeed struct {
	ID       string `json:"id,omitempty"`
	Category string `json:"category,omitempty"`
	Access   string `json:"access,omitempty"`
	// Reason is one line the consent card shows: what the app does with the connection.
	Reason string `json:"reason,omitempty"`
}

// access is the need's access with the default put in.
func (n ConnectionNeed) access() string {
	if n.Access == AccessAll {
		return AccessAll
	}
	return AccessRead
}

func (m *Manifest) checkConnections(add func(path, format string, a ...any)) {
	if len(m.Connections) == 0 {
		if m.Allows("connections.read") {
			add("permissions", "connections.read needs a `connections` list: name the services the app reads")
		}
		return
	}
	if len(m.Connections) > MaxConnectionNeeds {
		add("connections", "are up to %d", MaxConnectionNeeds)
	}
	if !m.Allows("connections.read") {
		add("connections", "need the permission connections.read")
	}
	seen := map[string]bool{}
	for i, n := range m.Connections {
		path := "connections[" + strconv.Itoa(i) + "]"
		switch {
		case n.ID != "" && n.Category != "":
			add(path, "names an id or a category, not both")
		case n.ID == "" && n.Category == "":
			add(path, "names a catalog id (`id`) or a group (`category`)")
		case n.ID != "" && !mcpconn.KnownConnectionID(n.ID):
			add(path+".id", "%q is not in the catalog of connections or a service of your own (custom-<slug>)", n.ID)
		case n.Category != "" && !mcpconn.KnownKind(n.Category):
			add(path+".category", "%q is not a group of the catalog (mail, calendar, tasks, docs, code, …)", n.Category)
		}
		if n.Access != "" && n.Access != AccessRead && n.Access != AccessAll {
			add(path+".access", "is read or all")
		}
		if n.access() == AccessAll && !m.Allows("connections.act") {
			add(path+".access", "all needs the permission connections.act")
		}
		if utf8.RuneCountInString(n.Reason) > 200 {
			add(path+".reason", "is up to 200 characters")
		}
		key := n.ID + "/" + n.Category
		if seen[key] {
			add(path, "is named twice")
		}
		seen[key] = true
	}
}

// DeclaredAccess is how far the app may go with the person's connection from catalog entry id: the widest of what the
// manifest's list names for the id or its group, and "all" for a server of .mcp.json that names it (that server is the
// whole connection). "" when the app names the connection nowhere.
func (p *Package) DeclaredAccess(id string) string {
	if p == nil || id == "" {
		return ""
	}
	out := ""
	if slices.Contains(p.Connections(), id) {
		out = AccessAll
	}
	if p.Manifest == nil {
		return out
	}
	kind := mcpconn.CatalogKind(id)
	for _, n := range p.Manifest.Connections {
		if n.ID == id || n.Category != "" && n.Category == kind {
			switch n.access() {
			case AccessAll:
				return AccessAll
			default:
				if out == "" {
					out = AccessRead
				}
			}
		}
	}
	return out
}

// ConnectionReason is the line the app gave for the connection, for the consent card ("" when it gave none).
func (p *Package) ConnectionReason(id string) string {
	if p == nil || p.Manifest == nil {
		return ""
	}
	kind := mcpconn.CatalogKind(id)
	for _, n := range p.Manifest.Connections {
		if (n.ID == id || n.Category != "" && n.Category == kind) && n.Reason != "" {
			return n.Reason
		}
	}
	return ""
}

// EffectiveConnectionAccess is how far the app may go with the connection now: the person's agreement, cut to what the
// installed version still declares (an update that dropped the connection or narrowed it ends what the old one was
// agreed to). "" when the app may not use it at all.
func (r *Registry) EffectiveConnectionAccess(app, id string) string {
	granted := r.ConnectionAccess(app, id)
	if granted == "" {
		return ""
	}
	pkg, err := r.Package(app)
	if err != nil {
		return ""
	}
	return min2(granted, pkg.DeclaredAccess(id))
}

// min2 is the narrower of two accesses: read under all, "" under both.
func min2(a, b string) string {
	if a == "" || b == "" {
		return ""
	}
	if a == AccessRead || b == AccessRead {
		return AccessRead
	}
	return AccessAll
}

// ConnectionNeeds is everything the package asks of the person's connections: the manifest's list, and each server of
// .mcp.json that names a connection (it takes the whole of it).
func (p *Package) ConnectionNeeds() []ConnectionNeed {
	if p == nil {
		return nil
	}
	var out []ConnectionNeed
	if p.Manifest != nil {
		out = append(out, p.Manifest.Connections...)
	}
	for _, id := range p.Connections() {
		if !slices.ContainsFunc(out, func(n ConnectionNeed) bool { return n.ID == id }) {
			out = append(out, ConnectionNeed{ID: id, Access: AccessAll})
		}
	}
	return out
}

// Level is the need's access with the default put in: read unless it says all.
func (n ConnectionNeed) Level() string { return n.access() }
