package main

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// Extensions as a person manages them, under the same flag as the slots (HARNESSLY_UI_SLOTS=on). The person's
// page session only; the CLI has the same actions as `harnessly app …` (routes_apps.go, this file's cli routes).
//
//	GET    <platform>/extensions                        every installed extension, off ones too: kind, settings, secrets (never values), data
//	POST   <platform>/extensions/inspect                {dir}: what installing the package would do, nothing is installed
//	POST   <platform>/extensions/install                {dir}: install or update
//	POST   <platform>/extensions/<id>/disable|enable
//	POST   <platform>/extensions/<id>/remove            {purge}: purge also forgets settings, secrets and data
//	PUT    <platform>/extensions/<id>/settings          {values}
//	PUT    <platform>/extensions/<id>/secrets/<name>    {value}: write-only
//	DELETE <platform>/extensions/<id>/secrets/<name>
//
// The slot lists the page gets from /installed-apps do not include an extension that is off.

// extView is an installed extension as the management page lists it.
type extView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Previous    string             `json:"previous,omitempty"`
	Kind        string             `json:"kind"`
	Trust       string             `json:"trust"`
	Targets     []string           `json:"targets"`
	Permissions []string           `json:"permissions"`
	Disabled    bool               `json:"disabled"`
	Team        bool               `json:"team,omitempty"`
	Dev         bool               `json:"dev,omitempty"`
	Settings    any                `json:"settings,omitempty"`
	Values      map[string]any     `json:"values,omitempty"`
	Secrets     []apps.SecretState `json:"secrets"`
	DataVersion int                `json:"dataVersion"`
	DataTarget  int                `json:"dataTarget"`
	Compat      []apps.CompatIssue `json:"compat,omitempty"`
	InstalledAt int64              `json:"installedAt"`
	UpdatedAt   int64              `json:"updatedAt"`
}

func extViewOf(a apps.Installed, safe *secrets.Store, st *apps.Storage, host string) extView {
	m := a.Manifest
	v := extView{ID: a.ID, Version: a.Version, Previous: a.Previous, Disabled: a.Off || a.Muted, Team: a.Team, Dev: a.Dev,
		InstalledAt: a.InstalledAt, UpdatedAt: a.UpdatedAt, Targets: []string{}, Permissions: []string{}, Secrets: []apps.SecretState{}}
	if m == nil {
		return v
	}
	v.Name, v.Description, v.Kind, v.Trust = m.Name, m.Description, m.ExtensionKind(), m.Trust
	v.Targets, v.Permissions = append(v.Targets, m.Targets...), append(v.Permissions, m.Permissions...)
	if len(m.Settings) > 0 {
		v.Settings = m.Settings
		if vals, err := st.Settings(m, a.ID); err == nil {
			v.Values = vals
		}
	}
	v.Secrets = m.SecretStates(func(name string) bool { return safe.HasApp(a.ID, name) })
	v.DataVersion = st.DataVersion(a.ID)
	if m.Data != nil {
		v.DataTarget = m.Data.Version
	}
	if rep := apps.CheckCompat([]apps.Installed{a}, host, st); len(rep.Apps) == 1 {
		v.Compat = rep.Apps[0].Issues
	}
	return v
}

