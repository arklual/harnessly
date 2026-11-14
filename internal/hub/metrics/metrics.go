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
	"time"

	"github.com/ly-solutions/harnessly/internal/webvitals"
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

// Reads of people's data by what answered them: the hub's store, or the machine the data came from. A read
// that goes to the machine is one the person cannot make while that machine is off, so the share of them is how far
// the data is from living in the hub.
const (
	ReadDesign = iota
	ReadUpload
	ReadDelivered
	readKinds
)

// Where a read's bytes came from.
const (
	FromStore = iota
	FromMachine
	readSources
)

var (
	readKindNames   = [readKinds]string{"design", "upload", "delivered"}
	readSourceNames = [readSources]string{"store", "machine"}
	dataReads       [readKinds][readSources]atomic.Int64
)

// CountRead counts a read of a kind of data by where it was answered from; anything else is ignored.
func CountRead(kind, from int) {
	if kind < 0 || kind >= readKinds || from < 0 || from >= readSources {
		return
	}
	dataReads[kind][from].Add(1)
}

// Why a read went to the machine: what stands between the data and the hub's store. The names are the
// journal's of the machine's move, and three of the hub's own.
const (
	MissUnreported = "unreported" // the machine never named the object to the hub: an old runtime, or a move not started
	MissPending    = "pending"    // named, not moved yet
	MissFailed     = "failed"     // the move of it failed; the hub asks the machine to try again
	MissSkipped    = "skipped"    // a person left it on the machine, or it is over a limit
	MissForeign    = "foreign"    // moved, but the reader is not the person it is bound to
	MissLost       = "lost"       // the journal says moved, the store does not have the bytes
)

var (
	missReasons = []string{MissUnreported, MissPending, MissFailed, MissSkipped, MissForeign, MissLost}
	dataMisses  [readKinds][6]atomic.Int64
)

// CountMiss counts a read that went to the machine by why; an unknown reason is ignored.
func CountMiss(kind int, reason string) {
	if kind < 0 || kind >= readKinds {
		return
	}
	for i, r := range missReasons {
		if r == reason {
			dataMisses[kind][i].Add(1)
		}
	}
}

// Misses is how many reads of a kind went to the machine for a reason since the process started.
func Misses(kind int, reason string) int64 {
	if kind < 0 || kind >= readKinds {
		return 0
	}
	for i, r := range missReasons {
		if r == reason {
			return dataMisses[kind][i].Load()
		}
	}
	return 0
}

// Reads is how many reads of a kind were answered from a source since the process started.
func Reads(kind, from int) int64 {
	if kind < 0 || kind >= readKinds || from < 0 || from >= readSources {
		return 0
	}
	return dataReads[kind][from].Load()
}

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

// How long the hub takes to answer, by a finer group than the counter's: a machine's design and history
// calls and the store's are what a person waits on in Sinopia and the chat list, so they are apart from the rest of
// /m/. Streams (a socket, an event stream, a long poll) are not timed: their time is how long someone listened.

const (
	areaDesign  = routeGroups + iota // /m/…/design/…, /m/…/designly/…
	areaHistory                      // /m/…/history/…, /api/…/history/…
	areaStore                        // /store/
	latencyAreas
)

var areaNames = [latencyAreas]string{"agent_link", "agent", "api", "machine", "workspace", "sites", "apps", "download", "health", "other", "design", "history", "store"}

// latencyBuckets are the upper bounds in seconds; 0.4 and 1 are the targets of lists and screens (docs/hub/deploy.md, HUB_METRICS_LISTEN).
var latencyBuckets = [...]float64{0.025, 0.05, 0.1, 0.2, 0.4, 0.7, 1, 2, 5, 15}

var methodNames = [...]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "other"}

type latency struct {
	buckets [len(latencyBuckets)]atomic.Int64
	count   atomic.Int64
	micros  atomic.Int64 // the sum, in microseconds
}

var httpLatency [latencyAreas][len(methodNames)]latency

// LatencyArea is the group a request's time is counted under.
func LatencyArea(path string) int {
	g := RouteGroup(path)
	switch {
	case g == RouteMachine && (strings.Contains(path, "/design/") || strings.Contains(path, "/designly/")):
		return areaDesign
	case (g == RouteMachine || g == RouteAPI) && strings.Contains(path, "/history/"):
		return areaHistory
	case path == "/store" || strings.HasPrefix(path, "/store/"):
		return areaStore
	}
	return g
}

// Streamed says the request is a stream whose time is not an answer's: a long poll or an event stream by its path.
func Streamed(path string) bool {
	return strings.HasSuffix(path, "/events") || strings.HasSuffix(path, "/changes") || strings.HasSuffix(path, "/stream") ||
		path == "/agent/v1"
}

func methodIndex(m string) int {
	for i, n := range methodNames[:len(methodNames)-1] {
		if n == m {
			return i
		}
	}
	return len(methodNames) - 1
}

