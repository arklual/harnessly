package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/preview"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/internal/preview/ports"
)

// The preview slice ("preview"): the reverse proxy behind the right pane's browser with the slots' DevTools record,
// the agents' DevTools routes and MCP server, and the ports and component lookup of the pane. The DevTools record lives
// in the process that proxies, so the proxy and every route reading the record move together.

func init() {
	routeGroups = append(routeGroups, previewRoutes)
	bodyRouteGroups = append(bodyRouteGroups, previewBodyRoutes)
}

// designLibraryPort is the loopback port of the design library server, which is no dev server whose components could
// be searched for; 0 while none runs here.
var designLibraryPort = func() int { return 0 }

type previewSet struct {
	store   *devtools.Store
	api     *devtools.API
	mcp     *devtools.MCP
	mcpName string
	// agentTools are the MCP tools besides the DevTools for the agent that calls (delegation, v2_delegate.go): set once
	// the conversations run, nil before.
	agentTools atomic.Pointer[func(*http.Request) mcpserver.Toolset]
	scanner    *ports.Scanner
	logs       *devtools.ServerLogs
	proxy      *preview.Proxy
	cfg        preview.Config
	search     ports.Searcher
}

var (
	previewMu   sync.Mutex
	previewSets = map[*gate.CLIToken]*previewSet{}
)

// previewHidden are the ports never listed nor opened: the platform's own (named by the deployment), this gate's and the
// previous runtime's.
func previewHidden(d deps) func() map[int]bool {
	return func() map[int]bool {
		hidden := map[int]bool{}
		for _, p := range splitList(os.Getenv("RUNTIME_RESERVED_PORTS")) {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				hidden[n] = true
			}
		}
		for _, addr := range []string{os.Getenv("RUNTIME_GATE_LISTEN"), d.Upstream} {
			if p := portOf(addr); p > 0 {
				hidden[p] = true
			}
		}
		if d.Standalone {
			for _, p := range standaloneSlots.hidden() {
				hidden[p] = true
			}
		}
		return hidden
	}
}

// previewBridgeScript reads the page bridge's script from the file the deployment names, again when the file changes.
func previewBridgeScript(file string) func() ([]byte, bool) {
	var mu sync.Mutex
	var mod time.Time
	var size int64
	var script []byte
	return func() ([]byte, bool) {
		if file == "" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		st, err := os.Stat(file)
		if err != nil {
			return nil, false
		}
		if script == nil || !st.ModTime().Equal(mod) || st.Size() != size {
			raw, err := os.ReadFile(file)
			if err != nil {
				return nil, false
			}
			script, mod, size = raw, st.ModTime(), st.Size()
		}
		return script, true
	}
}

// previewOldRuntime hands a request to the previous runtime as it came.
func previewOldRuntime(upstream string) http.HandlerFunc {
	target, _ := url.Parse("http://" + upstream)
	rp := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.URL.Path, r.Out.URL.RawPath = r.In.URL.Path, r.In.URL.RawPath
		r.Out.URL.RawQuery = r.In.URL.RawQuery
		r.Out.Host = r.In.Host
	}}
	return rp.ServeHTTP
}

