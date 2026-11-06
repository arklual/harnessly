package apps

import (
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// ConnectionSource is the person's connections (Settings → Connections) as far as a launch needs them: the connection
// made from a catalog entry, and whether it can be used now (turned on, signed in).
type ConnectionSource interface {
	// Transport is "http" or "sse" for the person's connection made from catalog entry id; ok is false when there is none
	// or it waits for a sign-in.
	Transport(id string) (transport string, ok bool)
}

// A server of .mcp.json that names a connection ("connection": "<catalog id>") reaches the person's own server, signed
// in as the person. The launch writes it like a server with a secret (launch_secrets.go): its entry names the runtime's
// proxy for the apps (ProxyPrefix) and carries the launch's own token, so neither the package, nor the snapshot of the
// guard, nor the agent's environment holds the person's token: the proxy adds it per call from the machine's safe. The
// connections live on the person's machine, so two people on one app each go under themselves.
//
// A person without the connection (or not signed in) leaves the app «ждёт вход»: the launch does not fail, the server is
// just not in it. A resume is a launch, so signing in takes effect at the next one.
func (l *Launcher) connectionEntry(servers map[string]any, name string, srv map[string]any, decl MCPServer, app, session, token, origin string, tokenHeaders []string) {
	transport := ""
	ok := false
	if l.Connections != nil && token != "" && session != "" && l.Registry.ConnectionGranted(app, decl.Connection) {
		transport, ok = l.Connections.Transport(decl.Connection)
	}
	if !ok {
		delete(servers, name)
		return
	}
	if origin == "" || len(tokenHeaders) == 0 {
		jsonlog.Log("warn", "apps: a server that uses a connection is left out: the runtime's address is unknown", jsonlog.F{K: "app", V: app})
		delete(servers, name)
		return
	}
	headers := map[string]any{}
	for _, h := range tokenHeaders {
		headers[h] = token
	}
	delete(srv, "connection")
	srv["type"] = transport
	srv["url"] = origin + ProxyPrefix + app + "/" + name
	srv["headers"] = headers
}
