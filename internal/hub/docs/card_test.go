package docs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
)

// A link's card and a board's picture answer only who may read it, and the board's key stays on the hub.
func TestLinkCardAndPreviewFollowTheRightToRead(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b/api/boards":
			_ = json.NewEncoder(w).Encode(map[string]any{"boards": []board{
				{Slug: "wall", Title: "Стена", Author: "author@example.com", UpdatedAt: "2026-11-14T10:00:00Z", EditedBy: "Боб", EditedAt: "2026-11-14T09:59:00Z",
					URL: srv.URL + "/b/wall?k=SECRETKEY", Preview: srv.URL + "/b/wall/preview.svg?k=SECRETKEY"},
				{Slug: "anon", Title: "anon", Author: "agent", Preview: "https://elsewhere.example.com/x.svg"}}})
		case "/b/wall/preview.svg":
			if r.URL.Query().Get("k") != "SECRETKEY" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="4" height="4"/></svg>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s.cfg.BoardsURL = srv.URL

	call := func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error, person, slug string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/x", nil)
		r.SetPathValue("slug", slug)
		w := httptest.NewRecorder()
		if err := h(w, r, who[person]); err != nil {
			s.answer(w, r, err)
		}
		return w
	}

	w := call(s.card, "mate", "wall")
	var c linkCard
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil || w.Code != 200 {
		t.Fatalf("card: %d %s", w.Code, w.Body.String())
	}
	if c.Kind != "board" || c.Title != "Стена" || c.Author != "author@example.com" || !c.Preview || c.At == "" ||
		c.Editor != "Боб" || c.EditedAt != "2026-11-14T09:59:00Z" || c.Page {
		t.Fatalf("card: %+v", c)
	}
	if strings.Contains(w.Body.String(), "SECRETKEY") {
		t.Fatal("the card carries the board's key")
	}
	w = call(s.preview, "mate", "wall")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<svg") || w.Header().Get("Content-Type") != "image/svg+xml" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("preview: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	for _, slug := range []string{"wall", "plan"} {
		if w := call(s.card, "stranger", slug); w.Code < 400 {
			t.Fatalf("a stranger gets the card of %s: %d %s", slug, w.Code, w.Body.String())
		}
	}
	if w := call(s.preview, "stranger", "wall"); w.Code < 400 {
		t.Fatalf("a stranger gets the picture: %d", w.Code)
	}
	// A picture the service names on another host is never fetched; a document has none.
	if w := call(s.preview, "admin", "anon"); w.Code != 404 {
		t.Fatalf("a foreign picture: %d", w.Code)
	}
	if w := call(s.card, "mate", "plan"); w.Code != 200 || strings.Contains(w.Body.String(), `"preview":true`) {
		t.Fatalf("a document's card: %d %s", w.Code, w.Body.String())
	}
}

// A document's card names who published its current version, quotes a text document's first lines and offers an HTML
// one's first page, in a sandbox that may not call anywhere.
func TestDocumentCardNamesTheLastEditorAndShowsTheStart(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []artifact{
				{Slug: "plan", Title: "План", Author: "author@example.com", UpdatedAt: "2026-11-14T10:00:00Z",
					CurrentVersion: &version{N: 3, Author: "Claude", CreatedAt: "2026-11-14T09:58:00Z"}},
				{Slug: "agent", Title: "Отчёт", CurrentVersion: &version{N: 1, Author: "Ада", CreatedAt: "2026-11-13T08:00:00Z"}}}})
		case r.URL.Path == "/api/v1/artifacts/plan/content":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"n": 3},
				"markdown": "# План\n\n> **Цель:** выпустить [доски](https://x) к утру.\n\n```\ncode\n```\n- первый шаг\n- второй шаг\n- третий",
				"html":     "<h1>План</h1>"})
		case r.URL.Path == "/api/v1/artifacts/agent/content":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"n": 1}, "html": "<p>x</p>"})
		case r.URL.Path == "/a/agent/raw":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body><h1>Отчёт</h1></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(art.Close)
	s.art = newArtifacts(art.URL, "t", nil)

	call := func(h func(http.ResponseWriter, *http.Request, *auth.Principal) error, person, slug string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/x", nil)
		r.SetPathValue("slug", slug)
		w := httptest.NewRecorder()
		if err := h(w, r, who[person]); err != nil {
			s.answer(w, r, err)
		}
		return w
	}
	card := func(slug string) linkCard {
		t.Helper()
		w := call(s.card, "mate", slug)
		var c linkCard
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil || w.Code != 200 {
			t.Fatalf("card %s: %d %s", slug, w.Code, w.Body.String())
		}
		return c
	}
	c := card("plan")
	if c.Editor != "Claude" || c.EditedAt != "2026-11-14T09:58:00Z" || c.Page || c.Preview ||
		c.Excerpt != "Цель: выпустить доски к утру.\nпервый шаг\nвторой шаг" {
		t.Fatalf("a text document's card: %+v", c)
	}
	c = card("agent")
	if c.Editor != "Ада" || !c.Page || c.Excerpt != "" {
		t.Fatalf("an HTML document's card: %+v", c)
	}
	w := call(s.preview, "mate", "agent")
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Отчёт") || !strings.Contains(csp, "sandbox allow-scripts;") ||
		!strings.Contains(csp, "connect-src 'none'") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("an HTML document's page: %d %q %s", w.Code, csp, w.Body.String())
	}
	if w := call(s.preview, "stranger", "agent"); w.Code < 400 {
		t.Fatalf("a stranger gets the page: %d", w.Code)
	}
}

func TestExcerptOfLeavesOutTheRepeatedTitleAndMarks(t *testing.T) {
	long := strings.Repeat("слово ", 80)
	for _, tc := range []struct{ md, title, want string }{
		{"# Заметки\nПервая строка", "Заметки", "Первая строка"},
		{"## Другое\nтекст", "Заметки", "Другое\nтекст"},
		{"| a | b |\n|---|---|\n<div>\n***\nстрока", "", "строка"},
		{"", "x", ""},
	} {
		if got := excerptOf(tc.md, tc.title); got != tc.want {
			t.Fatalf("excerptOf(%q) = %q, want %q", tc.md, got, tc.want)
		}
	}
	if got := []rune(excerptOf(long, "")); len(got) != excerptRunes || got[len(got)-1] != '…' {
		t.Fatalf("a long excerpt: %d runes", len(got))
	}
}
