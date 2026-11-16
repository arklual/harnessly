// Package blobserve is the one way a blob of a content-addressed store is given to a browser: the hub's
// uploads, a design's files, a chat's snapshot and a published site all answer by it, so the type, the validator, the
// cache and the ranges are the same everywhere and are tested once.
//
// A blob is named by the sha256 of its contents, so the hash is its ETag on every replica; what differs between the
// kinds of data is only the policy: how the address may be cached, and what the page may do.
package blobserve

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Cache is how long an address keeps its answer.
type Cache int

const (
	// Revalidate is a living address (the newest revision, a page): the browser asks each time and gets 304 while the
	// contents are the same.
	Revalidate Cache = iota
	// Immutable is an address that names its contents (a hash, a pinned revision): kept for good, never asked again.
	Immutable
	// NoStore is an answer that is not kept at all (a page behind a link that may be ended at any moment).
	NoStore
)

// Blob is the contents to give: Open is called only when the body is needed (not for 304, not for HEAD).
type Blob struct {
	Sha  string
	Open func(ctx context.Context) (io.ReadCloser, error)
}

// Policy is what the kind of data decides about an answer.
type Policy struct {
	// Type is the content type; "" is application/octet-stream.
	Type string
	// Disposition is "inline", "attachment" or "" (no header); Name is the file's name in it.
	Disposition, Name string
	// CSP is the page's Content-Security-Policy; "" sends none.
	CSP   string
	Cache Cache
	// Public lets shared caches keep the answer; false is the person's own (private).
	Public bool
	// NoIndex asks robots to leave the answer alone.
	NoIndex bool
}

// ErrNotFound is a blob the store has not got; nothing was written, the caller answers.
var ErrNotFound = errors.New("blobserve: no such blob")

// Serve answers GET and HEAD of a blob. On an error nothing was written and no header set: the caller answers.
func Serve(w http.ResponseWriter, r *http.Request, b Blob, p Policy) error {
	etag := ETag(b.Sha)
	if p.Cache != NoStore && matches(r.Header.Get("If-None-Match"), etag) {
		setHeaders(w.Header(), etag, p)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	if r.Method == http.MethodHead {
		setHeaders(w.Header(), etag, p)
		return nil
	}
	rc, err := b.Open(r.Context())
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	defer rc.Close()
	setHeaders(w.Header(), etag, p)
	if rs, ok := rc.(io.ReadSeeker); ok {
		// Ranges and the conditional headers ServeContent knows, for a store that can seek.
		http.ServeContent(w, r, "", time.Time{}, rs)
		return nil
	}
	_, _ = io.Copy(w, rc)
	return nil
}

func setHeaders(h http.Header, etag string, p Policy) {
	h.Set("X-Content-Type-Options", "nosniff")
	if p.CSP != "" {
		h.Set("Content-Security-Policy", p.CSP)
	}
	if p.NoIndex {
		h.Set("X-Robots-Tag", "noindex, nofollow")
	}
	vis := "private"
	if p.Public {
		vis = "public"
	}
	switch p.Cache {
	case Immutable:
		h.Set("Cache-Control", vis+", max-age=31536000, immutable")
		h.Set("ETag", etag)
	case NoStore:
		h.Set("Cache-Control", "private, no-store")
	default:
		h.Set("Cache-Control", vis+", no-cache")
		h.Set("ETag", etag)
	}
	if p.Type != "" {
		h.Set("Content-Type", p.Type)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	switch name := strings.TrimSpace(p.Name); {
	case p.Disposition == "":
	case name != "":
		h.Set("Content-Disposition", mime.FormatMediaType(p.Disposition, map[string]string{"filename": name}))
	default:
		h.Set("Content-Disposition", p.Disposition)
	}
}

// ETag is the validator a blob is given with: its hash, the same on every replica.
func ETag(sha string) string { return `"` + sha + `"` }

// Holds says whether the request's If-None-Match names the blob of this hash: the browser holds it already.
func Holds(r *http.Request, sha string) bool {
	return matches(r.Header.Get("If-None-Match"), ETag(sha))
}

// matches says whether an If-None-Match header names the validator: one of a list, weak or strong, or "*".
func matches(header, etag string) bool {
	for _, v := range strings.Split(header, ",") {
		v = strings.TrimSpace(v)
		if v == "*" || strings.TrimPrefix(v, "W/") == etag {
			return true
		}
	}
	return false
}

var types = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".json": "application/json",
	".map": "application/json", ".webmanifest": "application/manifest+json", ".svg": "image/svg+xml",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".avif": "image/avif", ".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf",
	".otf": "font/otf", ".wasm": "application/wasm", ".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
	".csv": "text/csv; charset=utf-8", ".xml": "application/xml", ".pdf": "application/pdf", ".mp4": "video/mp4",
	".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".zip": "application/zip",
}

// TypeByName is the content type of a file by its extension; "" for one the table does not know (the caller sends
// application/octet-stream, never a guess from the bytes).
func TypeByName(name string) string {
	return types[strings.ToLower(path.Ext(name))]
}
