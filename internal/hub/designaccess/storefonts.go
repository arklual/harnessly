package designaccess

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"path"
	"regexp"
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
	out, pass, err := buildSheet(r, segs, read)
	if err != nil {
		return err
	}
	if pass != nil {
		copyAnswer(w, r, pass)
		return nil
	}
	out.write(w, r)
	return nil
}

// buildSheet works out the sheet with its fonts inside; pass is the viewer's own answer when it is not a sheet to work
// on (missing, refused, packed some way the hub does not unpack), to go out as it came.
func buildSheet(r *http.Request, segs []string, read func(w http.ResponseWriter, r *http.Request, segs []string) error) (*sheetOut, *bufferedAnswer, error) {
	plain := plainRead(r)
	sheet := &bufferedAnswer{header: http.Header{}}
	if err := read(sheet, plain, segs); err != nil {
		return nil, nil, err
	}
	raw, ok := plainBody(sheet, inlineSheetMax)
	if sheet.status != http.StatusOK || !ok {
		return nil, sheet, nil
	}
	dir := path.Dir(path.Join(segs...))
	budget := inlineSheetMax
	fonts := map[string]string{}
	body := cssURL.ReplaceAllFunc(raw, func(m []byte) []byte {
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
		if err := read(font, plain, fontSegs); err != nil || font.status != http.StatusOK {
			return m
		}
		fb, ok := plainBody(font, inlineFontMax)
		if !ok || len(fb) > inlineFontMax || len(fb) > budget {
			return m
		}
		budget -= len(fb)
		data := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(fb)
		fonts[at] = data
		return []byte(`url("` + data + `")`)
	})
	return newSheetOut(sheet.header, body), nil, nil
}

// plainBody is the answer's body as the file's own bytes. The viewer gzips a file for a request that names no encoding
// (design.takesGzip), and plainRead names none: a sheet rewritten from its packed bytes went out as gzip without
// Content-Encoding, and the browser dropped every style of the board. False when the body is packed some
// other way, is not gzip after all, or unpacks past max.
func plainBody(a *bufferedAnswer, max int) ([]byte, bool) {
	switch enc := strings.ToLower(strings.TrimSpace(a.header.Get("Content-Encoding"))); enc {
	case "", "identity":
		return a.body.Bytes(), true
	case "gzip":
	default:
		return nil, false
	}
	zr, err := gzip.NewReader(bytes.NewReader(a.body.Bytes()))
	if err != nil {
		return nil, false
	}
	out, err := io.ReadAll(io.LimitReader(zr, int64(max)+1))
	if err != nil || len(out) > max {
		return nil, false
	}
	return out, true
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
