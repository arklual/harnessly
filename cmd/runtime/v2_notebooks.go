package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/notebooks"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// notebookPath is deliberately narrower than viewer access. A delivered or
// touched file outside the conversation's folder is not permission to execute
// code in that directory. Both spellings must pass the existing secret rule.
func (f *v2Files) notebookPath(ctx context.Context, conv, path string) (string, error) {
	root, err := f.root(conv)
	if err != nil {
		return "", err
	}
	deny := &wire.FileError{Code: wire.RefuseForbidden, Message: "Execute notebooks only inside the conversation's folder, outside credential directories."}
	if f.secret(path) {
		return "", deny
	}
	if filepath.IsAbs(path) {
		realRoot, rerr := files.Realpath(root)
		realPath, perr := files.Realpath(path)
		if rerr != nil || perr != nil {
			return "", &wire.FileError{Code: wire.RefuseNotFound, Message: "Notebook not found."}
		}
		if f.secret(realPath) {
			return "", deny
		}
		path, err = filepath.Rel(realRoot, realPath)
		if err != nil {
			return "", deny
		}
	}
	// Abs delegates to the file resolver, including canonical symlink containment.
	abs, err := f.Abs(ctx, conv, path)
	if err != nil {
		return "", err
	}
	if f.secret(abs) {
		return "", deny
	}
	if strings.ToLower(filepath.Ext(abs)) != ".ipynb" {
		return "", &wire.FileError{Code: wire.RefuseUnsupported, Message: "Only .ipynb notebooks can be executed."}
	}
	st, err := os.Stat(abs)
	if err != nil || !st.Mode().IsRegular() {
		return "", &wire.FileError{Code: wire.RefuseNotFound, Message: "Notebook not found."}
	}
	parent, err := os.Stat(filepath.Dir(abs))
	if err != nil || st.Mode().Perm()&0o222 == 0 || parent.Mode().Perm()&0o222 == 0 {
		return "", deny
	}
	// Reading through the same text adapter applies its notebook-specific size
	// limit, denies special files, and verifies actual read permissions and UTF-8.
	content, err := f.File(ctx, conv, path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Format int               `json:"nbformat"`
		Cells  []json.RawMessage `json:"cells"`
	}
	if json.Unmarshal([]byte(content.Content), &doc) != nil || doc.Format != 4 || doc.Cells == nil {
		return "", &wire.FileError{Code: wire.RefuseUnsupported, Message: "Expected a valid nbformat 4 notebook."}
	}
	return abs, nil
}

type v2Notebooks struct {
	files   *v2Files
	kernels *notebooks.Manager
}

func (n *v2Notebooks) Serve(w http.ResponseWriter, r *http.Request, conv string) {
	w.Header().Set("Cache-Control", "no-store")
	write := func(status int, value any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	fail := func(status int, code wire.RefuseCode, message string) {
		write(status, wire.FileRefusal{R: wire.ResultRefused, Code: code, Message: message})
	}
	q := r.URL.Query()
	path := q.Get("path")
	if len(q["path"]) != 1 || path == "" || len(path) > wire.FilesPathMax || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		fail(http.StatusBadRequest, wire.RefuseBadRequest, "A notebook path is required.")
		return
	}
	var body struct {
		Action string  `json:"action"`
		Code   *string `json:"code,omitempty"`
	}
	if r.Method == http.MethodPost {
		// JSON may escape each source byte to six bytes.
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6*notebooks.MaxCodeBytes+4096))
		dec.DisallowUnknownFields()
		err := dec.Decode(&body)
		if err == nil {
			var extra any
			if dec.Decode(&extra) != io.EOF {
				err = errors.New("Expected one JSON object.")
			}
		}
		if err != nil {
			fail(http.StatusBadRequest, wire.RefuseBadRequest, "Expected {action, code?} within the cell code limit.")
			return
		}
		switch body.Action {
		case "execute":
			if body.Code == nil || !utf8.ValidString(*body.Code) || len(*body.Code) > notebooks.MaxCodeBytes {
				fail(http.StatusRequestEntityTooLarge, wire.RefuseAttachmentTooLarge, "Execute requires UTF-8 code of at most 256 KiB.")
				return
			}
		case "interrupt", "restart", "shutdown":
			if body.Code != nil {
				fail(http.StatusBadRequest, wire.RefuseBadRequest, "Only execute accepts code.")
				return
			}
		default:
			fail(http.StatusBadRequest, wire.RefuseBadRequest, "Unknown notebook action.")
			return
		}
	}
	abs, err := n.files.notebookPath(r.Context(), conv, path)
	if err != nil {
		var fe *wire.FileError
		switch {
		case errors.Is(err, convo.ErrNotFound):
			fail(http.StatusNotFound, wire.RefuseNotFound, "Conversation not found.")
		case errors.As(err, &fe):
			status := http.StatusForbidden
			switch fe.Code {
			case wire.RefuseNotFound:
				status = http.StatusNotFound
			case wire.RefuseUnsupported:
				status = http.StatusUnsupportedMediaType
			case wire.RefuseInternal:
				status = http.StatusInternalServerError
			}
			fail(status, fe.Code, fe.Message)
		default:
			fail(http.StatusInternalServerError, wire.RefuseInternal, "Cannot resolve this notebook.")
		}
		return
	}
	// v2Session authenticates the request and supplies this identity, just as it
	// does for file routes. Kernels never cross identities, even for one path.
	key := notebooks.Key{Owner: design.PersonIn(r.Context()), Conversation: conv, Path: abs}
	var out any
	if r.Method == http.MethodGet {
		out = n.kernels.Status(key)
	} else {
		switch body.Action {
		case "execute":
			out, err = n.kernels.Execute(r.Context(), key, *body.Code)
		case "interrupt":
			out, err = n.kernels.Interrupt(r.Context(), key)
		case "restart":
			out, err = n.kernels.Restart(r.Context(), key)
		case "shutdown":
			out = n.kernels.Shutdown(key)
		}
	}
	if err == nil {
		write(http.StatusOK, out)
		return
	}
	var ne *notebooks.Error
	if !errors.As(err, &ne) {
		fail(http.StatusInternalServerError, wire.RefuseInternal, "Notebook execution failed.")
		return
	}
	status, code := http.StatusServiceUnavailable, wire.RefuseUnsupported
	switch ne.Code {
	case "busy", "limit":
		status, code = http.StatusConflict, wire.RefuseConflict
	case "code-limit", "output-limit":
		status, code = http.StatusRequestEntityTooLarge, wire.RefuseAttachmentTooLarge
	case "timeout", "closed":
		code = wire.RefuseTimeout
	case "internal":
		status, code = http.StatusInternalServerError, wire.RefuseInternal
	}
	fail(status, code, ne.Message)
}
