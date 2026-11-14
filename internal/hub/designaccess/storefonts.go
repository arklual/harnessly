package designaccess

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// A board shown from the hub's store runs sandboxed on an opaque origin. The fonts its styles name are fetched in CORS
// mode with credentials «same-origin», which an opaque origin is never: they go without any cookie, so the proxy in
// front of the hub (Teleport) answers them with its sign-in instead of the font, and the board falls back to another
// face. A stylesheet of the store therefore carries its fonts in itself: each url() to a font beside it is
// a data: address of that font, read from the same revision. A font or a sheet over the limits stays a link.
const (
	inlineFontMax  = 1 << 20 // one font
	inlineSheetMax = 8 << 20 // all the fonts of one stylesheet
)

var (
	cssURL   = regexp.MustCompile(`url\(\s*(['"]?)([^'")\s]+)(['"]?)\s*\)`)
	fontMIME = map[string]string{".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf"}
)

// bufferedAnswer holds an answer of the viewer to read it before it goes anywhere.
type bufferedAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedAnswer) Header() http.Header { return b.header }
func (b *bufferedAnswer) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}
func (b *bufferedAnswer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// plainRead is r as a read of the whole file: no conditional answer, no ranges, nothing compressed.
func plainRead(r *http.Request) *http.Request {
	c := r.Clone(r.Context())
	c.Method = http.MethodGet
	for _, h := range []string{"Accept-Encoding", "If-None-Match", "If-Modified-Since", "Range", "If-Range"} {
		c.Header.Del(h)
	}
	return c
}

// isStylesheet: the file of segs is a stylesheet, which is where fonts are named.
func isStylesheet(segs []string) bool {
	return len(segs) > 0 && strings.EqualFold(path.Ext(segs[len(segs)-1]), ".css")
}

// serveSheet answers a stylesheet of the store with its fonts inside; read is the viewer's answer for a file of the
// same revision.
func serveSheet(w http.ResponseWriter, r *http.Request, segs []string, read func(w http.ResponseWriter, r *http.Request, segs []string) error) error {
	plain := plainRead(r)
	sheet := &bufferedAnswer{header: http.Header{}}
	if err := read(sheet, plain, segs); err != nil {
		return err
	}
	if sheet.status != http.StatusOK {
		copyAnswer(w, r, sheet)
		return nil
	}
	dir := path.Dir(path.Join(segs...))
	budget := inlineSheetMax
	fonts := map[string]string{}
	body := cssURL.ReplaceAllFunc(sheet.body.Bytes(), func(m []byte) []byte {
		parts := cssURL.FindSubmatch(m)
		if string(parts[1]) != string(parts[3]) {
			return m
		}
		ref := string(parts[2])
		file, _, _ := strings.Cut(ref, "#")
		file, _, _ = strings.Cut(file, "?")
		mime := fontMIME[strings.ToLower(path.Ext(file))]
		if mime == "" || strings.Contains(file, ":") || strings.HasPrefix(file, "/") || strings.HasPrefix(file, "//") {
			return m
		}
		at := path.Join(dir, file)
		if at == ".." || strings.HasPrefix(at, "../") {
			return m
		}
		if data, ok := fonts[at]; ok {
			return []byte(`url("` + data + `")`)
		}
		fontSegs := strings.Split(at, "/")
		if !servedSegment(fontSegs[0]) {
			return m
		}
		font := &bufferedAnswer{header: http.Header{}}
		if err := read(font, plain, fontSegs); err != nil || font.status != http.StatusOK || font.body.Len() > inlineFontMax || font.body.Len() > budget {
			return m
		}
		budget -= font.body.Len()
		data := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(font.body.Bytes())
		fonts[at] = data
		return []byte(`url("` + data + `")`)
	})
	h := w.Header()
	for _, k := range []string{"Content-Type", "Cache-Control", "Last-Modified"} {
		if v := sheet.header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
	return nil
}

func copyAnswer(w http.ResponseWriter, r *http.Request, a *bufferedAnswer) {
	for k, v := range a.header {
		w.Header()[k] = v
	}
	w.WriteHeader(a.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(a.body.Bytes())
	}
}