func appExtRoutes(d deps, reg *apps.Registry, guests *apps.Guests, version string) []gate.Route {
	base := platformAPI + "/extensions"
	safe := safeFor(d).store.Local // an app's secrets stay in the file
	st := apps.OpenStorage(d.DataDir)
	fail := func(w http.ResponseWriter, err error) {
		agent.Log("warn", "extensions: "+err.Error())
		secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
	}
	// guard is the page session's check, and the flag's.
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if !uiSlotsOn() {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "off"})
			return false
		}
		return true
	}
	// under is what follows the base in the path ("" for the base itself).
	under := func(r *http.Request) (string, bool) {
		for _, prefix := range []string{base, "/agents" + base} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
				return strings.Trim(rest, "/"), true
			}
		}
		return "", false
	}
	listAll := func() ([]extView, error) {
		all, err := reg.ListAll()
		if err != nil {
			return nil, err
		}
		out := make([]extView, 0, len(all))
		for _, a := range all {
			out = append(out, extViewOf(a, safe, st, version))
		}
		return out, nil
	}
	one := func(id string) (extView, error) {
		a, err := reg.Find(id)
		if err != nil {
			return extView{}, err
		}
		return extViewOf(a, safe, st, version), nil
	}
	notFound := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
	}
	bad := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	method := func(w http.ResponseWriter, r *http.Request, m string) bool {
		if r.Method != m {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}

	type inspected struct {
		Package   *extView      `json:"package,omitempty"`
		Problems  apps.Problems `json:"problems"`
		Installed *string       `json:"installed,omitempty"`
		Host      string        `json:"host"`
		Blocked   string        `json:"blocked,omitempty"`
	}
	// readDir reads {dir} of a body: an absolute folder on this machine.
	readDir := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		var b struct {
			Dir string `json:"dir"`
		}
		if !readSecretBody(r, &b) || !filepath.IsAbs(b.Dir) {
			bad(w)
			return "", false
		}
		return filepath.Clean(b.Dir), true
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		rest, _ := under(r)
		parts := strings.Split(rest, "/")
		switch {
		case rest == "":
			if !method(w, r, http.MethodGet) {
				return
			}
			list, err := listAll()
			if err != nil {
				fail(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"extensions": list, "host": version})

		case rest == "inspect":
			if !method(w, r, http.MethodPost) {
				return
			}
			dir, ok := readDir(w, r)
			if !ok {
				return
			}
			pkg, ps := apps.LoadPackage(dir)
			out := inspected{Problems: ps, Host: version}
			if out.Problems == nil {
				out.Problems = apps.Problems{}
			}
			if pkg != nil {
				fake := apps.Installed{ID: pkg.Manifest.ID, Version: pkg.Manifest.Version, Dir: pkg.Dir, Manifest: pkg.Manifest}
				v := extViewOf(fake, safe, st, version)
				out.Package = &v
				if cur, err := reg.Find(pkg.Manifest.ID); err == nil {
					out.Installed = &cur.Version
				}
				if pkg.Manifest.Trust == apps.TrustTrusted {
					out.Blocked = "trusted"
				}
			}
			secretJSON(w, http.StatusOK, out)

		case rest == "install":
			if !method(w, r, http.MethodPost) {
				return
			}
			dir, ok := readDir(w, r)
			if !ok {
				return
			}
			if pkg, _ := apps.LoadPackage(dir); pkg != nil && pkg.Manifest.Trust == apps.TrustTrusted {
				secretJSON(w, http.StatusForbidden, map[string]any{"error": "trusted", "message": "a trusted app is installed by hand: harnessly app install"})
				return
			}
			got, problems, err := reg.Install(dir, apps.InstallOptions{HostVersion: version})
			if problems == nil {
				problems = apps.Problems{}
			}
			switch {
			case errors.Is(err, apps.ErrInvalid):
				secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": problems})
				return
			case errors.Is(err, apps.ErrMigration):
				secretJSON(w, http.StatusConflict, map[string]any{"error": "migration", "message": err.Error()})
				return
			case errors.Is(err, apps.ErrKindTaken), errors.Is(err, apps.ErrHostVersion), errors.Is(err, apps.ErrPluginName):
				secretJSON(w, http.StatusConflict, map[string]any{"error": "refused", "message": err.Error()})
				return
			case err != nil:
				fail(w, err)
				return
			}
			agent.Log("info", "extensions: installed", agent.F{K: "id", V: got.ID}, agent.F{K: "version", V: got.Version})
			v, _ := one(got.ID)
			secretJSON(w, http.StatusOK, map[string]any{"extension": v, "problems": problems})

		case len(parts) >= 2 && parts[1] == "disable" || len(parts) >= 2 && parts[1] == "enable":
			if !method(w, r, http.MethodPost) {
				return
			}
			off := parts[1] == "disable"
			switch err := reg.SetDisabled(parts[0], off); {
			case errors.Is(err, apps.ErrNotInstalled):
				notFound(w)
				return
			case errors.Is(err, apps.ErrTeamApp):
				secretJSON(w, http.StatusConflict, map[string]string{"error": "team"})
				return
			case err != nil:
				fail(w, err)
				return
			}
			if off {
				_, _ = guests.RevokeApp(parts[0])
			}
			agent.Log("info", "extensions: switched", agent.F{K: "id", V: parts[0]}, agent.F{K: "off", V: off})
			v, _ := one(parts[0])
			secretJSON(w, http.StatusOK, map[string]any{"extension": v})

		case len(parts) == 2 && parts[1] == "remove":
			if !method(w, r, http.MethodPost) {
				return
			}
			var b struct {
				Purge bool `json:"purge"`
			}
			if r.ContentLength != 0 && !readSecretBody(r, &b) {
				bad(w)
				return
			}
			switch err := reg.Uninstall(parts[0], b.Purge, safe); {
			case errors.Is(err, apps.ErrNotInstalled):
				notFound(w)
				return
			case errors.Is(err, apps.ErrTeamApp):
				secretJSON(w, http.StatusConflict, map[string]string{"error": "team"})
				return
			case err != nil:
				fail(w, err)
				return
			}
			_, _ = guests.RevokeApp(parts[0])
			agent.Log("info", "extensions: removed", agent.F{K: "id", V: parts[0]}, agent.F{K: "purge", V: b.Purge})
			secretJSON(w, http.StatusOK, map[string]any{"removed": parts[0], "purged": b.Purge})

		case len(parts) == 2 && parts[1] == "settings":
			if !method(w, r, http.MethodPut) {
				return
			}
			a, err := reg.Find(parts[0])
			if err != nil || a.Manifest == nil {
				notFound(w)
				return
			}
			var b struct {
				Values map[string]any `json:"values"`
			}
			if !readSecretBody(r, &b) || b.Values == nil {
				bad(w)
				return
			}
			if badFields, err := st.SaveSettings(a.Manifest, a.ID, b.Values); len(badFields) > 0 {
				secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": badFields})
				return
			} else if err != nil {
				fail(w, err)
				return
			}
			v, _ := one(parts[0])
			secretJSON(w, http.StatusOK, map[string]any{"extension": v})

		case len(parts) == 3 && parts[1] == "secrets":
			a, err := reg.Find(parts[0])
			if err != nil || a.Manifest == nil {
				notFound(w)
				return
			}
			switch r.Method {
			case http.MethodPut:
				var b struct {
					Value string `json:"value"`
				}
				if !readSecretBody(r, &b) {
					bad(w)
					return
				}
				switch err := apps.PutSecret(safe, a.Manifest, parts[2], b.Value); {
				case errors.Is(err, apps.ErrSecretUndeclared):
					notFound(w)
					return
				case err != nil:
					secretRefusal(w, err)
					return
				}
			case http.MethodDelete:
				switch err := apps.RemoveSecret(safe, a.Manifest, parts[2]); {
				case errors.Is(err, apps.ErrSecretUndeclared):
					notFound(w)
					return
				case err != nil:
					secretRefusal(w, err)
					return
				}
			default:
				secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			agent.Log("info", "extensions: secret changed", agent.F{K: "id", V: parts[0]}, agent.F{K: "name", V: parts[2]})
			v, _ := one(parts[0])
			secretJSON(w, http.StatusOK, map[string]any{"extension": v})

		default:
			notFound(w)
		}
	}

	// The CLI (`harnessly app disable|settings|secret|compat|run`): the person's cli-token or an agent's for what only
	// reads or runs a process; what changes the app is refused to an agent token (apps_agent.go).
	cliOK := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if !uiSlotsOn() {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "off"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	cliDisable := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID  string `json:"id"`
			Off bool   `json:"off"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			bad(w)
			return
		}
		switch err := reg.SetDisabled(b.ID, b.Off); {
		case errors.Is(err, apps.ErrNotInstalled):
			notFound(w)
			return
		case errors.Is(err, apps.ErrTeamApp):
			secretJSON(w, http.StatusConflict, map[string]string{"error": "team"})
			return
		case err != nil:
			fail(w, err)
			return
		}
		if b.Off {
			_, _ = guests.RevokeApp(b.ID)
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "disabled": b.Off})
	}
	cliList := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		list, err := listAll()
		if err != nil {
			fail(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"extensions": list})
	}
	cliCompat := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		host := r.URL.Query().Get("host")
		if host == "" {
			host = version
		}
		rep, err := reg.Compat(host)
		if err != nil {
			fail(w, err)
			return
		}
		secretJSON(w, http.StatusOK, rep)
	}
	cliSettings := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if !uiSlotsOn() {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "off"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			v, err := one(r.URL.Query().Get("id"))
			if errors.Is(err, apps.ErrNotInstalled) {
				notFound(w)
				return
			} else if err != nil {
				fail(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"extension": v})
		case http.MethodPost:
			var b struct {
				ID     string         `json:"id"`
				Values map[string]any `json:"values"`
			}
			if !readSecretBody(r, &b) || b.Values == nil {
				bad(w)
				return
			}
			a, err := reg.Find(b.ID)
			if err != nil || a.Manifest == nil {
				notFound(w)
				return
			}
			if badFields, err := st.SaveSettings(a.Manifest, a.ID, b.Values); len(badFields) > 0 {
				secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": badFields})
				return
			} else if err != nil {
				fail(w, err)
				return
			}
			v, _ := one(b.ID)
			secretJSON(w, http.StatusOK, map[string]any{"extension": v})
		default:
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	}
	cliSecret := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID     string  `json:"id"`
			Name   string  `json:"name"`
			Value  *string `json:"value"`
			Remove bool    `json:"remove"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.Name == "" {
			bad(w)
			return
		}
		a, err := reg.Find(b.ID)
		if err != nil || a.Manifest == nil {
			notFound(w)
			return
		}
		switch {
		case b.Remove:
			err = apps.RemoveSecret(safe, a.Manifest, b.Name)
		case b.Value != nil:
			err = apps.PutSecret(safe, a.Manifest, b.Name, *b.Value)
		default:
			bad(w)
			return
		}
		switch {
		case errors.Is(err, apps.ErrSecretUndeclared):
			notFound(w)
			return
		case err != nil:
			secretRefusal(w, err)
			return
		}
		v, _ := one(b.ID)
		secretJSON(w, http.StatusOK, map[string]any{"extension": v})
	}
	// cliUse hands the values of an app's secrets to `harnessly app run`, which puts them in the environment of one
	// process and masks them in its output. Only the secrets the manifest declares and grants (secrets:<name>).
	cliUse := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID    string   `json:"id"`
			Names []string `json:"names"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || len(b.Names) == 0 || len(b.Names) > 32 {
			bad(w)
			return
		}
		a, err := reg.Get(b.ID)
		if err != nil || a.Manifest == nil {
			notFound(w)
			return
		}
		values, err := apps.RevealSecrets(safe, a.Manifest, b.Names)
		switch {
		case errors.Is(err, apps.ErrSecretUndeclared), errors.Is(err, apps.ErrSecretRight):
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "no-right"})
			return
		case err != nil:
			secretRefusal(w, err)
			return
		}
		agent.Log("info", "extensions: secrets used", agent.F{K: "id", V: b.ID}, agent.F{K: "names", V: strings.Join(b.Names, ",")})
		secretJSON(w, http.StatusOK, map[string]any{"values": values})
	}

	match := func(r *http.Request) bool { _, ok := under(r); return ok }
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: match, Handler: http.HandlerFunc(handler)},
		{Match: is("/api/cli/apps/all"), Handler: http.HandlerFunc(cliList)},
		{Match: is("/api/cli/apps/disable"), Handler: http.HandlerFunc(cliDisable)},
		{Match: is("/api/cli/apps/compat"), Handler: http.HandlerFunc(cliCompat)},
		{Match: is("/api/cli/apps/settings"), Handler: http.HandlerFunc(cliSettings)},
		{Match: is("/api/cli/apps/secret"), Handler: http.HandlerFunc(cliSecret)},
		{Match: is("/api/cli/apps/secrets/use"), Handler: http.HandlerFunc(cliUse)},
	}
}
