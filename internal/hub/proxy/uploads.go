package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The workspace's uploads in the hub's store (docs/hub/history.md): what a person puts into a chat goes to the
// machine as before, and the page puts the same bytes into the `uploads` collection of the machine's space, by their
// sha256, with the id the machine gave them. The chat's pictures and files are then read from the hub, with the
// machine on or off; an upload the hub has not got (older than this, or put while it was off) is read from the machine.
//
//	PUT  <platform api>/uploads/blobs/<sha256>?machine=<id>         the bytes (they must hash to the name)
//	GET  <platform api>/uploads/blobs/<sha256>?machine=<id>&type=   the bytes
//	PUT  <platform api>/uploads/refs/<upload id>?machine=<id>       {sha256, mime, name, size}: what the machine's upload is
//	GET  <platform api>/uploads/files/<upload id>?machine=<id>      the upload from the hub, else a redirect to the machine
//	GET  <platform api>/uploads/delivered/<sha256>?machine=<id>&type=&name=&path=[&download=1]
//	                                                                a file the agent handed over, which the machine put
//	                                                                into the space's `files` collection; else a redirect
//	                                                                to the machine's file-preview of the path
//	GET  <platform api>/uploads/delivered/by-path?machine=<id>&path=   the same, found by its path in the person's turns
//	                                                                (the pane opens a handed-over file by its path)
//
// The person must be one who may open the machine; a blob is read only within the machine's space, and a ref only by
// the person who named it (a chat is its owner's, so is what its pictures are).

// Uploads is the store's side the workspace needs (storehub.Store).
type Uploads interface {
	HasBlob(ctx context.Context, spaceID, kind, sha string) (bool, error)
	OpenBlob(ctx context.Context, spaceID, kind, sha string) (io.ReadCloser, error)
	PutBlob(ctx context.Context, spaceID, kind, sha string, r io.Reader) error
}

// UseUploads lets the workspace keep uploads in the hub's store; nil (no store) leaves them to the machines.
func (s *Service) UseUploads(u Uploads) {
	s.mu.Lock()
	s.uploads = u
	s.mu.Unlock()
}

func (s *Service) uploadsStore() Uploads {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uploads
}

