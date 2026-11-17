package observe

import (
	"bufio"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// Prefix starts every name a machine exposes (the hub's own start with harnessly_hub_).
const Prefix = "harnessly_runtime_"

// metricsSeriesMax bounds the label sets of the journal's counter: an event name comes from the page too, and a
// scrape must stay small whatever it sends.
const metricsSeriesMax = 500

type lineKey struct{ level, source, event string }

// Metrics counts what the machine's journal saw and reads the process's state at a scrape.
type Metrics struct {
	Version string
	Started time.Time
	// Dropped, when set, is how many lines the journal lost; OTLPDropped how many the OTLP queue lost.
	Dropped     func() int64
	OTLPDropped func() int64
	// Extra writes more gauges at a scrape (chats, agents: what the runtime knows).
	Extra func(w *Writer)

	mu    sync.Mutex
	lines map[lineKey]int64
	other int64
}

// Sink is the journal's receiver that counts every line.
func (m *Metrics) Sink() journal.Sink { return journal.Sink{All: true, Entry: m.Count} }

// Count adds a journal line to its counter.
func (m *Metrics) Count(e journal.Entry) {
	k := lineKey{e.Level, e.Source, e.Event}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lines == nil {
		m.lines = map[lineKey]int64{}
	}
	if _, ok := m.lines[k]; !ok && len(m.lines) >= metricsSeriesMax {
		m.other++
		return
	}
	m.lines[k]++
}

// Writer writes samples in the Prometheus text format; the samples of one name go one after another.
type Writer struct {
	w    *bufio.Writer
	last string
}

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

// Counter writes a counter's sample; name is without Prefix, labels are pairs of a name and a value.
func (w *Writer) Counter(name, help string, v float64, labels ...string) {
	w.sample(name, help, "counter", v, labels)
}

// Gauge writes a gauge's sample.
func (w *Writer) Gauge(name, help string, v float64, labels ...string) {
	w.sample(name, help, "gauge", v, labels)
}

func (w *Writer) sample(name, help, typ string, v float64, labels []string) {
	name = Prefix + name
	if name != w.last {
		w.last = name
		_, _ = w.w.WriteString("# HELP " + name + " " + helpEscaper.Replace(help) + "\n# TYPE " + name + " " + typ + "\n")
	}
	_, _ = w.w.WriteString(name)
	for i := 0; i+1 < len(labels); i += 2 {
		sep := ","
		if i == 0 {
			sep = "{"
		}
		_, _ = w.w.WriteString(sep + labels[i] + `="` + labelEscaper.Replace(labels[i+1]) + `"`)
	}
	if len(labels) >= 2 {
		_ = w.w.WriteByte('}')
	}
	_, _ = w.w.WriteString(" " + strconv.FormatFloat(v, 'g', -1, 64) + "\n")
}

// Handler serves GET /metrics. It checks nobody: it belongs on a listener of its own that only the collector reaches
// (RUNTIME_METRICS_LISTEN, loopback by default), never on the page's.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w := &Writer{w: bufio.NewWriter(rw)}
		m.write(w)
		_ = w.w.Flush()
	})
	return mux
}

func (m *Metrics) write(w *Writer) {
	w.Gauge("build_info", "The running version; always 1.", 1, "version", m.Version, "goversion", runtime.Version(),
		"os", runtime.GOOS, "arch", runtime.GOARCH)
	if !m.Started.IsZero() {
		w.Gauge("start_time_seconds", "When the process started, in Unix seconds.", float64(m.Started.Unix()))
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	w.Gauge("goroutines", "Goroutines now.", float64(runtime.NumGoroutine()))
	w.Gauge("heap_alloc_bytes", "Bytes of the heap in use.", float64(ms.HeapAlloc))
	w.Gauge("sys_bytes", "Bytes taken from the system.", float64(ms.Sys))
	w.Counter("gc_total", "Garbage collections since the start.", float64(ms.NumGC))

	m.mu.Lock()
	keys := make([]lineKey, 0, len(m.lines))
	for k := range m.lines {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.level != b.level {
			return a.level < b.level
		}
		if a.source != b.source {
			return a.source < b.source
		}
		return a.event < b.event
	})
	counts := make([]int64, len(keys))
	for i, k := range keys {
		counts[i] = m.lines[k]
	}
	other := m.other
	m.mu.Unlock()
	const linesHelp = "Lines of the machine's journal by level, where they were seen (web, runtime, app, sinopia, agent, preview) and what happened."
	for i, k := range keys {
		w.Counter("journal_lines_total", linesHelp, float64(counts[i]), "level", k.level, "source", k.source, "event", k.event)
	}
	if other > 0 {
		w.Counter("journal_lines_total", linesHelp, float64(other), "level", "", "source", "", "event", "_other")
	}
	if m.Dropped != nil {
		w.Counter("journal_dropped_total", "Lines the journal lost to a full queue or a full hour.", float64(m.Dropped()))
	}
	if m.OTLPDropped != nil {
		w.Counter("otlp_dropped_total", "Journal lines the OTLP export lost while its collector did not take them.", float64(m.OTLPDropped()))
	}
	if m.Extra != nil {
		m.Extra(w)
	}
}
