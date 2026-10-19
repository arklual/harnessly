package usagereport

import (
	"net/http"
	"strings"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/nodejson"
)

// DefaultWait is how long a request waits for a report being built before it is answered with the progress.
const DefaultWait = 20 * time.Second

// Volatile are the fields of an answer that differ between two runtimes reading the same transcripts at different
// moments: when each built its report, and how far a pass still running has got.
var Volatile = []string{"generatedAt", "collecting", "runtimes.*.report.generatedAt", "runtimes.*.report.collecting"}

// Route answers GET <platform API>/stats: this runtime's usage report, alone (?shape=report) or as a team of one
// runtime. ?wait=0 does not wait for a report being built.
type Route struct {
	Collector *Collector
	// Authorized is the old API's own check (a page session or the cli-token), SessionOK the page session the handler
	// asks for besides; Claims are the session's claims.
	Authorized, SessionOK func(*http.Request) bool
	Claims                func(*http.Request) map[string]any
	// Owner is the person when the session names none (DefaultOwner when empty).
	Owner string
	// Machine is this host's name.
	Machine string
	// Wait replaces DefaultWait (tests).
	Wait time.Duration
}

func only(q map[string][]string, k, want string) bool {
	v := q[k]
	return len(v) == 1 && v[0] == want
}

// Answer is the whole answer, refusals included. The previous handler checked no method.
func (rt *Route) Answer(r *http.Request) (int, any, http.Header) {
	if !rt.Authorized(r) {
		return http.StatusUnauthorized, gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}, nil
	}
	if !rt.SessionOK(r) {
		return nodejson.Answer(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
	}
	h := http.Header{"Cache-Control": {"no-store"}}
	person := rt.Owner
	if person == "" {
		person = DefaultOwner(rt.Machine)
	}
	if rt.Claims != nil {
		if sub, ok := rt.Claims(r)["sub"].(string); ok && strings.Contains(sub, "@") {
			person = sub
		}
	}
	q := jscompat.Query(r.URL.RawQuery)
	wait := rt.Wait
	if wait == 0 {
		wait = DefaultWait
	}
	if only(q, "wait", "0") {
		wait = 0
	}
	report, err := rt.Collector.Report(person, wait)
	if err != nil {
		return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, h
	}
	var w jw
	if only(q, "shape", "report") {
		report.write(&w)
	} else {
		w.WriteString(`{"viewer":`)
		w.str(person)
		w.WriteString(`,"runtimes":[{"person":`)
		w.str(person)
		w.WriteString(`,"machineId":"local","machineName":`)
		w.str(rt.Machine)
		w.WriteString(`,"report":`)
		report.write(&w)
		w.WriteString(`}]}`)
	}
	body := w.Bytes()
	tag := nodejson.ETag(body)
	h.Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		return http.StatusNotModified, nil, h
	}
	return http.StatusOK, gate.Raw{ContentType: "application/json; charset=utf-8", Body: body}, h
}
