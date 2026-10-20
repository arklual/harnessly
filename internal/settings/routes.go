package settings

import (
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Slice is the gate slice of these routes.
const Slice = "state"

// Config is what the routes need; product names come from the deployment.
type Config struct {
	Store *Store
	// LayoutKey is the config's field of the workspace's column widths, DefaultsKey the one of the remembered launch
	// choices per agent CLI.
	LayoutKey, DefaultsKey string
	// AccessHost is the deployment's explicit access rule (the previous runtime's HOST); "" when there is none.
	AccessHost string
	// BindsLoopback: the previous runtime would have listened on the loopback address only.
	BindsLoopback bool
	// ValidateLaunch checks a launch choice against this machine's catalog (nil when the shape is "Auto").
	ValidateLaunch func(provider string, model, effort any) (*agents.Launch, string)
	APIRefusal     func(*http.Request) (status int, body any, h http.Header, refused bool)
}

func pathIs(r *http.Request, p string) bool { return r.URL.Path == p || r.URL.Path == "/agents"+p }

func errorBody(msg string) *jsvalue.Object { return jsvalue.NewObject("error", msg) }

// Answer is a JSON answer as the previous dispatcher sent it: its ETag, and 304 when the caller holds it already (the
// 304 keeps the JSON content type the handler had set).
func Answer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	status, body, h := nodejson.Answer(r, status, v, h)
	if status == http.StatusNotModified {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	return status, body, h
}

func answer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	return Answer(r, status, v, h)
}

// AnswerRaw is a body the dispatcher sent as bytes: its ETag over them, and 304 when the caller holds it already.
func AnswerRaw(r *http.Request, status int, contentType string, body []byte, h http.Header) (int, any, http.Header) {
	if h == nil {
		h = http.Header{}
	}
	tag := nodejson.ETag(body)
	h.Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		h.Set("Content-Type", contentType)
		return http.StatusNotModified, nil, h
	}
	return status, gate.Raw{ContentType: contentType, Body: body}, h
}

func methodNotAllowed(r *http.Request, allow string) (int, any, http.Header) {
	return answer(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), http.Header{"Allow": {allow}})
}

// failed is the previous dispatcher's answer when a handler threw.
func failed() (int, any, http.Header) {
	return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, nil
}

// Handler is a route's own part, after the dispatcher's authorization and body parsing.
type Handler func(r *http.Request, body any) (int, any, http.Header)

// Guarded runs a handler behind the previous dispatcher: the session or cli-token check, then the body (parsed for
// every method, as the dispatcher did).
func Guarded(refusal func(*http.Request) (int, any, http.Header, bool), h Handler) func(*http.Request) (int, any, http.Header) {
	return func(r *http.Request) (int, any, http.Header) {
		if refusal != nil {
			if status, v, hh, refused := refusal(r); refused {
				return status, v, hh
			}
		}
		body, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb")
		if status != 0 {
			return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
		}
		return h(r, body)
	}
}

// Query is req.query[name] of the request as sent.
func Query(r *http.Request, name string) jscompat.Param {
	return jscompat.Param{Values: jscompat.Query(rawQuery(r))[name]}
}

func rawQuery(r *http.Request) string {
	t := r.RequestURI
	if !strings.HasPrefix(t, "/") {
		t = r.URL.RequestURI()
	}
	t, _, _ = strings.Cut(t, "#")
	_, q, _ := strings.Cut(t, "?")
	return q
}

// in is JavaScript's `key in body` for a parsed body; thrown is true where `in` throws (a string, a number, a boolean).
func in(body any, key string) (has, thrown bool) {
	switch b := body.(type) {
	case *jsvalue.Object:
		return b.Has(key), false
	case []any:
		return false, false
	}
	return false, true
}

func get(body any, key string) any {
	if o, ok := body.(*jsvalue.Object); ok {
		v, _ := o.Get(key)
		return v
	}
	return nil
}

