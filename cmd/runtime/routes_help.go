package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/docs"
	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The guide to writing apps, built into the binary (docs/embed.go), for the person's /help page. Read only.
//
//	GET <platform>/help/guide?locale=ru              the pages in reading order: name, title, headings
//	GET <platform>/help/guide/page?name=views&locale=ru   one page's Markdown
//
// The locale is ru or en; a page with no translation comes in English. Only the person's page reads it.
func helpRoutes(d deps) []gate.Route {
	guarded := func(handle func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if sameorigin.Refuse(w, r) {
				return
			}
			if !d.SessionOK(r) {
				secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			if r.Method != http.MethodGet {
				secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			handle(w, r)
		}
	}
	locale := func(r *http.Request) string {
		if r.URL.Query().Get("locale") == docs.LocaleRU {
			return docs.LocaleRU
		}
		return docs.LocaleEN
	}

	list := guarded(func(w http.ResponseWriter, r *http.Request) {
		pages, err := docs.Pages(docs.Guide, locale(r))
		if err != nil {
			agent.Log("warn", "help: guide: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		type item struct {
			Name     string   `json:"name"`
			Title    string   `json:"title"`
			Headings []string `json:"headings"`
		}
		out := make([]item, 0, len(pages))
		for _, p := range pages {
			headings := p.Headings
			if headings == nil {
				headings = []string{}
			}
			out = append(out, item{Name: p.Name, Title: p.Title, Headings: headings})
		}
		secretJSON(w, http.StatusOK, map[string]any{"pages": out})
	})

	one := guarded(func(w http.ResponseWriter, r *http.Request) {
		raw, found, err := docs.Read(docs.Guide, locale(r), r.URL.Query().Get("name"))
		if err != nil {
			agent.Log("warn", "help: guide: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		if !found {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{"markdown": string(raw)})
	})

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var routes []gate.Route
	for _, prefix := range []string{platformAPI, "/agents" + platformAPI} {
		routes = append(routes,
			gate.Route{Match: is(prefix + "/help/guide"), Handler: list},
			gate.Route{Match: is(prefix + "/help/guide/page"), Handler: one},
		)
	}
	return routes
}
