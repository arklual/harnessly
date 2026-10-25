package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Machine is the machine id of a runtime on its own; in the cluster the gateway names machines by their fleet id.
const v2Machine = "local"

// v2Enabled: the live v2 runs unless RUNTIME_V2=off, which leaves the runtime without a v2 (a page reads that as "update
// the runtime", like a runtime that never had one).
func v2Enabled() bool { return os.Getenv("RUNTIME_V2") != "off" }

// v2Routes serves /api/v2 (spec §2–3), on the page's session like the Legacy API, under /agents and the app base path
// like it. RUNTIME_V2_FAKE=1 serves the fixtures and a scripted agent instead of the conversations (the page's
// development, the end-to-end tests). Otherwise it is the core's v2: while the core is starting it answers 503, and a
// runtime whose core is off, or with RUNTIME_V2=off, has no v2: /api/v2/hello is a 404.
func v2Routes(d deps, c *core) []gate.Route {
	if srv, extra := v2Fake(d); srv != nil {
		h := http.Handler(srv)
		if extra != nil {
			h = extra(h)
		}
		return []gate.Route{{Match: wire.Matches, Handler: v2Session(d, h)}}
	}
	if !v2Enabled() {
		return nil
	}
	live := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch v := c.v2.Load(); {
		case v == nil && !d.SliceOn("core"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"r":"refused","code":"not-found"}`))
		case v == nil:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"r":"refused","code":"timeout"}`))
		default:
			v.srv.ServeHTTP(w, r)
		}
	})
	return []gate.Route{{Match: wire.Matches, Handler: v2Session(d, live)}}
}

// v2Server is a Server on a service and a machine source, with the runtime's limits, its uploads and the answers of
// its commands under <data>/v2.
func v2Server(d deps, hub *wire.Hub, svc wire.Service, machine wire.MachineSource, features []wire.Feature, keepOps bool) *wire.Server {
	opsFile := ""
	if keepOps {
		opsFile = filepath.Join(d.DataDir, "v2", "ops.json")
	}
	return &wire.Server{
		Hub: hub, Service: svc, Machine: machine, Ops: wire.NewOpLog(opsFile),
		Uploads:  &wire.Uploads{Dir: filepath.Join(d.DataDir, "v2", "uploads"), Limits: wire.DefaultLimits},
		Me:       &wire.FileMe{Path: filepath.Join(d.DataDir, "v2", "me.json"), Name: os.Getenv("RUNTIME_OWNER_NAME")},
		Dirs:     func(ctx context.Context) ([]wire.Dir, error) { return v2Dirs(ctx, d.WorkspaceRoot) },
		Version:  env("AGENT_VERSION", agent.BuildVersion),
		Features: features, Limits: wire.DefaultLimits,
		NotFound:      func(err error) bool { return errors.Is(err, convo.ErrNotFound) },
		UnknownCursor: func(err error) bool { return errors.Is(err, convo.ErrUnknownCursor) },
	}
}

// v2Session lets in a page session and refuses anyone else with 401: the page shows its sign-in, it does not retry.
func v2Session(d deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !d.SessionOK(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"r":"refused","code":"forbidden"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
