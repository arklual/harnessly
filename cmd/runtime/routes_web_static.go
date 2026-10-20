package main

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

// Static files of the page are sent gzipped to browsers that take it. The compressed copy is made once per file and
// kept; a file that changes on disk (RUNTIME_WEB_DIR) gets a new copy. Files under 1 KB, Range requests and types that
// are already compressed go out as they are.

const gzipMinBytes = 1024

// compressible are the extensions worth compressing; images, fonts and archives are left alone.
var compressible = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".html": true, ".json": true, ".svg": true,
	".wasm": true, ".map": true, ".txt": true, ".xml": true, ".webmanifest": true,
}

// staticFile is what is known about one file of the page: its tag and, once asked for, its gzipped bytes.
type staticFile struct {
	size int64
	mod  time.Time
	load sync.Once
	etag string // strong tag of the plain bytes, quoted
	err  error
	gz   []byte // nil: not worth compressing
}

type staticCache struct {
	mu sync.Mutex
	m  map[string]*staticFile
}

// get returns the entry for name, a fresh one when the file's size or time differ from the cached one.
func (c *staticCache) get(name string, fi fs.FileInfo) *staticFile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f := c.m[name]; f != nil && f.size == fi.Size() && f.mod.Equal(fi.ModTime()) {
		return f
	}
	if c.m == nil {
		c.m = map[string]*staticFile{}
	}
	f := &staticFile{size: fi.Size(), mod: fi.ModTime()}
	c.m[name] = f
	return f
}

// forget drops an entry whose file could not be read, so the next request tries again.
func (c *staticCache) forget(name string, f *staticFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m[name] == f {
		delete(c.m, name)
	}
}

// prepare reads the file once to tag it and, for compressible types of a useful size, to compress it. It returns the
// plain bytes it read.
func (f *staticFile) prepare(files fs.FS, name string) []byte {
	var raw []byte
	f.load.Do(func() {
		raw, f.err = fs.ReadFile(files, name)
		if f.err != nil {
			return
		}
		sum := sha256.Sum256(raw)
		f.etag = `"` + hex.EncodeToString(sum[:12]) + `"`
		if compressible[path.Ext(name)] && len(raw) >= gzipMinBytes {
			if z := gzipBytes(raw); len(z) < len(raw) {
				f.gz = z
			}
		}
	})
	return raw
}

func gzipBytes(raw []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, 6)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return buf.Bytes()
}

// acceptsGzip is true when Accept-Encoding lists gzip with a quality above zero.
func acceptsGzip(r *http.Request) bool {
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

// serveStatic answers for one file of the page. It reports false when the file cannot be read, so the caller can fall
// through to the page's own routes.
func (c *webConfig) serveStatic(w http.ResponseWriter, r *http.Request, name string) bool {
	fi, err := fs.Stat(c.files, name)
	if err != nil || fi.IsDir() {
		return false
	}
	f := c.static.get(name, fi)
	raw := f.prepare(c.files, name)
	if f.err != nil {
		c.static.forget(name, f)
		return false
	}
	ext := path.Ext(name)
	h := w.Header()
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		if raw == nil {
			if raw, err = fs.ReadFile(c.files, name); err != nil {
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
	if compressible[ext] && f.size >= gzipMinBytes {
		h.Set("Vary", "Accept-Encoding")
	}
	body, tag := raw, f.etag
	if f.gz != nil && r.Header.Get("Range") == "" && acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		body, tag = f.gz, strings.TrimSuffix(f.etag, `"`)+`-gz"`
		// ServeContent sets the length itself only for plain bodies.
		h.Set("Content-Length", strconv.Itoa(len(body)))
	} else if body == nil {
		// The cache had the file already; the plain bytes are read again rather than kept.
		if body, err = fs.ReadFile(c.files, name); err != nil {
			return false
		}
	}
	h.Set("ETag", tag)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	return true
}

// serveHTML sends the rendered page, gzipped when the browser takes it. It stays uncached (no-cache, no tag).
func serveHTML(w http.ResponseWriter, r *http.Request, raw []byte) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	if len(raw) >= gzipMinBytes {
		h.Set("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			if z := gzipBytes(raw); len(z) < len(raw) {
				raw = z
				h.Set("Content-Encoding", "gzip")
			}
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(raw)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(raw)
	}
}