func previewOf(d deps) *previewSet {
	previewMu.Lock()
	defer previewMu.Unlock()
	if s := previewSets[d.CLI]; s != nil {
		return s
	}
	store := devtools.NewStore()
	hidden := previewHidden(d)
	scanner := &ports.Scanner{Home: d.Home, Worktrees: env("RUNTIME_WORKTREES_ROOT", filepath.Join(d.Home, ".worktrees")), Hidden: hidden}
	where := os.Getenv("RUNTIME_PLATFORM_HOST")
	if where == "" {
		where = locale.Pick("the platform", "платформы")
	}
	logs := &devtools.ServerLogs{Store: store, Tmux: d.Tmux, Listener: scanner.Listener,
		Outside: locale.Pick("The server was not started in a terminal of "+where+", and its output is not written to a file", "Сервер запущен не в терминале "+where+", и его вывод не пишется в файл")}
	api := &devtools.API{Store: store, CLI: env("RUNTIME_CLI_NAME", "harnessly"), ServerLog: logs.Get}
	secret, err := agent.Config{SecretFile: os.Getenv("AGENT_SECRET_FILE"), ConfigFile: d.ConfigFile}.LoadSecret()
	if err != nil {
		agent.Log("warn", "preview: no pair secret, every claim is refused", agent.F{K: "error", V: err.Error()})
	}
	cfg := preview.Config{
		Path:                env("RUNTIME_PREVIEW_PROXY_PATH", "/agents"+platformAPI+"/preview/proxy"),
		ClaimHeader:         env("RUNTIME_PREVIEW_CLAIM_HEADER", "x-preview-claim"),
		Audience:            env("RUNTIME_PREVIEW_CLAIM_AUDIENCE", "preview-proxy"),
		Secret:              secret,
		PrivateHeaderPrefix: env("RUNTIME_PREVIEW_PRIVATE_HEADER_PREFIX", "x-preview-"),
		Forbidden:           hidden,
		Stands: preview.Stands{Domain: os.Getenv("RUNTIME_STAND_DOMAIN"), SignInHosts: splitList(os.Getenv("RUNTIME_PREVIEW_SIGN_IN_HOSTS")),
			SessionHeader: os.Getenv("RUNTIME_PREVIEW_STAND_SESSION_HEADER")},
		SelfIPs: os.Getenv("RUNTIME_PREVIEW_SELF_IPS"),
		Store:   store,
		Cache:   preview.NewAssetCache(filepath.Join(d.DataDir, "preview-cache"), preview.DefaultMaxTotalBytes, preview.DefaultMaxFileBytes),
		Bridge:  previewBridgeScript(os.Getenv("RUNTIME_PREVIEW_BRIDGE_FILE")),
	}
	if d.Upstream != "" {
		cfg.Fallback = previewOldRuntime(d.Upstream)
	}
	// With no file named and no previous runtime to ask, the bridge is the one built next to the page's files.
	if os.Getenv("RUNTIME_PREVIEW_BRIDGE_FILE") == "" && (d.Standalone || d.Upstream == "") {
		cfg.Bridge = previewBridgeFromWeb()
	}
	if d.Standalone {
		// A standalone runtime signs its own claims for its own slots, which are plain http on a local host.
		cfg.LocalOrigins = true
	}
	s := &previewSet{store: store, api: api, mcp: &devtools.MCP{API: api}, mcpName: env("RUNTIME_MCP_SERVER", "harnessly"), scanner: scanner,
		logs: logs, proxy: preview.New(cfg), cfg: cfg}
	previewSets[d.CLI] = s
	if d.Standalone {
		standaloneSlots.attach(s, secret)
	}
	return s
}

func previewBodyRoutes(d deps) []gate.BodyRoute {
	s := previewOf(d)
	sliceOn := d.SliceOn
	if sliceOn == nil {
		sliceOn = gate.NewSlices(d.DataDir).On
	}
	return []gate.BodyRoute{{
		Slice: "preview",
		// The proxy has no shadow: a second run would ask the dev server twice, and a shadowed upgrade would not upgrade.
		// Matching only while the slice is on keeps "preview-shadow" off it.
		Match: func(r *http.Request) bool { return s.proxy.Matches(r) && sliceOn("preview") },
		Serve: s.proxy.ServeHTTP,
	}}
}

type previewError struct {
	Error string `json:"error"`
}

func previewBytes(r *http.Request, status int, body []byte, h http.Header) (int, any, http.Header) {
	if h == nil {
		h = http.Header{}
	}
	tag := nodejson.ETag(body)
	h.Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		h.Set("Content-Type", "application/json; charset=utf-8")
		return http.StatusNotModified, nil, h
	}
	return status, json.RawMessage(body), h
}

func previewNoStore() http.Header { return http.Header{"Cache-Control": {"no-store"}} }

func previewBodyRefusal(e *preview.BodyError) (int, any, http.Header) {
	return e.Status, gate.Raw{ContentType: "text/plain", Body: []byte(e.Text)}, nil
}

