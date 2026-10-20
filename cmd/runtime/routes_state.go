package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"math"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/settings"
	"github.com/ly-solutions/harnessly/internal/state"
)

// The state slice: the settings files the cluster serves from its database first (this runtime is their fallback and
// their writer), the automation routes, the little routes nothing else took, and the old runtime's dead routes answered
// as it answered them on a machine without pages.

func stateSettings(d deps) *settings.Store {
	return &settings.Store{DataDir: d.DataDir, ConfigFile: d.ConfigFile}
}

// accessHost is the deployment's explicit access rule: RUNTIME_ACCESS_HOST, the previous runtime's HOST.
func accessHost() string { return env("RUNTIME_ACCESS_HOST", os.Getenv("HOST")) }

// bindsLoopback says whether the previous runtime listened on the loopback address only: an explicit rule, else the
// config's network access, read once at start as it did.
func bindsLoopback(store *settings.Store) bool {
	spec := jscompat.Trim(accessHost())
	if spec == "" {
		switch get(store.ReadConfig(), "networkAccess") {
		case "localhost":
			spec = "localhost"
		case "tailscale":
			spec = "localhost,tailscale"
		default:
			spec = "all"
		}
	}
	for _, part := range strings.Split(spec, ",") {
		t := strings.ToLower(strings.TrimSpace(part))
		switch t {
		case "":
			continue
		case "all", "*", "0.0.0.0":
			return false
		case "localhost":
			continue
		case "tailscale", "lan":
			return false
		}
		addr, bits, hasBits := strings.Cut(strings.TrimSpace(part), "/")
		a, err := netip.ParseAddr(strings.TrimPrefix(addr, "::ffff:"))
		if err != nil {
			continue
		}
		a = a.WithZone("")
		n := a.BitLen()
		if hasBits {
			if n, err = strconv.Atoi(bits); err != nil || n < 0 || n > a.BitLen() {
				continue
			}
		}
		if a.Is4() {
			if a.As4()[0] != 127 {
				return false
			}
		} else if a != netip.IPv6Loopback() || n != 128 {
			return false
		}
	}
	return true
}

func get(v any, k string) any {
	o, _ := v.(*jsvalue.Object)
	x, _ := o.Get(k)
	return x
}

func noStore() http.Header { return http.Header{"Cache-Control": {"no-store"}} }

func empty(status int, h http.Header) (int, any, http.Header) { return status, nil, h }

func stateAnswer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	return settings.Answer(r, status, v, h)
}

func errBody(msg string) *jsvalue.Object { return jsvalue.NewObject("error", msg) }

