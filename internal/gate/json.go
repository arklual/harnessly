package gate

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// JSONRoute is a route a slice answers with JSON.
type JSONRoute struct {
	Slice string
	Match func(*http.Request) bool
	// Compute is the whole answer, a refusal to an unauthorized caller included: status, value (encoded as JSON; nil
	// for an empty body) and extra headers.
	Compute func(*http.Request) (int, any, http.Header)
}

// Raw is an answer that is not JSON: its bytes as they are, with their content type.
type Raw struct {
	ContentType string
	Body        []byte
}

// encode is the body of an answer value: Raw as it is, anything else as JSON; nil for an empty body.
func encode(v any) []byte {
	switch x := v.(type) {
	case nil:
		return nil
	case Raw:
		return x.Body
	}
	return encodeJSON(v)
}

// Encode is how an answer is written: like the previous runtime's JSON.stringify, no escaping of <, > and &, no
// trailing newline. A route that hashes its body (an ETag) hashes these bytes.
func Encode(v any) []byte { return encodeJSON(v) }

func writeJSON(w http.ResponseWriter, status int, v any, h http.Header) {
	for k, vs := range h {
		for _, x := range vs {
			w.Header().Add(k, x)
		}
	}
	if v == nil {
		w.WriteHeader(status)
		return
	}
	raw := encode(v)
	if r, ok := v.(Raw); ok {
		w.Header().Set("Content-Type", r.ContentType)
	} else if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// encodeJSON writes v as the old runtime did: "<", ">", "&" and the line separators U+2028/U+2029 as they are, not
// escaped, so the bytes (and an ETag computed over them) are the old ones.
func encodeJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	raw := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if !bytes.Contains(raw, []byte(`\u202`)) {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			if raw[i+1] == 'u' && i+5 < len(raw) && string(raw[i+2:i+5]) == "202" && (raw[i+5] == '8' || raw[i+5] == '9') {
				out = append(out, []byte(string(rune(0x2028+int(raw[i+5]-'8'))))...)
				i += 5
				continue
			}
			out = append(out, raw[i], raw[i+1])
			i++
			continue
		}
		out = append(out, raw[i])
	}
	return out
}

// AsRoutes turns JSON routes into gate routes, answered while the slice is on.
func (f *Gate) AsRoutes(routes ...JSONRoute) []Route {
	out := make([]Route, 0, len(routes))
	for _, jr := range routes {
		out = append(out, Route{
			Slice: jr.Slice,
			Match: jr.Match,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status, v, h := jr.Compute(r)
				writeJSON(w, status, v, h)
			}),
		})
	}
	return out
}
