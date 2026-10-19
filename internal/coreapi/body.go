package coreapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// bodyLimit is the previous runtime's default request body limit.
const bodyLimit = 1 << 20

// bodyError is the dispatcher's plain-text refusal of a body: 413 too large, 400 not JSON.
type bodyError struct {
	status int
	text   string
}

func (e bodyError) Error() string { return e.text }

var errTooLarge = bodyError{http.StatusRequestEntityTooLarge, "Body exceeded 1mb limit"}

// readBody parses a request body as the previous runtime's dispatcher did: JSON (an empty body is {}), a form as an
// object, anything else as text. Objects decode to map[string]any.
func readBody(r *http.Request) (any, error) {
	if r.ContentLength > bodyLimit {
		return nil, errTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		return nil, bodyError{http.StatusBadRequest, "Invalid body"}
	}
	if len(raw) > bodyLimit {
		return nil, errTooLarge
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, _ := mime.ParseMediaType(ct)
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	}
	switch kind {
	case "application/json", "application/ld+json":
		if len(raw) == 0 {
			return map[string]any{}, nil
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bodyError{http.StatusBadRequest, "Invalid JSON"}
		}
		return v, nil
	case "application/x-www-form-urlencoded":
		q, _ := url.ParseQuery(string(raw))
		out := map[string]any{}
		for k, vs := range q {
			if len(vs) == 1 {
				out[k] = vs[0]
			} else {
				arr := make([]any, len(vs))
				for i, v := range vs {
					arr[i] = v
				}
				out[k] = arr
			}
		}
		return out, nil
	}
	return strings.ToValidUTF8(string(raw), "�"), nil
}

// fields is the body as an object (JavaScript's destructuring of a string or null finds no fields).
func fields(body any) map[string]any {
	m, _ := body.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

// queryString is req.query[k] when it is one string (a repeated key is an array to the previous runtime).
func queryString(r *http.Request, k string) (string, bool) {
	vs := r.URL.Query()[k]
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

// truthy is JavaScript truthiness of a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && x == x
	case string:
		return x != ""
	}
	return true
}

func asErrBody(err error) (bodyError, bool) {
	var be bodyError
	ok := errors.As(err, &be)
	return be, ok
}