// stateRoutes are the state slice's routes that need no core; validate checks a launch choice against the catalog.
func stateRoutes(d deps, store *settings.Store, validate func(provider string, model, effort any) (*agents.Launch, string)) []gate.JSONRoute {
	at := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	guarded := func(h settings.Handler) func(*http.Request) (int, any, http.Header) {
		return settings.Guarded(d.APIRefusal, h)
	}
	open := func(h settings.Handler) func(*http.Request) (int, any, http.Header) { return settings.Guarded(nil, h) }
	route := func(p string, h settings.Handler) gate.JSONRoute {
		return gate.JSONRoute{Slice: settings.Slice, Match: at(p), Compute: guarded(h)}
	}
	routes := settings.Routes(settings.Config{
		Store:          store,
		LayoutKey:      env("RUNTIME_LAYOUT_KEY", "layout"),
		DefaultsKey:    env("RUNTIME_LAUNCH_DEFAULTS_KEY", "launchDefaults"),
		AccessHost:     accessHost(),
		BindsLoopback:  bindsLoopback(store),
		ValidateLaunch: validate,
		APIRefusal:     d.APIRefusal,
	})
	routes = append(routes,
		route("/api/tmux/info", tmuxInfo(d)),
		route("/api/system/tmux-sessions", func(r *http.Request, _ any) (int, any, http.Header) {
			return stateAnswer(r, 200, jsvalue.NewObject("count", float64(len(d.Tmux.ListSessions()))), nil)
		}),
		route(platformAPI+"/net-events", netEvents(d)),
		route(platformAPI+"/gateway-ping", func(*http.Request, any) (int, any, http.Header) { return empty(http.StatusNoContent, noStore()) }),
		route("/api/stats/daily-report/cache", dailyReportCache(d)),
		route("/api/react-scan", reactScan()),
	)
	guide := releaseStatic("RUNTIME_API_GUIDE_FILE", "api-guide.md")
	// With no guide file a standalone runtime answers with the built-in one.
	builtIn := d.Old == nil || d.Old.Upstream == ""
	routes = append(routes, gate.JSONRoute{Slice: settings.Slice,
		Match: func(r *http.Request) bool {
			return pathIs(r, "/api/cli/api-guide") && ((guide != "" && fileExists(guide)) || builtIn)
		},
		Compute: open(func(r *http.Request, _ any) (int, any, http.Header) {
			if r.Method != http.MethodGet {
				return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"GET"}})
			}
			if !d.Old.CLITokenOK(r) {
				return stateAnswer(r, http.StatusForbidden, errBody("Forbidden"), nil)
			}
			text := []byte(builtInAPIGuide)
			if guide != "" && fileExists(guide) {
				var err error
				if text, err = os.ReadFile(guide); err != nil {
					return http.StatusInternalServerError, handlerFailed, nil
				}
			}
			return settings.AnswerRaw(r, 200, "text/markdown; charset=utf-8", text, nil)
		})})
	routes = append(routes, authRoutes(d, store, open)...)
	routes = append(routes, tailscaleRoutes(d, guarded)...)
	return routes
}

// unshadowed are routes whose GET may write (the automation token is made on first use; a browser tab's lookup makes a
// missing layout): never run in the shadow next to the previous runtime doing the same.
func unshadowed(f *gate.Gate, routes []gate.JSONRoute) []gate.Route {
	out := make([]gate.Route, 0, len(routes))
	for _, jr := range routes {
		out = append(out, f.AsRoutes(jr)[0])
	}
	return out
}

// automationRoutes: the automation rules and runs (kept in the cluster's database), and the automation token.
func automationRoutes(d deps, store *settings.Store) []gate.JSONRoute {
	guarded := func(h settings.Handler) func(*http.Request) (int, any, http.Header) {
		return settings.Guarded(d.APIRefusal, h)
	}
	auto := automations{store: store, d: d}
	at := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.JSONRoute{
		{Slice: settings.Slice, Match: at(platformAPI + "/automations/rules"), Compute: guarded(auto.rules)},
		{Slice: settings.Slice, Match: func(r *http.Request) bool { _, ok := automationRule(r); return ok }, Compute: guarded(auto.rule)},
		{Slice: settings.Slice, Match: at(platformAPI + "/automations/runs"), Compute: guarded(auto.runs)},
		{Slice: settings.Slice, Match: at(platformAPI + "/automations/token"), Compute: guarded(auto.token)},
	}
}

// stateCoreRoutes are the state slice's routes that read the core: answered while it runs.
func (c *core) stateCoreRoutes(d deps) []gate.JSONRoute {
	return []gate.JSONRoute{{Slice: settings.Slice,
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/background") && c.manager() != nil },
		Compute: settings.Guarded(d.APIRefusal, func(r *http.Request, _ any) (int, any, http.Header) {
			if !d.SessionOK(r) {
				return empty(http.StatusForbidden, nil)
			}
			return stateAnswer(r, 200, map[string]any{"tabs": c.manager().Background()}, noStore())
		})}}
}

// --- automations: the rules live in the cluster's database, which a machine does not reach -----------------------------

type automations struct {
	store *settings.Store
	d     deps
}

var ruleRe = regexp.MustCompile(`^/api/[^/]+/automations/rules/([^/]+)$`)

func automationRule(r *http.Request) (string, bool) {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	m := ruleRe.FindStringSubmatch(p)
	if m == nil || !strings.HasPrefix(p, platformAPI+"/") {
		return "", false
	}
	return m[1], true
}

