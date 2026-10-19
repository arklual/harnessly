package machines

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/push"
)

func jsonText(b []byte) json.RawMessage { return json.RawMessage(b) }

var okBody = jsvalue.NewObject("ok", true)

func (s *service) vapidKey(r *http.Request, _ any) (int, any, http.Header) {
	if r.Method != http.MethodGet {
		return methodNotAllowed(r, "GET")
	}
	key, err := s.cfg.Push.PublicKey()
	if err != nil {
		s.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
		return textAnswer(500, "Internal Server Error")
	}
	return answer(r, 200, jsvalue.NewObject("publicKey", key), nil)
}

func (s *service) subscribe(r *http.Request, body any) (int, any, http.Header) {
	store := s.cfg.Push.Store
	switch r.Method {
	case http.MethodGet:
		return answer(r, 200, jsvalue.NewObject("devices", store.DeviceEndpoints()), nil)
	case http.MethodPost:
		sub, has := field(body, "subscription")
		if nullish(sub, has) {
			sub = body
		}
		endpoint, _ := getOr(sub, "endpoint").(string)
		keys := getOr(sub, "keys")
		if endpoint == "" || !httpsURL.MatchString(endpoint) || !jsvalue.Truthy(getOr(keys, "p256dh")) || !jsvalue.Truthy(getOr(keys, "auth")) {
			return answer(r, 400, errorBody("Invalid subscription"), nil)
		}
		deviceID, _ := getOr(body, "deviceId").(string)
		var mobile *bool
		if m, ok := getOr(body, "mobile").(bool); ok {
			mobile = &m
		}
		if err := store.Add(endpoint, keys, deviceID, mobile); err != nil {
			s.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
			return textAnswer(500, "Internal Server Error")
		}
		return answer(r, 200, okBody, nil)
	case http.MethodDelete:
		endpoint, _ := field(body, "endpoint")
		if !jsvalue.Truthy(endpoint) {
			return answer(r, 400, errorBody("Missing endpoint"), nil)
		}
		if e, ok := endpoint.(string); ok {
			if err := store.Remove(e); err != nil {
				s.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
				return textAnswer(500, "Internal Server Error")
			}
		}
		return answer(r, 200, okBody, nil)
	}
	return methodNotAllowed(r, "GET, POST, DELETE")
}

var httpsURL = regexp.MustCompile(`^https://`)

// registerSession: the page names the push endpoint of the session it shows. Nothing reads that mapping any more, so
// it is answered and not kept.
func (s *service) registerSession(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "POST")
	}
	if id, _ := field(body, "sessionId"); id == nil || id == "" {
		return answer(r, 400, errorBody("Invalid body"), nil)
	} else if _, ok := id.(string); !ok {
		return answer(r, 400, errorBody("Invalid body"), nil)
	}
	return answer(r, 200, okBody, nil)
}

// clipField is a reported string cut to max UTF-16 units, or a boolean as text; nothing else.
func clipField(v any, max int) any {
	switch x := v.(type) {
	case string:
		return jscompat.Slice(x, max)
	case bool:
		return strconv.FormatBool(x)
	}
	return nil
}

// diagnostic: a browser that failed to subscribe says why (the failure happens on the phone, where nothing else is
// logged).
func (s *service) diagnostic(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "POST")
	}
	var kv []any
	for _, f := range []struct {
		k   string
		max int
	}{{"step", 40}, {"name", 80}, {"message", 300}, {"permission", 20}, {"standalone", 5}, {"userAgent", 300}, {"detail", 2000}} {
		v, _ := field(body, f.k)
		if c := clipField(v, f.max); c != nil {
			kv = append(kv, f.k, c)
		}
	}
	s.log("warn", "push subscription failed in the browser", kv...)
	return http.StatusNoContent, nil, nil
}

// viewport: the Home Screen app's geometry as the phone sees it, for layout diagnosis.
func (s *service) viewport(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "POST")
	}
	var kv []any
	if m, ok := getOr(body, "moment").(string); ok {
		kv = append(kv, "moment", jscompat.Slice(m, 20))
	}
	standalone, _ := field(body, "standalone")
	kv = append(kv, "standalone", standalone == true)
	if ua, ok := getOr(body, "userAgent").(string); ok {
		kv = append(kv, "userAgent", jscompat.Slice(ua, 300))
	}
	sizes := map[string]any{}
	if o, ok := getOr(body, "sizes").(*jsvalue.Object); ok {
		for i, k := range o.Keys() {
			if i >= 30 {
				break
			}
			v, _ := o.Get(k)
			if f, ok := v.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
				sizes[jscompat.Slice(k, 30)] = jsRound(f*10) / 10
			} else {
				sizes[jscompat.Slice(k, 30)] = nil
			}
		}
	}
	kv = append(kv, "sizes", sizes)
	s.log("info", "phone viewport", kv...)
	return http.StatusNoContent, nil, nil
}

func (s *service) message(kind, title, body string) push.Message {
	return push.Message{Kind: kind, Title: title, Body: body, ConversationTitle: s.cfg.AppName, Detail: body, Provider: "claude",
		Tag: s.cfg.TagPrefix + kind, At: s.now().UnixMilli()}
}

