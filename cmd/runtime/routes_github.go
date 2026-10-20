package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/integrations/github"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// GitHub for the page (<platform>/github/…): an issue as a task card and tab (task?key=owner/repo#12) and the chip
// of an issue or pull request link (link-meta?url=). The answers have the shapes the YouTrack and GitLab routes
// answer with, so the page draws them the same way.

func githubRoutes(d deps, c github.Config) []gate.Route {
	prefix := platformAPI + "/github/"
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, github.ErrNotFound):
			write(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		default:
			write(w, http.StatusBadGateway, map[string]string{"error": "tracker"})
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			for k, vs := range h {
				w.Header()[k] = vs
			}
			write(w, status, v)
			return
		}
		if !d.SessionOK(r) {
			write(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		switch strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), prefix) {
		case "task":
			ref, ok := github.ParseKey(r.URL.Query().Get("key"))
			if !ok {
				write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("Unsupported key", "Неподдерживаемый ключ")})
				return
			}
			task, err := c.Task(r.Context(), ref)
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, task)
		case "link-meta":
			ref, ok := c.ParseURL(r.URL.Query().Get("url"))
			if !ok {
				write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("Not a GitHub issue or pull request", "Не задача и не pull request GitHub")})
				return
			}
			meta, err := c.LinkMeta(r.Context(), ref)
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, meta)
		default:
			write(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		}
	})
	return []gate.Route{{
		Match: func(r *http.Request) bool {
			return strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), prefix)
		},
		Handler: handler,
	}}
}

func githubFromEnv() github.Config { return github.FromEnv(os.Getenv) }
