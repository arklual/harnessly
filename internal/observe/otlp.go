// Package observe hands a machine's own signals to the operator's observability stack: the journal's lines as OTLP
// logs (an OpenTelemetry Collector, Grafana Alloy, Loki, Elastic, …) and its counters in the Prometheus text format.
// Both are off until the operator points them somewhere; nothing here leaves for anyone but them.
package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// OTLP batches journal lines and posts them as OTLP/HTTP JSON logs.
type OTLP struct {
	URL      string
	Headers  map[string]string
	Resource map[string]string
	// MinLevel is the least severe level sent: info, warn (the default) or error.
	MinLevel string
	Every    time.Duration
	Batch    int
	Client   *http.Client
	Log      func(level, msg string, kv ...any)

	mu      sync.Mutex
	queue   []journal.Entry
	timer   *time.Timer
	dropped int64
}

// otlpQueueMax is the most lines waiting to be sent; a collector that is down loses the newest, never the machine's
// memory.
const otlpQueueMax = 5000

// OTLPFromEnv reads the standard OpenTelemetry variables (OTEL_EXPORTER_OTLP_LOGS_ENDPOINT, OTEL_EXPORTER_OTLP_ENDPOINT
// plus /v1/logs, OTEL_EXPORTER_OTLP_[LOGS_]HEADERS, OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES), each of which a
// RUNTIME_OTLP_* variable of the same meaning overrides, and RUNTIME_OTLP_LEVEL. nil when no endpoint is set.
func OTLPFromEnv(getenv func(string) string, service, version string) *OTLP {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(getenv(k)); v != "" {
				return v
			}
		}
		return ""
	}
	endpoint := first("RUNTIME_OTLP_LOGS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
	if endpoint == "" {
		if base := first("RUNTIME_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"); base != "" {
			endpoint = strings.TrimRight(base, "/") + "/v1/logs"
		}
	}
	if endpoint == "" {
		return nil
	}
	if s := first("RUNTIME_OTLP_SERVICE_NAME", "OTEL_SERVICE_NAME"); s != "" {
		service = s
	}
	host, _ := os.Hostname()
	res := map[string]string{"service.name": service, "service.version": version, "host.name": host,
		"os.type": runtime.GOOS, "host.arch": runtime.GOARCH}
	for k, v := range pairs(first("RUNTIME_OTLP_RESOURCE_ATTRIBUTES", "OTEL_RESOURCE_ATTRIBUTES")) {
		res[k] = v
	}
	return &OTLP{URL: endpoint, Headers: pairs(first("RUNTIME_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_LOGS_HEADERS", "OTEL_EXPORTER_OTLP_HEADERS")),
		Resource: res, MinLevel: strings.ToLower(first("RUNTIME_OTLP_LEVEL"))}
}

// pairs reads "k=v,k2=v2" with percent-encoded values, as the OpenTelemetry variables are written.
func pairs(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		if dv, err := url.QueryUnescape(strings.TrimSpace(v)); err == nil {
			v = dv
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func rank(level string) int {
	switch level {
	case journal.Error:
		return 3
	case journal.Warn:
		return 2
	}
	return 1
}

// Sink is the journal's receiver for this exporter.
func (o *OTLP) Sink() journal.Sink {
	return journal.Sink{All: o.minRank() == 1, Entry: o.Push}
}

func (o *OTLP) minRank() int {
	if o.MinLevel == "" {
		return rank(journal.Warn)
	}
	return rank(o.MinLevel)
}

// Push queues a line; it never waits.
func (o *OTLP) Push(e journal.Entry) {
	if o == nil || rank(e.Level) < o.minRank() {
		return
	}
	every, batch := o.Every, o.Batch
	if every <= 0 {
		every = 5 * time.Second
	}
	if batch <= 0 {
		batch = 200
	}
	o.mu.Lock()
	if len(o.queue) >= otlpQueueMax {
		o.dropped++
		o.mu.Unlock()
		return
	}
	o.queue = append(o.queue, e)
	full := len(o.queue) >= batch
	if !full && o.timer == nil {
		o.timer = time.AfterFunc(every, o.Flush)
	}
	o.mu.Unlock()
	if full {
		go o.Flush()
	}
}

// Dropped is how many lines a full queue lost.
func (o *OTLP) Dropped() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dropped
}

// Flush sends what waits now.
func (o *OTLP) Flush() {
	o.mu.Lock()
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	pending := o.queue
	o.queue = nil
	o.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL, bytes.NewReader(LogsBody(o.Resource, pending)))
	if err != nil {
		o.log("otlp logs not sent", "error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		o.log("otlp logs not sent", "error", err.Error())
		return
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		o.log("otlp logs not accepted", "status", res.StatusCode)
	}
}

func (o *OTLP) log(msg string, kv ...any) {
	if o.Log != nil {
		o.Log("debug", msg, kv...)
	}
}

type otlpValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
}

type otlpKV struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

func str(k, v string) otlpKV { return otlpKV{Key: k, Value: otlpValue{StringValue: &v}} }

func num(k string, v int64) otlpKV {
	s := strconv.FormatInt(v, 10)
	return otlpKV{Key: k, Value: otlpValue{IntValue: &s}}
}

type otlpRecord struct {
	TimeUnixNano   string    `json:"timeUnixNano"`
	SeverityNumber int       `json:"severityNumber"`
	SeverityText   string    `json:"severityText"`
	Body           otlpValue `json:"body"`
	Attributes     []otlpKV  `json:"attributes"`
}

// LogsBody is an ExportLogsServiceRequest in the OTLP/HTTP JSON encoding.
func LogsBody(resource map[string]string, entries []journal.Entry) []byte {
	keys := make([]string, 0, len(resource))
	for k := range resource {
		keys = append(keys, k)
	}
	sortStrings(keys)
	res := make([]otlpKV, 0, len(keys))
	for _, k := range keys {
		res = append(res, str(k, resource[k]))
	}
	records := make([]otlpRecord, 0, len(entries))
	for _, e := range entries {
		sevN, sevT := 9, "INFO"
		switch e.Level {
		case journal.Warn:
			sevN, sevT = 13, "WARN"
		case journal.Error:
			sevN, sevT = 17, "ERROR"
		}
		body := e.Text
		if body == "" {
			body = e.Event
		}
		attrs := []otlpKV{str("harnessly.source", e.Source), str("harnessly.event", e.Event)}
		for _, a := range [][2]string{{"harnessly.trace", e.Trace}, {"harnessly.chat", e.Chat}, {"harnessly.app", e.App},
			{"harnessly.view", e.View}, {"harnessly.tool", e.Tool}, {"http.request.method", e.Method}, {"url.path", e.URL}} {
			if a[1] != "" {
				attrs = append(attrs, str(a[0], a[1]))
			}
		}
		if e.Status != 0 {
			attrs = append(attrs, num("http.response.status_code", int64(e.Status)))
		}
		if e.Dur != 0 {
			attrs = append(attrs, num("harnessly.duration_ms", e.Dur))
		}
		records = append(records, otlpRecord{TimeUnixNano: strconv.FormatInt(e.At*int64(time.Millisecond), 10),
			SeverityNumber: sevN, SeverityText: sevT, Body: otlpValue{StringValue: &body}, Attributes: attrs})
	}
	out, _ := json.Marshal(map[string]any{"resourceLogs": []any{map[string]any{
		"resource": map[string]any{"attributes": res},
		"scopeLogs": []any{map[string]any{
			"scope":      map[string]any{"name": "harnessly.journal"},
			"logRecords": records,
		}},
	}}})
	return out
}

func sortStrings(s []string) { sort.Strings(s) }