// databaseConfigured is databaseConfig's test: a URL, or a host, a database and credentials.
func databaseConfigured() bool {
	if os.Getenv("DATABASE_URL") != "" {
		return true
	}
	user := env("PGUSER", os.Getenv("POSTGRES_USER"))
	password := env("PGPASSWORD", os.Getenv("POSTGRES_PASSWORD"))
	return os.Getenv("PGHOST") != "" && os.Getenv("PGDATABASE") != "" && user != "" && password != ""
}

// noDatabase is the answer where the previous runtime opened its database pool: without one configured it threw;
// with one, the rules are the cluster's and this runtime does not open a database.
func noDatabase(r *http.Request) (int, any, http.Header) {
	if databaseConfigured() {
		agent.Log("warn", "automations: the rules are kept in the cluster's database, not answered on a machine", agent.F{K: "path", V: r.URL.Path})
	}
	return http.StatusInternalServerError, handlerFailed, nil
}

var bearerRe = regexp.MustCompile(`^Bearer (.+)$`)

// caller is verifyAutomationCaller: this runtime's owner, for a page session or the runtime's automation token; false
// for anyone else; failed where the old comparison threw (lengths equal in UTF-16, not in bytes).
func (a automations) caller(r *http.Request) (ok, failed bool) {
	// config.automationOwnerEmail ?? the deployment's owner ?? null
	owner := get(a.store.ReadConfig(), "automationOwnerEmail")
	if owner == nil {
		if v, ok := os.LookupEnv("RUNTIME_OWNER_EMAIL"); ok {
			owner = v
		}
	}
	if !jsvalue.Truthy(owner) {
		return false, false
	}
	if a.d.SessionOK(r) {
		return true, false
	}
	m := bearerRe.FindStringSubmatch(r.Header.Get("Authorization"))
	if m == nil {
		return false, false
	}
	stored, err := a.store.AutomationToken()
	if err != nil {
		return false, true
	}
	expected, isStr := stored.(string)
	if !isStr || utf16Len(m[1]) != utf16Len(expected) {
		return false, false
	}
	if len(m[1]) != len(expected) {
		return false, true
	}
	return subtle.ConstantTimeCompare([]byte(m[1]), []byte(expected)) == 1, false
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func (a automations) guard(r *http.Request) (int, any, http.Header, bool) {
	ok, failed := a.caller(r)
	if failed {
		s, v, h := http.StatusInternalServerError, any(handlerFailed), http.Header(nil)
		return s, v, h, true
	}
	if !ok {
		s, v, h := stateAnswer(r, http.StatusForbidden, errBody("Forbidden"), nil)
		return s, v, h, true
	}
	return 0, nil, nil, false
}

func (a automations) rules(r *http.Request, _ any) (int, any, http.Header) {
	if s, v, h, done := a.guard(r); done {
		return s, v, h
	}
	return noDatabase(r)
}

func (a automations) rule(r *http.Request, _ any) (int, any, http.Header) {
	if s, v, h, done := a.guard(r); done {
		return s, v, h
	}
	return noDatabase(r)
}

func (a automations) runs(r *http.Request, _ any) (int, any, http.Header) {
	if s, v, h, done := a.guard(r); done {
		return s, v, h
	}
	if r.Method != http.MethodGet {
		return empty(http.StatusMethodNotAllowed, http.Header{"Allow": {"GET"}})
	}
	return noDatabase(r)
}

func (a automations) token(r *http.Request, _ any) (int, any, http.Header) {
	if !a.d.SessionOK(r) {
		return stateAnswer(r, http.StatusForbidden, errBody("Forbidden"), nil)
	}
	if r.Method != http.MethodGet {
		return empty(http.StatusMethodNotAllowed, http.Header{"Allow": {"GET"}})
	}
	token, err := a.store.AutomationToken()
	if err != nil {
		return http.StatusInternalServerError, handlerFailed, nil
	}
	return stateAnswer(r, 200, jsvalue.NewObject("token", token), nil)
}

// --- tmux -------------------------------------------------------------------------------------------------------------------

// tmuxInfo: the pane of a session (folder, program, pid, size, the session's start) and its last command.
func tmuxInfo(d deps) settings.Handler {
	return func(r *http.Request, _ any) (int, any, http.Header) {
		if r.Method != http.MethodGet {
			return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"GET"}})
		}
		s := settings.Query(r, "session")
		if !s.Truthy() {
			return stateAnswer(r, 400, errBody("session parameter required"), nil)
		}
		if s.Array() {
			return http.StatusInternalServerError, handlerFailed, nil
		}
		name := s.Values[0]
		if !d.Tmux.HasSession(name) {
			return stateAnswer(r, 404, errBody("Session not found"), nil)
		}
		info := jsvalue.NewObject("cwd", nil, "command", nil, "pid", nil, "width", nil, "height", nil, "sessionCreated", nil)
		if out, ok := d.Tmux.Display(name, "#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{pane_width}\t#{pane_height}\t#{session_created}"); ok {
			f := strings.Split(jscompat.Trim(out), "\t")
			for len(f) < 6 {
				f = append(f, "")
			}
			orNil := func(s string) any {
				if s == "" {
					return nil
				}
				return s
			}
			num := func(s string) any {
				if s == "" {
					return nil
				}
				if n := jscompat.ParseInt(s, 10); n == n && n != 0 {
					return n
				}
				return nil
			}
			info = jsvalue.NewObject("cwd", orNil(f[0]), "command", orNil(f[1]), "pid", num(f[2]), "width", num(f[3]), "height", num(f[4]),
				"sessionCreated", num(f[5]))
		}
		_, last := machine.SessionCwd(d.Tmux, name)
		var lastCommand any
		if last != nil {
			lastCommand = *last
		}
		info.Set("lastCommand", lastCommand)
		info.Set("sessionName", name)
		return stateAnswer(r, 200, info, nil)
	}
}