var (
	uploadsPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/uploads/(blobs|refs|files|delivered)/([A-Za-z0-9_.-]{1,128})$`)
	uploadID    = regexp.MustCompile(`^up_[A-Z2-7]{4,64}$`)
	// inlineTypes are shown in the page as they are; anything else is a download, so nothing put here runs on the hub's
	// origin.
	inlineTypes = map[string]bool{
		"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
		"application/pdf": true, "text/plain": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true,
		"video/mp4": true, "video/webm": true,
	}
)

type uploadRef struct {
	Sha256 string `json:"sha256"`
	Mime   string `json:"mime"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

func (s *Service) serveUploads(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	u := s.uploadsStore()
	match := uploadsPath.FindStringSubmatch(rest)
	if u == nil || match == nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	kind, name := match[1], match[2]
	id := r.URL.Query().Get("machine")
	if id == "" || id == "local" {
		id = s.home(r.Context(), p.User.ID, true)
	}
	if !machineID.MatchString(id) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	m, err := s.admit(r.Context(), p.User.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	switch {
	case kind == "blobs" && r.Method == http.MethodPut:
		if !filestore.ValidSha(name) {
			httpx.WriteError(w, r, httpx.Invalid("sha256", "64 hex digits"))
			return
		}
		// The bytes are hashed here, all of them, whether the store had them already or not: only bytes that hash to the
		// name make the person their owner.
		h := sha256.New()
		body := io.TeeReader(http.MaxBytesReader(w, r.Body, filestore.MaxFileSize), h)
		if err := u.PutBlob(ctx, m.SpaceID, filestore.KindUploads, name, body); err != nil {
			if errors.Is(err, filestore.ErrBadRequest) {
				httpx.WriteError(w, r, httpx.Invalid("body", "the bytes do not hash to the name, or are too big"))
				return
			}
			s.fail(w, r, err)
			return
		}
		if _, err := io.Copy(io.Discard, body); err != nil || hex.EncodeToString(h.Sum(nil)) != name {
			httpx.WriteError(w, r, httpx.Invalid("body", "the bytes do not hash to the name, or are too big"))
			return
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO upload_owners (machine_id, sha256, owner_id, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (machine_id, sha256, owner_id) DO NOTHING`, m.ID, name, p.User.ID, store.Ms(s.now())); err != nil {
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case kind == "blobs" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		// Only bytes the person put: the collection is the space's machines', not the person's.
		if !s.ownsUpload(ctx, m.ID, p.User.ID, name) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		s.sendBlob(w, r, u, m, filestore.KindUploads, name, r.URL.Query().Get("type"), r.URL.Query().Get("name"))
	case kind == "refs" && r.Method == http.MethodPut:
		var ref uploadRef
		if !uploadID.MatchString(name) || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&ref) != nil || !filestore.ValidSha(ref.Sha256) || ref.Size < 0 {
			httpx.WriteError(w, r, httpx.Invalid("ref", "an upload id and {sha256, mime, name, size}"))
			return
		}
		// Only what is in the store already may be named: a ref never points at bytes the hub has not checked.
		if ok, err := u.HasBlob(ctx, m.SpaceID, filestore.KindUploads, ref.Sha256); err != nil || !ok || !s.ownsUpload(ctx, m.ID, p.User.ID, ref.Sha256) {
			httpx.WriteError(w, r, httpx.Invalid("sha256", "put the blob first"))
			return
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO upload_refs (machine_id, upload_id, owner_id, sha256, mime, name, size, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (machine_id, upload_id, owner_id) DO NOTHING`,
			m.ID, name, p.User.ID, ref.Sha256, clip(ref.Mime, 128), clip(ref.Name, 255), ref.Size, store.Ms(s.now())); err != nil {
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case kind == "files" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		if !uploadID.MatchString(name) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		var ref uploadRef
		err := s.db.QueryRow(ctx, `SELECT sha256, mime, name, size FROM upload_refs WHERE machine_id = ? AND upload_id = ? AND owner_id = ?`, m.ID, name, p.User.ID).
			Scan(&ref.Sha256, &ref.Mime, &ref.Name, &ref.Size)
		if err == nil {
			if ok, _ := u.HasBlob(ctx, m.SpaceID, filestore.KindUploads, ref.Sha256); ok {
				s.sendBlob(w, r, u, m, filestore.KindUploads, ref.Sha256, ref.Mime, ref.Name)
				return
			}
		}
		// Not in the hub (yet): the machine's own copy, the way the page read it before.
		target := s.cfg.BasePath + workspaceBase + "/api/v2/uploads/" + url.PathEscape(name) + "?machine=" + url.QueryEscape(m.ID)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	case kind == "delivered" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		q := r.URL.Query()
		sha, typ, title := name, q.Get("type"), q.Get("name")
		// by-path: the pane opens a handed-over file by its path; the person's own turns say which bytes it is.
		if name == "by-path" {
			sha = ""
			if f, ok, _ := history.DeliveredAt(ctx, s.db, p.User.ID, m.ID, q.Get("path")); ok {
				sha, typ, title = f.Sha256, f.Mime, f.Name
			}
		}
		// Only a file the person's own chats show: the collection is the space's machines', not the person's. Anything
		// else is the machine's to answer, the same way as a file the hub has not got.
		mine, _ := history.Delivered(ctx, s.db, p.User.ID, m.ID, sha)
		if ok, _ := u.HasBlob(ctx, m.SpaceID, filestore.KindFiles, sha); mine && ok {
			if q.Get("download") == "1" {
				typ = "" // a download is never shown in the page
			}
			s.sendBlob(w, r, u, m, filestore.KindFiles, sha, typ, title)
			return
		}
		// Not in the hub (older, too big, or put while it was away): the machine's own preview of the file, where the
		// machine decides again whether the person may read it.
		path := q.Get("path")
		if !strings.HasPrefix(path, "/") || len(path) > 4096 {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		target := url.Values{}
		if q.Get("download") == "1" {
			target.Set("download", "1")
		}
		target.Set("path", path)
		target.Set("machine", m.ID)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, s.cfg.BasePath+workspaceBase+platformOf(rest)+"/file-preview?"+target.Encode(), http.StatusTemporaryRedirect)
	default:
		httpx.WriteError(w, r, httpx.ErrNotFound)
	}
}

// ownsUpload tells whether the person put these bytes into the machine's space.
func (s *Service) ownsUpload(ctx context.Context, machine, user, sha string) bool {
	var one int
	return s.db.QueryRow(ctx, `SELECT 1 FROM upload_owners WHERE machine_id = ? AND sha256 = ? AND owner_id = ?`, machine, sha, user).Scan(&one) == nil
}

// platformOf is the platform api prefix of a workspace path (/api/acme).
func platformOf(rest string) string {
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	return "/" + parts[1] + "/" + parts[2]
}

func (s *Service) sendBlob(w http.ResponseWriter, r *http.Request, u Uploads, m store.Machine, collection, sha, kind, name string) {
	if !filestore.ValidSha(sha) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	rc, err := u.OpenBlob(r.Context(), m.SpaceID, collection, sha)
	if errors.Is(err, os.ErrNotExist) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rc.Close()
	kind, _, _ = mime.ParseMediaType(kind)
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+sha+`"`)
	disposition := "attachment"
	if inlineTypes[kind] {
		h.Set("Content-Type", kind)
		disposition = "inline"
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	if name = strings.TrimSpace(name); name != "" {
		h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": clip(name, 255)}))
	} else {
		h.Set("Content-Disposition", disposition)
	}
	if match := r.Header.Get("If-None-Match"); match == `"`+sha+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	if f, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, f)
		return
	}
	_, _ = io.Copy(w, rc)
}

func clip(v string, n int) string {
	if len(v) <= n {
		return v
	}
	cut := v[:n]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}
