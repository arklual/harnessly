// Package webstatic sends the built files of a page gzipped to browsers that take it. The compressed copy of a file is
// made once and kept in memory; a file that changes on disk gets a new copy. Files under 1 KB, Range requests and
// types that are already compressed go out as they are.
package webstatic

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MinBytes is the size below which a body is not worth compressing.
const MinBytes = 1024

// compressible are the extensions worth compressing; images, fonts and archives are left alone.
var compressible = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".html": true, ".json": true, ".svg": true,
	".wasm": true, ".map": true, ".txt": true, ".xml": true, ".webmanifest": true,
}

// file is what is known about one file of the page: its tag and, once asked for, its gzipped bytes.
type file struct {
	size int64
	mod  time.Time
	load sync.Once
	etag string // strong tag of the plain bytes, quoted
	err  error
	gz   []byte // nil: not worth compressing
}

// Cache keeps the tags and gzipped copies of a page's files. The zero value is ready to use.
type Cache struct {
	mu sync.Mutex
	m  map[string]*file
}

// get returns the entry for name, a fresh one when the file's size or time differ from the cached one.
func (c *Cache) get(name string, fi fs.FileInfo) *file {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f := c.m[name]; f != nil && f.size == fi.Size() && f.mod.Equal(fi.ModTime()) {
		return f
	}
	if c.m == nil {
		c.m = map[string]*file{}
	}
	f := &file{size: fi.Size(), mod: fi.ModTime()}
	c.m[name] = f
	return f
}

// forget drops an entry whose file could not be read, so the next request tries again.
func (c *Cache) forget(name string, f *file) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m[name] == f {
		delete(c.m, name)
	}
}

// prepare reads the file once to tag it and, for compressible types of a useful size, to compress it. It returns the
// plain bytes it read.
func (f *file) prepare(files fs.FS, name string) []byte {
	var raw []byte
	f.load.Do(func() {
		raw, f.err = fs.ReadFile(files, name)
		if f.err != nil {
			return
		}
		sum := sha256.Sum256(raw)
		f.etag = `"` + hex.EncodeToString(sum[:12]) + `"`
		if compressible[path.Ext(name)] && len(raw) >= MinBytes {
			if z := Gzip(raw); len(z) < len(raw) {
				f.gz = z
			}
		}
	})
	return raw
}

// Gzip compresses raw at the level used for the page's files.
func Gzip(raw []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, 6)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return buf.Bytes()
}

// AcceptsGzip is true when Accept-Encoding lists gzip with a quality above zero.
func AcceptsGzip(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(v, ",") {
			enc, params, _ := strings.Cut(part, ";")
			enc = strings.ToLower(strings.TrimSpace(enc))
			if enc != "gzip" && enc != "x-gzip" {
				continue
			}
			q := 1.0
			if p := strings.TrimSpace(params); strings.HasPrefix(p, "q=") || strings.HasPrefix(p, "Q=") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64); err == nil {
					q = f
				}
			}
			return q > 0
		}
	}
	return false
}

// Serve answers for one file of the page: its type, its cache rule (files under assets/ are named by their content),
// a tag that differs between the plain and the gzipped copy, and the gzipped copy when the browser takes it. It
// reports false, having written nothing, when the file cannot be read, so the caller can fall through to the page's
// own routes.
func (c *Cache) Serve(w http.ResponseWriter, r *http.Request, files fs.FS, name string) bool {
	fi, err := fs.Stat(files, name)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	f := c.get(name, fi)
	raw := f.prepare(files, name)
	if f.err != nil {
		c.forget(name, f)
		return false
	}
	ext := path.Ext(name)
	gzipped := f.gz != nil && r.Header.Get("Range") == "" && AcceptsGzip(r)
	if raw == nil && !gzipped {
		// The cache had the file already; the plain bytes are read again rather than kept.
		if raw, err = fs.ReadFile(files, name); err != nil {
			return false
		}
	}
	h := w.Header()
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		if raw == nil {
			if raw, err = fs.ReadFile(files, name); err != nil {
				return false
			}
		}
		ct = http.DetectContentType(raw)
	}
	h.Set("Content-Type", ct)
	// Vite names the built assets by their content.
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	if compressible[ext] && f.size >= MinBytes {
		h.Add("Vary", "Accept-Encoding")
	}
	body, tag := raw, f.etag
	if gzipped {
		h.Set("Content-Encoding", "gzip")
		body, tag = f.gz, strings.TrimSuffix(f.etag, `"`)+`-gz"`
		// ServeContent sets the length itself only for plain bodies.
		h.Set("Content-Length", strconv.Itoa(len(body)))
	}
	h.Set("ETag", tag)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	return true
}

// WriteBody sends a body made for this request (a rendered page), gzipped when it is worth it and the browser takes
// it. The caller sets the type and the cache rule first; gz, when not nil, is the body compressed beforehand.
func WriteBody(w http.ResponseWriter, r *http.Request, raw, gz []byte) {
	h := w.Header()
	if len(raw) >= MinBytes {
		h.Add("Vary", "Accept-Encoding")
		if AcceptsGzip(r) {
			if gz == nil {
				gz = Gzip(raw)
			}
			if len(gz) < len(raw) {
				raw = gz
				h.Set("Content-Encoding", "gzip")
			}
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(raw)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(raw)
	}
}
