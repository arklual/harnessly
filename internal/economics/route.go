package economics

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/history"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
)

const (
	cacheFor   = 5 * time.Second
	cacheLimit = 200
	// DefaultWait is how long a request waits for the first read of a large transcript.
	DefaultWait = 1500 * time.Millisecond
)

var tabID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Route answers GET <platform API>/economics?tab=<id>: the economics of the tab's conversation and its provider's
// subscription limits on this machine; {conversation: null, limits: null} for a tab with no agent conversation here.
type Route struct {
	Store *Store
	Stats *history.Stats
	// Authorized is the old API's own check (a page session or the cli-token), SessionOK the page session the handler
	// asks for besides.
	Authorized, SessionOK func(*http.Request) bool
	// FindTab is a tab of this runtime's layouts by id, as stored.
	FindTab func(tabID string) (*jsvalue.Object, bool)
	// Limits is a provider's subscription limits on this machine, written as JSON (nil for none).
	Limits func(provider string) any
	// Wait is how long a first read may take before the answer is "computing" (DefaultWait when 0).
	Wait time.Duration
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time

	mu    sync.Mutex
	order []string
	cache map[string]cachedAnswer
}

type cachedAnswer struct {
	at   time.Time
	body json.RawMessage
}

func (rt *Route) now() time.Time {
	if rt.Now != nil {
		return rt.Now()
	}
	return time.Now()
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

// Answer is the whole answer, refusals included.
func (rt *Route) Answer(r *http.Request) (int, any, http.Header) {
	if !rt.Authorized(r) {
		return http.StatusUnauthorized, gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}, nil
	}
	if !rt.SessionOK(r) {
		return nodejson.Answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	h := http.Header{"Cache-Control": {"no-store"}}
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return nodejson.Answer(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	q := jscompat.Query(r.URL.RawQuery)
	id := ""
	if v := q["tab"]; len(v) == 1 {
		id = v[0]
	}
	if !tabID.MatchString(id) {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверный id диалога"), h)
	}
	fresh := len(q["fresh"]) == 1 && q["fresh"][0] == "1"
	now := rt.now()
	rt.mu.Lock()
	c, hit := rt.cache[id]
	rt.mu.Unlock()
	if hit && !fresh && now.Sub(c.at) < cacheFor {
		return nodejson.Answer(r, http.StatusOK, c.body, h)
	}
	none := json.RawMessage(`{"conversation":null,"limits":null}`)
	tab, found := rt.FindTab(id)
	agentState := get(tab, "agentState")
	provider, _ := get(agentState, "providerId").(string)
	if !found || !jsvalue.Truthy(agentState) || (provider != "claude" && provider != "codex") {
		return nodejson.Answer(r, http.StatusOK, none, h)
	}
	field := func(k string) any {
		if v, ok := agentState.(*jsvalue.Object).Get(k); ok {
			return v
		}
		return jsvalue.Undefined
	}
	wait := rt.Wait
	if wait <= 0 {
		wait = DefaultWait
	}
	nowMs := func() float64 { return float64(rt.now().UnixMilli()) }
	conv, err := Conversation(rt.Store, rt.Stats, Source{Provider: provider, SessionID: field("sessionId"), JSONLPath: field("jsonlPath")}, wait, nowMs)
	if err != nil {
		return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, h
	}
	conv.Set("tabId", id)
	var limits any
	if rt.Limits != nil {
		limits = rt.Limits(provider)
	}
	body := json.RawMessage(`{"conversation":` + string(jsvalue.Stringify(conv, "")) + `,"limits":` + string(nodejson.Marshal(limits)) + `}`)
	rt.mu.Lock()
	if rt.cache == nil {
		rt.cache = map[string]cachedAnswer{}
	}
	if conv.String("status") == "ready" {
		if _, ok := rt.cache[id]; !ok {
			rt.order = append(rt.order, id)
		}
		rt.cache[id] = cachedAnswer{at: rt.now(), body: body}
	} else {
		rt.forget(id)
	}
	if len(rt.order) > cacheLimit {
		rt.forget(rt.order[0])
	}
	rt.mu.Unlock()
	return nodejson.Answer(r, http.StatusOK, body, h)
}

func (rt *Route) forget(id string) {
	if _, ok := rt.cache[id]; !ok {
		return
	}
	delete(rt.cache, id)
	for i, k := range rt.order {
		if k == id {
			rt.order = append(rt.order[:i:i], rt.order[i+1:]...)
			break
		}
	}
}
