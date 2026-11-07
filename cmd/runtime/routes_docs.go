package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/docprovider"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/integrations"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The page's document reader (<platform>/docs/…) on top of the artifacts catalog: the list, one document, its
// versions and their diff, a short-lived read link for the frame, the public link switch, and comment threads the
// runtime keeps itself. Without a catalog the list is empty and every document is not found, so the page simply has
// no documents.

var (
	docSlugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,120}$`)
	docThreadRe = regexp.MustCompile(`^[a-f0-9]{16}$`)
)

const docBodyMax = 16 << 10

func docsRoutes(d deps, c integrations.Config) []gate.Route {
	if !d.Standalone && c.ArtifactsURL == "" && !docprovider.Declared() {
		return nil
	}
	threads := &integrations.Threads{Path: filepath.Join(d.DataDir, "doc-threads.json")}
	// The built-in catalog, or the declared documents adapter (docs/doc-adapters.md).
	provider := docprovider.For(c, threads)
	prefix := platformAPI + "/docs"
	tail := func(r *http.Request) (string, bool) {
		p := strings.TrimPrefix(r.URL.Path, "/agents")
		if p == prefix {
			return "", true
		}
		if strings.HasPrefix(p, prefix+"/") {
			return strings.TrimPrefix(p, prefix+"/"), true
		}
		return "", false
	}
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		var ue *integrations.UpstreamError
		switch {
		case errors.Is(err, docprovider.ErrUnsupported):
			write(w, http.StatusNotImplemented, map[string]string{"code": "unsupported", "error": locale.Pick("The documents service does not do this", "Сервис документов этого не умеет")})
		case errors.Is(err, docprovider.ErrTooLarge):
			write(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "too_large", "error": locale.Pick(
				"The document is larger than the documents service hands over here; open it in the service itself",
				"Документ больше, чем сервис документов отдаёт сюда: откройте его в самом сервисе")})
		case errors.Is(err, integrations.ErrNotConfigured):
			write(w, http.StatusNotFound, map[string]string{"error": locale.Pick("Documents are not connected", "Документы не подключены")})
		case errors.Is(err, integrations.ErrNoThread):
			write(w, http.StatusNotFound, map[string]string{"error": locale.Pick("No such thread", "Нет такого обсуждения")})
		case errors.As(err, &ue) && ue.Status == http.StatusNotFound:
			write(w, http.StatusNotFound, map[string]string{"error": locale.Pick("Document not found", "Документ не найден")})
		case errors.As(err, &ue):
			write(w, http.StatusBadGateway, map[string]string{"error": locale.Pick("Documents catalog: ", "Каталог документов: ") + ue.Text})
		default:
			write(w, http.StatusBadGateway, map[string]string{"error": locale.Pick("The documents catalog is unreachable", "Каталог документов недоступен")})
		}
	}
	person := func(r *http.Request) integrations.DocAuthor {
		u := personOf(d.Claims(r))
		name := u.Name
		if name == "" {
			name = os.Getenv("RUNTIME_OWNER_NAME")
		}
		if name == "" {
			name = u.Email
		}
		return integrations.DocAuthor{Kind: "person", Email: u.Email, Name: name}
	}
	readBody := func(w http.ResponseWriter, r *http.Request, into any) bool {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, docBodyMax)).Decode(into); err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("Invalid JSON", "Неверный JSON")})
			return false
		}
		return true
	}
	withCounts := func(r *http.Request, docs []integrations.DocSummary) []integrations.DocSummary {
		n, err := provider.Counts(r.Context(), person(r).Email)
		if err != nil || n.Open == nil {
			return docs
		}
		for i := range docs {
			docs[i].OpenThreads, docs[i].Unread = n.Open[docs[i].Slug], n.Unread[docs[i].Slug]
			if n.Last[docs[i].Slug] > docs[i].LastActivityAt {
				docs[i].LastActivityAt = n.Last[docs[i].Slug]
			}
		}
		return docs
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			for k, vs := range h {
				w.Header()[k] = vs
			}
			write(w, status, v)
			return
		}
		rest, _ := tail(r)
		parts := strings.Split(rest, "/")
		switch {
		case rest == "" && r.Method == http.MethodGet:
			docs, err := provider.List(r.Context(), 100)
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, map[string]any{"docs": withCounts(r, docs), "capabilities": provider.Capabilities()})
		case rest == "people" && r.Method == http.MethodGet:
			q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
			type personOut struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Initial string `json:"initial"`
			}
			out := []personOut{}
			for login, name := range people(os.Getenv("RUNTIME_PEOPLE")) {
				if q != "" && !strings.Contains(strings.ToLower(login+" "+name), q) {
					continue
				}
				initial := ""
				for _, ch := range name {
					initial = strings.ToUpper(string(ch))
					break
				}
				out = append(out, personOut{ID: login, Name: name, Initial: initial})
			}
			write(w, 200, map[string]any{"people": out})
		case !docSlugRe.MatchString(parts[0]):
			write(w, http.StatusNotFound, map[string]string{"error": locale.Pick("Document not found", "Документ не найден")})
		case len(parts) == 1 && r.Method == http.MethodGet:
			doc, err := provider.Get(r.Context(), parts[0])
			if err != nil {
				fail(w, err)
				return
			}
			list, err := provider.Threads(r.Context(), parts[0])
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, map[string]any{"doc": withCounts(r, []integrations.DocSummary{doc})[0], "threads": list})
		case len(parts) == 2 && parts[1] == "versions" && r.Method == http.MethodGet:
			versions, err := provider.Versions(r.Context(), parts[0])
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, map[string]any{"versions": versions})
		case len(parts) == 2 && parts[1] == "diff" && r.Method == http.MethodGet:
			q := r.URL.Query()
			diff, err := provider.Diff(r.Context(), parts[0], q.Get("from"), q.Get("to"))
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, diff)
		case len(parts) == 2 && parts[1] == "content" && r.Method == http.MethodGet:
			embed, err := provider.Embed(r.Context(), parts[0])
			if err != nil {
				fail(w, err)
				return
			}
			link := embed.URL
			if v := r.URL.Query().Get("v"); v != "" {
				sep := "?"
				if strings.Contains(link, "?") {
					sep = "&"
				}
				link += sep + "v=" + v
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, link, http.StatusFound)
		case len(parts) == 2 && parts[1] == "access" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
			var open *bool
			if r.Method == http.MethodPost {
				var body struct {
					Public *bool `json:"public"`
				}
				if !readBody(w, r, &body) {
					return
				}
				if body.Public == nil {
					write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("public: true or false", "public: true или false")})
					return
				}
				open = body.Public
			}
			access, err := provider.SetAccess(r.Context(), parts[0], docprovider.AccessChange{Public: open})
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, access)
		case len(parts) == 2 && parts[1] == "read" && r.Method == http.MethodPost:
			if err := provider.MarkRead(r.Context(), person(r).Email, parts[0]); err != nil {
				fail(w, err)
				return
			}
			write(w, 200, map[string]bool{"ok": true})
		case len(parts) == 2 && parts[1] == "threads" && r.Method == http.MethodPost:
			var body struct {
				Anchor integrations.DocAnchor `json:"anchor"`
				Body   string                 `json:"body"`
			}
			if !readBody(w, r, &body) {
				return
			}
			if strings.TrimSpace(body.Body) == "" || strings.TrimSpace(body.Anchor.Quote) == "" {
				write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("A text and a place in the document are required", "Нужны текст и место в документе")})
				return
			}
			th, err := provider.CreateThread(r.Context(), parts[0], body.Anchor, strings.TrimSpace(body.Body), person(r))
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 201, map[string]any{"thread": th})
		case len(parts) == 4 && parts[1] == "threads" && docThreadRe.MatchString(parts[2]) && r.Method == http.MethodPost:
			var th integrations.DocThread
			var err error
			switch parts[3] {
			case "comments":
				var body struct {
					Body string `json:"body"`
				}
				if !readBody(w, r, &body) {
					return
				}
				if strings.TrimSpace(body.Body) == "" {
					write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("Empty comment", "Пустой комментарий")})
					return
				}
				th, err = provider.Reply(r.Context(), parts[0], parts[2], strings.TrimSpace(body.Body), person(r))
			case "status":
				var body struct {
					Status string `json:"status"`
				}
				if !readBody(w, r, &body) {
					return
				}
				if body.Status != "open" && body.Status != "resolved" {
					write(w, http.StatusBadRequest, map[string]string{"error": locale.Pick("status: open or resolved", "status: open или resolved")})
					return
				}
				th, err = provider.SetStatus(r.Context(), parts[0], parts[2], body.Status)
			default:
				write(w, http.StatusNotFound, map[string]string{"error": "Not found"})
				return
			}
			if err != nil {
				fail(w, err)
				return
			}
			write(w, 200, map[string]any{"thread": th})
		default:
			w.Header().Set("Allow", "GET, POST")
			write(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	})
	return []gate.Route{{Match: func(r *http.Request) bool { _, ok := tail(r); return ok }, Handler: handler}}
}