func previewQuery(r *http.Request) devtools.Query { return devtools.Query(files.RequestQuery(r)) }

// previewCLIRoute is a route under /api/cli/preview: the cli-token, no caching, the methods named, an outcome as JSON.
func previewCLIRoute(d deps, path string, methods []string, run func(*http.Request, any) devtools.Outcome) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: "preview",
		Match: routeAt(path),
		Compute: func(r *http.Request) (int, any, http.Header) {
			body, berr := preview.ParseBody(r)
			if berr != nil {
				return previewBodyRefusal(berr)
			}
			if !d.CLI.OK(r) {
				return answerJSON(r, 403, previewError{"Forbidden"}, nil)
			}
			allowed := false
			for _, m := range methods {
				allowed = allowed || r.Method == m
			}
			if !allowed {
				h := previewNoStore()
				h.Set("Allow", strings.Join(methods, ", "))
				return answerJSON(r, 405, previewError{"Method not allowed"}, h)
			}
			out := run(r, body)
			if !out.OK() {
				return answerJSON(r, out.Status, previewError{out.Error}, previewNoStore())
			}
			return answerJSON(r, 200, out.Value, previewNoStore())
		},
	}
}

func previewRefusal(status int, msg string) devtools.Outcome {
	return devtools.Outcome{Status: status, Error: msg}
}

// previewSessionRoute is a route of the pane under the platform API: a page session or the cli-token to pass the dispatcher,
// the method, then the page session itself.
func previewSessionRoute(d deps, path string, methods []string, compute func(*http.Request, any) (int, any, http.Header)) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: "preview",
		Match: routeAt(path),
		Compute: func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			body, berr := preview.ParseBody(r)
			if berr != nil {
				return previewBodyRefusal(berr)
			}
			allowed := false
			for _, m := range methods {
				allowed = allowed || r.Method == m
			}
			if !allowed {
				return answerJSON(r, 405, previewError{"Method not allowed"}, nil)
			}
			if !d.SessionOK(r) {
				return answerJSON(r, 403, previewError{"Forbidden"}, nil)
			}
			return compute(r, body)
		},
	}
}

// previewSlot is the pane's ?slot=: the named slot, or the one used last when none is named.
func previewSlot(s *previewSet, q devtools.Query) (string, int, string) {
	vs := q["slot"]
	if len(vs) > 1 {
		return "", 400, "invalid slot"
	}
	asked := ""
	if len(vs) == 1 {
		asked = vs[0]
	}
	if slot := s.store.Resolve(asked); slot != "" {
		return slot, 0, ""
	}
	if asked != "" {
		return "", 400, "invalid slot"
	}
	return "", 404, "no preview slot yet"
}

// previewNumber is Number(req.query[k]): a repeated key is an array, which is no number.
func previewNumber(q devtools.Query, k string) (float64, bool) {
	vs, ok := q[k]
	if !ok {
		return 0, false
	}
	if len(vs) != 1 {
		return math.NaN(), true
	}
	return devtools.JSNumber(vs[0]), true
}

func previewWhole(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f) }

