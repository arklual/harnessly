package proxy

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A machine that does not answer. The person stays on
// their machine and the hub never swaps it for another; while it is silent the hub shows what it last said of the lists
// the page reads (chats, agents, providers, apps), marked stale, instead of making the page wait for it again.
//
// It is the rollout switch fleetStaleSwitch, per person and machine; no row means off, and an off switch changes nothing.

// fleetStaleSwitch is the rollout switch that turns the silent-machine answer on (docs/hub/rollout-switches.md).
const fleetStaleSwitch = "fleet-stale"

// StaleHeader marks an answer the hub gave from its copy; the value is the age of the copy in seconds.
const StaleHeader = "X-Harnessly-Stale"

var (
	// silentFor is how long a machine that refused or went silent is answered for by the hub where it can.
	silentFor = 20 * time.Second
	// staleKeep is how long a copy stands in for a machine.
	staleKeep = 10 * time.Minute
	// staleListWait is how long a list that has a copy waits for the machine before the copy answers.
	staleListWait = 8 * time.Second
)

const (
	// staleMaxBody is the biggest answer kept; a longer one is passed on and not kept.
	staleMaxBody = 256 << 10
	// staleBudget is the memory all copies together may hold.
	staleBudget = 64 << 20
	// staleMaxMarks bounds the table of silent machines.
	staleMaxMarks = 5000
)

type staleCopy struct {
	body  []byte
	ctype string
	at    time.Time
}

// staleBook keeps the silent machines and the last good lists. The key of a list holds the person, so a copy is never
// shown to anyone else.
type staleBook struct {
	mu     sync.Mutex
	silent map[string]time.Time
	copies map[string]staleCopy
	size   int
}

type staleWaitKey struct{}

// staleList says whether the request is a read of a list the hub keeps a copy of, and the key of its copy.
func staleList(r *http.Request, userID, machine string) (string, bool) {
	if r.Method != http.MethodGet || strings.Contains(r.Header.Get("Accept"), "text/event-stream") ||
		r.Header.Get("Upgrade") != "" {
		return "", false
	}
	rest, ok := v2Path(r.URL.Path, machine)
	if !ok {
		return "", false
	}
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 1 && (rest == "hello" || rest == "agents" || rest == "providers" || rest == "apps" || rest == "conversations"):
	case len(parts) == 3 && parts[0] == "conversations" && parts[1] != "" && (parts[2] == "made" || parts[2] == "tasks"):
	default:
		return "", false
	}
	q := r.URL.Query()
	if q.Has("wait") || q.Has("timeout") {
		return "", false // a long poll
	}
	return userID + "\n" + machine + "\n" + rest + "?" + url.Values(q).Encode(), true
}

// staleOn says whether the switch is on for this person on this machine.
func (s *Service) staleOn(ctx context.Context, u store.User, m store.Machine) bool {
	return s.switches != nil && s.switches.On(ctx, fleetStaleSwitch, strings.ToLower(u.Email), m.ID)
}

func (b *staleBook) markDown(id string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.silent == nil {
		b.silent = map[string]time.Time{}
	}
	if len(b.silent) >= staleMaxMarks {
		for k, t := range b.silent {
			if !t.After(now) {
				delete(b.silent, k)
			}
		}
		if len(b.silent) >= staleMaxMarks {
			return
		}
	}
	b.silent[id] = now.Add(silentFor)
}

func (b *staleBook) markUp(id string) {
	b.mu.Lock()
	delete(b.silent, id)
	b.mu.Unlock()
}

func (b *staleBook) isDown(id string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, ok := b.silent[id]
	if !ok {
		return false
	}
	if until.After(now) {
		return true
	}
	delete(b.silent, id)
	return false
}

// keep stores the good answer to a list.
func (b *staleBook) keep(key, ctype string, body []byte, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.copies == nil {
		b.copies = map[string]staleCopy{}
	}
	if old, ok := b.copies[key]; ok {
		b.size -= len(old.body)
	}
	b.copies[key] = staleCopy{body: append([]byte(nil), body...), ctype: ctype, at: now}
	b.size += len(body)
	for b.size > staleBudget || len(b.copies) > 4096 {
		oldest, first := "", true
		var at time.Time
		for k, c := range b.copies {
			if first || c.at.Before(at) {
				oldest, at, first = k, c.at, false
			}
		}
		b.size -= len(b.copies[oldest].body)
		delete(b.copies, oldest)
	}
}

func (b *staleBook) get(key string, now time.Time) (staleCopy, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.copies[key]
	if !ok {
		return staleCopy{}, false
	}
	if now.Sub(c.at) > staleKeep {
		b.size -= len(c.body)
		delete(b.copies, key)
		return staleCopy{}, false
	}
	return c, true
}

// serveStale answers from the copy, and says whether there was one.
func (s *Service) serveStale(w http.ResponseWriter, key string) bool {
	c, ok := s.stale.get(key, s.now())
	if !ok {
		return false
	}
	h := w.Header()
	h.Set("Content-Type", c.ctype)
	h.Set("Cache-Control", "no-store")
	h.Set(StaleHeader, strconv.FormatInt(int64(s.now().Sub(c.at).Seconds()), 10))
	frameHeaders(h)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(c.body)
	return true
}

// staleTee passes a list to the page and keeps a copy of it.
type staleTee struct {
	http.ResponseWriter
	status int
	ctype  string
	body   bytes.Buffer
	over   bool
}

func (t *staleTee) WriteHeader(code int) {
	if t.status == 0 {
		t.status = code
		t.ctype = t.ResponseWriter.Header().Get("Content-Type")
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *staleTee) Write(b []byte) (int, error) {
	if t.status == 0 {
		t.WriteHeader(http.StatusOK)
	}
	if !t.over && t.body.Len()+len(b) <= staleMaxBody {
		t.body.Write(b)
	} else {
		t.over = true
	}
	return t.ResponseWriter.Write(b)
}

func (t *staleTee) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (t *staleTee) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// keepList keeps the answer when it was a whole good JSON one; the machine answered, so it is no longer silent.
func (s *Service) keepList(id, key string, t *staleTee) {
	if t.status == 0 {
		return
	}
	s.stale.markUp(id)
	if t.status != http.StatusOK || t.over || !strings.HasPrefix(t.ctype, "application/json") {
		return
	}
	s.stale.keep(key, t.ctype, t.body.Bytes(), s.now())
}
