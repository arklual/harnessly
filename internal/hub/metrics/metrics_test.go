package metrics

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var sampleLine = regexp.MustCompile(`^harnessly_hub_[a-z0-9_]+(\{[a-z_]+="[^"]*"(,[a-z_]+="[^"]*")*\})? -?[0-9.e+]+$`)

func TestWriteIsThePrometheusTextFormat(t *testing.T) {
	before := MachineBusy.Value()
	CountErrorCode("machine_busy")
	CountErrorCode("not_counted")
	if MachineBusy.Value() != before+1 {
		t.Fatalf("machine_busy = %d, want %d", MachineBusy.Value(), before+1)
	}
	var b strings.Builder
	err := Write(&b, func(w *Writer) {
		w.Gauge("links", "Live links.", 3)
		w.Gauge("queue_length", "Events waiting.", 2, "queue", "events")
		w.Gauge("queue_length", "Events waiting.", 0, "queue", `a"b`)
	})
	if err != nil {
		t.Fatal(err)
	}
	out := b.String()
	help, typ := map[string]int{}, map[string]int{}
	var current string
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "# HELP "):
			current = strings.Fields(line)[2]
			help[current]++
		case strings.HasPrefix(line, "# TYPE "):
			f := strings.Fields(line)
			if f[2] != current || (f[3] != "counter" && f[3] != "gauge") {
				t.Fatalf("TYPE line out of place: %q", line)
			}
			typ[current]++
		default:
			name, _, _ := strings.Cut(strings.Fields(line)[0], "{")
			if name != current {
				t.Fatalf("sample %q is not under its own HELP (%s)", line, current)
			}
			if !strings.Contains(line, `a\"b`) && !sampleLine.MatchString(line) {
				t.Fatalf("not a sample line: %q", line)
			}
		}
	}
	for name, n := range help {
		if n != 1 || typ[name] != 1 {
			t.Fatalf("%s has %d HELP and %d TYPE lines, want one of each", name, n, typ[name])
		}
		if !strings.HasPrefix(name, Prefix) {
			t.Fatalf("%s lacks the prefix", name)
		}
	}
	for _, want := range []string{
		"# TYPE harnessly_hub_machine_busy_total counter",
		"# TYPE harnessly_hub_links gauge",
		"harnessly_hub_links 3\n",
		`harnessly_hub_queue_length{queue="events"} 2`,
		`harnessly_hub_queue_length{queue="a\"b"} 0`,
		`harnessly_hub_http_requests_total{route="agent_link",class="5xx"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRequestsAreCountedByGroupNotByPath(t *testing.T) {
	groups := map[string]int{
		"/agent/v1": RouteAgentLink, "/agent/secrets": RouteAgent, "/api/machines/abc123": RouteAPI,
		"/m/abc123/api/chats": RouteMachine, "/agents": RouteWorkspace, "/agents/x": RouteWorkspace,
		"/sites/a--b/": RouteSites, "/healthz": RouteHealth, "/": RouteOther, "/agentsmith": RouteOther,
	}
	for path, want := range groups {
		if got := RouteGroup(path); got != want {
			t.Errorf("RouteGroup(%q) = %s, want %s", path, routeNames[got], routeNames[want])
		}
	}
	link5xx, unavailable := httpRequests[RouteAgentLink][4].Load(), AgentLinkUnavailable.Value()
	ObserveHTTP("/agent/v1", http.StatusServiceUnavailable)
	ObserveHTTP("/agent/v1", http.StatusInternalServerError)
	ObserveHTTP("/api/x", http.StatusServiceUnavailable)
	if got := httpRequests[RouteAgentLink][4].Load() - link5xx; got != 2 {
		t.Fatalf("agent_link 5xx grew by %d, want 2", got)
	}
	if got := AgentLinkUnavailable.Value() - unavailable; got != 1 {
		t.Fatalf("agent link 503 grew by %d, want 1", got)
	}
}

func TestHandlerServesOnlyMetrics(t *testing.T) {
	h := Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("GET /metrics = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/", nil),
		httptest.NewRequest(http.MethodGet, "/api/config", nil),
		httptest.NewRequest(http.MethodPost, "/metrics", nil),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s %s answered 200", req.Method, req.URL.Path)
		}
	}
}
