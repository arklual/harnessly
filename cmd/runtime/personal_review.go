package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

var personalMRPath = regexp.MustCompile(`^/([A-Za-z0-9_-][A-Za-z0-9._-]*/)+[A-Za-z0-9_-][A-Za-z0-9._-]*/-/merge_requests/[1-9][0-9]*/?$`)

func personalReviewURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || !personalMRPath.MatchString(u.EscapedPath()) {
		return nil
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u
}

func personalReviewOwner(d deps, r *http.Request) string {
	if d.Claims == nil {
		return ""
	}
	claims := d.Claims(r)
	verified, _ := claims["verifiedOwner"].(bool)
	sub, _ := claims["sub"].(string)
	owner := strings.TrimSpace(os.Getenv("RUNTIME_OWNER_EMAIL"))
	if !verified || !strings.Contains(sub, "@") || (owner != "" && !strings.EqualFold(owner, sub)) {
		return ""
	}
	return sub
}

// A zero status leaves a regular forge connection to the existing handler.
func personalReview(d deps, r *http.Request, raw string, view bool) (any, int) {
	u := personalReviewURL(raw)
	if u == nil {
		return nil, 0
	}
	cs := connectionsFor(d)
	return readPersonalReview(d, r, u, view, cs, connections.Providers())
}

func readPersonalReview(d deps, r *http.Request, u *url.URL, view bool, cs *connections.Service, providers []connections.Provider) (any, int) {
	ctx, cancel := context.WithTimeout(r.Context(), codeReviewTimeout)
	defer cancel()
	origin := "https://" + u.Host
	for _, candidate := range providers {
		p, ok := candidate.(*connections.ExtProvider)
		if !ok || !p.ReadsMergeRequests() {
			continue
		}
		if _, own, _ := reviewConn(cs, u.Hostname()); own {
			return nil, 0
		}
		// Detection needs only an origin. The repo and MR are disclosed after owner verification.
		detection, err := p.Detect(ctx, nil, &url.URL{Scheme: u.Scheme, Host: u.Host})
		if err != nil {
			return map[string]string{"error": "unreachable"}, http.StatusBadGateway
		}
		if detection == nil || !strings.EqualFold(strings.TrimSuffix(detection.BaseURL, "/"), origin) {
			continue
		}
		subject := personalReviewOwner(d, r)
		if site, _ := fetchSite(r); site == "cross-site" || subject == "" {
			return map[string]string{"error": "denied"}, http.StatusForbidden
		}
		if view && r.URL.Query().Has("path") {
			return map[string]string{"error": "file-content-unavailable"}, http.StatusNotFound
		}
		out, err := p.ReadMergeRequest(ctx, origin, u.String(), subject)
		if err != nil {
			if errors.Is(err, connections.ErrDenied) {
				return map[string]string{"error": "denied"}, http.StatusForbidden
			}
			var upstream *ext.Error
			if errors.As(err, &upstream) && (upstream.Status == 401 || upstream.Status == 403) {
				return map[string]string{"error": "denied"}, upstream.Status
			}
			if errors.Is(err, ext.ErrNotFound) || (errors.As(err, &upstream) && upstream.Status == 404) {
				return map[string]string{"error": "not-found"}, http.StatusNotFound
			}
			return map[string]string{"error": "unreachable"}, http.StatusBadGateway
		}
		if view {
			return out.View, http.StatusOK
		}
		return map[string]any{"review": out.Review, "connection": map[string]any{"kind": "gitlab", "id": p.Kind(), "name": u.Host, "host": u.Host, "personal": false}}, http.StatusOK
	}
	return nil, 0
}
