// Package webvitals is how the page tells the server how fast it was: the time from opening Sinopia's
// home to every card drawn, and the largest contentful paint. The page sends one small POST per number; the hub
// counts it in a histogram (harnessly_hub_web_vital_seconds{name}) and a runtime without a hub writes it to its own
// journal. The set of names is closed: a name nobody listed is dropped, so a label never grows from what a client sends.
package webvitals

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// The names the page may send.
const (
	SinopiaHomeFull = "sinopia_home_full" // from opening the home to all its cards drawn
	LCP             = "lcp"               // largest contentful paint of the page
)

// Names is the closed set, in the order the metric lists them.
var Names = [...]string{SinopiaHomeFull, LCP}

// MaxSeconds is the longest time taken as a measurement; a tab left in the background for an hour says nothing of speed.
const MaxSeconds = 120

// bodyMax is the most a report may weigh.
const bodyMax = 512

// Known says the name is in the closed set.
func Known(name string) bool {
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}

// Sample is one number of the page.
type Sample struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
}

// Valid says the sample is a name of the set and a time that can be one.
func (s Sample) Valid() bool {
	return Known(s.Name) && !math.IsNaN(s.Seconds) && s.Seconds > 0 && s.Seconds <= MaxSeconds
}

// Limiter lets one key send so many reports in a window; the rest are dropped.
type Limiter struct {
	Max    int
	Window time.Duration
	mu     sync.Mutex
	seen   map[string]*bucket
}

type bucket struct {
	start time.Time
	n     int
}

// Allow counts a report of the key and says whether it may pass.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string]*bucket{}
	}
	if len(l.seen) > 4096 {
		for k, b := range l.seen {
			if now.Sub(b.start) >= l.Window {
				delete(l.seen, k)
			}
		}
	}
	b := l.seen[key]
	if b == nil || now.Sub(b.start) >= l.Window {
		b = &bucket{start: now}
		l.seen[key] = b
	}
	b.n++
	return b.n <= l.Max
}

// NewLimiter is 60 reports a minute for one key: a page sends a couple in a visit.
func NewLimiter() *Limiter { return &Limiter{Max: 60, Window: time.Minute} }

// Serve answers one report: POST {name, seconds}. A sample that passes is given to sink; a name nobody listed or a
// time that cannot be one is answered 204 and dropped (the page has nothing to fix), a bad body is 400, too many
// reports from one key 429. key names who sends (the person, the address).
func Serve(w http.ResponseWriter, r *http.Request, lim *Limiter, key string, sink func(Sample)) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyMax+1))
	var s Sample
	if err != nil || len(raw) > bodyMax || json.Unmarshal(raw, &s) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if lim != nil && !lim.Allow(key, time.Now()) {
		http.Error(w, "too many reports", http.StatusTooManyRequests)
		return
	}
	if s.Valid() {
		sink(s)
	}
	w.WriteHeader(http.StatusNoContent)
}
