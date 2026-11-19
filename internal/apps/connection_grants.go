package apps

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// A server of .mcp.json that names a connection ("connection": "<catalog id>") reaches the person's own sign-in, so an
// app uses it only after the person agreed to that connection for that app (harnessly app connect, or the page). The
// agreements are the machine's, in <registry>/connection-grants.json as {appID: [catalogID…]}. Only the person's routes
// write it (the agent's token is refused there). An update of an app does not widen it: a connection the new version
// names for the first time waits for its own agreement; removing the app ends its agreements.

const connectionGrantsFile = "connection-grants.json"

var connectionGrantsMu sync.Mutex

func (r *Registry) connectionGrantsPath() string { return filepath.Join(r.Root, connectionGrantsFile) }

func (r *Registry) readConnectionGrants() map[string][]string {
	out := map[string][]string{}
	raw, err := os.ReadFile(r.connectionGrantsPath())
	if err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = map[string][]string{}
	}
	return out
}

func (r *Registry) writeConnectionGrants(m map[string][]string) error {
	for k, v := range m {
		if len(v) == 0 {
			delete(m, k)
		}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.connectionGrantsPath(), append(raw, '\n'))
}

// The agreement names how far it goes: an entry "<id>" lets the app use the whole connection (a server of .mcp.json, and
// hl.connections with access all), "<id>:read" only what reads (hl.connections: tools the server marks read-only, GET
// requests). One app holds one of the two for a connection.
const readSuffix = ":read"

func splitGrant(entry string) (id, access string) {
	if id, ok := strings.CutSuffix(entry, readSuffix); ok {
		return id, AccessRead
	}
	return entry, AccessAll
}

// ConnectionAccess is how far the person agreed to the app using their connection from catalog entry id: "" (not at
// all), "read" or "all".
func (r *Registry) ConnectionAccess(app, id string) string {
	if r == nil || app == "" || id == "" {
		return ""
	}
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	out := ""
	for _, e := range r.readConnectionGrants()[app] {
		if gid, access := splitGrant(e); gid == id && (out == "" || access == AccessAll) {
			out = access
		}
	}
	return out
}

// ConnectionGranted says whether the person agreed to the app using their whole connection from catalog entry id (a
// server of .mcp.json goes through it).
func (r *Registry) ConnectionGranted(app, id string) bool {
	return r.ConnectionAccess(app, id) == AccessAll
}

// GrantConnection records the person's agreement as far as the app's package asks for the connection. The package must
// name the connection.
func (r *Registry) GrantConnection(app, id string) error { return r.GrantConnectionAccess(app, id, "") }

// GrantConnectionAccess records the agreement as far as access says (read or all; "" is as far as the app asks). The
// app may not be given more than it declared: .mcp.json's servers and `connections` of the manifest.
func (r *Registry) GrantConnectionAccess(app, id, access string) error {
	pkg, err := r.Package(app)
	if err != nil {
		return err
	}
	declared := pkg.DeclaredAccess(id)
	if declared == "" {
		return errors.New("the app names no such connection")
	}
	switch access {
	case "":
		access = declared
	case AccessRead, AccessAll:
		if access == AccessAll && declared != AccessAll {
			return errors.New("the app asks for read access to this connection only")
		}
	default:
		return errors.New("access is read or all")
	}
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	m := r.readConnectionGrants()
	m[app] = slices.DeleteFunc(m[app], func(e string) bool { gid, _ := splitGrant(e); return gid == id })
	entry := id
	if access == AccessRead {
		entry = id + readSuffix
	}
	m[app] = append(m[app], entry)
	slices.Sort(m[app])
	return r.writeConnectionGrants(m)
}

// RevokeConnection takes the agreement back (a missing one is fine).
func (r *Registry) RevokeConnection(app, id string) error {
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	m := r.readConnectionGrants()
	m[app] = slices.DeleteFunc(m[app], func(e string) bool { gid, _ := splitGrant(e); return gid == id })
	return r.writeConnectionGrants(m)
}

// dropConnectionGrants ends all of an app's agreements.
func (r *Registry) dropConnectionGrants(app string) {
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	m := r.readConnectionGrants()
	if _, ok := m[app]; !ok {
		return
	}
	delete(m, app)
	if err := r.writeConnectionGrants(m); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
}