// Routes are the slice's routes; their GETs also run in the "state-shadow" slice.
func Routes(cfg Config) []gate.JSONRoute {
	route := func(path string, h Handler) gate.JSONRoute {
		return gate.JSONRoute{Slice: Slice, Match: func(r *http.Request) bool { return pathIs(r, path) }, Compute: Guarded(cfg.APIRefusal, h)}
	}
	config := route("/api/config", cfg.config)
	// A config the previous runtime had not written yet is stamped at the moment of the read.
	config.Volatile = []string{"updatedAt"}
	return []gate.JSONRoute{
		config,
		route("/api/keybindings", cfg.keybindings),
		route("/api/message-history", cfg.messageHistory),
		route("/api/quick-prompts", cfg.quickPrompts),
		route("/api/sidebar-items", cfg.sidebarItems),
	}
}

// --- the config -----------------------------------------------------------------------------------------------------------

func (cfg Config) allowedFields() []string {
	return []string{"appTheme", "terminalTheme", "customCSS", "dangerouslySkipPermissions", "claudeShowTerminal", "gitAskProvider",
		"noteSummaryProvider", "editorUrl", "editorPreset", "authPassword", "notificationsEnabled", "toastOnCompleteEnabled",
		"toastDuration", "toastPositionDesktop", "toastPositionMobile", "locale", "fontSize", "lineHeight", "lineHeightCustom",
		"terminalKeyBar", "systemResourcesEnabled", "networkAccess", cfg.LayoutKey, "voiceSettings"}
}

var (
	editorPresets  = []string{"code-server", "vscode", "vscode-insiders", "cursor", "windsurf", "zed", "custom", "off"}
	toastPositions = []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}
	voiceEngines   = []string{"auto", "local", "browser", "server"}
	voiceLangs     = []string{"ru", "en", "auto"}
	voiceModes     = []string{"hold", "toggle"}
)

func oneOf(v any, list ...string) bool {
	s, ok := v.(string)
	return ok && slices.Contains(list, s)
}

func (cfg Config) config(r *http.Request, body any) (int, any, http.Header) {
	switch r.Method {
	case http.MethodGet:
		return cfg.readConfig(r)
	case http.MethodPatch:
		return cfg.patchConfig(r, body)
	}
	return methodNotAllowed(r, "GET, PATCH")
}

func (cfg Config) readConfig(r *http.Request) (int, any, http.Header) {
	data := cfg.Store.Config()
	safe := jsvalue.NewObject()
	Spread(safe, data)
	var password any
	if o, ok := data.(*jsvalue.Object); ok {
		password, _ = o.Get("authPassword")
	}
	for _, k := range []string{"authPassword", "authSecret", "automationApiToken"} {
		safe.Delete(k)
	}
	safe.Set("hasAuthPassword", jsvalue.Truthy(password))
	safe.Set("hostEnvLocked", jscompat.Trim(cfg.AccessHost) != "")
	safe.Set("bindHostIsLocal", cfg.BindsLoopback)
	return answer(r, 200, safe, nil)
}

// width is a column width clamped to its range and rounded; undefined for anything but a finite number.
func width(v any, lo, hi float64) any {
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return jsvalue.Undefined
	}
	return math.Floor(math.Min(hi, math.Max(lo, n)) + 0.5)
}

func validVoicePatch(v any) bool {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return false
	}
	check := func(k string, valid func(any) bool) bool {
		x, has := o.Get(k)
		return !has || valid(x)
	}
	isBool := func(x any) bool { _, ok := x.(bool); return ok }
	return check("engine", func(x any) bool { return oneOf(x, voiceEngines...) }) &&
		check("lang", func(x any) bool { return oneOf(x, voiceLangs...) }) &&
		check("mode", func(x any) bool { return oneOf(x, voiceModes...) }) &&
		check("autoSend", isBool) && check("punctuation", isBool) &&
		check("deviceId", func(x any) bool { _, s := x.(string); return x == nil || s }) &&
		check("key", func(x any) bool { return x == nil || assignableVoiceKey(x) })
}

