package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// checkConv names the browser's tabs of checks: the app's view is opened there and closed at once.
const checkConv = "app-check"

// How long a check waits for the handshake, and for the errors that follow it.
const (
	checkWait  = 10 * time.Second
	checkAfter = 1200 * time.Millisecond
)

// errNoBrowser: this machine has no headless browser for a check.
var errNoBrowser = errors.New("this machine has no browser to open the view in (harnessly app logs shows what the person's page reported)")

// runAppCheck opens an app's view in the machine's own browser through a short link, waits for the handshake and
// answers how the load went. An app that waits for consent or is off is answered as it is, with no browser.
func runAppCheck(ctx context.Context, d deps, reg *apps.Registry, guests *apps.Guests, id, view string) (apps.LoadCheck, error) {
	a, err := reg.Find(id)
	if err != nil {
		return apps.LoadCheck{}, err
	}
	switch {
	case a.NeedsConsent():
		return apps.LoadCheck{App: id, View: view, State: apps.CheckWaiting, Lines: []apps.ViewLogLine{},
			Problem: "waits for the person's consent to " + strings.Join(apps.AccessCodes(a.WaitingAccess()), ", ") + ": " + apps.ConsentWhere(id)}, nil
	case a.Off || a.Muted:
		return apps.LoadCheck{App: id, View: view, State: apps.CheckOff, Lines: []apps.ViewLogLine{}, Problem: "the app is turned off"}, nil
	}
	if d.BrowserTools == nil || d.BrowserTools.Manager == nil || !d.BrowserTools.Manager.Enabled() {
		return apps.LoadCheck{}, errNoBrowser
	}
	token, link, err := guests.CreateCheck(id, view)
	if err != nil {
		return apps.LoadCheck{}, err
	}
	defer func() { _ = guests.Revoke(link.ID) }()
	port := d.RuntimePort
	if port == 0 {
		port = portOf(d.Upstream)
	}
	ctx, cancel := context.WithTimeout(ctx, checkWait+20*time.Second)
	defer cancel()
	inst, err := d.BrowserTools.Manager.InstanceFor(ctx, checkConv)
	if err != nil {
		return apps.LoadCheck{}, fmt.Errorf("%w: %v", errNoBrowser, err)
	}
	start := time.Now().UnixMilli()
	tab, err := inst.NewTab(ctx, checkConv, "agent", fmt.Sprintf("http://127.0.0.1:%d%s%s/", port, apps.GuestPrefix, token))
	if err != nil {
		return apps.LoadCheck{}, fmt.Errorf("the view could not be opened: %w", err)
	}
	defer func() { _ = inst.CloseTab(context.WithoutCancel(ctx), tab.ID) }()
	deadline := time.Now().Add(checkWait)
	settle := time.Time{}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return apps.LoadCheck{}, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		lines, _ := reg.ViewLogSince(id, start, 0)
		got := apps.Judge(id, link.View, lines)
		if got.Handshake && settle.IsZero() {
			settle = time.Now().Add(checkAfter)
		}
		if !settle.IsZero() && time.Now().After(settle) {
			break
		}
	}
	lines, _ := reg.ViewLogSince(id, start, 0)
	res := apps.Judge(id, link.View, lines)
	if !res.Handshake && res.Errors == 0 {
		reg.LogEvent(id, link.View, apps.ViewLogError, "check: no handshake in "+checkWait.String())
		lines, _ = reg.ViewLogSince(id, start, 0)
		res = apps.Judge(id, link.View, lines)
	}
	return res, nil
}

// checkRoute is POST /api/cli/apps/check {id, view}: the CLI's `harnessly app check <id> --open`.
func checkRoute(d deps, reg *apps.Registry, guests *apps.Guests) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			ID   string `json:"id"`
			View string `json:"view"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		res, err := runAppCheck(r.Context(), d, reg, guests, b.ID, b.View)
		switch {
		case err == nil:
			secretJSON(w, http.StatusOK, res)
		case errors.Is(err, errNoBrowser):
			secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no-browser", "message": err.Error()})
		case errors.Is(err, apps.ErrCheckRemote), errors.Is(err, apps.ErrGuestTarget):
			secretJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "no-view", "message": err.Error()})
		case errors.Is(err, apps.ErrNotInstalled):
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found", "message": err.Error()})
		default:
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed", "message": err.Error()})
		}
	}
}
