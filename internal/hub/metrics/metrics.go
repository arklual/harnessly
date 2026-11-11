// Package metrics is the hub's numbers in the Prometheus text format: counters the hot paths add to without a lock,
// and gauges read from the hub's own state at the moment of a scrape. It holds no histograms and imports nothing but
// the standard library, so every part of the hub may count into it.
package metrics

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// Prefix starts every name the hub exposes.
const Prefix = "harnessly_hub_"

// Counter only grows. The zero value is ready; Inc and Add never block.
type Counter struct{ v atomic.Int64 }

func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Add(n int64)  { c.v.Add(n) }
func (c *Counter) Value() int64 { return c.v.Load() }

// What the hub counts since the process started. They are the process's own, like the count of held panics.
var (
	// LinkConnects is every agent link taken; LinkReconnects those of a machine this process had a link of before;
	// LinkDisconnects the links that ended.
	LinkConnects, LinkReconnects, LinkDisconnects Counter
	// EventsDropped is the bulk events (frames of the chats' history) dropped because their queue was full;
	// LinksStuck the links closed because nobody was taking their events.
	EventsDropped, LinksStuck Counter
	// StreamsDropped is the browsers' event streams ended because they were too slow to take an event.
	StreamsDropped Counter
	// MachineTimeouts and MachineBusy are the answers 504 machine_timeout and 503 machine_busy.
	MachineTimeouts, MachineBusy Counter
	// UsagePullErrors is the pulls of a machine's spend that failed.
	UsagePullErrors Counter
	// AgentLinkUnavailable is the 503 answered to an agent that dialed /agent/v1.
	AgentLinkUnavailable Counter
	// EventsRelayed is the events this replica passed to the streams of the others; EventsRelayDropped those it could
	// not pass (too long for a notification, or its queue was full); EventsRelayReceived those it took from the others.
	EventsRelayed, EventsRelayDropped, EventsRelayReceived Counter
	// UsageQuarantined is the records of machines' spend this hub could not read and set aside.
	UsageQuarantined Counter
	// AutomationsRequeued is the automation runs put back in the queue: their start did not finish in time or failed.
	AutomationsRequeued Counter
	// HistoryChatsPurged is the copies of chats deleted on their machines that this replica removed after their term;
	// HistoryRowsPurged the turns and entries removed with them; HistoryPurgeErrors the passes that failed.
	HistoryChatsPurged, HistoryRowsPurged, HistoryPurgeErrors Counter
)

// CountErrorCode counts an answered error by its code; codes the hub does not count are ignored.
func CountErrorCode(code string) {
	switch code {
	case "machine_timeout":
		MachineTimeouts.Inc()
	case "machine_busy":
		MachineBusy.Inc()
	}
}

// The groups of routes requests are counted by. A request is never counted by its path: a path carries identifiers,
// and a label must have few values.
const (
	RouteAgentLink = iota // /agent/v1: an agent dialing or unlinking
	RouteAgent            // the rest of /agent/
	RouteAPI              // /api/
	RouteMachine          // /m/: a machine's own pages and calls
	RouteWorkspace        // /agents
	RouteSites            // /sites/
	RouteApps             // /apps/
	RouteDownload         // /dl/
	RouteHealth           // /healthz, /readyz
	RouteOther            // the hub's page and everything else
	routeGroups
)

var routeNames = [routeGroups]string{"agent_link", "agent", "api", "machine", "workspace", "sites", "apps", "download", "health", "other"}

var classNames = [5]string{"1xx", "2xx", "3xx", "4xx", "5xx"}

var httpRequests [routeGroups][5]atomic.Int64