// assignableVoiceKey: a lone modifier with its side, a function key or a key that types nothing.
func assignableVoiceKey(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, m := range []string{"Alt", "Control", "Shift", "Meta"} {
		if s == m+"Left" || s == m+"Right" {
			return true
		}
	}
	if n, ok := strings.CutPrefix(s, "F"); ok && n != "" && n[0] != '0' && len(n) <= 2 {
		k := 0
		for _, c := range n {
			if c < '0' || c > '9' {
				return false
			}
			k = k*10 + int(c-'0')
		}
		return k >= 1 && k <= 24
	}
	return slices.Contains([]string{"Pause", "ScrollLock", "Insert", "ContextMenu"}, s)
}

func (cfg Config) patchConfig(r *http.Request, raw any) (int, any, http.Header) {
	body := raw
	if body == nil {
		body = jsvalue.NewObject()
	}
	bad := func(msg string) (int, any, http.Header) { return answer(r, 400, errorBody(msg), nil) }
	set := func(k string, v any) {
		if o, ok := body.(*jsvalue.Object); ok {
			o.Set(k, v)
		}
	}
	has, thrown := in(body, cfg.LayoutKey)
	if thrown {
		return failed()
	}
	if has {
		layout := get(body, cfg.LayoutKey)
		switch layout.(type) {
		case *jsvalue.Object, []any:
		default:
			return bad("Invalid " + cfg.LayoutKey)
		}
		o := jsvalue.NewObject("left", width(get(layout, "left"), 200, 480), "right", width(get(layout, "right"), 280, 3200),
			"collapsed", get(layout, "collapsed") == true)
		set(cfg.LayoutKey, o)
	}
	if has, _ := in(body, "voiceSettings"); has {
		patch := get(body, "voiceSettings")
		if !validVoicePatch(patch) {
			return bad("Invalid voiceSettings")
		}
		merged := jsvalue.NewObject()
		Spread(merged, get(cfg.Store.Config(), "voiceSettings"))
		Spread(merged, patch)
		set("voiceSettings", merged)
	}
	if has, _ := in(body, cfg.DefaultsKey); has {
		defaults, ok := get(body, cfg.DefaultsKey).(*jsvalue.Object)
		if !ok {
			return bad(locale.Pick("Invalid launch defaults", "Неверные умолчания запуска"))
		}
		patch := jsvalue.NewObject()
		for _, provider := range defaults.Keys() {
			choice, _ := defaults.Get(provider)
			if provider != agents.Claude && provider != agents.Codex {
				return bad(locale.Pick("Unknown agent", "Неизвестный агент"))
			}
			switch choice.(type) {
			case *jsvalue.Object, []any:
			default:
				return bad(locale.Pick("Invalid launch defaults", "Неверные умолчания запуска"))
			}
			model, effort := get(choice, "model"), get(choice, "effort")
			var launch *agents.Launch
			if model != nil || effort != nil {
				var why string
				if launch, why = cfg.ValidateLaunch(provider, model, effort); why != "" {
					return bad(why)
				}
			}
			if launch == nil {
				launch = &agents.Launch{}
			}
			patch.Set(provider, jsvalue.NewObject("model", strOrNil(launch.Model), "effort", strOrNil(launch.Effort)))
		}
		err := cfg.Store.ChangeConfig(func(data *jsvalue.Object) {
			merged := jsvalue.NewObject()
			if v, ok := data.Get(cfg.DefaultsKey); ok {
				Spread(merged, v)
			}
			merged.Assign(patch)
			data.Set(cfg.DefaultsKey, merged)
		})
		if err != nil {
			return failed()
		}
	}
	updates := jsvalue.NewObject()
	for _, k := range cfg.allowedFields() {
		if has, _ := in(body, k); has {
			updates.Set(k, get(body, k))
		}
	}
	checks := []struct {
		key, msg string
		valid    func(any) bool
	}{
		{"editorUrl", "editorUrl must be a string.", func(v any) bool { _, ok := v.(string); return ok }},
		{"editorPreset", "editorPreset is invalid.", func(v any) bool { return oneOf(v, editorPresets...) }},
		{"networkAccess", "networkAccess must be one of: localhost, tailscale, all.", func(v any) bool { return oneOf(v, "localhost", "tailscale", "all") }},
		{"gitAskProvider", "gitAskProvider must be one of: claude, codex.", func(v any) bool { return oneOf(v, "claude", "codex") }},
		{"noteSummaryProvider", "noteSummaryProvider must be one of: claude, codex.", func(v any) bool { return oneOf(v, "claude", "codex") }},
		{"toastPositionDesktop", "toastPositionDesktop is invalid.", func(v any) bool { return oneOf(v, toastPositions...) }},
		{"toastPositionMobile", "toastPositionMobile is invalid.", func(v any) bool { return oneOf(v, toastPositions...) }},
		{"toastDuration", "toastDuration must be a number between 1000 and 60000.", func(v any) bool {
			n, ok := v.(float64)
			return ok && !math.IsInf(n, 0) && n >= 1000 && n <= 60000
		}},
	}
	for _, c := range checks {
		if v, ok := updates.Get(c.key); ok && !c.valid(v) {
			return bad(c.msg)
		}
	}
	// A password set here is hashed and replaces the pair's secret in the file, as before; this process keeps checking
	// sessions with the secret it started with, so the sessions the cluster signs stay valid until a restart.
	if p, ok := updates.Get("authPassword"); ok && jsvalue.Truthy(p) && isString(p) {
		updates.Set("authPassword", cfg.Store.HashPassword(p.(string)))
		updates.Set("authSecret", cfg.Store.Secret())
	} else {
		updates.Delete("authPassword")
	}
	if err := cfg.Store.UpdateConfig(updates); err != nil {
		return failed()
	}
	return answer(r, 200, jsvalue.NewObject("ok", true), nil)
}

