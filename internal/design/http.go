package design

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// API answers the library's routes as the previous runtime's dispatcher and handlers did.
type API struct {
	L *Library
	// Session: the request carries a page session. CLIToken: it carries the runtime's cli-token.
	Session, CLIToken func(*http.Request) bool
	// APIRefusal is the dispatcher's answer to a caller with neither; refused false for anyone else.
	APIRefusal func(*http.Request) (status int, body any, h http.Header, refused bool)
	// PlatformAPI prefixes the platform's routes.
	PlatformAPI string
	// Launch starts an agent conversation (nil while the core does not run).
	Launch func(LaunchRequest) LaunchResult
	// Terminal reaches an agent's terminal (nil while the core does not run).
	Terminal Terminal
	// Push sends a board to the person's phones: the statuses of the push services, nothing when there was no device.
	Push func(PushRequest) []int
	// AppBase is the platform pages' mount point (a pushed board's link).
	AppBase string
	Log     func(level, msg string, err error)
}

// Answer is a route's answer: status, value (JSON, gate.Raw, or nil for no body) and headers.
type Answer struct {
	Status int
	Body   any
	Header http.Header
}

func (a Answer) Split() (int, any, http.Header) { return a.Status, a.Body, a.Header }

// JSON is res.json: the exact bytes with their ETag, 304 when the caller holds them.
func JSON(r *http.Request, status int, v any, h http.Header) Answer {
	s, b, hh := nodejson.Answer(r, status, v, h)
	return Answer{s, b, hh}
}

// Empty is res.status(n).end().
func Empty(status int, h http.Header) Answer { return Answer{Status: status, Header: h} }

func text(status int, msg string) Answer {
	return Answer{Status: status, Body: gate.Raw{ContentType: "text/plain", Body: []byte(msg)}}
}

func noStore() http.Header { return http.Header{"Cache-Control": {"no-store"}} }

func errorBody(msg string) *jsvalue.Object { return jsvalue.NewObject("error", msg) }

const bodyLimit = 1 << 20

// ReadBody parses a request body as the dispatcher did: JSON (an empty body is {}), a form as an object, anything
// else as text; refused is the dispatcher's plain-text answer to a body too large or not JSON.
func ReadBody(r *http.Request) (body any, refused *Answer) {
	if r.ContentLength > bodyLimit {
		a := text(http.StatusRequestEntityTooLarge, "Body exceeded 1mb limit")
		return nil, &a
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		a := text(http.StatusBadRequest, "Invalid body")
		return nil, &a
	}
	if len(raw) > bodyLimit {
		a := text(http.StatusRequestEntityTooLarge, "Body exceeded 1mb limit")
		return nil, &a
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, err := mime.ParseMediaType(ct)
	if err != nil || kind == "" {
		kind = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	}
	s := jscompat.DecodeUTF8(raw)
	switch kind {
	case "application/json", "application/ld+json":
		if len(raw) == 0 {
			return jsvalue.NewObject(), nil
		}
		v, err := jsvalue.Parse([]byte(s))
		if err != nil {
			a := text(http.StatusBadRequest, "Invalid JSON")
			return nil, &a
		}
		return v, nil
	case "application/x-www-form-urlencoded":
		q, _ := url.ParseQuery(s)
		o := jsvalue.NewObject()
		for k, vs := range q {
			if len(vs) == 1 {
				o.Set(k, vs[0])
			} else {
				o.Set(k, jsvalue.StringArray(vs))
			}
		}
		return o, nil
	}
	return s, nil
}

// Fields is the body as an object: destructuring a string or null finds no fields.
func Fields(body any) *jsvalue.Object {
	if o, ok := body.(*jsvalue.Object); ok {
		return o
	}
	return jsvalue.NewObject()
}

// Field is a body field; nil when absent (undefined) or null.
func Field(body *jsvalue.Object, k string) any { return get(body, k) }

// Has is whether a body field is present (not undefined), null included.
func Has(body *jsvalue.Object, k string) bool { return body.Has(k) }

// Query is req.query[k] when it is one string; "" for an absent or repeated key.
func Query(r *http.Request, k string) (string, bool) {
	vs := jscompat.Query(r.URL.RawQuery)[k]
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

// QueryString is the design routes' queryString: the value when it is one string, else "".
func QueryString(r *http.Request, k string) string {
	v, _ := Query(r, k)
	return v
}

// Guard is the design routes' check: the dispatcher's session-or-token, then a page session, the allowed methods and a
// JSON body for anything but GET. The body is read in between, as the dispatcher read it before the handler ran.
func (a *API) Guard(r *http.Request, methods ...string) (body *jsvalue.Object, refused *Answer) {
	if status, v, h, no := a.APIRefusal(r); no {
		return nil, &Answer{status, v, h}
	}
	raw, bad := ReadBody(r)
	if bad != nil {
		return nil, bad
	}
	h := noStore()
	if !a.Session(r) {
		ans := JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
		return nil, &ans
	}
	allowed := false
	for _, m := range methods {
		allowed = allowed || r.Method == m
	}
	if !allowed {
		h.Set("Allow", strings.Join(methods, ", "))
		ans := Empty(http.StatusMethodNotAllowed, h)
		return nil, &ans
	}
	if r.Method != http.MethodGet && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		ans := JSON(r, http.StatusUnsupportedMediaType, errorBody(locale.Pick("application/json is expected", "Ожидается application/json")), h)
		return nil, &ans
	}
	return Fields(raw), nil
}

// CLIGuard is a CLI route's check: open to the dispatcher, the handler wants the cli-token.
func (a *API) CLIGuard(r *http.Request) (body any, refused *Answer) {
	raw, bad := ReadBody(r)
	if bad != nil {
		return nil, bad
	}
	if !a.CLIToken(r) {
		ans := JSON(r, http.StatusForbidden, errorBody("Forbidden"), nil)
		return nil, &ans
	}
	return raw, nil
}

func (a *API) warn(msg string, err error) {
	if a.Log != nil {
		a.Log("warn", msg, err)
	}
}