// --- the page's socket telemetry -----------------------------------------------------------------------------------------

var (
	netKeyRe = regexp.MustCompile(`^[\w:.-]{1,64}$`)
	mobileRe = regexp.MustCompile(`(?i)Android|iPhone|iPad`)
)

func netCount(v any) float64 {
	n, ok := v.(float64)
	if !ok || math.IsInf(n, 0) || math.Trunc(n) != n || n < 0 {
		return 0
	}
	return min(n, 10_000)
}

// netEvents: what the page saw of its sockets over the last minute, logged as one line.
func netEvents(d deps) settings.Handler {
	return func(r *http.Request, body any) (int, any, http.Header) {
		if r.Method != http.MethodPost {
			return empty(http.StatusMethodNotAllowed, http.Header{"Allow": {"POST"}})
		}
		if !d.SessionOK(r) {
			return empty(http.StatusForbidden, nil)
		}
		if text, ok := body.(string); ok {
			body, _ = jsvalue.Parse([]byte(text))
		}
		raw := get(body, "sockets")
		all := jsvalue.NewObject()
		switch raw.(type) {
		case *jsvalue.Object, []any:
			settings.Spread(all, raw)
		default:
			return empty(http.StatusBadRequest, nil)
		}
		sockets := jsvalue.NewObject()
		for i, key := range all.Keys() {
			if i >= 24 {
				break
			}
			v, _ := all.Get(key)
			switch v.(type) {
			case *jsvalue.Object, []any:
			default:
				continue
			}
			if !netKeyRe.MatchString(key) {
				continue
			}
			downMs := []any{}
			if arr, ok := get(v, "downMs").([]any); ok {
				for j, e := range arr {
					if j >= 20 {
						break
					}
					downMs = append(downMs, netCount(e))
				}
			}
			sockets.Set(key, jsvalue.NewObject("drops", netCount(get(v, "drops")), "dead", netCount(get(v, "dead")),
				"recovered", netCount(get(v, "recovered")), "downMs", downMs))
		}
		agent.Log("info", "socket report", agent.F{K: "sockets", V: sockets}, agent.F{K: "mobile", V: mobileRe.MatchString(r.Header.Get("User-Agent"))},
			agent.F{K: "hidden", V: get(body, "hidden") == true}, agent.F{K: "online", V: get(body, "online") != false})
		return empty(http.StatusNoContent, nil)
	}
}

