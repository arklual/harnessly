package main

// The day feed of a start-page stage view, read through the runtime (docs/apps/day-feed.md). A view whose manifest names
// feedSecret is signed in by the machine: the page asks this route, the route calls the feed with the app's secret as a
// bearer token and hands the answer back. The page never holds the secret and never shows the feed's own sign-in, which
// a feed on another site cannot have in the host's page anyway (its cookie is a third party's there).

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// feedProxySegment is where the route answers under the platform API: <api>/apps/feed/<app>/<view>[/items/<id>[/<action>]].
const feedProxySegment = "/apps/feed/"

func feedProxyPath(app, view string) string { return feedProxySegment + app + "/" + view }

// What the page may ask of a feed: the day, one item, and an action on an item. Nothing else is forwarded.
var (
	feedItemPath   = regexp.MustCompile(`^/items/[A-Za-z0-9._:-]{1,128}$`)
	feedActionPath = regexp.MustCompile(`^/items/[A-Za-z0-9._:-]{1,128}/[a-z]{1,32}$`)
)

const (
	feedTimeout  = 20 * time.Second
	feedMaxBody  = 64 << 10
	feedMaxReply = 8 << 20
)

func appFeedRoute(d deps, reg *apps.Registry) gate.Route {
	prefixes := []string{platformAPI + feedProxySegment, "/agents" + platformAPI + feedProxySegment}
	h := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		var rest string
		for _, p := range prefixes {
			if s, ok := strings.CutPrefix(r.URL.Path, p); ok {
				rest = s
				break
			}
		}
		appID, rest, _ := strings.Cut(rest, "/")
		viewID, sub, _ := strings.Cut(rest, "/")
		if sub != "" {
			sub = "/" + sub
		}
		switch {
		case r.Method == http.MethodGet && (sub == "" || feedItemPath.MatchString(sub)):
		case r.Method == http.MethodPost && feedActionPath.MatchString(sub):
		default:
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		a, err := reg.Get(appID)
		if err != nil || a.Off || a.Muted || a.NeedsConsent() {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		m := a.LiveManifest()
		var view *apps.View
		for i := range m.Views {
			if m.Views[i].ID == viewID {
				view = &m.Views[i]
			}
		}
		if view == nil || view.FeedSecret == "" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		feed, ok := m.FeedURL(view.Feed)
		if !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		token := revealApp(safeFor(d).store.Local, appID, []string{view.FeedSecret})[view.FeedSecret]
		if token == "" {
			// The app waits for its secret: the page says so instead of a sign-in it cannot do.
			secretJSON(w, http.StatusConflict, map[string]string{"error": "secret-missing", "secret": view.FeedSecret})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), feedTimeout)
		defer cancel()
		var body io.Reader
		if r.Method == http.MethodPost {
			body = io.LimitReader(r.Body, feedMaxBody)
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, strings.TrimRight(feed, "/")+sub, body)
		if err != nil {
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": "feed"})
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Requested-With", "harnessly")
		if r.Method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		// The app's network rules (a public address checked at the dial, or an internal host the deployment lists), no redirects.
		client := &http.Client{Transport: apps.Transport(req.URL.Hostname()), CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		res, err := client.Do(req)
		if err != nil {
			reg.LogEventOnce(appID, viewID, apps.ViewLogMCP, "feed "+r.Method+" "+clip(sub, 80)+" — "+clip(err.Error(), 200))
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": "feed-unreachable"})
			return
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			// The feed refused the machine's token: a sign-in on the page would not help; the secret needs a new value.
			reg.LogEventOnce(appID, viewID, apps.ViewLogMCP, "feed "+r.Method+" "+clip(sub, 80)+" — "+res.Status)
			secretJSON(w, http.StatusConflict, map[string]string{"error": "secret-refused", "secret": view.FeedSecret})
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(res.Body, feedMaxReply))
	}
	return gate.Route{
		Match: func(r *http.Request) bool {
			for _, p := range prefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					return true
				}
			}
			return false
		},
		Handler: http.HandlerFunc(h),
	}
}
