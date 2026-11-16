package looks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
	"unicode/utf8"
)

// The scopes a look is put on: everything, the folder of the agent's chat, or the chat itself.
const (
	ScopeAll    = "all"
	ScopeFolder = "folder"
	ScopeChat   = "chat"
)

// The states of a request.
const (
	StatePending = "pending"
	StateApplied = "applied"
	StateFailed  = "failed"
	StateExpired = "expired"
)

// Request is an agent's change of the look, waiting for the person's page to put it on (the page owns the prefs: on a
// hub they are the hub user's, which a machine never writes).
type Request struct {
	ID    string `json:"id"`
	At    int64  `json:"at"`
	Conv  string `json:"conv,omitempty"`
	Scope string `json:"scope"`
	// Look is the look to put on; nil with Reset takes the scope's own look away.
	Look  *Look `json:"look,omitempty"`
	Reset bool  `json:"reset,omitempty"`
	// FxCode is the JS of a custom live effect: the page uploads it (PUT /api/v2/me/looks) and sets look.fx.script to
	// its hash itself, so Look.Fx may be absent or have no script yet. Look is then set.
	FxCode string `json:"fxCode,omitempty"`
	Why    string `json:"why,omitempty"`
	State  string `json:"state"`
	// Note is what the page said: why it failed.
	Note string `json:"note,omitempty"`
}

// MaxFxCode is the longest fxCode of a request, in bytes (the store takes up to wire.MaxLookFx).
const MaxFxCode = 128 << 10

// RequestTTL is how long a request waits for a page.
const RequestTTL = 10 * time.Minute

// Inbox holds the agents' requests and the last looks a page reported (for `harnessly theme get`).
type Inbox struct {
	mu    sync.Mutex
	list  []*Request
	state json.RawMessage
	ch    chan struct{}
	// Moved is told when a request comes, so pages that watch the settings read the inbox (settingsctl's revision).
	Moved func()
	Now   func() time.Time
}

func (b *Inbox) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Inbox) wake() {
	if b.ch != nil {
		close(b.ch)
	}
	b.ch = make(chan struct{})
}

func (b *Inbox) prune() {
	cut := b.now().Add(-RequestTTL).UnixMilli()
	keep := b.list[:0]
	for _, r := range b.list {
		if r.At < cut {
			if r.State == StatePending {
				r.State = StateExpired
			}
			continue
		}
		keep = append(keep, r)
	}
	b.list = keep
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "lk_" + hex.EncodeToString(b[:])
}

// Add puts a request in and tells the pages.
func (b *Inbox) Add(r Request) Request {
	b.mu.Lock()
	b.prune()
	r.ID, r.At, r.State = newID(), b.now().UnixMilli(), StatePending
	b.list = append(b.list, &r)
	if len(b.list) > 50 {
		b.list = b.list[len(b.list)-50:]
	}
	b.wake()
	out := r
	b.mu.Unlock()
	if b.Moved != nil {
		b.Moved()
	}
	return out
}

// Pending is the requests no page has put on yet, oldest first.
func (b *Inbox) Pending() []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()
	out := []Request{}
	for _, r := range b.list {
		if r.State == StatePending {
			out = append(out, *r)
		}
	}
	return out
}

// Done records what a page did with a request; false for one it does not hold (or answered already).
func (b *Inbox) Done(id, state, note string) bool {
	if state != StateApplied && state != StateFailed {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.list {
		if r.ID == id && r.State == StatePending {
			r.State, r.Note = state, note
			b.wake()
			return true
		}
	}
	return false
}

// Wait answers the request once a page put it on (or failed), or as it is after d.
func (b *Inbox) Wait(ctx context.Context, id string, d time.Duration) (Request, bool) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		b.mu.Lock()
		var cur *Request
		for _, r := range b.list {
			if r.ID == id {
				cur = r
			}
		}
		if b.ch == nil {
			b.ch = make(chan struct{})
		}
		ch := b.ch
		if cur == nil {
			b.mu.Unlock()
			return Request{}, false
		}
		out := *cur
		b.mu.Unlock()
		if out.State != StatePending {
			return out, true
		}
		select {
		case <-ch:
		case <-deadline.C:
			return out, true
		case <-ctx.Done():
			return out, true
		}
	}
}

// SetState keeps the looks a page reported (the prefs value hx-looks).
func (b *Inbox) SetState(raw json.RawMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = append(json.RawMessage{}, raw...)
}

// State is the looks a page last reported; nil when none did.
func (b *Inbox) State() json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Ask is an agent's change as it comes (the CLI's body, the MCP tool's arguments).
type Ask struct {
	Scope  string
	Conv   string
	Look   json.RawMessage
	Preset string
	Reset  bool
	// FxCode is the JS of a custom live effect; it needs a look of the agent's own.
	FxCode string
	Why    string
	Wait   time.Duration
}

// Submit checks an agent's change, puts it in and waits up to a.Wait for a page: the one core of `harnessly theme` and
// the MCP tool theme_set. The status is applied, failed or waiting.
func (b *Inbox) Submit(ctx context.Context, a Ask) (Request, string, error) {
	if a.Scope == "" {
		a.Scope = ScopeAll
	}
	if a.Scope != ScopeAll && a.Scope != ScopeFolder && a.Scope != ScopeChat {
		return Request{}, "", bad("scope is all, folder or chat")
	}
	if a.Scope != ScopeAll && a.Conv == "" {
		return Request{}, "", bad("scope %s needs a chat: run it from the agent's chat", a.Scope)
	}
	req := Request{Scope: a.Scope, Conv: a.Conv, Why: a.Why, Reset: a.Reset}
	if a.FxCode != "" {
		switch {
		case len(a.FxCode) > MaxFxCode:
			return Request{}, "", bad("fxCode is longer than %d bytes", MaxFxCode)
		case !utf8.ValidString(a.FxCode):
			return Request{}, "", bad("fxCode is not valid UTF-8")
		case a.Reset || a.Preset != "" || len(a.Look) == 0:
			return Request{}, "", bad("fxCode goes with a look of your own (not a preset or reset)")
		}
		req.FxCode = a.FxCode
	}
	switch {
	case a.Reset:
	case a.Preset != "":
		p, ok := PresetByID(a.Preset)
		if !ok {
			return Request{}, "", bad("no preset %q (harnessly theme presets)", a.Preset)
		}
		l := p.Look
		if l.Name == "" {
			l.Name = p.Name
		}
		req.Look = &l
	case len(a.Look) > 0:
		l, err := parse(a.Look, a.FxCode != "")
		if err != nil {
			return Request{}, "", err
		}
		req.Look = &l
	default:
		return Request{}, "", bad("give a look, a preset or reset")
	}
	added := b.Add(req)
	out, _ := b.Wait(ctx, added.ID, max(min(a.Wait, 25*time.Second), 0))
	status := out.State
	if status == StatePending || status == StateExpired {
		status = "waiting"
	}
	return out, status, nil
}
