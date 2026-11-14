package metrics

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
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
			if f[2] != current || (f[3] != "counter" && f[3] != "gauge" && f[3] != "histogram") {
				t.Fatalf("TYPE line out of place: %q", line)
			}
			typ[current]++
		default:
			name, _, _ := strings.Cut(strings.Fields(line)[0], "{")
			if typ[current] == 1 && strings.HasSuffix(current, "_seconds") {
				name = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, "_bucket"), "_sum"), "_count")
			}
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

// a read of people's data is counted by its kind and by what answered it.
func TestReadsAreCountedByKindAndSource(t *testing.T) {
	before := Reads(ReadUpload, FromMachine)
	CountRead(ReadUpload, FromMachine)
	CountRead(99, FromStore)
	CountRead(ReadDesign, -1)
	if got := Reads(ReadUpload, FromMachine) - before; got != 1 {
		t.Fatalf("counted %d reads", got)
	}
	var out strings.Builder
	if err := Write(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data_reads_total{kind="upload",source="machine"} `, `data_reads_total{kind="design",source="store"} `, `data_reads_total{kind="delivered",source="machine"} `} {
		if !strings.Contains(out.String(), Prefix+want) {
			t.Fatalf("no sample %s in:\n%s", want, out.String())
		}
	}
}

// the time to answer, by the finer group of the route, as a Prometheus histogram; streams are not timed.
func TestLatencyIsAHistogramByArea(t *testing.T) {
	for _, c := range []struct {
		path string
		want string
	}{
		{"/m/abc/api/platform/design/store-library", "design"},
		{"/m/abc/api/v2/designly/library", "design"},
		{"/api/history/sidebar", "history"},
		{"/m/abc/api/history/conversations", "history"},
		{"/store/v1/collections/design/files", "store"},
		{"/m/abc/api/v2/convs", "machine"},
		{"/api/me", "api"},
	} {
		if got := areaNames[LatencyArea(c.path)]; got != c.want {
			t.Fatalf("%s: area %q, want %q", c.path, got, c.want)
		}
	}
	ObserveLatency("/m/x/api/platform/design/store-library", "GET", 200, 300*time.Millisecond)
	ObserveLatency("/m/x/api/platform/design/store-library", "GET", 200, 3*time.Second)
	ObserveLatency("/m/x/api/platform/design/store-library", "GET", 200, 40*time.Second)
	ObserveLatency("/store/v1/collections/design/changes", "GET", 200, time.Minute)
	ObserveLatency("/m/x/api/v2/ws", "GET", 101, time.Hour)
	var b strings.Builder
	if err := Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# TYPE harnessly_hub_http_request_duration_seconds histogram",
		`harnessly_hub_http_request_duration_seconds_bucket{route="design",method="GET",le="0.2"} 0`,
		`harnessly_hub_http_request_duration_seconds_bucket{route="design",method="GET",le="0.4"} 1`,
		`harnessly_hub_http_request_duration_seconds_bucket{route="design",method="GET",le="5"} 2`,
		`harnessly_hub_http_request_duration_seconds_bucket{route="design",method="GET",le="+Inf"} 3`,
		`harnessly_hub_http_request_duration_seconds_count{route="design",method="GET"} 3`,
		`harnessly_hub_http_request_duration_seconds_sum{route="design",method="GET"} 43.3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `route="store"`) || strings.Contains(out, `route="machine",method="GET"`) {
		t.Fatalf("a stream was timed:\n%s", out)
	}
}

func TestWebVitalHistogram(t *testing.T) {
	ObserveWebVital("lcp", 1.2)
	ObserveWebVital("lcp", 3)
	ObserveWebVital("fcp", 1)  // not in the set
	ObserveWebVital("lcp", -1) // not a time
	var b strings.Builder
	if err := Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`harnessly_hub_web_vital_seconds_bucket{name="lcp",le="1.5"} 1`,
		`harnessly_hub_web_vital_seconds_bucket{name="lcp",le="4"} 2`,
		`harnessly_hub_web_vital_seconds_count{name="lcp"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, `name="fcp"`) {
		t.Error("a name outside the set became a label")
	}
}
