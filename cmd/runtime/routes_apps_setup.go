package main

import (
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// What the person sets up for an app (internal/apps/setup.go): its settings, drawn as a form from the manifest's
// schema, and its secrets, put into the machine's safe off the chat. Only the page's session reaches these routes: an
// agent's token and a view's session do not. A secret's value goes in and never comes out: the answer says whether
// each one has a value.
//
//	POST <platform>/apps/setup           {id}                 the form, the values and what the app waits for
//	POST <platform>/apps/setup/settings  {id, values}         saves the values once they pass the schema (else problems)
//	POST <platform>/apps/setup/secret    {id, name, value}    puts a secret's value ({remove: true} forgets it)
//	POST <platform>/apps/setup/connection {id, name}          allows the app the person's connection ({remove: true} takes it back)

type setupBody struct {
	ID     string         `json:"id"`
	Values map[string]any `json:"values"`
	Name   string         `json:"name"`
	Value  string         `json:"value"`
	Remove bool           `json:"remove"`
}

// setupAnswer is the whole of «Настройка» of an app as the page draws it.
type setupAnswer struct {
	Fields  []apps.SettingField `json:"fields"`
	Values  map[string]any      `json:"values"`
	Secrets []apps.SecretState  `json:"secrets"`
	// Waiting are the names of the required secrets that have no value: the app is «ждёт секрет».
	Waiting []string `json:"waiting"`
	// Problems are by setting: what a save did not accept (nothing was saved then); absent after a good save.
	Problems map[string]string `json:"problems,omitempty"`
	// Connections are the person's connections the app's servers go through: allowed to this app, and signed in.
	Connections []setupConnection `json:"connections"`
}

// setupConnection is one connection an app names: the catalog's id and name, whether the person allowed it to this app
// and whether they are signed in to it (Настройки → Подключения).
type setupConnection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Granted   bool   `json:"granted"`
	Connected bool   `json:"connected"`
}

