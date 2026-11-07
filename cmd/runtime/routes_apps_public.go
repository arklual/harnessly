package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The public catalog (internal/apps/public.go). Who may call what:
//
//	the cli-token (the person's shell: harnessly app public …)
//	  GET  /api/cli/apps/public?q=            browse
//	  POST /api/cli/apps/public/publish       the author puts the version on stable in the catalog
//	  POST /api/cli/apps/public/install       install for oneself, naming the sum that was shown
//	the person's page session
//	  GET  <platform>/apps/public?q=          browse
//	  POST <platform>/apps/public/install     install for oneself
//	the machine agent (the platform's relay)
//	  POST <platform>/apps/public/sync        the platform's reviewed list
//	  POST <platform>/apps/public/snapshot    this machine's entries, for the platform to review
//	  POST <platform>/apps/public/pending     the entries that wait for a look
//	  POST <platform>/apps/public/review      a person at the platform looked: approve or refuse
//
// A trusted app is in none of these: the registry refuses it on the way in and on the way out.

// publicRefused words the registry's refusals of the public catalog; any other goes to shareRefused.
func publicRefused(w http.ResponseWriter, err error) {
	for _, r := range []struct {
		err    error
		code   string
		status int
	}{
		{apps.ErrTrustedPublic, "trusted", http.StatusUnprocessableEntity},
		{apps.ErrNotOnStable, "not-on-stable", http.StatusConflict},
		{apps.ErrPublishChecks, "checks", http.StatusUnprocessableEntity},
		{apps.ErrNotPublic, "not-public", http.StatusNotFound},
		{apps.ErrPublicChanged, "changed", http.StatusConflict},
	} {
		if errors.Is(err, r.err) {
			secretJSON(w, r.status, map[string]string{"error": r.code, "message": err.Error()})
			return
		}
	}
	shareRefused(w, err)
}

func appPublicRoutes(d deps, reg *apps.Registry, version string) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	now := time.Now
	method := func(w http.ResponseWriter, r *http.Request, m string) bool {
		if r.Method != m {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	cli := func(w http.ResponseWriter, r *http.Request, m string) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		return method(w, r, m)
	}
	page := func(w http.ResponseWriter, r *http.Request, m string) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return method(w, r, m)
	}
	platform := func(w http.ResponseWriter, r *http.Request) bool {
		if d.MachineAgent == nil || !d.MachineAgent(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		return method(w, r, http.MethodPost)
	}
	who := func(r *http.Request) string {
		if d.Claims != nil {
			if sub, ok := d.Claims(r)["sub"].(string); ok && sub != "" && len(sub) <= 254 {
				return sub
			}
		}
		return "cli"
	}

	browse := func(w http.ResponseWriter, r *http.Request) {
		list, err := reg.PublicCatalog(r.URL.Query().Get("q"))
		if err != nil {
			publicRefused(w, err)
			return
		}
		entries := make([]*apps.CatalogEntry, len(list))
		for i := range list {
			entries[i] = &list[i].CatalogEntry
		}
		reg.Precheck(entries, version)
		secretJSON(w, http.StatusOK, map[string]any{"apps": list})
	}
	install := func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID  string `json:"id"`
			Sum string `json:"sum"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		got, problems, err := reg.InstallFromPublic(b.ID, apps.PublicInstallOptions{HostVersion: version, Sum: b.Sum})
		if problems == nil {
			problems = apps.Problems{}
		}
		if errors.Is(err, apps.ErrInvalid) {
			secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": problems})
			return
		}
		if errors.Is(err, apps.ErrKindTaken) {
			secretJSON(w, http.StatusConflict, map[string]any{"error": "kind-taken", "message": err.Error()})
			return
		}
		if errors.Is(err, apps.ErrCommandTaken) {
			secretJSON(w, http.StatusConflict, map[string]any{"error": "command-taken", "message": err.Error()})
			return
		}
		if err != nil {
			publicRefused(w, err)
			return
		}
		agent.Log("info", "apps: installed from the public catalog", agent.F{K: "id", V: got.ID}, agent.F{K: "version", V: got.Version})
		secretJSON(w, http.StatusOK, map[string]any{"app": viewOfApp(got, true), "problems": problems})
	}

	return []gate.Route{
		{Match: is("/api/cli/apps/public"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cli(w, r, http.MethodGet) {
				browse(w, r)
			}
		})},
		{Match: is("/api/cli/apps/public/install"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cli(w, r, http.MethodPost) {
				install(w, r)
			}
		})},
		{Match: is("/api/cli/apps/public/publish"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cli(w, r, http.MethodPost) {
				return
			}
			var b struct {
				ID      string `json:"id"`
				Version string `json:"version"`
			}
			if !readSecretBody(r, &b) || b.ID == "" || b.Version == "" {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			e, err := reg.PublishPublic(b.ID, b.Version, who(r), now())
			if err != nil {
				publicRefused(w, err)
				return
			}
			agent.Log("info", "apps: published to the public catalog", agent.F{K: "id", V: e.ID}, agent.F{K: "version", V: e.Version}, agent.F{K: "review", V: e.Review})
			secretJSON(w, http.StatusOK, e)
		})},
		{Match: is(platformAPI + "/apps/public"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if page(w, r, http.MethodGet) {
				browse(w, r)
			}
		})},
		{Match: is("/agents" + platformAPI + "/apps/public"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if page(w, r, http.MethodGet) {
				browse(w, r)
			}
		})},
		{Match: is(platformAPI + "/apps/public/install"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if page(w, r, http.MethodPost) {
				install(w, r)
			}
		})},
		{Match: is("/agents" + platformAPI + "/apps/public/install"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if page(w, r, http.MethodPost) {
				install(w, r)
			}
		})},
		{Match: is(platformAPI + "/apps/public/sync"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !platform(w, r) {
				return
			}
			var s apps.PublicSnapshot
			if !readAppsBody(r, &s) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			problems, err := reg.ApplyPublic(s, version, now())
			if err != nil {
				if errors.Is(err, apps.ErrStaleTeam) {
					shareRefused(w, err)
					return
				}
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": err.Error()})
				return
			}
			if problems == nil {
				problems = []string{}
			}
			secretJSON(w, http.StatusOK, map[string]any{"ok": true, "problems": problems})
		})},
		{Match: is(platformAPI + "/apps/public/snapshot"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !platform(w, r) {
				return
			}
			s, err := reg.PublicSnapshot()
			if err != nil {
				publicRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, s)
		})},
		{Match: is(platformAPI + "/apps/public/pending"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !platform(w, r) {
				return
			}
			list, err := reg.PendingPublic()
			if err != nil {
				publicRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"apps": list})
		})},
		{Match: is(platformAPI + "/apps/public/review"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !platform(w, r) {
				return
			}
			var b struct {
				ID      string `json:"id"`
				Version string `json:"version"`
				Approve bool   `json:"approve"`
			}
			if !readSecretBody(r, &b) || b.ID == "" || b.Version == "" {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
				return
			}
			if err := reg.ReviewPublic(b.ID, b.Version, b.Approve, now()); err != nil {
				publicRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "version": b.Version, "approved": b.Approve})
		})},
	}
}
