package gate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// recorder writes every request through the gate as one JSON line, for replaying a machine's real traffic into two
// runtimes and comparing them. Credentials are never written: the headers carrying them say only that they were there.
type recorder struct {
	mu sync.Mutex
	f  *os.File
}

const recordBodyMax = 1 << 20

var secretHeaders = map[string]bool{"Cookie": true, "Authorization": true, "Set-Cookie": true}

func newRecorder(path string) (*recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &recorder{f: f}, nil
}

type recordedBody struct {
	Text   *string `json:"text,omitempty"`
	Base64 *string `json:"base64,omitempty"`
	Cut    bool    `json:"cut,omitempty"`
}

func bodyOf(b []byte, cut bool) *recordedBody {
	if len(b) == 0 && !cut {
		return nil
	}
	rb := &recordedBody{Cut: cut}
	if utf8.Valid(b) {
		s := string(b)
		rb.Text = &s
	} else {
		s := base64.StdEncoding.EncodeToString(b)
		rb.Base64 = &s
	}
	return rb
}

func redact(h http.Header, tokenHeaders map[string]bool) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		if secretHeaders[k] || tokenHeaders[strings.ToLower(k)] || strings.Contains(strings.ToLower(k), "token") {
			out[k] = []string{"<present>"}
			continue
		}
		out[k] = v
	}
	return out
}

type recordEntry struct {
	TMs      int64               `json:"t_ms"`
	Ms       float64             `json:"ms"`
	Method   string              `json:"method"`
	Path     string              `json:"path"`
	Query    string              `json:"query,omitempty"`
	Headers  map[string][]string `json:"headers"`
	Body     *recordedBody       `json:"body,omitempty"`
	Upgrade  bool                `json:"upgrade,omitempty"`
	Status   int                 `json:"status"`
	RHeaders map[string][]string `json:"respHeaders,omitempty"`
	Resp     *recordedBody       `json:"resp,omitempty"`
	ByGo     bool                `json:"byGo,omitempty"`
}

// teeResponse keeps the status and the first bytes of an answer.
type teeResponse struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	cut    bool
}

func (t *teeResponse) WriteHeader(code int) {
	if t.status == 0 {
		t.status = code
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *teeResponse) Write(b []byte) (int, error) {
	if t.status == 0 {
		t.status = http.StatusOK
	}
	if room := recordBodyMax - t.body.Len(); room > 0 {
		if len(b) > room {
			t.body.Write(b[:room])
			t.cut = true
		} else {
			t.body.Write(b)
		}
	} else if len(b) > 0 {
		t.cut = true
	}
	return t.ResponseWriter.Write(b)
}

func (t *teeResponse) Flush() {
	if fl, ok := t.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// Unwrap lets the proxy reach the hijacker under it (an upgraded request).
func (t *teeResponse) Unwrap() http.ResponseWriter { return t.ResponseWriter }

func (rec *recorder) wrap(w http.ResponseWriter, r *http.Request, tokenHeaders map[string]bool, serve func(http.ResponseWriter, *http.Request) bool) {
	start := time.Now()
	e := recordEntry{TMs: start.UnixMilli(), Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Headers: redact(r.Header, tokenHeaders),
		Upgrade: strings.EqualFold(r.Header.Get("Upgrade"), "websocket")}
	if r.Body != nil && !e.Upgrade {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, recordBodyMax+1))
		cut := len(raw) > recordBodyMax
		if cut {
			raw = raw[:recordBodyMax]
		}
		e.Body = bodyOf(raw, cut)
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	}
	if e.Upgrade {
		e.ByGo = serve(w, r)
		e.Ms = float64(time.Since(start).Microseconds()) / 1000
		rec.write(e)
		return
	}
	tee := &teeResponse{ResponseWriter: w}
	e.ByGo = serve(tee, r)
	e.Ms = float64(time.Since(start).Microseconds()) / 1000
	e.Status = tee.status
	e.RHeaders = redact(tee.Header(), tokenHeaders)
	e.Resp = bodyOf(tee.body.Bytes(), tee.cut)
	rec.write(e)
}

func (rec *recorder) write(e recordEntry) {
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	_, _ = rec.f.Write(append(raw, '\n'))
}
