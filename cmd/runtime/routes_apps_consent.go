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

// The person's word on an app: the install screen's «Установить» and «Отмена», the switch of its card and
// its journal. The page's session only: no CLI token and no agent reaches these, so what an app may do changes only by
// a click of the person in the host's page (internal/apps consent.go).

// journalShown is how many lines the inspector reads.
const journalShown = 100

func appConsentRoutes(d deps, reg *apps.Registry) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	pageOK := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	failed := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, apps.ErrNotInstalled):
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		case errors.Is(err, apps.ErrNoConsentWaiting):
			secretJSON(w, http.StatusConflict, map[string]string{"error": "nothing-waits"})
		case errors.Is(err, apps.ErrConsentChanged):
			secretJSON(w, http.StatusConflict, map[string]string{"error": "changed", "message": "the app changed after the screen was drawn: look at it again"})
		default:
			agent.Log("warn", "apps: consent: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
		}
	}

	// consent: «Установить» (or «Обновить») on the install screen, for the version the screen showed.
	consent := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			// Access are the keys the install screen showed: only those are granted.
			Access []string `json:"access"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.Version == "" || b.Access == nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		got, err := reg.Consent(b.ID, b.Version, b.Access, time.Now())
		if err != nil {
			failed(w, err)
			return
		}
		_ = reg.Journal(b.ID, apps.JournalEntry{What: "consent", Class: apps.ClassHigh, OK: true, Detail: got.Version})
		agent.Log("info", "apps: consent", agent.F{K: "id", V: got.ID}, agent.F{K: "version", V: got.Version})
		secretJSON(w, http.StatusOK, map[string]any{"app": viewOfApp(got, false)})
	}

	// decline: «Отмена»: a waiting update is dropped, a first install that waited is removed.
	decline := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		removed, err := reg.Decline(b.ID)
		if err != nil {
			failed(w, err)
			return
		}
		if !removed {
			_ = reg.Journal(b.ID, apps.JournalEntry{What: "declined", Class: apps.ClassHigh, OK: true})
		}
		secretJSON(w, http.StatusOK, map[string]any{"removed": removed})
	}

	// toggle: the switch of the card. The person's own install is turned off on the machine; a team's or the
	// environment's app is turned off for the person.
	toggle := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID string `json:"id"`
			On bool   `json:"on"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		err := reg.SetOff(b.ID, !b.On)
		if errors.Is(err, apps.ErrNotInstalled) {
			err = reg.MuteTeamApp(b.ID, !b.On)
		}
		if err != nil {
			failed(w, err)
			return
		}
		what := "off"
		if b.On {
			what = "on"
		}
		_ = reg.Journal(b.ID, apps.JournalEntry{What: what, Class: apps.ClassHigh, OK: true})
		agent.Log("info", "apps: switch", agent.F{K: "id", V: b.ID}, agent.F{K: "on", V: b.On})
		secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "on": b.On})
	}

	// asks: the apps that wait for the person's word, light (no view server, no states): what the chat's card, its
	// right pane and the app's own window read, and read again while the page is seen.
	asks := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r, http.MethodGet) {
			return
		}
		installed, err := reg.ListAll()
		if err != nil {
			failed(w, err)
			return
		}
		out := []appView{}
		for _, a := range installed {
			if !a.Muted && a.WaitsConsent() {
				out = append(out, viewOfApp(a, false))
			}
		}
		secretJSON(w, http.StatusOK, map[string]any{"apps": out})
	}

	journal := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r, http.MethodGet) {
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		list, err := reg.ReadJournal(id, journalShown)
		if err != nil {
			failed(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"journal": list})
	}

	// Written out whole: tools/web-routes-check.mjs reads the served paths from these literals.
	return []gate.Route{
		{Match: is(platformAPI + "/apps/consent"), Handler: http.HandlerFunc(consent)},
		{Match: is("/agents" + platformAPI + "/apps/consent"), Handler: http.HandlerFunc(consent)},
		{Match: is(platformAPI + "/apps/decline"), Handler: http.HandlerFunc(decline)},
		{Match: is("/agents" + platformAPI + "/apps/decline"), Handler: http.HandlerFunc(decline)},
		{Match: is(platformAPI + "/apps/switch"), Handler: http.HandlerFunc(toggle)},
		{Match: is("/agents" + platformAPI + "/apps/switch"), Handler: http.HandlerFunc(toggle)},
		{Match: is(platformAPI + "/apps/consent/asks"), Handler: http.HandlerFunc(asks)},
		{Match: is("/agents" + platformAPI + "/apps/consent/asks"), Handler: http.HandlerFunc(asks)},
		{Match: is(platformAPI + "/apps/journal"), Handler: http.HandlerFunc(journal)},
		{Match: is("/agents" + platformAPI + "/apps/journal"), Handler: http.HandlerFunc(journal)},
	}
}