// --- dead routes, answered as the old runtime answered them ---------------------------------------------------------------

// dailyReportCache: every cached daily report (no page asks for it any more).
func dailyReportCache(d deps) settings.Handler {
	return func(r *http.Request, _ any) (int, any, http.Header) {
		if r.Method != http.MethodGet {
			return stateAnswer(r, http.StatusMethodNotAllowed, errBody("method-not-allowed"), http.Header{"Allow": {"GET"}})
		}
		days := jsvalue.NewObject()
		dir := filepath.Join(d.DataDir, "stats", "daily-reports")
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
			if err != nil {
				continue
			}
			date, brief := get(v, "date"), get(v, "brief")
			if jsvalue.Truthy(date) && jsvalue.Truthy(brief) {
				days.Set(jscompat.String(date), v)
			}
		}
		return stateAnswer(r, 200, jsvalue.NewObject("days", days), nil)
	}
}

// reactScan: the render inspector script a developer's page may ask for, from the release when it ships one.
func reactScan() settings.Handler {
	file := os.Getenv("RUNTIME_REACT_SCAN_FILE")
	var mu sync.Mutex
	var script []byte
	return func(r *http.Request, _ any) (int, any, http.Header) {
		mu.Lock()
		if script == nil && file != "" {
			script, _ = os.ReadFile(file)
		}
		s := script
		mu.Unlock()
		if s == nil {
			return stateAnswer(r, 404, errBody("react-scan is not installed"), nil)
		}
		return settings.AnswerRaw(r, 200, "text/javascript; charset=utf-8", s, http.Header{"Cache-Control": {"private, max-age=3600"}})
	}
}

// --- sign-in: the cluster signs the sessions; these pages are not served any more --------------------------------------------

type signIn struct {
	mu       sync.Mutex
	failures int
	first    time.Time
	initMode bool
	initHash string
	setup    sync.Mutex
}

