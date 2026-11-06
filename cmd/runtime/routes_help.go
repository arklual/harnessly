package main

import (
	"net/http"
	"os"
	"strconv"

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
// The whole documentation (docs/library.go): the built-in sections and the machine's own folders (HARNESSLY_DOCS_DIR,
// else <data>/docs), read again on every ask so a page written there shows at once.
//
//	GET <platform>/help/library?locale=ru                       the sections in order, with their pages
//	GET <platform>/help/library/page?section=start&name=overview&locale=ru   one page's Markdown
//	GET <platform>/help/library/search?q=words&locale=ru         the pages that have every word, best first
//	GET <platform>/help/library/picture?section=…&name=…&src=img/x.png       a picture the page shows
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

	library := func(w http.ResponseWriter, r *http.Request) (docs.Library, bool) {
		lib, err := docs.Load(docs.Sources(docs.LocalDirs(os.Getenv, d.DataDir)), locale(r))
		if err != nil {
			agent.Log("warn", "help: library: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return docs.Library{}, false
		}
		return lib, true
	}

	sections := guarded(func(w http.ResponseWriter, r *http.Request) {
		lib, ok := library(w, r)
		if !ok {
			return
		}
		out := lib.Sections
		if out == nil {
			out = []docs.Section{}
		}
		secretJSON(w, http.StatusOK, map[string]any{"sections": out})
	})

	find := func(w http.ResponseWriter, r *http.Request) (docs.Doc, bool) {
		lib, ok := library(w, r)
		if !ok {
			return docs.Doc{}, false
		}
		q := r.URL.Query()
		doc, found := lib.Find(q.Get("section"), q.Get("name"))
		if !found {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		}
		return doc, found
	}

	libraryPage := guarded(func(w http.ResponseWriter, r *http.Request) {
		doc, ok := find(w, r)
		if !ok {
			return
		}
		raw, err := doc.Markdown()
		if err != nil {
			agent.Log("warn", "help: page: "+err.Error())
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]string{"markdown": string(raw), "file": doc.File, "origin": doc.Origin})
	})

	search := guarded(func(w http.ResponseWriter, r *http.Request) {
		lib, ok := library(w, r)
		if !ok {
			return
		}
		hits := lib.Search(r.URL.Query().Get("q"), 20)
		if hits == nil {
			hits = []docs.Hit{}
		}
		secretJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})

	picture := guarded(func(w http.ResponseWriter, r *http.Request) {
		doc, ok := find(w, r)
		if !ok {
			return
		}
		raw, kind, err := doc.Picture(r.URL.Query().Get("src"))
		if err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		h := w.Header()
		h.Set("Content-Type", kind)
		h.Set("Content-Length", strconv.Itoa(len(raw)))
		h.Set("X-Content-Type-Options", "nosniff")
		// An SVG opened on its own must not run anything.
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
		h.Set("Cache-Control", "private, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	})

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var routes []gate.Route
	for _, prefix := range []string{platformAPI, "/agents" + platformAPI} {
		routes = append(routes,
			gate.Route{Match: is(prefix + "/help/guide"), Handler: list},
			gate.Route{Match: is(prefix + "/help/guide/page"), Handler: one},
			gate.Route{Match: is(prefix + "/help/library"), Handler: sections},
			gate.Route{Match: is(prefix + "/help/library/page"), Handler: libraryPage},
			gate.Route{Match: is(prefix + "/help/library/search"), Handler: search},
			gate.Route{Match: is(prefix + "/help/library/picture"), Handler: picture},
		)
	}
	return routes
}
