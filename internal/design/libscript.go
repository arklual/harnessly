package design

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// The board script (client.js) is in every board and every slide of a deck, ahead of the board's own scripts. It is
// addressed by what it holds (/__lib/client.js?v=<hash>), so a browser keeps it for good and a new runtime's script
// is a new address; it travels gzipped and answers a known ETag with 304.

// libScriptParam is the version in the script's address. Not versionParam: a page's own assets carry that one.
const libScriptParam = "v"

type libScript struct {
	version string
	gz      []byte
}

var libScripts sync.Map // the script's text -> *libScript

func libScriptOf(body string) *libScript {
	if v, ok := libScripts.Load(body); ok {
		return v.(*libScript)
	}
	sum := sha256.Sum256([]byte(body))
	s := &libScript{version: hex.EncodeToString(sum[:8])}
	var buf bytes.Buffer
	if zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression); err == nil {
		if _, err := zw.Write([]byte(body)); err == nil && zw.Close() == nil {
			s.gz = buf.Bytes()
		}
	}
	libScripts.Store(body, s)
	return s
}

// clientSrc is the board script's address with its version.
func (l *Library) clientSrc() string {
	return scriptPath + "?" + libScriptParam + "=" + libScriptOf(l.clientScript()).version
}

// serveLibScript sends a script of the library: kept for good when the address names this version (keep), else under
// cacheControl with the version as the ETag.
func serveLibScript(w http.ResponseWriter, r *http.Request, body, keep, cacheControl string, head bool) {
	s := libScriptOf(body)
	etag := `"` + s.version + `"`
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("ETag", etag)
	h.Set("Vary", "Accept-Encoding")
	if keep != "" && r.URL.Query().Get(libScriptParam) == s.version {
		cacheControl = keep
	}
	h.Set("Cache-Control", cacheControl)
	for _, tag := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if t := strings.TrimPrefix(strings.TrimSpace(tag), "W/"); t == etag || t == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	out := []byte(body)
	if s.gz != nil && gzipWord.MatchString(r.Header.Get("Accept-Encoding")) {
		out = s.gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	if !head {
		_, _ = w.Write(out)
	}
}