func authRoutes(d deps, store *settings.Store, open func(settings.Handler) func(*http.Request) (int, any, http.Header)) []gate.JSONRoute {
	s := &signIn{}
	if p := os.Getenv("INIT_PASSWORD"); len(utf16.Encode([]rune(p))) >= 4 {
		cfg, _ := store.ReadConfig().(*jsvalue.Object)
		pw, _ := cfg.Get("authPassword")
		secret, _ := cfg.Get("authSecret")
		if !settings.IsHashedPassword(pw) || !jsvalue.Truthy(secret) {
			s.initMode = true
			s.initHash = settings.HashPassword(p)
		}
	}
	route := func(p string, h settings.Handler) gate.JSONRoute {
		return gate.JSONRoute{Slice: settings.Slice, Match: func(r *http.Request) bool { return pathIs(r, p) }, Compute: open(h)}
	}
	needsSetup := func() bool { return !settings.IsHashedPassword(get(store.ReadConfig(), "authPassword")) }
	hostLocked := func() bool { return jscompat.Trim(accessHost()) != "" }
	return []gate.JSONRoute{
		route("/api/auth/login", func(r *http.Request, body any) (int, any, http.Header) {
			if r.Method != http.MethodPost {
				return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"POST"}})
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			const window = 15 * time.Minute
			if s.failures > 0 && time.Since(s.first) > window {
				s.failures = 0
			}
			if s.failures >= 16 {
				return stateAnswer(r, http.StatusTooManyRequests, errBody("Too many login attempts. Please try again later."), nil)
			}
			if body == nil {
				body = jsvalue.NewObject()
			}
			password := get(body, "password")
			stored := os.Getenv("AUTH_PASSWORD")
			if pw, _ := get(store.ReadConfig(), "authPassword").(string); settings.IsHashedPassword(pw) {
				stored = pw
			} else if s.initMode {
				stored = s.initHash
			}
			ok := false
			if jsvalue.Truthy(password) && stored != "" {
				p, isStr := password.(string)
				if !isStr && settings.IsHashedPassword(stored) {
					return http.StatusInternalServerError, handlerFailed, nil // scrypt of a non-string threw
				}
				ok = isStr && settings.VerifyPassword(p, stored)
			}
			if !ok {
				if s.failures == 0 || time.Since(s.first) > window {
					s.failures, s.first = 1, time.Now()
				} else {
					s.failures++
				}
				return stateAnswer(r, http.StatusUnauthorized, errBody("Invalid password."), nil)
			}
			s.failures = 0
			cookie := d.Cookie + "=" + d.SignSession() + "; HttpOnly; SameSite=Lax; Path=/; Max-Age=" + strconv.Itoa(int(sessionMaxAge/time.Second))
			if r.Header.Get("X-Forwarded-Proto") == "https" {
				cookie += "; Secure"
			}
			return stateAnswer(r, 200, jsvalue.NewObject("ok", true), http.Header{"Set-Cookie": {cookie}})
		}),
		route("/api/auth/logout", func(r *http.Request, _ any) (int, any, http.Header) {
			return stateAnswer(r, 200, jsvalue.NewObject("ok", true), http.Header{"Set-Cookie": {d.Cookie + "=; HttpOnly; SameSite=Lax; Path=/; Max-Age=0"}})
		}),
		route("/api/auth/preflight", func(r *http.Request, _ any) (int, any, http.Header) {
			if r.Method != http.MethodGet {
				return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"GET"}})
			}
			if !needsSetup() && !d.Old.CLITokenOK(r) && !d.SessionOK(r) {
				return stateAnswer(r, http.StatusUnauthorized, errBody("Unauthorized"), nil)
			}
			res := d.Tools.Runtime()
			return stateAnswer(r, 200, struct {
				Tmux preflight.Tmux `json:"tmux"`
				Git  preflight.Tool `json:"git"`
			}{res.Tmux, res.Git}, nil)
		}),
		route("/api/auth/setup", func(r *http.Request, body any) (int, any, http.Header) {
			switch r.Method {
			case http.MethodGet:
				setup := needsSetup()
				return stateAnswer(r, 200, jsvalue.NewObject("needsSetup", setup, "requiresAuth", setup && s.initMode && !d.SessionOK(r),
					"hostEnvLocked", hostLocked()), nil)
			case http.MethodPost:
				if body == nil {
					body = jsvalue.NewObject()
				}
				pw, isStr := get(body, "authPassword").(string)
				if !isStr || pw == "" {
					return stateAnswer(r, 400, errBody("Password is required."), nil)
				}
				access := get(body, "networkAccess")
				valid := access == "localhost" || access == "tailscale" || access == "all"
				s.setup.Lock()
				defer s.setup.Unlock()
				if !needsSetup() {
					return stateAnswer(r, 400, errBody("Setup already completed."), nil)
				}
				if s.initMode && !d.SessionOK(r) {
					return stateAnswer(r, 401, errBody("Authentication required."), nil)
				}
				hashed := store.HashPassword(pw)
				or := func(k string, fallback any) any {
					if v := get(body, k); jsvalue.Truthy(v) {
						return v
					}
					return fallback
				}
				skip := get(body, "dangerouslySkipPermissions")
				if o, ok := body.(*jsvalue.Object); !ok || !o.Has("dangerouslySkipPermissions") || skip == nil {
					skip = false
				}
				theme, hasTheme := any(jsvalue.Undefined), false
				if o, ok := body.(*jsvalue.Object); ok {
					theme, hasTheme = o.Get("terminalTheme")
				}
				if !hasTheme {
					theme = jsvalue.Undefined
				}
				// The pair's secret is replaced in the file as before; this process keeps the one it started with.
				updates := jsvalue.NewObject("authPassword", hashed, "authSecret", store.Secret(), "locale", or("locale", "en"),
					"appTheme", or("appTheme", "dark"))
				err := store.ChangeConfig(func(data *jsvalue.Object) {
					data.Assign(updates)
					data.Set("terminalTheme", theme)
					data.Set("dangerouslySkipPermissions", skip)
					if valid {
						data.Set("networkAccess", access)
					}
				})
				if err != nil {
					return http.StatusInternalServerError, handlerFailed, nil
				}
				s.initMode = false
				return stateAnswer(r, 200, jsvalue.NewObject("ok", true), nil)
			}
			return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"GET, POST"}})
		}),
	}
}

