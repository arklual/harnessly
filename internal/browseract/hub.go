// Package browseract lets the coding agent act on the page in the browser tab of the person's right pane. The agent's
// tool call becomes an action; the person's page takes it with a long poll, runs it in the page's frame, asks the person
// where the site is new, and posts the outcome, which goes back to the agent that waits.
package browseract

import (
	"context"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/panel"
)

// Defaults of a Hub; each is a field so a test can shorten it.
const (
	holdFor       = 25 * time.Second  // a poll with nothing to give ends after this (under any proxy's idle limit)
	presentFor    = 35 * time.Second  // a page stays present this long after its last poll ended
	answerTimeout = 30 * time.Second  // the agent waits this long for the page
	askTimeout    = 120 * time.Second // ... this long once the page is asking the person
	maxPolls      = 4                 // open polls of one chat
	rateLimit     = 120               // actions a minute of one chat
	rateWindowMs  = 60_000
	maxChats      = 512 // chats kept without a page or an action; the idle ones are dropped past it
)

// Action is what the agent asks of the page. All fields are always sent; the unused ones are zero.
type Action struct {
	ID      string `json:"id"`
	Op      string `json:"op"`
	Ref     string `json:"ref"`
	Element string `json:"element"`
	Text    string `json:"text"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	URL     string `json:"url"`
	DY      int    `json:"dy"`
	MS      int    `json:"ms"`
	Submit  bool   `json:"submit"`
	// Tool and Input are a site tool's name and its arguments as JSON text (op "call").
	Tool  string `json:"tool"`
	Input string `json:"input"`
}

// Outcome is how an action ended: answered (Failed and Text tell how), no-ui, timeout, busy or rate-limited.
type Outcome struct {
	Kind   string
	Failed bool
	Text   string
}

// Poll results.
const (
	PollAction = iota
	PollNothing
	PollTooMany
)

// Hub holds each chat's page presence and its one action in flight.
type Hub struct {
	Now func() time.Time
	// Hold, Present, Timeout and AskTimeout are the durations above.
	Hold, Present, Timeout, AskTimeout time.Duration

	mu    sync.Mutex
	chats map[string]*chat
}

type chat struct {
	polls    int
	lastPoll time.Time
	act      *flight
	// wake is closed when an action arrives, and replaced, so every open poll hears it.
	wake  chan struct{}
	stamp []int64
}

type flight struct {
	action Action
	taken  bool
	done   chan Outcome
	extend chan struct{}
}

// NewHub is a hub with the default durations.
func NewHub() *Hub {
	return &Hub{Now: time.Now, Hold: holdFor, Present: presentFor, Timeout: answerTimeout, AskTimeout: askTimeout, chats: map[string]*chat{}}
}

func (h *Hub) chat(id string) *chat {
	c := h.chats[id]
	if c == nil {
		if len(h.chats) >= maxChats {
			for k, o := range h.chats {
				if o.polls == 0 && o.act == nil && h.Now().Sub(o.lastPoll) > h.Present {
					delete(h.chats, k)
				}
			}
		}
		c = &chat{wake: make(chan struct{})}
		h.chats[id] = c
	}
	return c
}

func (c *chat) present(now time.Time, window time.Duration) bool {
	return c.polls > 0 || (!c.lastPoll.IsZero() && now.Sub(c.lastPoll) < window)
}

// Poll holds the page's request until an action comes for the chat, Hold passes or ctx ends. It marks the page present
// for the whole time and for Present after.
func (h *Hub) Poll(ctx context.Context, chatID string) (Action, int) {
	h.mu.Lock()
	c := h.chat(chatID)
	if c.polls >= maxPolls {
		h.mu.Unlock()
		return Action{}, PollTooMany
	}
	c.polls++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		c.polls--
		c.lastPoll = h.Now()
		h.mu.Unlock()
	}()

	hold := time.NewTimer(h.Hold)
	defer hold.Stop()
	for {
		h.mu.Lock()
		if f := c.act; f != nil && !f.taken {
			f.taken = true
			h.mu.Unlock()
			return f.action, PollAction
		}
		wake := c.wake
		h.mu.Unlock()
		select {
		case <-wake:
		case <-hold.C:
			return Action{}, PollNothing
		case <-ctx.Done():
			return Action{}, PollNothing
		}
	}
}

// Dispatch gives the action to the chat's page and waits for its outcome.
func (h *Hub) Dispatch(ctx context.Context, chatID string, a Action) Outcome {
	h.mu.Lock()
	c := h.chat(chatID)
	now := h.Now()
	switch {
	case !c.present(now, h.Present):
		h.mu.Unlock()
		return Outcome{Kind: "no-ui"}
	case c.act != nil:
		h.mu.Unlock()
		return Outcome{Kind: "busy"}
	}
	ok, stamps := panel.RateDecision(c.stamp, now.UnixMilli(), rateLimit, rateWindowMs)
	c.stamp = stamps
	if !ok {
		h.mu.Unlock()
		return Outcome{Kind: "rate-limited"}
	}
	a.ID = panel.UUID()
	f := &flight{action: a, done: make(chan Outcome, 1), extend: make(chan struct{}, 1)}
	c.act = f
	close(c.wake)
	c.wake = make(chan struct{})
	h.mu.Unlock()

	timer := time.NewTimer(h.Timeout)
	defer timer.Stop()
	for {
		select {
		case o := <-f.done:
			return o
		case <-f.extend:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(h.AskTimeout)
		case <-timer.C:
			return h.abandon(chatID, f, Outcome{Kind: "timeout"})
		case <-ctx.Done():
			return h.abandon(chatID, f, Outcome{Kind: "timeout"})
		}
	}
}

// abandon frees the chat's slot; an answer that came at the last moment wins over the reason.
func (h *Hub) abandon(chatID string, f *flight, reason Outcome) Outcome {
	h.mu.Lock()
	if c := h.chats[chatID]; c != nil && c.act == f {
		c.act = nil
	}
	h.mu.Unlock()
	select {
	case o := <-f.done:
		return o
	default:
		return reason
	}
}

// Answer settles the chat's action: "done" and "failed" end it with text, "asking" only extends the wait. False for an
// id that is not the chat's action in flight.
func (h *Hub) Answer(chatID, id, state, text string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.chats[chatID]
	if c == nil || c.act == nil || c.act.action.ID != id {
		return false
	}
	f := c.act
	if state == "asking" {
		select {
		case f.extend <- struct{}{}:
		default:
		}
		return true
	}
	c.act = nil
	f.done <- Outcome{Kind: "answered", Failed: state == "failed", Text: text}
	return true
}
