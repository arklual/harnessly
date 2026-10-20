package gate

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
)

// A request to /api/cli/ without the cli-token is refused with a bare {"error":"Forbidden"}, which tells a person who
// signed in on the page, and sends their cookie, nothing: the CLI routes take the token only. The standalone gate
// replaces that body with one that names the header; the status and the other headers stay.

const bareForbidden = `{"error":"Forbidden"}`

// cliHintBody is the 403 body of a CLI route, naming the header the token goes in.
func cliHintBody(header string) []byte {
	msg := "Forbidden: /api/cli/ routes need the runtime's cli-token in the " + header +
		" request header (the cli-token file in the runtime's data folder, ~/.harnessly by default). " +
		"A page session cookie is not accepted on them."
	return []byte(`{"error":` + strconv.Quote(msg) + "}\n")
}

func isCLIPath(p string) bool {
	return strings.HasPrefix(strings.TrimPrefix(p, "/agents"), "/api/cli/")
}

// hintWriter holds back a 403 until its body is known; any other answer passes through untouched.
type hintWriter struct {
	http.ResponseWriter
	header   string
	holding  bool
	buf      bytes.Buffer
	released bool
}

func (w *hintWriter) WriteHeader(status int) {
	if status == http.StatusForbidden && !w.released {
		w.holding = true
		return
	}
	w.released = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *hintWriter) Write(b []byte) (int, error) {
	if w.holding {
		return w.buf.Write(b)
	}
	w.released = true
	return w.ResponseWriter.Write(b)
}

func (w *hintWriter) Flush() {
	if w.holding {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection.
func (w *hintWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// finish sends what was held back.
func (w *hintWriter) finish() {
	if !w.holding {
		return
	}
	w.holding = false
	w.released = true
	if strings.TrimSpace(w.buf.String()) == bareForbidden {
		h := w.Header()
		h.Del("Etag")
		h.Set("Content-Length", strconv.Itoa(len(cliHintBody(w.header))))
		w.ResponseWriter.WriteHeader(http.StatusForbidden)
		_, _ = w.ResponseWriter.Write(cliHintBody(w.header))
		return
	}
	w.ResponseWriter.WriteHeader(http.StatusForbidden)
	_, _ = w.ResponseWriter.Write(w.buf.Bytes())
}

// withCLIHint answers a standalone gate's CLI routes through a hintWriter.
func (f *Gate) withCLIHint(w http.ResponseWriter, r *http.Request, serve func(http.ResponseWriter, *http.Request)) {
	if !f.cfg.Standalone() || !isCLIPath(r.URL.Path) || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		serve(w, r)
		return
	}
	header := "x-cli-token"
	if len(f.cfg.TokenHeaders) > 0 {
		header = f.cfg.TokenHeaders[0]
	}
	hw := &hintWriter{ResponseWriter: w, header: header}
	defer hw.finish()
	serve(hw, r)
}