func appSetupRoutes(d deps, reg *apps.Registry, storage *apps.Storage) []gate.Route {
	store := safeFor(d).store.Local // an app's secrets stay in the file
	answer := func(app apps.Installed) (setupAnswer, error) {
		values, err := storage.Settings(app.Manifest, app.ID)
		if err != nil {
			return setupAnswer{}, err
		}
		has := func(name string) bool { return store.HasApp(app.ID, name) }
		out := setupAnswer{Fields: app.Manifest.SettingFields(), Values: values, Secrets: app.Manifest.SecretStates(has), Waiting: app.Manifest.WaitingFor(has)}
		if out.Fields == nil {
			out.Fields = []apps.SettingField{}
		}
		if out.Waiting == nil {
			out.Waiting = []string{}
		}
		out.Connections = []setupConnection{}
		if pkg, _ := apps.CachedPackage(app.Dir); pkg != nil {
			for _, id := range pkg.Connections() {
				_, connected := mcpFor(d).ByCatalog(id)
				out.Connections = append(out.Connections, setupConnection{ID: id, Name: mcpconn.CatalogName(id),
					Granted: reg.ConnectionGranted(app.ID, id), Connected: connected})
			}
		}
		return out, nil
	}
	handle := func(do func(app apps.Installed, b setupBody) (int, any)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if sameorigin.Refuse(w, r) {
				return
			}
			if !d.SessionOK(r) {
				secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			if r.Method != http.MethodPost {
				secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			var b setupBody
			if !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			app, err := reg.Get(b.ID)
			if err != nil || app.Manifest == nil {
				secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
				return
			}
			status, out := do(app, b)
			secretJSON(w, status, out)
		}
	}
	failed := func(err error) (int, any) {
		status, out := bridgeRefused(err)
		return status, out
	}
	read := func(app apps.Installed, _ setupBody) (int, any) {
		out, err := answer(app)
		if err != nil {
			return failed(err)
		}
		return http.StatusOK, out
	}
	save := func(app apps.Installed, b setupBody) (int, any) {
		bad, err := storage.SaveSettings(app.Manifest, app.ID, b.Values)
		if err != nil {
			return failed(err)
		}
		out, err := answer(app)
		if err != nil {
			return failed(err)
		}
		out.Problems = bad
		return http.StatusOK, out
	}
	secret := func(app apps.Installed, b setupBody) (int, any) {
		if !app.Manifest.Allows("secrets:" + b.Name) {
			return http.StatusNotFound, map[string]string{"error": "not-found"}
		}
		var err error
		if b.Remove {
			err = store.RemoveApp(app.ID, b.Name)
			if secrets.ReasonOf(err) == secrets.ReasonNotFound {
				err = nil
			}
		} else {
			err = store.PutApp(app.ID, b.Name, b.Value)
		}
		if err != nil {
			status := http.StatusInternalServerError
			switch secrets.ReasonOf(err) {
			case secrets.ReasonEmpty, secrets.ReasonTooLarge, secrets.ReasonBadName:
				status = http.StatusBadRequest
			}
			return status, map[string]string{"error": secrets.ReasonOf(err)}
		}
		return read(app, b)
	}
	// connection: the person allows the app one of the connections its package names, or takes it back. A page route:
	// the agent has no page session, and its CLI word (harnessly app connect) is refused to it.
	connection := func(app apps.Installed, b setupBody) (int, any) {
		var err error
		if b.Remove {
			err = reg.RevokeConnection(app.ID, b.Name)
		} else {
			err = reg.GrantConnection(app.ID, b.Name)
		}
		if err != nil {
			return http.StatusNotFound, map[string]string{"error": "not-found"}
		}
		return read(app, b)
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var routes []gate.Route
	for _, e := range []struct {
		path string
		h    http.HandlerFunc
	}{{"/apps/setup", handle(read)}, {"/apps/setup/settings", handle(save)}, {"/apps/setup/secret", handle(secret)},
		{"/apps/setup/connection", handle(connection)}} {
		routes = append(routes,
			gate.Route{Match: is(platformAPI + e.path), Handler: e.h},
			gate.Route{Match: is("/agents" + platformAPI + e.path), Handler: e.h})
	}
	return routes
}

// appMCPProxyRoute is the runtime's proxy for the remote MCP servers of apps that use a secret (internal/apps/mcpproxy.go):
//
//	ANY /api/cli/apps/mcp/<app>/<server>
//
// Only an agent's launch token passes, and only for an app on in that launch's chat (the grant the launch wrote). The
// person's cli-token is refused: the proxy is for the agent's MCP client, which carries the launch's token.
func appMCPProxyRoute(d deps, reg *apps.Registry) gate.Route {
	store := safeFor(d).store.Local // an app's secrets stay in the file
	ms := mcpFor(d)
	connProxy := &mcpProxy{d: d, ms: ms, client: mcpProxyClient(nil)}
	deny := func(w http.ResponseWriter, status int, why string) {
		secretJSON(w, status, map[string]string{"error": why})
	}
	return gate.Route{
		Match: func(r *http.Request) bool {
			return strings.HasPrefix(r.URL.Path, apps.ProxyPrefix) || strings.HasPrefix(r.URL.Path, "/agents"+apps.ProxyPrefix)
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, agent := d.CLI.AgentOf(r)
			if !agent {
				deny(w, http.StatusForbidden, "Forbidden")
				return
			}
			rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), apps.ProxyPrefix)
			id, tail, ok := strings.Cut(rest, "/")
			server, sub, _ := strings.Cut(tail, "/")
			if !ok || id == "" || server == "" {
				deny(w, http.StatusNotFound, "not-found")
				return
			}
			if !slices.Contains(reg.GrantedApps(session), id) {
				deny(w, http.StatusForbidden, "not-on")
				return
			}
			pkg, err := reg.Package(id)
			if err != nil {
				deny(w, http.StatusNotFound, "not-found")
				return
			}
			srv, has := pkg.MCP[server]
			if has && srv.Connection != "" && srv.Command == "" {
				// The person's own connection: the connections' proxy signs the call in as the person. The legacy
				// transport's /message follows the server's name.
				conn, ok := ms.ByCatalog(srv.Connection)
				if !reg.ConnectionGranted(id, srv.Connection) || !ok {
					deny(w, http.StatusConflict, "waiting-connection")
					return
				}
				_ = reg.Journal(id, apps.JournalEntry{What: "connection.call", Class: apps.ClassHigh, OK: true, Detail: srv.Connection})
				connProxy.serve(w, r, conn.ID+strings.TrimSuffix("/"+sub, "/"))
				return
			}
			if sub != "" {
				deny(w, http.StatusNotFound, "not-found")
				return
			}
			if !has || srv.Command != "" || len(srv.Secrets()) == 0 {
				deny(w, http.StatusNotFound, "not-found")
				return
			}
			values := map[string]string{}
			for _, n := range srv.Secrets() {
				if v, err := store.RevealApp(id, []string{n}); err == nil {
					values[n] = v[n]
				}
			}
			names := slices.Sorted(maps.Keys(values))
			_ = reg.Journal(id, apps.JournalEntry{What: "secret.call", Class: apps.ClassHigh, OK: true, Detail: server + ": " + strings.Join(names, ", ")})
			apps.ProxyMCP(w, r, srv, values)
		}),
	}
}
