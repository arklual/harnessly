package design

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// The motion kit of a video's board: GSAP and its plugins, and the deck's fonts, served by the library itself,
// so a scene loads no third-party address and draws the same on a machine with nothing installed. Only what a board asks
// for is loaded; a board that asks for nothing costs nothing.
const motionKitPrefix = "/__lib/motion/"

//go:embed vendor/motion/*.js vendor/motion/*.css vendor/motion/fonts/*.woff2 vendor/motion/fonts/*.txt vendor/motion/art/*.js
var motionKitFS embed.FS

type motionKitFile struct {
	raw, gz []byte
	etag    string
	typ     string
}

var (
	motionKitOnce  sync.Once
	motionKitFiles map[string]*motionKitFile
)

func motionKit() map[string]*motionKitFile {
	motionKitOnce.Do(func() {
		motionKitFiles = map[string]*motionKitFile{}
		err := fs.WalkDir(motionKitFS, "vendor/motion", func(file string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			raw, err := motionKitFS.ReadFile(file)
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, 9)
			_, _ = zw.Write(raw)
			_ = zw.Close()
			sum := sha256.Sum256(raw)
			motionKitFiles[strings.TrimPrefix(file, "vendor/motion/")] = &motionKitFile{raw: raw, gz: buf.Bytes(),
				etag: `"` + hex.EncodeToString(sum[:16]) + `"`, typ: motionKitType(file)}
			return nil
		})
		if err != nil {
			panic("design: the motion kit is missing from the build: " + err.Error())
		}
	})
	return motionKitFiles
}

func motionKitType(file string) string {
	switch path.Ext(file) {
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "text/javascript; charset=utf-8"
}

// serveMotionKit answers /__lib/motion/<file>; the address has no version, so the browser asks again after five minutes.
func (l *Library) serveMotionKit(w http.ResponseWriter, r *http.Request, pathname string, head bool) {
	f := motionKit()[strings.TrimPrefix(pathname, motionKitPrefix)]
	if f == nil {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	h := w.Header()
	h.Set("ETag", f.etag)
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", f.typ)
	h.Add("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == f.etag {
		w.WriteHeader(304)
		return
	}
	body := f.raw
	if takesGzip(r) && f.typ != "font/woff2" {
		body = f.gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(200)
	if !head {
		_, _ = w.Write(body)
	}
}
