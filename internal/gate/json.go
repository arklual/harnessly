package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/agent"
)

// JSONRoute is a read-only route a slice answers with JSON. Before the slice is switched on it can run in the shadow:
// the old runtime answers the page, the Go answer is computed next to it and the two are compared, so a slice is
// switched on only after its answers have matched on live traffic.
type JSONRoute struct {
	Slice string
	Match func(*http.Request) bool
	// Compute is the whole answer, a refusal to an unauthorized caller included: status, value (encoded as JSON; nil
	// for an empty body) and extra headers.
	Compute func(*http.Request) (int, any, http.Header)
	// Volatile are fields of the answer that differ between any two reads (a clock): the shadow compares only that both
	// answers have them. A name is a top-level field, or a dotted path into the answer where "*" stands for every key
	// of an object or every element of an array ("machines.*.uptimeSec").
	Volatile []string
	// Shape: the answer carries data of an external service that may change between the two reads (a pipeline that
	// finished, an issue moved on); the shadow compares the status and the answer's structure, not its values.
	Shape bool
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

// shadowStats counts the comparisons of each slice for the periodic log.
type shadowStats struct {
	mu      sync.Mutex
	matched map[string]int
	differ  map[string]int
}

func (s *shadowStats) note(slice string, same bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if same {
		s.matched[slice]++
	} else {
		s.differ[slice]++
	}
}

func (s *shadowStats) take() (map[string]int, map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, d := s.matched, s.differ
	s.matched, s.differ = map[string]int{}, map[string]int{}
	return m, d
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

// AsRoutes turns JSON routes into gate routes: answered while the slice is on, shadowed while "<slice>-shadow" is.
func (f *Gate) AsRoutes(routes ...JSONRoute) []Route {
	out := make([]Route, 0, 2*len(routes))
	for _, jr := range routes {
		jr := jr
		out = append(out, Route{
			Slice: jr.Slice,
			Match: jr.Match,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status, v, h := jr.Compute(r)
				writeJSON(w, status, v, h)
			}),
		}, Route{
			Slice: jr.Slice + "-shadow",
			Match: func(r *http.Request) bool { return r.Method == http.MethodGet && jr.Match(r) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				f.shadow(w, r, jr)
			}),
		})
	}
	return out
}

// teeWriter keeps a copy of what the old runtime answered.
type teeWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (t *teeWriter) WriteHeader(code int) {
	t.status = code
	t.ResponseWriter.WriteHeader(code)
}

func (t *teeWriter) Write(b []byte) (int, error) {
	if t.status == 0 {
		t.status = http.StatusOK
	}
	if t.body.Len() < 4<<20 {
		t.body.Write(b)
	}
	return t.ResponseWriter.Write(b)
}

func (t *teeWriter) Flush() {
	if fl, ok := t.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (f *Gate) shadow(w http.ResponseWriter, r *http.Request, jr JSONRoute) {
	tee := &teeWriter{ResponseWriter: w}
	start := time.Now()
	f.proxy.ServeHTTP(tee, r)
	f.count(r.URL.Path, time.Since(start), false)
	if tee.status == http.StatusNotModified || tee.status == 0 {
		return // 0: the old runtime wrote nothing (the page gave up waiting, or the runtime dropped the connection)
	}
	oldStatus, oldBody := tee.status, tee.body.Bytes()
	r, cancel := detached(r)
	go func() {
		defer cancel()
		status, v, _ := jr.Compute(r)
		newBody := encode(v)
		same, why := sameAnswer(oldStatus, oldBody, status, newBody, jr.Volatile...)
		if jr.Shape {
			same, why = sameShapeAnswer(oldStatus, oldBody, status, newBody)
		}
		f.shadows.note(jr.Slice, same)
		if !same {
			agent.Log("warn", "shadow answer differs", agent.F{K: "slice", V: jr.Slice}, agent.F{K: "path", V: r.URL.RequestURI()},
				agent.F{K: "why", V: why}, agent.F{K: "old", V: clip(oldBody)}, agent.F{K: "go", V: clip(newBody)})
		}
	}()
}

// markVolatile replaces the value at path with true wherever it is present.
func markVolatile(v any, path []string) {
	if len(path) == 0 {
		return
	}
	last := len(path) == 1
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			if path[0] != "*" && path[0] != key {
				continue
			}
			if last {
				x[key] = true
			} else {
				markVolatile(child, path[1:])
			}
		}
	case []any:
		for i, child := range x {
			if path[0] != "*" && path[0] != strconv.Itoa(i) {
				continue
			}
			if last {
				x[i] = true
			} else {
				markVolatile(child, path[1:])
			}
		}
	}
}