// RouteGroup is the group of a request path (without the hub's base path).
func RouteGroup(path string) int {
	under := func(prefix string) bool {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	switch {
	case path == "/agent/v1":
		return RouteAgentLink
	case under("/agent"):
		return RouteAgent
	case under("/api"):
		return RouteAPI
	case under("/m"):
		return RouteMachine
	case under("/agents"):
		return RouteWorkspace
	case under("/sites"):
		return RouteSites
	case under("/apps"):
		return RouteApps
	case under("/dl"):
		return RouteDownload
	case path == "/healthz" || path == "/readyz":
		return RouteHealth
	}
	return RouteOther
}

// ObserveHTTP counts an answered request by the group of its path and the class of its status.
func ObserveHTTP(path string, status int) {
	group := RouteGroup(path)
	class := status/100 - 1
	if class < 0 || class >= len(classNames) {
		class = len(classNames) - 1
	}
	httpRequests[group][class].Add(1)
	if group == RouteAgentLink && status == http.StatusServiceUnavailable {
		AgentLinkUnavailable.Inc()
	}
}

// Writer writes samples in the text format. The samples of one name must be written one after another; the name's
// HELP and TYPE lines go before the first of them.
type Writer struct {
	w    *bufio.Writer
	last string
}

// Counter writes a sample of a counter; name is without Prefix and labels are pairs of a name and a value.
func (w *Writer) Counter(name, help string, v float64, labels ...string) {
	w.sample(name, help, "counter", v, labels)
}

// Gauge writes a sample of a gauge.
func (w *Writer) Gauge(name, help string, v float64, labels ...string) {
	w.sample(name, help, "gauge", v, labels)
}

func (w *Writer) sample(name, help, typ string, v float64, labels []string) {
	name = Prefix + name
	if name != w.last {
		w.last = name
		w.str("# HELP " + name + " " + helpEscaper.Replace(help) + "\n# TYPE " + name + " " + typ + "\n")
	}
	w.str(name)
	for i := 0; i+1 < len(labels); i += 2 {
		if i == 0 {
			w.char('{')
		} else {
			w.char(',')
		}
		w.str(labels[i] + `="` + labelEscaper.Replace(labels[i+1]) + `"`)
	}
	if len(labels) >= 2 {
		w.char('}')
	}
	w.char(' ')
	w.str(strconv.FormatFloat(v, 'g', -1, 64))
	w.char('\n')
}

// str and char write into the buffer. A bufio.Writer keeps its first error and returns it from Flush, so the error of
// a single write is not looked at here.
func (w *Writer) str(s string) { _, _ = w.w.WriteString(s) }

func (w *Writer) char(c byte) { _ = w.w.WriteByte(c) }

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

// Collector writes the gauges of one part of the hub, read at the moment of the scrape.
type Collector func(w *Writer)

// Write writes the process's counters and then what the collectors say.
func Write(out io.Writer, collectors ...Collector) error {
	w := &Writer{w: bufio.NewWriter(out)}
	w.Counter("link_connects_total", "Agent links taken since the hub started.", float64(LinkConnects.Value()))
	w.Counter("link_reconnects_total", "Agent links taken from a machine this hub had a link of before.", float64(LinkReconnects.Value()))
	w.Counter("link_disconnects_total", "Agent links that ended.", float64(LinkDisconnects.Value()))
	w.Counter("link_events_dropped_total", "History frames of machines dropped because their queue was full.", float64(EventsDropped.Value()))
	w.Counter("link_stuck_total", "Agent links closed because their events were not being taken.", float64(LinksStuck.Value()))
	w.Counter("event_streams_dropped_total", "Event streams of browsers ended because they were too slow.", float64(StreamsDropped.Value()))
	w.Counter("machine_timeout_total", "Answers 504 machine_timeout: a machine did not answer in time.", float64(MachineTimeouts.Value()))
	w.Counter("machine_busy_total", "Answers 503 machine_busy: too many requests to a machine at once.", float64(MachineBusy.Value()))
	w.Counter("usage_pull_errors_total", "Pulls of a machine's spend that failed.", float64(UsagePullErrors.Value()))
	w.Counter("agent_link_unavailable_total", "Answers 503 to an agent dialing /agent/v1.", float64(AgentLinkUnavailable.Value()))
	w.Counter("events_relayed_total", "Events passed to the event streams of the other replicas.", float64(EventsRelayed.Value()))
	w.Counter("events_relay_dropped_total", "Events not passed to the other replicas: too long for a notification, or the queue was full.", float64(EventsRelayDropped.Value()))
	w.Counter("events_relay_received_total", "Events taken from the other replicas.", float64(EventsRelayReceived.Value()))
	w.Counter("usage_quarantined_total", "Records of machines' spend the hub could not read and set aside.", float64(UsageQuarantined.Value()))
	w.Counter("automation_runs_requeued_total", "Automation runs put back in the queue because their start did not finish.", float64(AutomationsRequeued.Value()))
	w.Counter("history_chats_purged_total", "Copies of chats deleted on their machines removed after their term.", float64(HistoryChatsPurged.Value()))
	w.Counter("history_rows_purged_total", "Turns and entries removed with the copies of deleted chats.", float64(HistoryRowsPurged.Value()))
	w.Counter("history_purge_errors_total", "Passes of the removal of deleted chats that failed.", float64(HistoryPurgeErrors.Value()))
	for g := range httpRequests {
		for c := range httpRequests[g] {
			w.Counter("http_requests_total", "Requests answered, by the group of the route and the class of the status.",
				float64(httpRequests[g][c].Load()), "route", routeNames[g], "class", classNames[c])
		}
	}
	for _, collect := range collectors {
		if collect != nil {
			collect(w)
		}
	}
	return w.w.Flush()
}

// Handler serves GET /metrics and nothing else. It checks nobody: it belongs on a listener of its own that only the
// collector reaches, never on the hub's public one.
func Handler(collectors ...Collector) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = Write(w, collectors...)
	})
	return mux
}
