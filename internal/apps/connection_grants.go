package apps

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// ConnectionGranted says whether the person agreed to the app using their connection from catalog entry id.
func (r *Registry) ConnectionGranted(app, id string) bool {
	if r == nil || app == "" || id == "" {
		return false
	}
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	return slices.Contains(r.readConnectionGrants()[app], id)
}

// GrantConnection records the person's agreement. The app's package must name the connection.
func (r *Registry) GrantConnection(app, id string) error {
	pkg, err := r.Package(app)
	if err != nil {
		return err
	}
	if !slices.Contains(pkg.Connections(), id) {
		return errors.New("the app names no such connection")
	}
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	m := r.readConnectionGrants()
	if !slices.Contains(m[app], id) {
		m[app] = append(m[app], id)
		slices.Sort(m[app])
	}
	return r.writeConnectionGrants(m)
}

// RevokeConnection takes the agreement back (a missing one is fine).
func (r *Registry) RevokeConnection(app, id string) error {
	connectionGrantsMu.Lock()
	defer connectionGrantsMu.Unlock()
	m := r.readConnectionGrants()
	m[app] = slices.DeleteFunc(m[app], func(s string) bool { return s == id })
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
