package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/gitread"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func init() { routeGroups = append(routeGroups, changesRoutes) }

// changesRoutes: the POST of the changes route (slice fsread; the GET is in gitRoutes). It only reads: git in each
// repository the paths belong to and the head of the conversation's transcript.
func changesRoutes(d deps) []gate.JSONRoute {
	roots := gitread.TranscriptRoots(d.Home)
	return []gate.JSONRoute{{
		Slice: "fsread",
		Match: func(r *http.Request) bool { return r.Method == http.MethodPost && pathIs(r, platformAPI+"/changes") },
		Compute: func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			body, refusal := readPromptBody(r)
			if refusal != nil {
				return refusal.status, gate.Raw{ContentType: "text/plain", Body: []byte(refusal.text)}, nil
			}
			if !d.SessionOK(r) { // a cli-token alone is not a person
				return answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			}
			h := http.Header{"Cache-Control": {"no-store"}}
			paths := []string{}
			transcript := ""
			v, _ := jsvalue.Parse([]byte(jscompat.DecodeUTF8(body)))
			if o, ok := v.(*jsvalue.Object); ok {
				if list, ok := o.Get("paths"); ok {
					if arr, ok := list.([]any); ok {
						for _, p := range arr {
							if s, ok := p.(string); ok {
								paths = append(paths, s)
							}
						}
					}
				}
				if s, ok := o.Get("jsonlPath"); ok {
					transcript, _ = s.(string)
				}
			}
			failed := func() (int, any, http.Header) {
				return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, h
			}
			since, err := gitread.ReadSessionStart(transcript, roots)
			if err != nil {
				return failed()
			}
			repos, err := gitread.CollectChanges(paths, since)
			if err != nil {
				return failed()
			}
			if repos == nil {
				repos = []*gitread.RepoChanges{}
			}
			return answerJSON(r, http.StatusOK, gitread.SessionChangesBody{Since: since, Repos: repos}, h)
		},
	}}
}
