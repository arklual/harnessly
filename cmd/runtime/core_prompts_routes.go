package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/prompts"
)

// The prompt routes (A.2.1–A.2.8) of the core slice. Each checks what the previous runtime's handler checked, in its
// order: the API's own refusal (no page session and no cli-token: 401), a page session for the person's routes (403),
// the method (405), a JSON body where required (415); the body is read as that runtime read it (JSON, an empty body as
// {}, a form as an object, 1 MB at most). Every JSON answer carries an ETag over its bytes, 304 when the caller has it.

const promptBodyLimit = 1 << 20

// promptRoute is one route's checks and its operation.
type promptRoute struct {
	path    string
	methods []string
	// person: a page session is required (a cli-token alone is refused with 403); forbiddenJSON gives that 403 a body.
	person, forbiddenJSON bool
	// noStore sets Cache-Control: no-store once the session is checked.
	noStore bool
	// allow is the Allow header of a 405 ("" none); methodJSON gives the 405 a body.
	allow      string
	methodJSON bool
	// needJSON: a POST without a JSON content type is refused with 415.
	needJSON bool
	serve    func(svc *prompts.Service, r *http.Request, body []byte) prompts.Response
}

// queryValue is a query parameter as the previous runtime's handlers took it: several values joined by commas.
func queryValue(r *http.Request, key string) string {
	return strings.Join(nodeQuery(r.URL.RawQuery)[key], ",")
}

// readPromptBody is the request body as JSON for the operations: JSON as it is (empty: {}), a form as an object of
// strings (arrays for repeated keys), anything else as no fields. A non-nil answer is the refusal to send instead.
func readPromptBody(r *http.Request) ([]byte, *gateAnswer) {
	if n, err := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64); err == nil && n > promptBodyLimit {
		return nil, &gateAnswer{status: 413, text: "Body exceeded 1mb limit"}
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, promptBodyLimit+1))
	if err != nil {
		return nil, &gateAnswer{status: 400, text: "Invalid body"}
	}
	if len(raw) > promptBodyLimit {
		return nil, &gateAnswer{status: 413, text: "Body exceeded 1mb limit"}
	}
	header := r.Header.Get("Content-Type")
	if header == "" {
		header = "text/plain"
	}
	kind, _, _ := strings.Cut(header, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if len(raw) == 0 {
			return []byte("{}"), nil
		}
		if !json.Valid(raw) {
			return nil, &gateAnswer{status: 400, text: "Invalid JSON"}
		}
		return raw, nil
	case "application/x-www-form-urlencoded":
		form := map[string]any{}
		for k, vs := range nodeQuery(string(raw)) {
			if len(vs) == 1 {
				form[k] = vs[0]
			} else {
				form[k] = vs
			}
		}
		return gate.Encode(form), nil
	}
	return []byte(`""`), nil
}

// gateAnswer is a plain-text refusal of the body reader.
type gateAnswer struct {
	status int
	text   string
}

// writeTagged writes a JSON answer with its ETag (304 when the caller holds it), or an empty one for a nil body.
func writeTagged(w http.ResponseWriter, r *http.Request, status int, body any, h http.Header) {
	if body == nil {
		for k, vs := range h {
			w.Header()[k] = vs
		}
		w.WriteHeader(status)
		return
	}
	status, v, h := tagged(r, status, body, h)
	for k, vs := range h {
		w.Header()[k] = vs
	}
	if v == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(v.(json.RawMessage))
}

