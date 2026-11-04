package agentbrowser

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// ProfileAsk is shown only to the person; profile names are supplied separately by page routes.
type ProfileAsk struct {
	ID      string `json:"id"`
	Conv    string `json:"conv"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	At      int64  `json:"at"`
	Project string `json:"-"`
}

// ProfileAnswer is the complete agent-visible result of a request.
type ProfileAnswer struct {
	Status  string `json:"status"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
}

type profileAskState struct {
	ProfileAsk
	answer  ProfileAnswer
	done    chan struct{}
	settled time.Time
}

// ProfileAsks coalesces retries per conversation and bounds pending cards and waits.
type ProfileAsks struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*profileAskState
	limiter *vault.Limiter
}

// NewProfileAsks creates a request queue independent of browser panel connections.
func NewProfileAsks() *ProfileAsks {
	return &ProfileAsks{now: time.Now, entries: map[string]*profileAskState{},
		limiter: vault.NewLimiter(vault.Rule{Limit: 6, Window: 10 * time.Minute})}
}

func (a *ProfileAsks) prune() {
	now := a.now()
	for id, x := range a.entries {
		if x.answer.Status == "waiting" && now.Sub(time.UnixMilli(x.At)) >= 30*time.Minute {
			x.answer = ProfileAnswer{Status: "declined"}
			x.settled = now
			close(x.done)
		}
		if x.answer.Status != "waiting" && now.Sub(x.settled) >= 2*time.Minute {
			delete(a.entries, id)
		}
	}
}

// Open keeps a repeated request waiting on the same card and remembers a recent answer.
func (a *ProfileAsks) Open(conv, project, title, why string) (ProfileAsk, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	why = secrets.CleanWhy(why)
	if why == "" {
		return ProfileAsk{}, errors.New("A reason is required")
	}
	waiting := 0
	var recent *profileAskState
	for _, x := range a.entries {
		if x.answer.Status == "waiting" {
			waiting++
			if x.Conv == conv && x.Project == project {
				return x.ProfileAsk, nil
			}
		} else if x.Conv == conv && x.Project == project && x.Why == why && (recent == nil || x.settled.After(recent.settled)) {
			recent = x
		}
	}
	if recent != nil {
		return recent.ProfileAsk, nil
	}
	if waiting >= 16 {
		return ProfileAsk{}, errors.New("Too many browser profile requests")
	}
	if ok, _ := a.limiter.Take(conv, a.now().UnixMilli()); !ok {
		return ProfileAsk{}, errors.New("Too many browser profile requests")
	}
	x := &profileAskState{ProfileAsk: ProfileAsk{ID: randomProfileID(), Conv: conv, Project: project,
		Title: secrets.CleanWhy(title), Why: why, At: a.now().UnixMilli()}, answer: ProfileAnswer{Status: "waiting"}, done: make(chan struct{})}
	a.entries[x.ID] = x
	return x.ProfileAsk, nil
}

// Wait returns within d; a timeout preserves the pending card for the next call.
func (a *ProfileAsks) Wait(ctx context.Context, id string, d time.Duration) ProfileAnswer {
	a.mu.Lock()
	a.prune()
	x := a.entries[id]
	a.mu.Unlock()
	if x == nil {
		return ProfileAnswer{Status: "declined"}
	}
	timer := time.NewTimer(max(0, min(d, 55*time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-x.done:
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	return x.answer
}

// Pending lists open cards for the person's page in creation order.
func (a *ProfileAsks) Pending() []ProfileAsk {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	out := []ProfileAsk{}
	for _, x := range a.entries {
		if x.answer.Status == "waiting" {
			out = append(out, x.ProfileAsk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At || out[i].At == out[j].At && out[i].ID < out[j].ID })
	return out
}

// Find returns a pending request for a page decision.
func (a *ProfileAsks) Find(id string) (ProfileAsk, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	x := a.entries[id]
	if x == nil || x.answer.Status != "waiting" {
		return ProfileAsk{}, false
	}
	return x.ProfileAsk, true
}

// Settle wakes every retry after the permission change has been persisted successfully.
func (a *ProfileAsks) Settle(id string, answer ProfileAnswer) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	x := a.entries[id]
	if x == nil || x.answer.Status != "waiting" {
		return false
	}
	x.answer = answer
	x.settled = a.now()
	close(x.done)
	return true
}

// Forget declines pending requests when a conversation moves or disappears.
func (a *ProfileAsks) Forget(conv string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, x := range a.entries {
		if x.Conv == conv && x.answer.Status == "waiting" {
			x.answer = ProfileAnswer{Status: "declined"}
			x.settled = a.now()
			close(x.done)
		}
	}
}