func previewRoutes(d deps) []gate.JSONRoute {
	s := previewOf(d)
	api := s.api
	cli := "/api/cli/preview/"
	pane := platformAPI + "/preview/"
	routes := []gate.JSONRoute{
		previewCLIRoute(d, cli+"slots", []string{"GET"}, func(*http.Request, any) devtools.Outcome { return devtools.Outcome{Value: api.Slots()} }),
		previewCLIRoute(d, cli+"network", []string{"GET"}, func(r *http.Request, _ any) devtools.Outcome {
			q := previewQuery(r)
			parsed, msg := devtools.ParseNetworkQuery(q)
			if msg != "" {
				return previewRefusal(400, msg)
			}
			return api.Network(q.Slot(), parsed)
		}),
		previewCLIRoute(d, cli+"console", []string{"GET"}, func(r *http.Request, _ any) devtools.Outcome {
			q := previewQuery(r)
			parsed, msg := devtools.ParseConsoleQuery(q)
			if msg != "" {
				return previewRefusal(400, msg)
			}
			return api.Console(q.Slot(), parsed)
		}),
		previewCLIRoute(d, cli+"server-log", []string{"GET"}, func(r *http.Request, _ any) devtools.Outcome {
			q := previewQuery(r)
			all, lines, msg := devtools.ParseServerLogQuery(q)
			if msg != "" {
				return previewRefusal(400, msg)
			}
			return api.ServerLogOf(q.Slot(), all, lines)
		}),
		previewCLIRoute(d, cli+"settings", []string{"GET", "PATCH"}, func(r *http.Request, body any) devtools.Outcome {
			slot := previewQuery(r).Slot()
			if r.Method == http.MethodGet {
				return api.SettingsOf(slot)
			}
			if ct, _ := previewHeader(r, "Content-Type"); !strings.HasPrefix(ct, "application/json") {
				return previewRefusal(415, locale.Pick("application/json is required", "Нужен application/json"))
			}
			return api.ChangeSettings(slot, body)
		}),
		previewCLIRoute(d, cli+"clear", []string{"DELETE"}, func(r *http.Request, _ any) devtools.Outcome { return api.ClearLogs(previewQuery(r).Slot()) }),
		{
			Slice:   "preview",
			Match:   routeAt(cli + "mcp"),
			Compute: func(r *http.Request) (int, any, http.Header) { return s.serveMCP(d, r) },
		},
		previewSessionRoute(d, pane+"devtools", []string{"GET", "PATCH", "DELETE"}, func(r *http.Request, body any) (int, any, http.Header) {
			q := previewQuery(r)
			slot, status, msg := previewSlot(s, q)
			if slot == "" {
				return answerJSON(r, status, previewError{msg}, previewNoStore())
			}
			switch r.Method {
			case http.MethodGet:
				since := 0.0
				if n, given := previewNumber(q, "since"); given {
					since = n
				}
				if !previewWhole(since) || since < 0 {
					return answerJSON(r, 400, previewError{"invalid since"}, previewNoStore())
				}
				return answerJSON(r, 200, s.store.Read(slot, int64(math.Min(since, math.MaxInt64))), previewNoStore())
			case http.MethodDelete:
				s.store.Clear(slot)
				return answerJSON(r, 200, struct {
					OK bool `json:"ok"`
				}{true}, previewNoStore())
			}
			patch, msg := devtools.ParseSettingsPatch(body)
			if msg != "" {
				return answerJSON(r, 400, previewError{msg}, previewNoStore())
			}
			return answerJSON(r, 200, struct {
				Settings devtools.Settings `json:"settings"`
			}{s.store.SetSettings(slot, patch)}, previewNoStore())
		}),
		previewSessionRoute(d, pane+"devtools/server-log", []string{"GET"}, func(r *http.Request, _ any) (int, any, http.Header) {
			q := previewQuery(r)
			slot, status, msg := previewSlot(s, q)
			if slot == "" {
				return answerJSON(r, status, previewError{msg}, previewNoStore())
			}
			all := len(q["all"]) == 1 && (q["all"][0] == "1" || q["all"][0] == "true")
			lines := 0
			if n, given := previewNumber(q, "lines"); given {
				if !previewWhole(n) || n < 1 {
					return answerJSON(r, 400, previewError{"invalid lines"}, previewNoStore())
				}
				lines = int(math.Min(n, 1e9))
			}
			log, err := s.logs.Get(slot, all, lines)
			if err != nil {
				return answerJSON(r, 500, previewError{"server log failed"}, previewNoStore())
			}
			return answerJSON(r, 200, log, previewNoStore())
		}),
		previewSessionRoute(d, pane+"ports", []string{"GET"}, func(r *http.Request, _ any) (int, any, http.Header) {
			paths, ok := previewWorkPaths(previewQuery(r))
			if !ok {
				return answerJSON(r, 400, previewError{"invalid paths"}, nil)
			}
			list, err := s.scanner.List(paths)
			if err != nil {
				return answerJSON(r, 500, previewError{"port scan failed"}, previewNoStore())
			}
			return answerJSON(r, 200, struct {
				Ports []ports.Port `json:"ports"`
			}{list}, previewNoStore())
		}),
		previewSessionRoute(d, pane+"resolve", []string{"POST"}, func(r *http.Request, body any) (int, any, http.Header) {
			o, _ := body.(*jsvalue.Object)
			if o == nil {
				o = jsvalue.NewObject()
			}
			rawPort, _ := o.Get("port")
			rawComponent, _ := o.Get("component")
			component, ok := ports.ParseComponent(rawComponent)
			port, isNum := rawPort.(float64)
			if !isNum || !previewWhole(port) || port < 1 || port > 65535 || !ok {
				return answerJSON(r, 400, previewError{"invalid request"}, nil)
			}
			if int(port) == designLibraryPort() {
				return answerJSON(r, 404, previewError{"port is not a preview dev server"}, previewNoStore())
			}
			root, err := s.scanner.PortRoot(int(port))
			if err != nil {
				return answerJSON(r, 500, previewError{"resolve failed"}, previewNoStore())
			}
			if root == "" {
				return answerJSON(r, 404, previewError{"port is not a preview dev server"}, previewNoStore())
			}
			matches, err := s.search.Resolve(root, component)
			if err == ports.ErrSearchTimedOut {
				return answerJSON(r, 504, previewError{"search timed out"}, previewNoStore())
			}
			if err != nil {
				return answerJSON(r, 500, previewError{"resolve failed"}, previewNoStore())
			}
			return answerJSON(r, 200, struct {
				Matches []ports.Match `json:"matches"`
			}{matches}, previewNoStore())
		}),
	}
	if d.Standalone {
		// Behind the fleet gateway the gateway answers this itself; standalone, the runtime's own slots do.
		routes = append(routes, previewOpenRoute(d))
	}
	return routes
}