// pushTest sends the sample notification to the one device that asked, so the person sees the real thing on it.
func (s *service) pushTest(r *http.Request, body any) (int, any, http.Header) {
	if r.Method != http.MethodPost {
		return methodNotAllowed(r, "POST")
	}
	endpoint, _ := field(body, "endpoint")
	e, _ := endpoint.(string)
	var target *push.Subscription
	if _, isString := endpoint.(string); isString {
		for _, sub := range s.cfg.Push.Store.Subscriptions() {
			if sub.Endpoint == e {
				target = &sub
				break
			}
		}
	}
	if target == nil {
		return answer(r, 404, errorBody("Subscription not found"), nil)
	}
	text := "Так будет выглядеть сообщение, когда агент закончит работу или задаст вопрос."
	status, err := s.cfg.Push.Send(*target, s.message("test", "Уведомления включены", text))
	if err != nil {
		s.log("warn", "web push failed", "err", err.Error())
	}
	if status >= 200 && status < 300 {
		return answer(r, 200, okBody, nil)
	}
	return answer(r, 502, jsvalue.NewObject("error", "Push service rejected", "status", float64(status)), nil)
}

var (
	buildID       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	versionNumber = regexp.MustCompile(`^\d{1,4}\.\d{1,5}\.\d{1,6}$`)
)

// releaseNotice: the cluster serves a new build of the pages and tells each runtime once; the runtime pushes the
// release to every device of its person, once per build. Notices are taken one at a time: two cluster replicas
// starting together read the marker one after the other.
func (s *service) releaseNotice(r *http.Request, body any) (int, any, http.Header) {
	if s.cfg.SessionOK == nil || !s.cfg.SessionOK(r) {
		return answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, nil, http.Header{"Allow": {"POST"}}
	}
	build, _ := getOr(body, "build").(string)
	if !buildID.MatchString(build) {
		return answer(r, 400, errorBody("build required"), nil)
	}
	var version *string
	if v, ok := getOr(body, "version").(string); ok && versionNumber.MatchString(v) {
		version = &v
	}
	s.notices.Lock()
	defer s.notices.Unlock()
	file := filepath.Join(s.cfg.DataDir, "release-notice.json")
	if raw, err := os.ReadFile(file); err == nil {
		if v, err := jsvalue.Parse(raw); err == nil {
			if b, ok := getOr(v, "build").(string); ok && b == build {
				return answer(r, 200, jsvalue.NewObject("sent", 0.0, "repeated", true), nil)
			}
		}
	}
	// Marked before the sends: a second replica asking meanwhile must not push the same release twice.
	at, _ := jscompat.ISOString(float64(s.now().UnixMilli()))
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, jsvalue.Stringify(jsvalue.NewObject("build", build, "at", at), ""), 0o644); err != nil {
		s.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
		return textAnswer(500, "Internal Server Error")
	}
	if err := os.Rename(tmp, file); err != nil {
		s.log("error", "api handler failed", "path", r.URL.Path, "err", err.Error())
		return textAnswer(500, "Internal Server Error")
	}
	text := "Нажмите, чтобы обновить"
	if version != nil {
		text = "Версия " + *version + " — нажмите, чтобы обновить"
	}
	msg := s.message("release", "Вышла новая версия "+s.cfg.AppName, text)
	sent := 0
	for _, sub := range s.cfg.Push.Store.Subscriptions() {
		status, err := s.cfg.Push.Send(sub, msg)
		if err != nil {
			s.log("warn", "web push failed", "err", err.Error())
		}
		switch {
		case status == http.StatusNotFound || status == http.StatusGone:
			if err := s.cfg.Push.Store.Remove(sub.Endpoint); err != nil {
				s.log("warn", "push subscription not removed", "err", err.Error())
			}
		case status >= 200 && status < 300:
			sent++
		}
	}
	s.log("info", "release announced", "build", build, "version", version, "sent", sent)
	return answer(r, 200, jsvalue.NewObject("sent", float64(sent), "repeated", false), nil)
}

// docsNotify: the machine agent passes on a new comment or answer in a document of this runtime's person; it is
// pushed to the devices the person watches from, or to all of them.
func (s *service) docsNotify(r *http.Request, body any) (int, any, http.Header) {
	if !s.cfg.MachineAgent(r) {
		return answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, nil, http.Header{"Allow": {"POST"}}
	}
	text := func(k string, max int) string {
		v, _ := getOr(body, k).(string)
		if v == "" {
			return ""
		}
		return push.Clip(v, max)
	}
	url, _ := getOr(body, "url").(string)
	if !strings.HasPrefix(url, s.cfg.DocsReaderPath) {
		url = ""
	}
	title, detail := text("title", 120), text("body", 240)
	if url == "" || title == "" {
		return answer(r, 400, errorBody("Нужны title и url документа"), nil)
	}
	tag := text("tag", 120)
	if tag == "" {
		tag = url
	}
	msg := push.Message{Kind: "doc", Title: title, Body: detail, ConversationTitle: title, Detail: detail, Provider: "claude",
		Tag: s.cfg.TagPrefix + tag, At: s.now().UnixMilli(), URL: url}
	sent := 0
	for _, sub := range push.SelectTargets(s.cfg.Push.Store.Subscriptions(), s.cfg.Push.Store.Visible()) {
		status, err := s.cfg.Push.Send(sub, msg)
		if err != nil {
			s.log("warn", "web push failed", "err", err.Error())
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			if err := s.cfg.Push.Store.Remove(sub.Endpoint); err != nil {
				s.log("warn", "push subscription not removed", "err", err.Error())
			}
			continue
		}
		sent++
	}
	return answer(r, 200, jsvalue.NewObject("sent", float64(sent)), nil)
}