// --- the desktop app's browser tabs: there is no browser bridge on a machine -------------------------------------------------

var browserTabRe = regexp.MustCompile(`^/api/cli/tabs/([^/]+)/browser/(console|eval|network|screenshot|url)$`)

func (c *core) browserTabRoutes(d deps) []gate.JSONRoute {
	match := func(r *http.Request) (tab, what string, ok bool) {
		m := browserTabRe.FindStringSubmatch(strings.TrimPrefix(r.URL.Path, "/agents"))
		if m == nil {
			return "", "", false
		}
		return m[1], m[2], true
	}
	return []gate.JSONRoute{{
		Slice: settings.Slice,
		Match: func(r *http.Request) bool { _, _, ok := match(r); return ok && c.manager() != nil },
		Compute: settings.Guarded(nil, func(r *http.Request, _ any) (int, any, http.Header) {
			tabID, what, _ := match(r)
			method := http.MethodGet
			if what == "eval" {
				method = http.MethodPost
			}
			if r.Method != method {
				return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {method}})
			}
			if !d.Old.CLITokenOK(r) {
				return stateAnswer(r, http.StatusForbidden, errBody("Forbidden"), nil)
			}
			ws := settings.Query(r, "workspaceId")
			if ws.Array() || !ws.Truthy() {
				return stateAnswer(r, 400, errBody("workspaceId is required"), nil)
			}
			if c.store.Workspace(ws.Values[0]) == nil {
				return stateAnswer(r, 404, errBody("Tab not found"), nil)
			}
			layout, err := c.store.GetLayout(ws.Values[0], "")
			if err != nil {
				return http.StatusInternalServerError, handlerFailed, nil
			}
			for _, t := range state.AllTabs(layout) {
				if t.String("id") != tabID {
					continue
				}
				if t.String("panelType") != "web-browser" {
					return stateAnswer(r, 400, errBody("Tab is not a web-browser panel"), nil)
				}
				return stateAnswer(r, http.StatusServiceUnavailable, errBody("Browser bridge unavailable (Electron-only feature)"), nil)
			}
			return stateAnswer(r, 404, errBody("Tab not found"), nil)
		}),
	}}
}

// --- tailscale: the settings tab of a machine reached over tailscale ------------------------------------------------------

var tailscaleVersionRe = regexp.MustCompile(`^(\d+\.\d+[\d.]*)`)

// runTool runs a program as Node's execFile did and returns its stdout, or the message its error carried.
func runTool(timeout time.Duration, name string, args ...string) (string, string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", "spawn " + name + " ENOENT", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), "Command failed: " + strings.Join(append([]string{name}, args...), " ") + "\n" + stderr.String(), false
	}
	return stdout.String(), "", true
}