func isString(v any) bool { _, ok := v.(string); return ok }

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// --- keyboard shortcuts ----------------------------------------------------------------------------------------------------

// Actions are the ids of the page's keyboard actions.
var Actions = []string{"workspace.new", "workspace.rename", "workspace.switch", "tab.new", "tab.close", "tab.prev", "tab.next",
	"tab.goto", "pane.split_right", "pane.split_down", "pane.focus_left", "pane.focus_right", "pane.focus_up", "pane.focus_down",
	"pane.clear_screen", "pane.resize_left", "pane.resize_right", "pane.resize_up", "pane.resize_down", "pane.equalize",
	"panel.focus_input", "view.toggle_sidebar", "view.toggle_sidebar_tab", "view.notes", "view.stats", "view.mode_terminal",
	"view.mode_claude", "view.mode_codex", "view.mode_diff", "app.settings", "app.voice_hold", "app.new_window"}

func (cfg Config) keybindings(r *http.Request, body any) (int, any, http.Header) {
	switch r.Method {
	case http.MethodGet:
		return answer(r, 200, cfg.Store.Keybindings(), nil)
	case http.MethodPatch:
		id, key := get(body, "id"), any(jsvalue.Undefined)
		if o, ok := body.(*jsvalue.Object); ok && o.Has("key") {
			key, _ = o.Get("key")
		}
		ids, _ := id.(string)
		if ids == "" || !slices.Contains(Actions, ids) {
			return answer(r, 400, errorBody("unknown action id"), nil)
		}
		if _, isStr := key.(string); key != nil && !isStr {
			return answer(r, 400, errorBody("key must be string or null"), nil)
		}
		data, err := cfg.Store.SetKeybinding(ids, key)
		if err != nil {
			return failed()
		}
		return answer(r, 200, data, nil)
	case http.MethodDelete:
		id := Query(r, "id")
		var data *jsvalue.Object
		var err error
		if !id.Array() && id.Truthy() {
			if !slices.Contains(Actions, id.Values[0]) {
				return answer(r, 400, errorBody("unknown action id"), nil)
			}
			data, err = cfg.Store.ResetKeybinding(id.Values[0])
		} else {
			data, err = cfg.Store.ResetAllKeybindings()
		}
		if err != nil {
			return failed()
		}
		return answer(r, 200, data, nil)
	}
	return methodNotAllowed(r, "GET, PATCH, DELETE")
}

