package gate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"

	"harnessly/internal/agent"
)

// BodyRoute is a read-only route whose answer is not only JSON — a file, a picture, a range of a video. Like a
// JSONRoute it is answered while its slice is on and shadowed while "<slice>-shadow" is: the old runtime answers, the
// Go handler writes into a recorder, and status, the headers a caller acts on and the body bytes are compared.
type BodyRoute struct {
	Slice string
	Match func(*http.Request) bool
	// Serve writes the whole answer, a refusal included. It must only read: in the shadow it runs a second time.
	Serve http.HandlerFunc
}

// comparedHeaders are the headers whose values must be the same in both answers.
var comparedHeaders = []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition",
	"Content-Security-Policy", "X-Content-Type-Options", "Cache-Control", "ETag", "Allow", "Location"}

// AsBodyRoutes turns body routes into gate routes: answered while the slice is on, shadowed while "<slice>-shadow" is.
func (f *Gate) AsBodyRoutes(routes ...BodyRoute) []Route {
	out := make([]Route, 0, 2*len(routes))
	for _, br := range routes {
		out = append(out, Route{Slice: br.Slice, Match: br.Match, Handler: br.Serve}, Route{
			Slice: br.Slice + "-shadow",
			Match: func(r *http.Request) bool { return r.Method == http.MethodGet && br.Match(r) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				f.shadowBody(w, r, br)
			}),
		})
	}
	return out
}

// bodyDigest is what is kept of an answer to compare it: status, headers, length, hash and the first bytes.
type bodyDigest struct {
	status int
	header http.Header
	n      int64
	sum    hash.Hash
	head   bytes.Buffer
}

func newDigest() *bodyDigest { return &bodyDigest{sum: sha256.New()} }

func (d *bodyDigest) write(b []byte) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	d.n += int64(len(b))
	d.sum.Write(b)
	if room := 600 - d.head.Len(); room > 0 {
		d.head.Write(b[:min(room, len(b))])
	}
}

// headerValue is a compared header as the caller sees it: an empty answer without a length is sent with length 0.
func (d *bodyDigest) headerValue(name string) string {
	v := strings.Join(d.header.Values(name), ", ")
	if name == "Content-Length" && v == "" && d.n == 0 && d.status != http.StatusNotModified && d.status != http.StatusNoContent {
		return "0"
	}
	return v
}

// bodyTee passes the old runtime's answer through and digests it.
type bodyTee struct {
	http.ResponseWriter
	d *bodyDigest
}

func (t *bodyTee) WriteHeader(code int) {
	if t.d.status == 0 {
		t.d.status = code
		t.d.header = t.ResponseWriter.Header().Clone()
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *bodyTee) Write(b []byte) (int, error) {
	if t.d.status == 0 {
		t.WriteHeader(http.StatusOK)
	}
	t.d.write(b)
	return t.ResponseWriter.Write(b)
}

func (t *bodyTee) Flush() {
	if fl, ok := t.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// bodyRecorder takes the Go answer in the shadow.
type bodyRecorder struct {
	h http.Header
	d *bodyDigest
}

func (r *bodyRecorder) Header() http.Header { return r.h }

func (r *bodyRecorder) WriteHeader(code int) {
	if r.d.status == 0 {
		r.d.status = code
		r.d.header = r.h.Clone()
	}
}

func (r *bodyRecorder) Write(b []byte) (int, error) {
	if r.d.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	r.d.write(b)
	return len(b), nil
}

func (f *Gate) shadowBody(w http.ResponseWriter, r *http.Request, br BodyRoute) {
	old := newDigest()
	start := time.Now()
	f.proxy.ServeHTTP(&bodyTee{ResponseWriter: w, d: old}, r)
	f.count(r.URL.Path, time.Since(start), false)
	if old.status == 0 {
		return // the old runtime dropped the connection
	}
	r, cancel := detached(r)
	go func() {
		defer cancel()
		rec := &bodyRecorder{h: http.Header{}, d: newDigest()}
		func() {
			defer func() {
				if v := recover(); v != nil && v != http.ErrAbortHandler {
					agent.Log("warn", "shadow handler panicked", agent.F{K: "path", V: r.URL.RequestURI()}, agent.F{K: "panic", V: fmt.Sprint(v)})
				}
			}()
			br.Serve(rec, r)
		}()
		if rec.d.status == 0 {
			rec.d.status = http.StatusOK
			rec.d.header = rec.h.Clone()
		}
		why := sameBody(old, rec.d)
		f.shadows.note(br.Slice, why == "")
		if why != "" {
			agent.Log("warn", "shadow answer differs", agent.F{K: "slice", V: br.Slice}, agent.F{K: "path", V: r.URL.RequestURI()},
				agent.F{K: "why", V: why}, agent.F{K: "old", V: describe(old)}, agent.F{K: "go", V: describe(rec.d)})
		}
	}()
}

func sameBody(a, b *bodyDigest) string {
	if a.status != b.status {
		return fmt.Sprintf("status %d vs %d", a.status, b.status)
	}
	var diffs []string
	for _, name := range comparedHeaders {
		x, y := a.headerValue(name), b.headerValue(name)
		if name == "Content-Length" && (x == "" || y == "") {
			continue // one side streamed without a length: framing, the body length below still counts
		}
		if x != y {
			diffs = append(diffs, fmt.Sprintf("%s %q vs %q", name, x, y))
		}
	}
	if a.n != b.n || !bytes.Equal(a.sum.Sum(nil), b.sum.Sum(nil)) {
		diffs = append(diffs, "body "+strconv.FormatInt(a.n, 10)+" vs "+strconv.FormatInt(b.n, 10)+" bytes")
	}
	return strings.Join(diffs, "; ")
}

func describe(d *bodyDigest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d", d.status)
	for _, name := range comparedHeaders {
		if v := d.headerValue(name); v != "" {
			fmt.Fprintf(&sb, " %s=%q", name, v)
		}
	}
	fmt.Fprintf(&sb, " %d bytes: %s", d.n, clip(d.head.Bytes()))
	return sb.String()
}