// ObserveLatency records how long an answered request took. A switch of protocols (a socket) is not an answer.
func ObserveLatency(path, method string, status int, d time.Duration) {
	if status == http.StatusSwitchingProtocols || Streamed(path) {
		return
	}
	l := &httpLatency[LatencyArea(path)][methodIndex(method)]
	sec := d.Seconds()
	for i, le := range latencyBuckets {
		if sec <= le {
			l.buckets[i].Add(1)
			break
		}
	}
	l.count.Add(1)
	l.micros.Add(d.Microseconds())
}

// How long the page took to show what a person waits for (internal/webvitals): the page reports it, the hub
// counts it. The names are the closed set of webvitals.Names; anything else is dropped before it is a label.
var webVitalBuckets = [...]float64{0.25, 0.5, 1, 1.5, 2, 2.5, 4, 6, 10, 30}

type vital struct {
	buckets [len(webVitalBuckets)]atomic.Int64
	count   atomic.Int64
	micros  atomic.Int64
}

var webVitals [len(webvitals.Names)]vital

// ObserveWebVital records a number of the page; a name outside the set or a time that cannot be one is ignored.
func ObserveWebVital(name string, seconds float64) {
	s := webvitals.Sample{Name: name, Seconds: seconds}
	if !s.Valid() {
		return
	}
	for i, n := range webvitals.Names {
		if n != name {
			continue
		}
		l := &webVitals[i]
		for j, le := range webVitalBuckets {
			if seconds <= le {
				l.buckets[j].Add(1)
				break
			}
		}
		l.count.Add(1)
		l.micros.Add(int64(seconds * 1e6))
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
	w.sample(name, help, "counter", v, labels, name)
}

// Gauge writes a sample of a gauge.
func (w *Writer) Gauge(name, help string, v float64, labels ...string) {
	w.sample(name, help, "gauge", v, labels, name)
}

// sample writes one line; family is the name the HELP and TYPE lines carry (a histogram's, for its _bucket, _sum and
// _count lines).
func (w *Writer) sample(name, help, typ string, v float64, labels []string, family string) {
	name, family = Prefix+name, Prefix+family
	if family != w.last {
		w.last = family
		w.str("# HELP " + family + " " + helpEscaper.Replace(help) + "\n# TYPE " + family + " " + typ + "\n")
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
	const durName, durHelp = "http_request_duration_seconds", "Time to answer a request, by the group of the route and the method; streams are left out."
	for a := range httpLatency {
		for m := range httpLatency[a] {
			l := &httpLatency[a][m]
			n := l.count.Load()
			if n == 0 {
				continue
			}
			labels := []string{"route", areaNames[a], "method", methodNames[m]}
			var cum int64
			for i, le := range latencyBuckets {
				cum += l.buckets[i].Load()
				w.sample(durName+"_bucket", durHelp, "histogram", float64(cum), append(labels, "le", strconv.FormatFloat(le, 'g', -1, 64)), durName)
			}
			w.sample(durName+"_bucket", durHelp, "histogram", float64(n), append(labels, "le", "+Inf"), durName)
			w.sample(durName+"_sum", durHelp, "histogram", float64(l.micros.Load())/1e6, labels, durName)
			w.sample(durName+"_count", durHelp, "histogram", float64(n), labels, durName)
		}
	}
	const vitalName, vitalHelp = "web_vital_seconds", "How long the page took to show what a person waits for (sinopia_home_full: from opening Sinopia's home to every card drawn; lcp: largest contentful paint), as the page reported it."
	for i := range webVitals {
		l := &webVitals[i]
		n := l.count.Load()
		if n == 0 {
			continue
		}
		labels := []string{"name", webvitals.Names[i]}
		var cum int64
		for j, le := range webVitalBuckets {
			cum += l.buckets[j].Load()
			w.sample(vitalName+"_bucket", vitalHelp, "histogram", float64(cum), append(labels, "le", strconv.FormatFloat(le, 'g', -1, 64)), vitalName)
		}
		w.sample(vitalName+"_bucket", vitalHelp, "histogram", float64(n), append(labels, "le", "+Inf"), vitalName)
		w.sample(vitalName+"_sum", vitalHelp, "histogram", float64(l.micros.Load())/1e6, labels, vitalName)
		w.sample(vitalName+"_count", vitalHelp, "histogram", float64(n), labels, vitalName)
	}
	for k := range dataMisses {
		for i, reason := range missReasons {
			w.Counter("data_read_misses_total", "Reads of people's data that went to the machine, by its kind and by why the hub's store could not answer.",
				float64(dataMisses[k][i].Load()), "kind", readKindNames[k], "reason", reason)
		}
	}
	for k := range dataReads {
		for f := range dataReads[k] {
			w.Counter("data_reads_total", "Reads of people's data, by its kind and by what answered: the hub's store or the machine it came from.",
				float64(dataReads[k][f].Load()), "kind", readKindNames[k], "source", readSourceNames[f])
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