func clip(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "…"
	}
	return string(b)
}

// sameShapeAnswer compares the status and the structure: the same keys in every object, values of the same kind; a
// null stands for any value, and arrays may differ in length (their common elements are compared).
func sameShapeAnswer(oldStatus int, oldBody []byte, status int, body []byte) (bool, string) {
	if oldStatus != status {
		return false, fmt.Sprintf("status %d vs %d", oldStatus, status)
	}
	var a, b any
	if json.Unmarshal(oldBody, &a) != nil || json.Unmarshal(body, &b) != nil {
		if bytes.Equal(bytes.TrimSpace(oldBody), bytes.TrimSpace(body)) {
			return true, ""
		}
		return false, "body"
	}
	if !sameShape(a, b) {
		return false, "shape"
	}
	return true, ""
}

func sameShape(a, b any) bool {
	if a == nil || b == nil {
		return true
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !sameShape(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok {
			return false
		}
		for i := 0; i < len(x) && i < len(y); i++ {
			if !sameShape(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b)
}

// sameAnswer compares as data: JSON without key order, numbers as numbers; volatile top-level fields only by presence.
func sameAnswer(oldStatus int, oldBody []byte, status int, body []byte, volatile ...string) (bool, string) {
	if oldStatus != status {
		return false, fmt.Sprintf("status %d vs %d", oldStatus, status)
	}
	var a, b any
	errA := json.Unmarshal(oldBody, &a)
	errB := json.Unmarshal(body, &b)
	if errA != nil || errB != nil {
		if bytes.Equal(bytes.TrimSpace(oldBody), bytes.TrimSpace(body)) {
			return true, ""
		}
		return false, "body"
	}
	for _, k := range volatile {
		path := strings.Split(k, ".")
		markVolatile(a, path)
		markVolatile(b, path)
	}
	if !reflect.DeepEqual(a, b) {
		return false, "json at " + firstDiff(a, b, "$")
	}
	return true, ""
}

// firstDiff names where two decoded JSON values first part, with both sides there, for the log of a differing shadow.
func firstDiff(a, b any, at string) string {
	switch x := a.(type) {
	case map[string]any:
		if y, ok := b.(map[string]any); ok {
			keys := make([]string, 0, len(x)+len(y))
			for k := range x {
				keys = append(keys, k)
			}
			for k := range y {
				if _, ok := x[k]; !ok {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				v, okA := x[k]
				w, okB := y[k]
				switch {
				case !okA:
					return at + "." + k + ": only go"
				case !okB:
					return at + "." + k + ": only old"
				case !reflect.DeepEqual(v, w):
					return firstDiff(v, w, at+"."+k)
				}
			}
		}
	case []any:
		if y, ok := b.([]any); ok {
			if len(x) != len(y) {
				return fmt.Sprintf("%s: %d vs %d elements", at, len(x), len(y))
			}
			for i := range x {
				if !reflect.DeepEqual(x[i], y[i]) {
					return firstDiff(x[i], y[i], fmt.Sprintf("%s[%d]", at, i))
				}
			}
		}
	}
	old, _ := json.Marshal(a)
	got, _ := json.Marshal(b)
	return fmt.Sprintf("%s: %s vs %s", at, clipTo(old, 120), clipTo(got, 120))
}

func clipTo(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// detached is the request for a shadow computed after the answer went out: the request's own context ends with the
// answer, and a shadow that reads the network would fail on it.
func detached(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), shadowTimeout)
	return r.WithContext(ctx), cancel
}

const shadowTimeout = time.Minute
