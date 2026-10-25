package main

import (
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/settings"
)

func init() { routeGroups = append(routeGroups, otherMethodRoutes) }

// otherMethod is what an old handler answered to a method its route here does not take: the dispatcher's session
// check and body (in its limit; none for a handler that read the body itself), the handler's own session check where
// it came first, then 405.
type otherMethod struct {
	slice   string
	match   func(*http.Request) bool
	methods []string // what the route here answers
	allow   string   // the Allow header; "" for none
	body    any      // the 405 body; nil for an empty one
	limit   int64    // 0: the old dispatcher left the body alone
	name    string   // the limit as the refusal names it
	session any      // the handler's session check before its method check: the 403 body (empty when "empty")
	noStore bool     // the handler said no-store before its method check
}

var methodNotAllowedBody = jsvalue.NewObject("error", "Method not allowed")

func (m otherMethod) route(d deps) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: m.slice,
		Match: func(r *http.Request) bool { return !slices.Contains(m.methods, r.Method) && m.match(r) },
		Compute: func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			if m.limit > 0 {
				if _, _, status, text := nodejson.ParseBody(r, m.limit, m.name); status != 0 {
					return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
				}
			}
			if m.session != nil && !d.SessionOK(r) {
				if m.session == "empty" {
					return http.StatusForbidden, nil, nil
				}
				return settings.Answer(r, http.StatusForbidden, m.session, nil)
			}
			h := http.Header{}
			if m.noStore {
				h.Set("Cache-Control", "no-store")
			}
			if m.allow != "" {
				h.Set("Allow", m.allow)
			}
			if m.body == nil {
				return http.StatusMethodNotAllowed, nil, h
			}
			return settings.Answer(r, http.StatusMethodNotAllowed, m.body, h)
		},
	}
}

// otherMethodRoutes: the methods the read-only and the writing slices leave to these answers, each in its slice.
func otherMethodRoutes(d deps) []gate.JSONRoute {
	at := func(p string) func(*http.Request) bool {
		return func(r *http.Request) bool { return files.RouteIs(r, p) }
	}
	const mb = 1 << 20
	get := func(slice, path string) otherMethod {
		return otherMethod{slice: slice, match: at(path), methods: []string{http.MethodGet}, allow: "GET", body: methodNotAllowedBody, limit: mb, name: "1mb"}
	}
	post := func(path string) otherMethod {
		return otherMethod{slice: "fswrite", match: at(path), methods: []string{http.MethodPost}, allow: "POST", body: methodNotAllowedBody, limit: mb, name: "1mb"}
	}
	forbidden := jsvalue.NewObject("error", "Forbidden")
	list := []otherMethod{
		get("fsread", "/api/check-agent"),
		{slice: "fsread", match: at("/api/codex/sessions"), methods: []string{http.MethodGet}, allow: "GET",
			body: jsvalue.NewObject("error", "method-not-allowed"), limit: mb, name: "1mb"},
		{slice: "fsread", match: at("/api/editor/files"), methods: []string{http.MethodGet}, allow: "GET", body: methodNotAllowedBody,
			limit: mb, name: "1mb", noStore: true},
		{slice: "fsread", match: at("/api/editor/file"), methods: []string{http.MethodGet, http.MethodPut}, allow: "GET, PUT",
			body: methodNotAllowedBody, limit: 6 * mb, name: "6mb", noStore: true},
		get("fsread", "/api/git/branch"),
		get("fsread", "/api/git/status"),
		get("fsread", "/api/layout/commit-diff"),
		get("fsread", "/api/layout/diff"),
		get("fsread", "/api/layout/file-content"),
		get("fsread", "/api/layout/git-log"),
		get("fsread", "/api/timeline/sessions"),
		{slice: "fsread", match: at("/api/preflight/runtime"), methods: []string{http.MethodGet, http.MethodPost}, allow: "GET, POST",
			body: methodNotAllowedBody, limit: mb, name: "1mb"},
		{slice: "fsread", match: at(platformAPI + "/changes"), methods: []string{http.MethodGet, http.MethodPost}, allow: "GET, POST",
			limit: mb, name: "1mb", session: forbidden, noStore: true},
		{slice: "fsread", match: at(platformAPI + "/context-files"), methods: []string{http.MethodGet}, allow: "GET",
			limit: mb, name: "1mb", session: forbidden},
		{slice: "fsread", match: func(r *http.Request) bool { _, _, ok := uploadsRouteAny(r); return ok }, methods: []string{http.MethodGet},
			allow: "GET", body: methodNotAllowedBody, limit: mb, name: "1mb"},
		post("/api/git/sync"),
		post("/api/uploads/cleanup"),
		{slice: "fswrite", match: at("/api/upload-file"), methods: []string{http.MethodPost}, allow: "POST", body: methodNotAllowedBody},
		{slice: "fswrite", match: at("/api/upload-image"), methods: []string{http.MethodPost}, allow: "POST", body: methodNotAllowedBody},
		{slice: "fswrite", match: at(platformAPI + "/machine/stop"), methods: []string{http.MethodPost}, body: methodNotAllowedBody, limit: mb, name: "1mb"},
		{slice: "voice", match: func(r *http.Request) bool { _, ok := voiceAssetSegmentsAny(r); return ok }, methods: []string{http.MethodGet},
			allow: "GET", body: methodNotAllowedBody, limit: mb, name: "1mb"},
	}
	out := make([]gate.JSONRoute, 0, len(list))
	for _, m := range list {
		out = append(out, m.route(d))
	}
	return out
}

// uploadsRouteAny is uploadsRoute for any method.
func uploadsRouteAny(r *http.Request) ([]string, string, bool) {
	get := *r
	get.Method = http.MethodGet
	return uploadsRoute(&get)
}

// voiceAssetSegmentsAny is voiceAssetSegments for any method.
func voiceAssetSegmentsAny(r *http.Request) ([]string, bool) {
	get := *r
	get.Method = http.MethodGet
	return voiceAssetSegments(&get)
}
