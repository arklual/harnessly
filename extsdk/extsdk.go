// Package extsdk is the skeleton of an adapter of the hub's extension protocol (docs/hub/extensions.md): a separate
// process that the hub calls over HTTP. It answers what every adapter must answer the same way, and leaves the calls of
// its own kind to the author:
//
//   - GET /v1/describe: who the adapter is, the protocol it speaks (harnessly.<kind>/v<major>) and its capabilities;
//   - the bearer token, checked on every call (constant time), when one is set;
//   - the Idempotency-Key of a call that changes something: a repeat with the same key gets the first answer again and
//     the work is not done twice (the hub resends a call after a timeout or a restart);
//   - the error body {"error": "<code>", "message": "<text>"} (WriteError); 501 or `not_supported` means «this call is
//     not available».
//
// It imports nothing of the hub; an adapter is built and released on its own.
//
//	s := extsdk.New(extsdk.Description{Protocol: "harnessly.machine-provider/v1", Name: "acme", Version: "1.0.0",
//		Capabilities: []string{"state"}}, extsdk.Options{Token: os.Getenv("ADAPTER_TOKEN")})
//	s.Handle("GET /v1/machines/{id}", func(w http.ResponseWriter, r *http.Request) { ... })
//	s.Handle("POST /v1/machines", s.Idempotent(create))
//	log.Fatal(http.ListenAndServe(":8080", s))
package extsdk

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Description is the answer of GET /v1/describe.
type Description struct {
	Protocol     string         `json:"protocol"`
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Capabilities []string       `json:"capabilities"`
	ConfigSchema map[string]any `json:"config_schema,omitempty"`
}

// Options are the adapter's settings.
type Options struct {
	// Token is the bearer token the hub calls with; empty means no check (only for a trusted network).
	Token string
	// MaxReplays is how many answers of mutating calls are kept to be replayed; zero means 1024. The oldest go first.
	MaxReplays int
}

// Server is the adapter's HTTP handler.
type Server struct {
	desc Description
	opt  Options
	mux  *http.ServeMux

	mu      sync.Mutex
	replays map[string]*replay
	order   []string
}

// New makes a server that already answers describe.
func New(d Description, o Options) *Server {
	if o.MaxReplays <= 0 {
		o.MaxReplays = 1024
	}
	if d.Capabilities == nil {
		d.Capabilities = []string{}
	}
	s := &Server{desc: d, opt: o, mux: http.NewServeMux(), replays: map[string]*replay{}}
	s.mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, s.desc) })
	return s
}

// Handle registers a call of the adapter's kind, with a pattern as http.ServeMux takes it («POST /v1/machines»).
func (s *Server) Handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// ServeHTTP checks the token and routes the call.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.opt.Token != "" {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opt.Token)) != 1 {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "the token is wrong or missing")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

// WriteJSON answers with a JSON body.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError answers with the error body of the protocol.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]string{"error": code, "message": message})
}

// NotSupported answers a call the adapter does not do.
func NotSupported(w http.ResponseWriter, r *http.Request) {
	WriteError(w, http.StatusNotImplemented, "not_supported", r.Method+" "+r.URL.Path+" is not available")
}

type replay struct {
	status int
	header http.Header
	body   []byte
}

// Idempotent wraps the handler of a call that changes something. A request with an Idempotency-Key that was answered
// before gets that answer again and the handler does not run; a request without a key is refused, because the hub always
// sends one and an adapter that works without it would do the work twice after a retry. The answer is kept only when it
// is not a 5xx: a failure may be tried again. (Two requests with the same key at the same moment are not serialized
// here: an adapter with a real side effect keeps the key with the work, in its own store.)
func (s *Server) Idempotent(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			WriteError(w, http.StatusBadRequest, "idempotency_key_required", "a call that changes something carries an Idempotency-Key")
			return
		}
		id := r.Method + " " + r.URL.Path + " " + key
		if rp := s.replayOf(id); rp != nil {
			for k, v := range rp.header {
				w.Header()[k] = v
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(rp.status)
			_, _ = w.Write(rp.body)
			return
		}
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, r)
		if rec.status < 500 {
			s.keep(id, &replay{status: rec.status, header: rec.Header().Clone(), body: rec.buf.Bytes()})
		}
	}
}

func (s *Server) replayOf(id string) *replay {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replays[id]
}

func (s *Server) keep(id string, rp *replay) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.replays[id]; !ok {
		s.order = append(s.order, id)
		for len(s.order) > s.opt.MaxReplays {
			delete(s.replays, s.order[0])
			s.order = s.order[1:]
		}
	}
	s.replays[id] = rp
}

type recorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *recorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *recorder) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}
