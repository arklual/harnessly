package wire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Files: the folder of a conversation as the person browses it beside the chat. Paths are relative to the conversation's
// folder; the machine refuses what leaves it.

// FileKind is what a listed entry is. Links and special files are not listed.
type FileKind string

const (
	FileFile      FileKind = "file"
	FileDirectory FileKind = "directory"
)

// FileKinds is every FileKind, in the schema's order.
var FileKinds = []FileKind{FileFile, FileDirectory}

// FilesPathMax is the longest path a page may ask for, in bytes.
const FilesPathMax = 4096

// FileEntry is one entry of a folder: its name, its path from the conversation's folder.
type FileEntry struct {
	Name string   `json:"name"`
	Path string   `json:"path"`
	Kind FileKind `json:"kind"`
}

func (e FileEntry) Validate() error {
	return errors.Join(need("name", e.Name), need("path", e.Path), in("kind", e.Kind, FileKinds))
}

// DirListing is the answer of GET /api/v2/conversations/{id}/files?path=<folder>: folders first, then files, by name.
// Truncated: the folder holds more than the machine looks at (2000 entries).
type DirListing struct {
	Path      string      `json:"path"`
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

func (d DirListing) Validate() error { return each("entries", d.Entries) }

// FileContent is the answer of GET /api/v2/conversations/{id}/file?path=<file>: a UTF-8 text file of at most 1 MB, with
// the revision a save must name; a save answers with the file as it is now.
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
	Bytes    int    `json:"bytes"`
}

func (f FileContent) Validate() error {
	if f.Bytes < 0 {
		return errors.New("bytes: negative")
	}
	return errors.Join(need("path", f.Path), need("revision", f.Revision))
}

// FileSave is the body of PUT /api/v2/conversations/{id}/file?path=<file>: the whole new text over the revision the page
// read. A file that changed since is refused as a conflict.
type FileSave struct {
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

func (f FileSave) Validate() error {
	if !utf8.ValidString(f.Content) {
		return errors.New("content: not UTF-8")
	}
	return need("revision", f.Revision)
}

// FileSaveBodyMax is the largest body a save may send: a text of 1 MB whose every byte is escaped, and the envelope.
const FileSaveBodyMax = 6<<20 + 4096

// FileRefusal is the answer of a files page that refuses: the code of the protocol and a message worded for the person.
type FileRefusal struct {
	R       ResultKind `json:"r"`
	Code    RefuseCode `json:"code"`
	Message string     `json:"message"`
}

func (f FileRefusal) Validate() error {
	return errors.Join(in("r", f.R, []ResultKind{ResultRefused}), in("code", f.Code, RefuseCodes), need("message", f.Message))
}

// FileError is what a FilesSource refuses with: forbidden (outside the folder, no access), not-found (gone, or not a
// folder), unsupported (not text, not UTF-8, over 1 MB), conflict (a save over a file that changed), internal.
type FileError struct {
	Code    RefuseCode
	Message string
}

func (e *FileError) Error() string { return e.Message }

// FilesSource reads and saves the folder of a conversation. Errors: convo.ErrNotFound (no such conversation), a *FileError.
type FilesSource interface {
	Dir(ctx context.Context, conv, path string) (DirListing, error)
	File(ctx context.Context, conv, path string) (FileContent, error)
	Save(ctx context.Context, conv, path, content, revision string) (FileContent, error)
}

// files serves GET /api/v2/conversations/{id}/files (a folder) and .../file (a text file), PUT .../file saves one; the
// folder is ?path=, empty for the conversation's own.
func (s *Server) files(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string, one bool) {
	if s.Files == nil {
		refuse(w, RefuseNotFound)
		return
	}
	path := r.URL.Query().Get("path")
	if len(path) > FilesPathMax || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || one && path == "" {
		writeJSON(w, http.StatusBadRequest, FileRefusal{R: ResultRefused, Code: RefuseBadRequest, Message: "path"})
		return
	}
	var out interface{ Validate() error }
	var err error
	switch {
	case one && r.Method == http.MethodPut:
		var body FileSave
		raw, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, FileSaveBodyMax))
		if rerr == nil {
			rerr = decodeStrict(raw, &body)
		}
		if rerr == nil {
			rerr = body.Validate()
		}
		if rerr != nil {
			writeJSON(w, http.StatusBadRequest, FileRefusal{R: ResultRefused, Code: RefuseBadRequest, Message: rerr.Error()})
			return
		}
		out, err = s.Files.Save(ctx, conv, path, body.Content, body.Revision)
	case one:
		out, err = s.Files.File(ctx, conv, path)
	default:
		var list DirListing
		list, err = s.Files.Dir(ctx, conv, path)
		if list.Entries == nil {
			list.Entries = []FileEntry{}
		}
		out = list
	}
	var fe *FileError
	switch {
	case err == nil:
		s.writeChecked(w, r, out)
	case errors.As(err, &fe):
		status := refusalStatus[fe.Code]
		if fe.Code == RefuseUnsupported {
			status = http.StatusUnsupportedMediaType
		}
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, FileRefusal{R: ResultRefused, Code: fe.Code, Message: fe.Message})
	case s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	default:
		jsonlog.Log("warn", "v2: files failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}

// ShotFile is a listed picture opened to be sent.
type ShotFile struct {
	Body    io.ReadSeekCloser
	Mime    string
	ModTime time.Time
}

// ErrNoShot is what Server.Shot answers for an id the conversation does not list, and for a listed picture that
// cannot be sent (gone, too big, not a picture).
var ErrNoShot = errors.New("no such shot")

// shot serves GET /api/v2/conversations/{id}/shot?id=<shot id>: the bytes of a picture of the conversation's shots.
// The request names an id of the list the conversation keeps, never a path.
func (s *Server) shot(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	id := r.URL.Query().Get("id")
	if s.Shot == nil || r.Method != http.MethodGet && r.Method != http.MethodHead {
		refuse(w, RefuseNotFound)
		return
	}
	if id == "" || len(id) > 64 {
		refuse(w, RefuseBadRequest)
		return
	}
	file, err := s.Shot(ctx, conv, id)
	switch {
	case err == nil:
		defer file.Body.Close()
		h := w.Header()
		h.Set("Content-Type", file.Mime)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		// The id changes when the picture is looked at again, so the answer to one id may be kept.
		h.Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, "", file.ModTime, file.Body)
	case errors.Is(err, ErrNoShot), s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	default:
		jsonlog.Log("warn", "v2: shot failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}
