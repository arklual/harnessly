package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The agent's hooks of the apps (internal/apps hooks.go).
//
//	POST /api/cli/apps/hook          the guard of an agent's launch: {session, tool, input} → {deny, app, name, reason, context}
//	POST <platform>/apps/hooks-off   the person's page: {id, off}, the feed's «Выключить хук»

// hookInputMax bounds what of the call's input goes to the views (a page call carries 64 KiB with its wrapper).
const hookInputMax = 60 << 10

func appHookRoutes(d deps, reg *apps.Registry, chats appChats) []gate.Route {
	hook := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		launch, byAgent := d.CLI.AgentOf(r)
		if !byAgent || launch == "" {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		var b struct {
			Session string          `json:"session"`
			Tool    string          `json:"tool"`
			Input   json.RawMessage `json:"input"`
		}
		if !readSecretBody(r, &b) || b.Tool == "" || len(b.Tool) > 200 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		// The chat is the launch's own, from its grant; the body can only name that chat's tab.
		granted, conv := reg.Grant(launch)
		rec, ok := chats.record(conv)
		if conv == "" || !ok || chats.ofSession(b.Session) != conv {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-on"})
			return
		}
		// A call too big to show whole goes to the views marked cut, so they can stop it; a strict app is not asked and
		// stops it, since it cannot judge what it does not see.
		input := b.Input
		cut := len(input) > hookInputMax || (len(input) > 0 && !json.Valid(input))
		if len(input) == 0 || cut {
			input = json.RawMessage(`{}`)
		}
		call, _ := json.Marshal(map[string]any{"tool": b.Tool, "input": input, "cut": cut})
		var asks []apps.HookAsk
		for _, id := range granted {
			if !slices.Contains(rec.Apps, id) || reg.HooksOff(id) {
				continue
			}
			app, err := reg.Get(id)
			// Off, muted or waiting for the person's word: the app hears nothing (its rights hold only once agreed to).
			if err != nil || app.Manifest == nil || app.Off || app.Muted || app.NeedsConsent() || !app.Manifest.Allows("hooks.agent") {
				continue
			}
			strict := app.Manifest.Allows("hooks.strict")
			asks = append(asks, apps.HookAsk{App: id, Name: app.Manifest.Name, Strict: strict,
				Live: appPages.Hooked(conv, id) && !(cut && strict)})
		}
		ctx, cancel := context.WithTimeout(r.Context(), apps.HookTimeout)
		defer cancel()
		var wg sync.WaitGroup
		for i := range asks {
			if !asks[i].Live {
				continue
			}
			wg.Add(1)
			go func(a *apps.HookAsk) {
				defer wg.Done()
				got, err := appPages.Hook(ctx, conv, a.App, call, apps.HookTimeout)
				if err != nil || got.Error != "" {
					return
				}
				if word, err := apps.ParseHookWord(got.Result); err == nil {
					a.Word = word
				}
			}(&asks[i])
		}
		wg.Wait()
		v := apps.MergeHooks(asks)
		if cut && len(asks) > 0 {
			_ = reg.Journal(asks[0].App, apps.JournalEntry{What: "hook.cut", Class: apps.ClassCritical, Chat: conv, OK: !v.Deny, Detail: b.Tool})
		}
		for _, id := range v.Silent {
			_ = reg.Journal(id, apps.JournalEntry{What: "hook.silent", Class: apps.ClassCritical, Chat: conv, OK: true, Detail: b.Tool})
		}
		if v.Deny {
			_ = reg.Journal(v.App, apps.JournalEntry{What: "hook.deny", Class: apps.ClassCritical, Chat: conv, OK: true,
				Detail: b.Tool + ": " + v.Reason})
		}
		secretJSON(w, http.StatusOK, v)
	}

	// hooksOff: the person's switch of an app's hook, from the feed's line of a stop.
	hooksOff := func(w http.ResponseWriter, r *http.Request) {
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
		var b struct {
			ID  string `json:"id"`
			Off bool   `json:"off"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if _, err := reg.Get(b.ID); err != nil {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
			return
		}
		if err := reg.SetHooksOff(b.ID, b.Off); err != nil {
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		what := "hooks.on"
		if b.Off {
			what = "hooks.off"
		}
		_ = reg.Journal(b.ID, apps.JournalEntry{What: what, Class: apps.ClassCritical, OK: true})
		secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "off": b.Off})
	}

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is("/api/cli/apps/hook"), Handler: http.HandlerFunc(hook)},
		{Match: is(platformAPI + "/apps/hooks-off"), Handler: http.HandlerFunc(hooksOff)},
		{Match: is("/agents" + platformAPI + "/apps/hooks-off"), Handler: http.HandlerFunc(hooksOff)},
	}
}