func tailscaleRoutes(d deps, guarded func(settings.Handler) func(*http.Request) (int, any, http.Header)) []gate.JSONRoute {
	port := d.RuntimePort
	if port == 0 {
		port = portOf(d.Upstream)
	}
	status := func(r *http.Request, _ any) (int, any, http.Header) {
		if r.Method != http.MethodGet {
			return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"GET"}})
		}
		res := jsvalue.NewObject("installed", false, "running", false, "version", nil, "hostname", nil, "dnsName", nil,
			"tailscaleIp", nil, "serveEntries", []any{}, "serverPort", float64(port))
		out, _, ok := runTool(5*time.Second, "tailscale", "version")
		if !ok {
			return stateAnswer(r, 200, res, nil)
		}
		res.Set("installed", true)
		if m := tailscaleVersionRe.FindStringSubmatch(jscompat.Trim(out)); m != nil {
			res.Set("version", m[1])
		}
		out, _, ok = runTool(5*time.Second, "tailscale", "status", "--json")
		st, err := jsvalue.Parse([]byte(out))
		if !ok || err != nil {
			return stateAnswer(r, 200, res, nil)
		}
		self := get(st, "Self")
		res.Set("running", get(st, "BackendState") == "Running")
		res.Set("hostname", get(self, "HostName"))
		dns, _ := get(self, "DNSName").(string)
		if dns = strings.TrimSuffix(dns, "."); dns != "" {
			res.Set("dnsName", dns)
		}
		if ips, ok := get(self, "TailscaleIPs").([]any); ok && len(ips) > 0 {
			res.Set("tailscaleIp", ips[0])
		}
		out, _, ok = runTool(5*time.Second, "tailscale", "serve", "status", "--json")
		serve, err := jsvalue.Parse([]byte(out))
		if !ok || err != nil {
			return stateAnswer(r, 200, res, nil)
		}
		entries := []any{}
		if web, isObj := get(serve, "Web").(*jsvalue.Object); isObj {
			portRe := regexp.MustCompile(`:(\d+)$`)
			for _, hostPort := range web.Keys() {
				cfg, _ := web.Get(hostPort)
				httpsPort := "443"
				if m := portRe.FindStringSubmatch(hostPort); m != nil {
					httpsPort = m[1]
				}
				if proxy := get(get(get(cfg, "Handlers"), "/"), "Proxy"); jsvalue.Truthy(proxy) {
					entries = append(entries, jsvalue.NewObject("httpsPort", httpsPort, "proxy", proxy))
				}
			}
		}
		res.Set("serveEntries", entries)
		return stateAnswer(r, 200, res, nil)
	}
	parsePort := func(v any) (int, bool) {
		n, ok := jsNumber(v)
		return int(n), ok && n == float64(int64(n)) && n > 0 && n <= 65535
	}
	serve := func(r *http.Request, body any) (int, any, http.Header) {
		bad := func() (int, any, http.Header) {
			return stateAnswer(r, 400, errBody("Valid port number required (1-65535)"), nil)
		}
		run := func(args ...string) (int, any, http.Header) {
			if _, msg, ok := runTool(10*time.Second, "tailscale", args...); !ok {
				return stateAnswer(r, 500, errBody(msg), nil)
			}
			return stateAnswer(r, 200, jsvalue.NewObject("ok", true), nil)
		}
		switch r.Method {
		case http.MethodPost:
			https, ok1 := parsePort(get(body, "httpsPort"))
			local, ok2 := parsePort(get(body, "localPort"))
			if !ok1 || !ok2 {
				return bad()
			}
			return run("serve", "--bg", "--https="+strconv.Itoa(https), "http://localhost:"+strconv.Itoa(local))
		case http.MethodDelete:
			https, ok := parsePort(get(body, "httpsPort"))
			if !ok {
				return bad()
			}
			return run("serve", "off", "--https="+strconv.Itoa(https))
		}
		return stateAnswer(r, http.StatusMethodNotAllowed, errBody("Method not allowed"), http.Header{"Allow": {"POST, DELETE"}})
	}
	return []gate.JSONRoute{
		{Slice: settings.Slice, Match: func(r *http.Request) bool { return pathIs(r, "/api/tailscale/status") }, Compute: guarded(status)},
		{Slice: settings.Slice, Match: func(r *http.Request) bool { return pathIs(r, "/api/tailscale/serve") }, Compute: guarded(serve)},
	}
}

// jsNumber is Number(v) for a JSON value.
func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		t := jscompat.Trim(x)
		if t == "" {
			return 0, true
		}
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	}
	return 0, false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// builtInAPIGuide is what /api/cli/api-guide answers when the deployment names no guide file.
//
//go:embed api-guide.md
var builtInAPIGuide string
