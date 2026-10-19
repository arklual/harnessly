// Package telemetry reports a runtime's own events to a PostHog-compatible batch endpoint: queued, sent a minute after
// the first of them (or at once when a batch is full), never more than a batch per request. Only a released runtime
// reports; a checkout stays quiet. Events carry technical properties only, never a prompt, a title or a path.
package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"

	"harnessly/internal/jsvalue"
)

// MaxEvents is the most events one request carries.
const MaxEvents = 40

// Config says where events go and what each of them carries.
type Config struct {
	// URL is the batch endpoint ("" sends nothing); Key the project key it takes.
	URL, Key string
	// Common are the properties every event carries first, in their order.
	Common *jsvalue.Object
	// DistinctID names who the events are about, read at each send.
	DistinctID func() string
	Delay      time.Duration
	Client     *http.Client
	Log        func(level, msg string, kv ...any)
}

// Event is one event: its name, when it happened and its own properties.
type Event struct {
	Name       string
	At         time.Time
	Properties *jsvalue.Object
}

// Queue batches events.
type Queue struct {
	cfg   Config
	mu    sync.Mutex
	queue []Event
	timer *time.Timer
}

func New(cfg Config) *Queue {
	if cfg.Delay == 0 {
		cfg.Delay = time.Minute
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	return &Queue{cfg: cfg}
}

// Enabled says whether events are sent anywhere.
func (q *Queue) Enabled() bool { return q != nil && q.cfg.URL != "" }

// Push queues an event.
func (q *Queue) Push(e Event) {
	if !q.Enabled() {
		return
	}
	q.mu.Lock()
	q.queue = append(q.queue, e)
	full := len(q.queue) >= MaxEvents
	if !full && q.timer == nil {
		q.timer = time.AfterFunc(q.cfg.Delay, q.Flush)
	}
	q.mu.Unlock()
	if full {
		q.Flush()
	}
}

// Flush sends what is queued now.
func (q *Queue) Flush() {
	q.mu.Lock()
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
	pending := q.queue
	q.queue = nil
	q.mu.Unlock()
	for i := 0; i < len(pending); i += MaxEvents {
		go q.send(pending[i:min(i+MaxEvents, len(pending))])
	}
}

// Body is what the batch endpoint takes: the person and the common properties go into every event.
func Body(key, distinctID string, common *jsvalue.Object, events []Event) []byte {
	batch := make([]any, len(events))
	for i, e := range events {
		props := jsvalue.NewObject()
		if common != nil {
			props.Assign(common)
		}
		if e.Properties != nil {
			props.Assign(e.Properties)
		}
		props.Set("distinct_id", distinctID)
		props.Set("$process_person_profile", false)
		batch[i] = jsvalue.NewObject("event", e.Name, "timestamp", e.At.UTC().Format("2006-01-02T15:04:05.000Z"), "properties", props)
	}
	return jsvalue.Stringify(jsvalue.NewObject("api_key", key, "batch", batch), "")
}

func (q *Queue) send(events []Event) {
	id := ""
	if q.cfg.DistinctID != nil {
		id = q.cfg.DistinctID()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.cfg.URL, bytes.NewReader(Body(q.cfg.Key, id, q.cfg.Common, events)))
	if err != nil {
		q.cfg.Log("debug", "telemetry not sent", "error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := q.cfg.Client.Do(req)
	if err != nil {
		q.cfg.Log("debug", "telemetry not sent", "error", err.Error())
		return
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		q.cfg.Log("debug", "telemetry not accepted", "status", res.StatusCode)
	}
}