// --- sent messages ---------------------------------------------------------------------------------------------------------

func (cfg Config) messageHistory(r *http.Request, body any) (int, any, http.Header) {
	required := func(what string) (int, any, http.Header) { return answer(r, 400, errorBody(what+" is required"), nil) }
	switch r.Method {
	case http.MethodGet:
		ws := Query(r, "wsId")
		if !ws.Truthy() {
			return required("wsId")
		}
		if ws.Array() {
			return failed() // path.join of an array threw
		}
		if !cfg.Store.ValidWorkspace(ws.Values[0]) {
			return required("wsId")
		}
		return answer(r, 200, jsvalue.NewObject("entries", cfg.Store.MessageHistory(ws.Values[0])), nil)
	case http.MethodPost, http.MethodDelete:
		if body == nil {
			return failed() // destructuring null threw
		}
		ws := get(body, "wsId")
		if !jsvalue.Truthy(ws) {
			return required("wsId")
		}
		if r.Method == http.MethodPost {
			msg := get(body, "message")
			if msg == nil {
				return required("message")
			}
			m, ok := msg.(string)
			if !ok {
				return failed() // message.trim is not a function
			}
			if jscompat.Trim(m) == "" {
				return required("message")
			}
			wsID, ok := ws.(string)
			if !ok {
				return answer(r, 500, errorBody("Failed to save"), nil) // path.join of a non-string threw
			}
			if !cfg.Store.ValidWorkspace(wsID) {
				return required("wsId")
			}
			entry, err := cfg.Store.AddMessage(wsID, m)
			if err != nil {
				return answer(r, 500, errorBody("Failed to save"), nil)
			}
			return answer(r, 201, jsvalue.NewObject("entry", entry), nil)
		}
		id := get(body, "id")
		if !jsvalue.Truthy(id) {
			return required("id")
		}
		wsID, ok := ws.(string)
		if !ok {
			return answer(r, 500, errorBody("Failed to save"), nil)
		}
		if !cfg.Store.ValidWorkspace(wsID) {
			return required("wsId")
		}
		if err := cfg.Store.DeleteMessage(wsID, id); err != nil {
			return answer(r, 500, errorBody("Failed to save"), nil)
		}
		return answer(r, 200, jsvalue.NewObject("success", true), nil)
	}
	return methodNotAllowed(r, "GET, POST, DELETE")
}

// --- quick prompts and sidebar items ----------------------------------------------------------------------------------------

// validListFile: the three lists a quick prompts or sidebar items file must have; thrown where reading them threw.
func validListFile(body any) (ok, thrown bool) {
	if body == nil {
		return false, true
	}
	for _, k := range []string{"custom", "disabledBuiltinIds", "order"} {
		if _, isArr := get(body, k).([]any); !isArr {
			return false, false
		}
	}
	return true, false
}

func (cfg Config) quickPrompts(r *http.Request, body any) (int, any, http.Header) {
	return cfg.listFile(r, body, cfg.Store.QuickPrompts, cfg.Store.WriteQuickPrompts)
}

func (cfg Config) sidebarItems(r *http.Request, body any) (int, any, http.Header) {
	return cfg.listFile(r, body, cfg.Store.SidebarItems, cfg.Store.WriteSidebarItems)
}

func (cfg Config) listFile(r *http.Request, body any, read func() *jsvalue.Object, write func(any) error) (int, any, http.Header) {
	switch r.Method {
	case http.MethodGet:
		return answer(r, 200, read(), nil)
	case http.MethodPut:
		ok, thrown := validListFile(body)
		if thrown {
			return failed()
		}
		if !ok {
			return answer(r, 400, errorBody("Invalid format"), nil)
		}
		if err := write(body); err != nil {
			return failed()
		}
		return answer(r, 200, jsvalue.NewObject("success", true), nil)
	}
	return methodNotAllowed(r, "GET, PUT")
}
