package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/gitread"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

func init() { routeGroups = append(routeGroups, gitRoutes) }

// oldQuery is the request's query as the old handlers read it: Node's querystring, a malformed escape kept as it is
// and a repeated key an array.
func oldQuery(r *http.Request) url.Values { return url.Values(files.RequestQuery(r)) }

// flags drops the keys read as `=== 'true'`: an array equals nothing.
func flags(q url.Values, keys ...string) {
	for _, k := range keys {
		if len(q[k]) > 1 {
			delete(q, k)
		}
	}
}

func truthyParam(q url.Values, k string) bool {
	v := q[k]
	return len(v) > 1 || len(v) == 1 && v[0] != ""
}

// gitRoutes: the read-only git answers about a tab's working folder and a repository (slice fsread; the diff that
// fetches from the remote first writes, and is the fswrite slice's). Each answer carries the ETag the old runtime's
// dispatcher gave it. A repeated parameter is read as the old handlers read an array.
func gitRoutes(d deps) []gate.JSONRoute {
	svc := gitread.New(d.Tmux)
	failed := func() (int, any) { return http.StatusInternalServerError, handlerFailed }
	// The old handlers asked tmux about an array of sessions and threw before their try.
	sessionThrows := func(q url.Values, k string) bool { return len(q[k]) > 1 }
	route := func(slice, path string, match func(url.Values) bool, answer func(url.Values) (int, any)) gate.JSONRoute {
		return gate.JSONRoute{
			Slice: slice,
			Match: func(r *http.Request) bool {
				return r.Method == http.MethodGet && pathIs(r, path) && (match == nil || match(oldQuery(r)))
			},
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if status, v, refused := bodyRefusal(r); refused {
					return status, v, nil
				}
				status, v := answer(oldQuery(r))
				if raw, ok := v.(gate.Raw); ok {
					return status, raw, nil
				}
				return nodejson.Answer(r, status, v, nil)
			},
		}
	}
	tab := func(answer func(url.Values) (int, any)) func(url.Values) (int, any) {
		return func(q url.Values) (int, any) {
			// An array of sessions has no folder: getSessionCwd swallowed the error.
			if len(q["tmuxSession"]) > 1 {
				return 404, map[string]string{"error": "tmux-session-not-found"}
			}
			flags(q, "force")
			return answer(q)
		}
	}
	diff := func(q url.Values) (int, any) {
		flags(q, "hashOnly", "fetch")
		if truthyParam(q, "session") && sessionThrows(q, "session") {
			return failed()
		}
		return svc.Diff(q)
	}
	fetches := func(q url.Values) bool { v := q["fetch"]; return len(v) == 1 && v[0] == "true" }
	return []gate.JSONRoute{
		route("fsread", "/api/git/branch", nil, tab(svc.Branch)),
		route("fsread", "/api/git/status", nil, tab(svc.Status)),
		route("fsread", "/api/layout/diff", func(q url.Values) bool { return !fetches(q) }, diff),
		route("fswrite", "/api/layout/diff", fetches, diff),
		route("fsread", "/api/layout/commit-diff", nil, func(q url.Values) (int, any) {
			if !truthyParam(q, "session") || !truthyParam(q, "hash") {
				return 400, map[string]string{"error": "session and hash parameters required"}
			}
			if len(q["hash"]) > 1 {
				return 400, map[string]string{"error": "Invalid hash format"} // "a,b" is no hash
			}
			if sessionThrows(q, "session") && hashLike(q["hash"][0]) {
				return failed()
			}
			return svc.CommitDiff(q)
		}),
		route("fsread", "/api/layout/git-log", nil, func(q url.Values) (int, any) {
			for _, k := range []string{"limit", "skip"} {
				if v := q[k]; len(v) > 1 {
					q[k] = []string{strings.Join(v, ",")}
				}
			}
			if truthyParam(q, "session") && sessionThrows(q, "session") {
				return failed()
			}
			return svc.GitLog(q)
		}),
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { return r.Method == http.MethodGet && pathIs(r, platformAPI+"/changes") },
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if status, v, refused := bodyRefusal(r); refused {
					return status, v, nil
				}
				if !d.SessionOK(r) { // a cli-token alone is not a person
					return nodejson.Answer(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
				}
				status, v := gitread.CommitChanges(oldQuery(r))
				return nodejson.Answer(r, status, v, http.Header{"Cache-Control": {"no-store"}})
			},
		},
	}
}

func hashLike(s string) bool {
	if len(s) < 4 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// bodyRefusal is the old dispatcher's answer to a body it could not read, before any handler ran (a GET's too).
func bodyRefusal(r *http.Request) (int, any, bool) {
	if _, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb"); status != 0 {
		return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, true
	}
	return 0, nil, false
}
