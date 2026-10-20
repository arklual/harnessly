// Package gate is the runtime gate: the process on the runtime's port. It answers what the Go runtime already does — slice by slice,
// each switched per machine in state-mode.json — and hands everything else, sockets included, to the old runtime on an
// inner port. Callers (the cluster through the machine agent's tunnels, the agent's relays, the CLIs) see one port.
package gate

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Config is what the gate takes from its environment.
type Config struct {
	// Listen is the runtime's port, the one every caller knows.
	Listen string
	// Upstream is the old runtime on its inner port; empty runs the gate standalone (see Standalone).
	Upstream string
	// DataDir holds state-mode.json, where the machine agent writes which slices are on.
	DataDir string
	Version string
	// CountEvery is how often the paths still handed to the old runtime are logged.
	CountEvery time.Duration
	// RecordFile, when set, gets every request through the gate as a JSON line (credentials left out), for replay.
	RecordFile string
	// TokenHeaders name the headers that carry a token; their values are never recorded.
	TokenHeaders []string
}

// LoadConfig reads the environment.
func LoadConfig() (Config, error) {
	c := Config{
		Listen:     os.Getenv("RUNTIME_GATE_LISTEN"),
		Upstream:   os.Getenv("RUNTIME_GATE_UPSTREAM"),
		DataDir:    os.Getenv("AGENT_DATA_DIR"),
		Version:    os.Getenv("AGENT_VERSION"),
		CountEvery: 10 * time.Minute,
	}
	c.RecordFile = os.Getenv("RUNTIME_GATE_RECORD")
	for _, h := range strings.Split(os.Getenv("RUNTIME_CLI_TOKEN_HEADERS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.TokenHeaders = append(c.TokenHeaders, h)
		}
	}
	if secs, err := strconv.Atoi(os.Getenv("RUNTIME_GATE_COUNT_SECONDS")); err == nil && secs > 0 {
		c.CountEvery = time.Duration(secs) * time.Second
	}
	if c.Listen == "" {
		return c, errors.New("RUNTIME_GATE_LISTEN is required")
	}
	if c.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return c, err
		}
		c.DataDir = filepath.Join(home, ".harnessly")
	}
	if c.Version == "" {
		c.Version = agent.BuildVersion
	}
	return c, nil
}

// Standalone: no old runtime behind the gate. Every slice is on, whatever state-mode.json says (there is nobody else to
// answer), no shadow runs (there is nothing to compare with), and what no route takes gets the fallback's answer.
func (c Config) Standalone() bool { return c.Upstream == "" }

// Slices says which slices are on, re-reading state-mode.json at most once a second.
type Slices struct {
	file string
	// all: every slice is on but the shadows (a standalone gate).
	all bool
	mu  sync.Mutex
	at  time.Time
	on  map[string]bool
}

func NewSlices(dataDir string) *Slices {
	return &Slices{file: filepath.Join(dataDir, "state-mode.json")}
}

func (s *Slices) On(name string) bool {
	if s.all {
		return !strings.HasSuffix(name, "-shadow")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.at) > time.Second {
		s.at = time.Now()
		s.on = map[string]bool{}
		var mode struct {
			GoSlices []string `json:"goSlices"`
		}
		if raw, err := os.ReadFile(s.file); err == nil && json.Unmarshal(raw, &mode) == nil {
			for _, n := range mode.GoSlices {
				s.on[n] = true
			}
		}
	}
	return s.on[name]
}

// Route is one thing a slice answers itself. A route without a slice is always the Go runtime's.
type Route struct {
	Slice   string
	Match   func(*http.Request) bool
	Handler http.Handler
}

// Gate is the handler on the runtime's port.
type Gate struct {
	cfg    Config
	proxy  *httputil.ReverseProxy
	slices *Slices
	routes []Route

	countMu sync.Mutex
	counts  map[string]*pathCount
	shadows shadowStats

	handover *handover
	recorder *recorder
	refresh  *SessionRefresh
	tokens   map[string]bool
	// fallback answers, on a standalone gate, what no route took.
	fallback http.Handler
}

// pathCount is what went to the old runtime on one path: how often and how long it took, the gate's hop included.
type pathCount struct {
	n       int
	totalMs float64
	maxMs   float64
}

func New(cfg Config, routes ...Route) (*Gate, error) {
	target, err := url.Parse("http://" + cfg.Upstream)
	if err != nil {
		return nil, err
	}
	f := &Gate{cfg: cfg, slices: NewSlices(cfg.DataDir), routes: routes, counts: map[string]*pathCount{},
		shadows: shadowStats{matched: map[string]int{}, differ: map[string]int{}}, handover: newHandover(), tokens: map[string]bool{},
		fallback: http.HandlerFunc(notFound)}
	f.slices.all = cfg.Standalone()
	for _, h := range cfg.TokenHeaders {
		f.tokens[strings.ToLower(h)] = true
	}
	if cfg.RecordFile != "" {
		rec, err := newRecorder(cfg.RecordFile)
		if err != nil {
			return nil, err
		}
		f.recorder = rec
		agent.Log("info", "recording every request", agent.F{K: "file", V: cfg.RecordFile})
	}
	f.proxy = &httputil.ReverseProxy{
		// A page opens a few dozen requests at once, all to the same old runtime on loopback: keep enough connections
		// open that none waits for a new one, and pass bodies through as they are (no compression on this hop).
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   256,
			IdleConnTimeout:       90 * time.Second,
			DisableCompression:    true,
			ExpectContinueTimeout: time.Second,
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// The query goes on as the caller wrote it: the proxy would drop the parts Go cannot parse (";", bad
			// escapes), which the old runtime reads.
			r.Out.URL.RawQuery = r.In.URL.RawQuery
			// The old runtime tells preview slots and its own pages apart by the host the caller asked for.
			r.Out.Host = r.In.Host
		},
		// Streams (SSE, long downloads) reach the caller as they are written.
		FlushInterval: -1,
		ErrorHandler:  f.upstreamFailed,
	}
	return f, nil
}

