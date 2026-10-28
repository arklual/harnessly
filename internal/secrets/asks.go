package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// How an ask ended, or that it still waits.
const (
	AskWaiting  = "waiting"
	AskReady    = "ready"
	AskDeclined = "declined"
)

// Reasons an ask is refused with.
const (
	ReasonRateLimited = "rate-limited"
	ReasonTooMany     = "too-many"
)

const (
	// askTTL is how long an ask stays on the person's screen.
	askTTL = 30 * time.Minute
	// settledKept is how long a declined ask still answers a repeated ask of the same name.
	settledKept = 2 * time.Minute
	// pageSeen: a page that looked at the asks within this is open.
	pageSeen = 15 * time.Second
	maxAsks  = 16
	maxWhy   = 300
)

// Ask is an agent's request that the person put a secret in the safe, as the page shows it.
type Ask struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Why is the agent's one line of what it needs the secret for.
	Why string `json:"why"`
	// Conv and Title are the conversation that asked ("" when the asking command ran outside one).
	Conv  string `json:"conv"`
	Title string `json:"title"`
	// Replace: the agent asks for a new value of a secret the safe has (it expired, it was revoked).
	Replace bool  `json:"replace"`
	At      int64 `json:"at"`
}

type ask struct {
	Ask
	state   string
	settled time.Time
	done    chan struct{}
}

// Asks are the open asks of this machine: an agent waits on one, the page lists them and settles them.
type Asks struct {
	Now func() time.Time

	mu      sync.Mutex
	list    map[string]*ask
	limiter *vault.Limiter
	seen    time.Time
}

// NewAsks is the asks with at most 6 a conversation in 10 minutes.
func NewAsks() *Asks {
	return &Asks{Now: time.Now, list: map[string]*ask{}, limiter: vault.NewLimiter(vault.Rule{Limit: 6, Window: 10 * time.Minute})}
}

// CleanWhy is the agent's reason as one safe line.
func CleanWhy(why string) string {
	return jscompat.Slice(strings.TrimSpace(tmux.OneLineSafe(why)), maxWhy)
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "sa_" + hex.EncodeToString(b)
}

// prune drops what is past its time; the caller holds mu.
func (a *Asks) prune(now time.Time) {
	for id, x := range a.list {
		switch {
		case x.state == AskWaiting && now.Sub(time.UnixMilli(x.At)) >= askTTL:
			x.state = AskDeclined
			x.settled = now
			close(x.done)
		case x.state != AskWaiting && now.Sub(x.settled) >= settledKept:
			delete(a.list, id)
		}
	}
}

// Open is an ask of name: the waiting one of that name when there is one (an agent asking again keeps waiting on it),
// a declined one of the last two minutes (the person said no), else a new one.
func (a *Asks) Open(name, why, conv, title string, replace bool) (Ask, string, error) {
	if !ValidName(name) {
		return Ask{}, "", refuse(ReasonBadName, nil)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.Now()
	a.prune(now)
	for _, x := range a.list {
		if x.Name == name && (x.state == AskWaiting || x.state == AskDeclined) {
			return x.Ask, x.state, nil
		}
	}
	waiting := 0
	for _, x := range a.list {
		if x.state == AskWaiting {
			waiting++
		}
	}
	if waiting >= maxAsks {
		return Ask{}, "", refuse(ReasonTooMany, nil)
	}
	if ok, _ := a.limiter.Take("conv:"+conv, now.UnixMilli()); !ok {
		return Ask{}, "", refuse(ReasonRateLimited, nil)
	}
	x := &ask{Ask: Ask{ID: newID(), Name: name, Why: CleanWhy(why), Conv: conv, Title: jscompat.Slice(tmux.OneLineSafe(title), 200),
		Replace: replace, At: now.UnixMilli()}, state: AskWaiting, done: make(chan struct{})}
	a.list[x.ID] = x
	return x.Ask, AskWaiting, nil
}

// Wait is how the ask id stands after at most d (or ctx's end): ready, declined, or still waiting.
func (a *Asks) Wait(ctx context.Context, id string, d time.Duration) string {
	a.mu.Lock()
	x := a.list[id]
	a.mu.Unlock()
	if x == nil {
		return AskDeclined
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-x.done:
	case <-t.C:
	case <-ctx.Done():
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return x.state
}

// Pending are the asks waiting for the person, oldest first; the call counts as a page looking.
func (a *Asks) Pending() []Ask {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.Now()
	a.prune(now)
	a.seen = now
	out := []Ask{}
	for _, x := range a.list {
		if x.state == AskWaiting {
			out = append(out, x.Ask)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At || (out[i].At == out[j].At && out[i].ID < out[j].ID) })
	return out
}

// PageOpen says whether a page looked at the asks lately (the new interface asks every few seconds while open).
func (a *Asks) PageOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.seen.IsZero() && a.Now().Sub(a.seen) < pageSeen
}

func (a *Asks) settle(x *ask, state string) {
	if x.state != AskWaiting {
		return
	}
	x.state = state
	x.settled = a.Now()
	close(x.done)
}

// Fulfil settles every waiting ask of name as ready: the secret was put.
func (a *Asks) Fulfil(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, x := range a.list {
		if x.Name == name {
			a.settle(x, AskReady)
		}
	}
}

// Decline settles a waiting ask as declined; false for an id that waits no more.
func (a *Asks) Decline(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	x := a.list[id]
	if x == nil || x.state != AskWaiting {
		return false
	}
	a.settle(x, AskDeclined)
	return true
}
