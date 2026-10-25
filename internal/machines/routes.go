// Package machines answers what a runtime says about its machine: the machine snapshot (load, provider limits with
// their forecast, features, release), the machine load panel, the health probe and the app manifest, and the web push
// subscription routes. It reads files, /proc and tmux itself and needs no agent state, so it works whether the core
// runs in this process or not.
package machines

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/push"
)

// Slice is the gate slice of these routes.
const Slice = "machines"

// Config is what the routes need; product names come from the deployment.
type Config struct {
	// PlatformAPI is the prefix of the platform's own routes (RUNTIME_PLATFORM_API).
	PlatformAPI string
	DataDir     string
	SessionOK   func(*http.Request) bool
	// APIRefusal is the API's answer to a caller with neither a page session nor the cli-token.
	APIRefusal func(*http.Request) (status int, body any, h http.Header, refused bool)
	Snapshot   *Snapshot
	Load       *LoadMonitor
	// Push is the web push sender the core sends with: one subscription store, one writer.
	Push *push.Sender
	// CanSend: the deployment named the push subject and tag prefix and the app name the notifications carry; without
	// them the routes that send are left to the previous runtime.
	CanSend   bool
	AppName   string
	TagPrefix string
	Health    Health
	// ManifestFile holds the app manifest (RUNTIME_MANIFEST_FILE, else the release's copy); without one the manifest is
	// the previous runtime's.
	ManifestFile string
	// MachineAgent: the request is the machine agent's own (loopback, a session it signed with its name).
	MachineAgent func(*http.Request) bool
	// DocsReaderPath is where the platform's document reader lives: a document notification must lead there.
	DocsReaderPath string
	// DesignReaderPath is where the design library lives: a design notification must lead there.
	DesignReaderPath string
	Log              func(level, msg string, kv ...any)
	Now              func() time.Time
}

// Health is the probe's answer: the app's name, and the release's version and commit.
type Health struct {
	App             string
	FallbackVersion string
	ReleaseFile     string
	// Alive says whether the runtime as a whole is up (the previous runtime answers on its port): a runtime that is
	// down must look down to the rollout and the cluster.
	Alive func() bool
}

type service struct {
	cfg      Config
	notices  sync.Mutex
	release  *Release
	relOnce  sync.Once
	manifest struct {
		sync.Mutex
		mtime time.Time
		size  int64
		body  []byte
	}
}

func (s *service) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

func (s *service) log(level, msg string, kv ...any) {
	if s.cfg.Log != nil {
		s.cfg.Log(level, msg, kv...)
	}
}

func pathIs(r *http.Request, p string) bool { return r.URL.Path == p || r.URL.Path == "/agents"+p }

func errorBody(msg string) *jsvalue.Object { return jsvalue.NewObject("error", msg) }

// answer is a JSON answer as the previous dispatcher sent it: its ETag, and 304 when the caller holds it already (the
// 304 keeps the JSON content type the handler had set).
func answer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	status, body, h := nodejson.Answer(r, status, v, h)
	if status == http.StatusNotModified {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	return status, body, h
}

func methodNotAllowed(r *http.Request, allow string) (int, any, http.Header) {
	return answer(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), http.Header{"Allow": {allow}})
}

func textAnswer(status int, text string) (int, any, http.Header) {
	return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
}

const bodyLimit = 1 << 20