func previewHeader(r *http.Request, name string) (string, bool) {
	vs := r.Header.Values(name)
	if len(vs) == 0 {
		return "", false
	}
	return strings.Join(vs, ", "), true
}

// previewWorkPaths is ?paths= of the pane: a JSON list of at most 32 absolute paths.
func previewWorkPaths(q devtools.Query) ([]string, bool) {
	vs, ok := q["paths"]
	if !ok {
		return []string{}, true
	}
	if len(vs) != 1 {
		return nil, false
	}
	v, err := jsvalue.Parse([]byte(vs[0]))
	list, isList := v.([]any)
	if err != nil || !isList || len(list) > 32 {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		p, ok := item.(string)
		if !ok || !strings.HasPrefix(p, "/") {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

const previewMCPLimit = 256 * 1024

// serveMCP is the agents' MCP server of the preview DevTools: stateless JSON-RPC over POST, answered as JSON.
func (s *previewSet) serveMCP(d deps, r *http.Request) (int, any, http.Header) {
	if !d.CLI.OK(r) {
		return answerJSON(r, 403, previewError{"Forbidden"}, nil)
	}
	if r.Method != http.MethodPost {
		h := previewNoStore()
		h.Set("Allow", "POST")
		return answerJSON(r, 405, previewError{"Method not allowed"}, h)
	}
	if ct, _ := previewHeader(r, "Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return answerJSON(r, 415, previewError{locale.Pick("application/json is required", "Нужен application/json")}, previewNoStore())
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, previewMCPLimit+1))
	if err != nil || len(raw) > previewMCPLimit {
		return answerJSON(r, 413, previewError{locale.Pick("The message is too large", "Сообщение слишком большое")}, previewNoStore())
	}
	payload, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
	if err != nil {
		return previewBytes(r, 400, mcpserver.ParseError(), previewNoStore())
	}
	srv := &mcpserver.Server{Name: s.mcpName, Sets: []mcpserver.Toolset{s.mcp}}
	if more := s.agentTools.Load(); more != nil {
		if set := (*more)(r); set != nil {
			srv.Sets = append(srv.Sets, set)
		}
	}
	status, body := srv.Handle(r.Context(), payload)
	if body == nil {
		return status, nil, previewNoStore()
	}
	return previewBytes(r, status, body, previewNoStore())
}
