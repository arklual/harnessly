package preview

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

// Content codings the proxy can read and write; the rest of a response is never touched.

var (
	codings        = []string{"br", "gzip", "deflate"}
	compressibleRe = regexp.MustCompile(`(?i)^(text/|application/(javascript|json|xml|manifest\+json|importmap\+json|wasm|x-javascript)|[a-z]+/[a-z.+-]*\+(json|xml)|image/svg\+xml|font/(ttf|otf)|application/(x-font-ttf|vnd\.ms-fontobject))`)
	eventStreamRe  = regexp.MustCompile(`(?i)^text/event-stream\b`)
	qParamRe       = regexp.MustCompile(`^\s*q\s*=\s*([\d.]+)\s*$`)
)

// minCompressBytes: below this a compressed body saves less than its framing costs.
const minCompressBytes = 1024

// acceptedCodings are the codings a client accepts, best first.
func acceptedCodings(header string) []string {
	weights := map[string]float64{}
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.ToLower(strings.TrimSpace(part)), ";")
		name := fields[0]
		if name == "" {
			continue
		}
		q := 1.0
		for _, p := range fields[1:] {
			if m := qParamRe.FindStringSubmatch(p); m != nil {
				if n, err := strconv.ParseFloat(m[1], 64); err == nil && strings.Count(m[1], ".") <= 1 && m[1] != "." {
					q = n
				} else {
					q = math.NaN()
				}
				break
			}
		}
		weights[strings.TrimSpace(name)] = q
	}
	weightOf := func(c string) float64 {
		if w, ok := weights[c]; ok {
			return w
		}
		if w, ok := weights["*"]; ok {
			return w
		}
		return 0
	}
	var out []string
	for _, c := range codings {
		if weightOf(c) > 0 {
			out = append(out, c)
		}
	}
	index := func(c string) int {
		for i, x := range codings {
			if x == c {
				return i
			}
		}
		return 0
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := weightOf(out[i]), weightOf(out[j])
		if a != b {
			return a > b
		}
		return index(out[i]) < index(out[j])
	})
	return out
}

// upstreamAcceptEncoding asks the upstream for exactly what the browser accepts, so a pass-through body is always
// readable by it.
func upstreamAcceptEncoding(header string) string {
	if c := acceptedCodings(header); len(c) > 0 {
		return strings.Join(c, ", ")
	}
	return "identity"
}

// codingOf is a response's coding: "identity", one the proxy reads, or "" for another.
func codingOf(h Headers) string {
	v := strings.ToLower(strings.TrimSpace(h.Get("content-encoding")))
	if v == "" || v == "identity" {
		return "identity"
	}
	for _, c := range codings {
		if c == v {
			return c
		}
	}
	return ""
}

func isCompressible(contentType string) bool {
	return compressibleRe.MatchString(contentType) && !eventStreamRe.MatchString(contentType)
}

// outgoingCoding is br when the browser takes it, else gzip; deflate is only ever read.
func outgoingCoding(header string) string {
	for _, c := range acceptedCodings(header) {
		if c == "br" || c == "gzip" {
			return c
		}
	}
	return ""
}

func decoderFor(coding string, r io.Reader) (io.Reader, error) {
	switch coding {
	case "br":
		return brotli.NewReader(r), nil
	case "gzip":
		return gzip.NewReader(r)
	}
	return zlib.NewReader(r)
}

// flushingWriter flushes the encoder after every write: a streamed JSON or HTML response must not wait for the
// encoder's block to fill.
type flushingWriter struct {
	enc interface {
		io.WriteCloser
		Flush() error
	}
}

func (f flushingWriter) Write(b []byte) (int, error) {
	n, err := f.enc.Write(b)
	if err != nil {
		return n, err
	}
	return n, f.enc.Flush()
}

func (f flushingWriter) Close() error { return f.enc.Close() }

// A design's runtime bundle is several megabytes of script: brotli at 4 spends over a second of the page's first load
// on it (it goes out of the proxy a second time on every open), at 2 a third of that for a tenth more bytes. Pages and
// data are small and keep the better level.
const (
	brotliPage   = 4
	brotliBundle = 2
)

var bundleRe = regexp.MustCompile(`(?i)^(text/(css|javascript)|application/(javascript|x-javascript|wasm))\b`)

func encoderFor(coding string, w io.Writer, contentType string) io.WriteCloser {
	if coding == "br" {
		level := brotliPage
		if bundleRe.MatchString(contentType) {
			level = brotliBundle
		}
		return flushingWriter{brotli.NewWriterLevel(w, level)}
	}
	gz, _ := gzip.NewWriterLevel(w, 5)
	return flushingWriter{gz}
}

func withVary(existing, name string) string {
	var current []string
	for _, p := range strings.Split(existing, ",") {
		if p = strings.TrimSpace(p); p != "" {
			current = append(current, p)
		}
	}
	for _, p := range current {
		if p == "*" || strings.EqualFold(p, name) {
			return strings.Join(current, ", ")
		}
	}
	return strings.Join(append(current, name), ", ")
}

// describeBody: the body is not the upstream's byte for byte, so length and coding are ours, and a body whose coding
// changed is another representation, so its strong validator becomes weak.
func describeBody(h Headers, coding string, recoded bool) {
	h.Del("content-length")
	if coding != "" {
		h.Set("content-encoding", coding)
	} else {
		h.Del("content-encoding")
	}
	h.Set("vary", withVary(h.Get("vary"), "Accept-Encoding"))
	if recoded && h.Has("etag") {
		if e := h.Get("etag"); !strings.HasPrefix(e, "W/") {
			h.Set("etag", "W/"+e)
		}
	}
}