// promptRoutes are the routes of internal/prompts, answered while the core slice is on.
func (c *core) promptRoutes(sessionOK func(*http.Request) bool, apiRefusal func(*http.Request) (int, any, http.Header, bool)) []gate.Route {
	post := []string{http.MethodPost}
	get := []string{http.MethodGet}
	bodyOp := func(op func(*prompts.Service, []byte) prompts.Response) func(*prompts.Service, *http.Request, []byte) prompts.Response {
		return func(svc *prompts.Service, _ *http.Request, body []byte) prompts.Response { return op(svc, body) }
	}
	queryOp := func(op func(*prompts.Service, string) prompts.Response) func(*prompts.Service, *http.Request, []byte) prompts.Response {
		return func(svc *prompts.Service, r *http.Request, _ []byte) prompts.Response {
			return op(svc, queryValue(r, "session"))
		}
	}
	// The composer's routes answer 403 and 405 with no body and no Allow.
	composer := func(path string, op func(*prompts.Service, []byte) prompts.Response) promptRoute {
		return promptRoute{path: platformAPI + path, methods: post, person: true, serve: bodyOp(op)}
	}
	raw := func(path string, methods []string, serve func(*prompts.Service, *http.Request, []byte) prompts.Response) promptRoute {
		return promptRoute{path: "/api/tmux/" + path, methods: methods, allow: strings.Join(methods, ", "), methodJSON: true, serve: serve}
	}
	defs := []promptRoute{
		composer("/prompt-queue", (*prompts.Service).PromptQueue),
		composer("/prompt-text", (*prompts.Service).PromptText),
		composer("/send-now", (*prompts.Service).SendNowRoute),
		composer("/interrupt", (*prompts.Service).InterruptRoute),
		{path: platformAPI + "/switch-model", methods: post, person: true, forbiddenJSON: true, noStore: true, allow: "POST", methodJSON: true,
			serve: bodyOp((*prompts.Service).SwitchModel)},
		{path: platformAPI + "/commands/run", methods: post, person: true, forbiddenJSON: true, noStore: true, allow: "POST", methodJSON: true,
			needJSON: true, serve: bodyOp((*prompts.Service).RunCommandRoute)},
		{path: platformAPI + "/commands/screen", methods: []string{http.MethodGet, http.MethodPost}, person: true, forbiddenJSON: true, noStore: true,
			allow: "GET, POST", methodJSON: true, needJSON: true,
			serve: func(svc *prompts.Service, r *http.Request, body []byte) prompts.Response {
				if r.Method == http.MethodPost {
					return svc.CommandScreenKey(body)
				}
				vals := nodeQuery(r.URL.RawQuery)["session"]
				if len(vals) != 1 {
					return svc.CommandScreen("")
				}
				return svc.CommandScreen(vals[0])
			}},
		raw("send-input", post, bodyOp((*prompts.Service).SendInput)),
		raw("answer-question", post, bodyOp((*prompts.Service).AnswerQuestion)),
		raw("capture", get, queryOp((*prompts.Service).Capture)),
		raw("permission-options", get, queryOp((*prompts.Service).PermissionOptions)),
		raw("plan-options", get, queryOp((*prompts.Service).PlanOptions)),
	}
	routes := make([]gate.Route, 0, len(defs))
	for _, d := range defs {
		routes = append(routes, gate.Route{
			Slice: "core",
			Match: func(r *http.Request) bool { return pathIs(r, d.path) || pathIs(r, d.path+"/") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.servePrompt(w, r, d, sessionOK, apiRefusal)
			}),
		})
	}
	return routes
}

func (c *core) servePrompt(w http.ResponseWriter, r *http.Request, d promptRoute, sessionOK func(*http.Request) bool,
	apiRefusal func(*http.Request) (int, any, http.Header, bool)) {
	if status, v, h, refused := apiRefusal(r); refused {
		writeTagged(w, r, status, v, h)
		return
	}
	var body []byte
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		var refusal *gateAnswer
		if body, refusal = readPromptBody(r); refusal != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(refusal.status)
			_, _ = io.WriteString(w, refusal.text)
			return
		}
	}
	if d.person && !sessionOK(r) {
		if d.forbiddenJSON {
			writeTagged(w, r, http.StatusForbidden, errorBody("Forbidden"), nil)
		} else {
			w.WriteHeader(http.StatusForbidden)
		}
		return
	}
	h := http.Header{}
	if d.noStore {
		h.Set("Cache-Control", "no-store")
	}
	allowed := false
	for _, m := range d.methods {
		allowed = allowed || r.Method == m
	}
	if !allowed {
		if d.allow != "" {
			h.Set("Allow", d.allow)
		}
		if d.methodJSON {
			writeTagged(w, r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
		} else {
			writeTagged(w, r, http.StatusMethodNotAllowed, nil, h)
		}
		return
	}
	if d.needJSON && r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeTagged(w, r, http.StatusUnsupportedMediaType, errorBody(locale.Pick("JSON is required", "Нужен JSON")), h)
		return
	}
	svc := c.promptService()
	if svc == nil {
		writeTagged(w, r, http.StatusServiceUnavailable, errorBody("the Go core is starting"), h)
		return
	}
	res := d.serve(svc, r, body)
	writeTagged(w, r, res.Status, res.Body, h)
}