// parseBody is the previous dispatcher's: JSON (empty is {}), url-encoded forms as an object, anything else as text;
// a status and the text/plain refusal when it cannot be read.
func parseBody(r *http.Request) (any, int, string) {
	if r.ContentLength > bodyLimit {
		return nil, 413, "Body exceeded 1mb limit"
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		return nil, 400, "Invalid body"
	}
	if len(raw) > bodyLimit {
		return nil, 413, "Body exceeded 1mb limit"
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	text := strings.ToValidUTF8(string(raw), "�")
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, _ := strings.Cut(ct, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if text == "" {
			return jsvalue.NewObject(), 0, ""
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			return nil, 400, "Invalid JSON"
		}
		return v, 0, ""
	case "application/x-www-form-urlencoded":
		o := jsvalue.NewObject()
		for k, vs := range jscompat.Query(text) {
			if len(vs) == 1 {
				o.Set(k, vs[0])
			} else {
				o.Set(k, jsvalue.StringArray(vs))
			}
		}
		return o, 0, ""
	}
	return text, 0, ""
}

// handler is a route's own part, after the dispatcher's authorization and body parsing.
type handler func(r *http.Request, body any) (int, any, http.Header)

// guarded runs a handler behind the dispatcher: the session or cli-token check unless the path is open, then the body.
func (s *service) guarded(open bool, h handler) func(*http.Request) (int, any, http.Header) {
	return func(r *http.Request) (int, any, http.Header) {
		if !open && s.cfg.APIRefusal != nil {
			if status, v, hh, refused := s.cfg.APIRefusal(r); refused {
				return status, v, hh
			}
		}
		body, status, text := parseBody(r)
		if status != 0 {
			return textAnswer(status, text)
		}
		return h(r, body)
	}
}

// field is a property of the parsed body as destructuring reads it (nothing on a string, null or a number).
func field(body any, k string) (any, bool) {
	if o, ok := body.(*jsvalue.Object); ok {
		return o.Get(k)
	}
	return nil, false
}

// Routes are the slice's routes; idempotent GETs also run in the "machines-shadow" slice.
func Routes(cfg Config) []gate.JSONRoute {
	s := &service{cfg: cfg}
	route := func(path string, open bool, h handler) gate.JSONRoute {
		return gate.JSONRoute{Slice: Slice, Match: func(r *http.Request) bool { return pathIs(r, path) }, Compute: s.guarded(open, h)}
	}
	sending := func(path string, h handler) gate.JSONRoute {
		jr := route(path, false, h)
		jr.Match = func(r *http.Request) bool { return cfg.CanSend && cfg.Push != nil && pathIs(r, path) }
		return jr
	}
	machines := route(cfg.PlatformAPI+"/machines", false, s.machines)
	load := route(cfg.PlatformAPI+"/machine/load", false, s.machineLoad)
	health := route("/api/health", true, s.health)
	health.Match = func(r *http.Request) bool {
		return pathIs(r, "/api/health") && (cfg.Health.Alive == nil || cfg.Health.Alive())
	}
	manifest := route("/api/manifest", true, s.manifestRoute)
	manifest.Match = func(r *http.Request) bool {
		return cfg.ManifestFile != "" && pathIs(r, "/api/manifest") && s.manifestBody() != nil
	}
	routes := []gate.JSONRoute{machines, load, health, manifest,
		route("/api/push/vapid-key", false, s.vapidKey),
		route("/api/push/subscribe", false, s.subscribe),
		route("/api/push/register-session", false, s.registerSession),
		route("/api/push/diagnostic", false, s.diagnostic),
		route("/api/push/viewport", false, s.viewport),
		sending("/api/push/test", s.pushTest),
		sending(cfg.PlatformAPI+"/release-notice", s.releaseNotice),
	}
	if cfg.MachineAgent != nil && cfg.DocsReaderPath != "" {
		routes = append(routes, sending(cfg.PlatformAPI+"/docs/notify", s.docsNotify))
	}
	if cfg.MachineAgent != nil && cfg.DesignReaderPath != "" {
		routes = append(routes, sending(cfg.PlatformAPI+"/design/notify", s.designNotify))
	}
	return routes
}

func (s *service) machines(r *http.Request, _ any) (int, any, http.Header) {
	if s.cfg.SessionOK == nil || !s.cfg.SessionOK(r) {
		return answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	m := s.cfg.Snapshot.Machine()
	return answer(r, 200, struct {
		Machines []Machine `json:"machines"`
	}{[]Machine{m}}, http.Header{"Cache-Control": {"no-store"}})
}

func (s *service) machineLoad(r *http.Request, _ any) (int, any, http.Header) {
	if r.Method != http.MethodGet {
		return answer(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), nil)
	}
	if s.cfg.SessionOK == nil || !s.cfg.SessionOK(r) {
		return answer(r, http.StatusForbidden, errorBody("unauthorized"), nil)
	}
	noStore := http.Header{"Cache-Control": {"no-store"}}
	l, err := s.cfg.Load.Load()
	if err == ErrScanTimeout {
		return answer(r, http.StatusServiceUnavailable, errorBody("scan-timeout"), noStore)
	}
	if err != nil {
		return answer(r, http.StatusInternalServerError, errorBody("machine load scan failed"), noStore)
	}
	return answer(r, 200, l, noStore)
}

func (s *service) health(r *http.Request, _ any) (int, any, http.Header) {
	s.relOnce.Do(func() { s.release = ReadRelease(s.cfg.Health.ReleaseFile) })
	o := jsvalue.NewObject("app", s.cfg.Health.App, "version", s.cfg.Health.FallbackVersion, "commit", nil)
	if s.release != nil {
		o.Set("version", s.release.Version)
		if s.release.Commit != nil {
			o.Set("commit", *s.release.Commit)
		}
	}
	return answer(r, 200, o, nil)
}

// manifestBody is the manifest file as JSON.stringify writes it, read again when the file changes; nil without one.
func (s *service) manifestBody() []byte {
	m := &s.manifest
	m.Lock()
	defer m.Unlock()
	st, err := os.Stat(s.cfg.ManifestFile)
	if err != nil {
		return nil
	}
	if m.body != nil && st.ModTime().Equal(m.mtime) && st.Size() == m.size {
		return m.body
	}
	raw, err := os.ReadFile(s.cfg.ManifestFile)
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	m.body, m.mtime, m.size = jsvalue.Stringify(v, ""), st.ModTime(), st.Size()
	return m.body
}

// manifestRoute: the installable app's manifest. The previous handler set a manifest content type and then res.json
// replaced it with JSON's; the answer keeps that.
func (s *service) manifestRoute(r *http.Request, _ any) (int, any, http.Header) {
	body := s.manifestBody()
	if body == nil {
		return textAnswer(500, "Internal Server Error")
	}
	return answer(r, 200, jsonText(body), http.Header{"Cache-Control": {"no-cache"}})
}
