package observe

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
)

func TestOTLPFromEnv(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if OTLPFromEnv(get, "harnessly-runtime", "1.0.0") != nil {
		t.Fatal("no endpoint, yet an exporter")
	}
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://collector:4318/"
	env["OTEL_EXPORTER_OTLP_HEADERS"] = "Authorization=Basic%20abc,X-Scope-OrgID=team"
	env["OTEL_RESOURCE_ATTRIBUTES"] = "deployment.environment=prod"
	o := OTLPFromEnv(get, "harnessly-runtime", "1.0.0")
	if o.URL != "http://collector:4318/v1/logs" || o.Headers["Authorization"] != "Basic abc" || o.Headers["X-Scope-OrgID"] != "team" {
		t.Fatalf("%+v", o)
	}
	if o.Resource["service.name"] != "harnessly-runtime" || o.Resource["deployment.environment"] != "prod" {
		t.Fatalf("%+v", o.Resource)
	}
	env["RUNTIME_OTLP_LOGS_ENDPOINT"] = "https://loki.example.com/otlp/v1/logs"
	if o := OTLPFromEnv(get, "x", "1"); o.URL != "https://loki.example.com/otlp/v1/logs" {
		t.Fatal(o.URL)
	}
}

// Lines below the level stay; what goes is a valid OTLP logs request with its headers.
func TestOTLPSend(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(raw, &got)
		auth = r.Header.Get("Authorization")
		mu.Unlock()
	}))
	defer srv.Close()
	o := &OTLP{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer t"}, Resource: map[string]string{"service.name": "harnessly-runtime"}, Every: time.Hour}
	o.Push(journal.Entry{At: 1_800_000_000_000, Level: journal.Info, Source: "runtime", Event: "http"})
	o.Push(journal.Entry{At: 1_800_000_000_000, Level: journal.Error, Source: "web", Event: "exception", Text: "boom", Status: 500})
	o.Flush()
	mu.Lock()
	defer mu.Unlock()
	if auth != "Bearer t" {
		t.Fatal("headers not sent")
	}
	raw, _ := json.Marshal(got)
	s := string(raw)
	for _, want := range []string{`"severityText":"ERROR"`, `"stringValue":"boom"`, `"timeUnixNano":"1800000000000000000"`, `"intValue":"500"`, `"service.name"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lacks %s: %s", want, s)
		}
	}
	if strings.Contains(s, `"harnessly.event","value":{"stringValue":"http"}`) {
		t.Error("an info line went at level warn")
	}
}

func TestMetrics(t *testing.T) {
	m := &Metrics{Version: "1.2.3", Started: time.Unix(1_800_000_000, 0), Dropped: func() int64 { return 4 }}
	m.Count(journal.Entry{Level: journal.Error, Source: "web", Event: "exception"})
	m.Count(journal.Entry{Level: journal.Error, Source: "web", Event: "exception"})
	for i := 0; i < metricsSeriesMax+10; i++ {
		m.Count(journal.Entry{Level: journal.Info, Source: "web", Event: "ev" + strings.Repeat("x", i%700) + string(rune('a'+i%26))})
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`harnessly_runtime_build_info{version="1.2.3"`,
		`harnessly_runtime_journal_lines_total{level="error",source="web",event="exception"} 2`,
		`harnessly_runtime_journal_dropped_total 4`,
		`event="_other"`,
		"# TYPE harnessly_runtime_journal_lines_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %s", want)
		}
	}
	if n := strings.Count(body, "# TYPE harnessly_runtime_journal_lines_total"); n != 1 {
		t.Errorf("TYPE written %d times", n)
	}
}