// upstreamFailed: an old runtime that is down must look down. A caller that met a refused connection before (the
// cluster deciding a machine is unreachable) now gets its connection dropped, not an answer from a live gate.
func (f *Gate) upstreamFailed(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, syscall.ECONNREFUSED) || isDial(err) {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, herr := hj.Hijack(); herr == nil {
				_ = conn.Close()
				return
			}
		}
	}
	if r.Context().Err() != nil {
		return // the caller went away
	}
	agent.Log("warn", "old runtime failed a request", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
	w.WriteHeader(http.StatusBadGateway)
}

func isDial(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// notFound is the old runtime's answer to a path it had no route for.
func notFound(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
				_ = conn.Close()
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"Not found"}`))
}

// Standalone says whether the gate runs without an old runtime behind it.
func (f *Gate) Standalone() bool { return f.cfg.Standalone() }

// SetFallback names what answers, on a standalone gate, a request no route took (by default the old runtime's "Not
// found"); call before serving.
func (f *Gate) SetFallback(h http.Handler) { f.fallback = h }

// Add appends routes; call before serving.
func (f *Gate) Add(routes ...Route) { f.routes = append(f.routes, routes...) }

func (f *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.withCLIHint(w, r, func(w http.ResponseWriter, r *http.Request) {
		if f.recorder != nil {
			f.recorder.wrap(w, r, f.tokens, f.serve)
			return
		}
		f.serve(w, r)
	})
}

// serve answers r; true when a Go route did.
func (f *Gate) serve(w http.ResponseWriter, r *http.Request) bool {
	r = canonical(r)
	for _, route := range f.routes {
		if route.Match(r) && (route.Slice == "" || f.slices.On(route.Slice)) {
			// A shadow's answer is the old runtime's, which refreshes the session itself.
			if f.refresh != nil && !strings.HasSuffix(route.Slice, "-shadow") {
				f.refresh.apply(w, r)
			}
			route.Handler.ServeHTTP(w, r)
			return true
		}
	}
	if f.cfg.Standalone() {
		f.fallback.ServeHTTP(w, r)
		f.count(r.URL.Path, 0, true)
		return false
	}
	done := f.trackProxied(r)
	defer done()
	start := time.Now()
	f.proxy.ServeHTTP(w, r)
	// An upgraded socket lasts as long as the page keeps it: only its count says anything.
	f.count(r.URL.Path, time.Since(start), strings.EqualFold(r.Header.Get("Upgrade"), "websocket"))
	return false
}

// pathKey groups paths for the count: the API path without ids.
func pathKey(p string) string {
	p = strings.TrimPrefix(p, "/agents")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if i >= 5 {
			parts = append(parts[:5], "…")
			break
		}
		if len(part) >= 16 || strings.ContainsAny(part, "0123456789") && len(part) > 6 {
			parts[i] = "*"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func (f *Gate) count(path string, took time.Duration, upgrade bool) {
	key := pathKey(path)
	f.countMu.Lock()
	defer f.countMu.Unlock()
	c := f.counts[key]
	if c == nil {
		c = &pathCount{}
		f.counts[key] = c
	}
	c.n++
	if !upgrade {
		ms := float64(took.Microseconds()) / 1000
		c.totalMs += ms
		c.maxMs = max(c.maxMs, ms)
	}
}

// LogCounts writes, and resets, which paths went to the old runtime, how often and how long they took: the day it stays
// empty the old runtime can go, and until then it shows what the hop costs.
func (f *Gate) LogCounts() {
	if matched, differ := f.shadows.take(); len(matched)+len(differ) > 0 {
		agent.Log("info", "shadow answers compared", agent.F{K: "matched", V: matched}, agent.F{K: "differ", V: differ})
	}
	f.countMu.Lock()
	counts := f.counts
	f.counts = map[string]*pathCount{}
	f.countMu.Unlock()
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	total := 0
	for k, c := range counts {
		keys = append(keys, k)
		total += c.n
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]].n > counts[keys[j]].n })
	if len(keys) > 40 {
		keys = keys[:40]
	}
	type entry struct {
		N     int     `json:"n"`
		AvgMs float64 `json:"avgMs,omitempty"`
		MaxMs float64 `json:"maxMs,omitempty"`
	}
	top := make(map[string]entry, len(keys))
	for _, k := range keys {
		c := counts[k]
		e := entry{N: c.n, MaxMs: round1(c.maxMs)}
		if c.totalMs > 0 {
			e.AvgMs = round1(c.totalMs / float64(c.n))
		}
		top[k] = e
	}
	msg := "handed to the old runtime"
	if f.cfg.Standalone() {
		msg = "answered by no route"
	}
	agent.Log("info", msg, agent.F{K: "requests", V: total}, agent.F{K: "paths", V: len(counts)}, agent.F{K: "top", V: top})
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// Proxy hands a request to the old runtime as the gate does for paths it does not answer (to the fallback on a
// standalone gate).
func (f *Gate) Proxy(w http.ResponseWriter, r *http.Request) {
	if f.cfg.Standalone() {
		f.fallback.ServeHTTP(w, r)
		return
	}
	start := time.Now()
	f.proxy.ServeHTTP(w, r)
	f.count(r.URL.Path, time.Since(start), false)
}
