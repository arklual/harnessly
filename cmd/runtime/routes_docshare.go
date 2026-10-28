package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/docshare"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// «Поделиться» of a file open in the editor: the file becomes a document of the catalog, private to its
// owner, and stays bound to the file (docshare). The page asks here, with the tab's session and the file's path in the
// conversation's folder as the pane's file tab does; the access to the document is then the gateway's (/docs/<slug>/acl).
//
//	GET  <platform>/doc-share/state?conversation=&path=   {shared, slug, linked, version, pending, refusedLine}
//	POST <platform>/doc-share/publish {conversation, path}   the state; 422 {error, line, kind} for a secret
//	POST <platform>/doc-share/unlink  {conversation, path}   the state, now unlinked

const docShareBodyMax = 4 << 10

func docShareRoutes(d deps, c *core) []gate.Route {
	svc := docshare.New(docshare.Config{DataDir: d.DataDir, AgentTokenHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")})
	go svc.Run(context.Background())
	prefix := platformAPI + "/doc-share/"
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, status int, text string) {
		write(w, status, map[string]string{"error": text})
	}
	// file is the absolute path of the file the request names, or a refusal already written.
	file := func(w http.ResponseWriter, r *http.Request, conversation, relative string) (string, bool) {
		if conversation == "" || relative == "" {
			fail(w, http.StatusBadRequest, locale.Pick("No file is selected.", "Не выбран файл."))
			return "", false
		}
		v := c.v2.Load()
		if v == nil {
			fail(w, http.StatusServiceUnavailable, locale.Pick("The workspace is starting, try again in a moment.", "Рабочее пространство запускается, повторите через минуту."))
			return "", false
		}
		abs, err := (&v2Files{record: v.rt.Record, touched: v.rt.Touched}).Abs(r.Context(), conversation, relative)
		if err != nil {
			var fe *wire.FileError
			status, text := http.StatusNotFound, locale.Pick("The file was not found.", "Файл не найден.")
			if errors.As(err, &fe) {
				text = fe.Message
				switch fe.Code {
				case wire.RefuseForbidden:
					status = http.StatusForbidden
				case wire.RefuseUnsupported:
					status = http.StatusUnsupportedMediaType
				}
			}
			fail(w, status, text)
			return "", false
		}
		return filepath.Clean(abs), true
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
			fail(w, http.StatusUnauthorized, locale.Pick("Sign in to the workspace.", "Войдите в рабочее пространство."))
			return
		}
		action := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), prefix)
		var body struct {
			Conversation string `json:"conversation"`
			Path         string `json:"path"`
		}
		switch {
		case action == "state" && r.Method == http.MethodGet:
			q := r.URL.Query()
			body.Conversation, body.Path = q.Get("conversation"), q.Get("path")
		case (action == "publish" || action == "unlink") && r.Method == http.MethodPost:
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, docShareBodyMax)).Decode(&body); err != nil {
				fail(w, http.StatusBadRequest, locale.Pick("Invalid JSON", "Неверный JSON"))
				return
			}
		default:
			w.Header().Set("Allow", "GET, POST")
			fail(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		abs, ok := file(w, r, body.Conversation, body.Path)
		if !ok {
			return
		}
		switch action {
		case "state":
			write(w, http.StatusOK, svc.State(abs))
		case "unlink":
			st, found := svc.Unlink(abs)
			if !found {
				fail(w, http.StatusNotFound, locale.Pick("The file is not shared.", "Файл не опубликован."))
				return
			}
			write(w, http.StatusOK, st)
		default:
			st, f := svc.Publish(docshare.Request{Path: abs, Conversation: body.Conversation})
			if f != nil {
				out := map[string]any{"error": f.Text}
				if f.Line > 0 {
					out["line"], out["kind"] = f.Line, f.Kind
				}
				write(w, f.Status, out)
				return
			}
			write(w, http.StatusOK, st)
		}
	})
	return []gate.Route{{Match: func(r *http.Request) bool {
		return strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), prefix)
	}, Handler: handler}}
}
