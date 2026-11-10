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
	holdFor    = 10 * time.Second // a poll with nothing to give ends after this, well before a gateway gives up on it
	presentFor = 35 * time.Second // a page stays present this long after its last poll ended
	// answerTimeout is how long the agent waits for the page to take an action and answer. askTimeout is the whole
	// wait of one call once the page asks the person: the agent's MCP bridge cuts a call without an answer at 60 s,
	// so the call ends before that with «the person is being asked», and the next call keeps waiting (HRN-681).
	answerTimeout = 30 * time.Second
	askTimeout    = 55 * time.Second
	// askStale is how long a page may stay silent while it asks: it repeats «asking» every few seconds, so a question
	// older than this belongs to a tab that is gone (closed without a word) and no longer holds the chat.
	askStale     = 30 * time.Second
	maxPolls     = 4   // open polls of one chat
	rateLimit    = 120 // actions a minute of one chat
	rateWindowMs = 60_000
	maxChats     = 512 // chats kept without a page or an action; the idle ones are dropped past it
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
	// Event is what a wait is for on a video or audio of the page: ended, playing or paused.
	Event string `json:"event"`
	// Tool and Input are a site tool's name and its arguments as JSON text (op "call").
	Tool  string `json:"tool"`
	Input string `json:"input"`
}

// Outcome is how an action ended: answered (Failed and Text tell how), asking (Text is the site the person is asked
// about, "" when the page did not say), no-ui, timeout, busy or rate-limited.
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
	// Hold, Present, Timeout, AskTimeout and AskStale are the durations above.
	Hold, Present, Timeout, AskTimeout, AskStale time.Duration

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
	// asked is closed once the page says it asks the person; asking is the site, askedAt when the page last said so.
	asked   chan struct{}
	asking  string
	askedAt time.Time
	// detached: the call that sent the action went back to the agent with «asking»; the question is still open in the
	// pane. waited: a later call waits for its answer before sending its own action.
	detached bool
	waited   bool
}

// NewHub is a hub with the default durations.
func NewHub() *Hub {
	return &Hub{Now: time.Now, Hold: holdFor, Present: presentFor, Timeout: answerTimeout, AskTimeout: askTimeout,
		AskStale: askStale, chats: map[string]*chat{}}
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

// Dispatch gives the action to the chat's page and waits for its outcome. While the page asks the person about the
// site, the call ends with «asking» before the bridge's cut and the question stays open: the next call waits for the
// person's answer, then sends its own action.
func (h *Hub) Dispatch(ctx context.Context, chatID string, a Action) Outcome {
	started := h.Now()
	whole := time.NewTimer(h.AskTimeout)
	defer whole.Stop()

	h.mu.Lock()
	c := h.chat(chatID)
	if f := c.act; f != nil {
		switch {
		case !isClosed(f.asked):
			// Another call's action, not asked about: the page is busy with it.
			h.mu.Unlock()
			return Outcome{Kind: "busy"}
		case !f.detached || f.waited:
			// The question is open and a call already waits on it.
			h.mu.Unlock()
			return Outcome{Kind: "asking", Text: f.asking}
		case started.Sub(f.askedAt) > h.AskStale:
			// The asking tab went away without a word: the question no longer holds the chat.
			c.act = nil
		default:
			f.waited = true
			h.mu.Unlock()
			select {
			case <-f.done:
				// The person answered and the earlier action ran or was refused: this one goes the usual way, and a
				// refusal for the site comes back from the page at once.
			case <-whole.C:
				h.unwait(f)
				return Outcome{Kind: "asking", Text: f.asking}
			case <-ctx.Done():
				h.unwait(f)
				return Outcome{Kind: "asking", Text: f.asking}
			}
			h.mu.Lock()
			if c.act == f {
				c.act = nil
			}
		}
	}
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
	f := &flight{action: a, done: make(chan Outcome, 1), asked: make(chan struct{})}
	c.act = f
	close(c.wake)
	c.wake = make(chan struct{})
	h.mu.Unlock()

	answer := time.NewTimer(h.Timeout)
	defer answer.Stop()
	asked, asking := f.asked, false
	for {
		select {
		case o := <-f.done:
			return o
		case <-asked:
			// The page asks the person: the answer's timer no longer counts, only the whole call's.
			asked, asking = nil, true
			answer.Stop()
		case <-answer.C:
			return h.abandon(chatID, f, Outcome{Kind: "timeout"})
		case <-whole.C:
			if asking {
				return h.detach(f)
			}
			return h.abandon(chatID, f, Outcome{Kind: "timeout"})
		case <-ctx.Done():
			if asking {
				return h.detach(f)
			}
			return h.abandon(chatID, f, Outcome{Kind: "timeout"})
		}
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (h *Hub) unwait(f *flight) {
	h.mu.Lock()
	f.waited = false
	h.mu.Unlock()
}

// detach leaves the question open in the pane and tells the agent whom it waits for; an answer that came at the last
// moment wins.
func (h *Hub) detach(f *flight) Outcome {
	h.mu.Lock()
	f.detached = true
	host := f.asking
	h.mu.Unlock()
	select {
	case o := <-f.done:
		return o
	default:
		return Outcome{Kind: "asking", Text: host}
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

// Answer settles the chat's action: "done" and "failed" end it with text, "asking" says the page asks the person about
// the site in text (repeated while the question is open). False for an id that is not the chat's action in flight.
func (h *Hub) Answer(chatID, id, state, text string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.chats[chatID]
	if c == nil || c.act == nil || c.act.action.ID != id {
		return false
	}
	f := c.act
	// A page that answers is there, whatever its polls: it stops polling while it asks the person.
	c.lastPoll = h.Now()
	if state == "asking" {
		f.askedAt = c.lastPoll
		if text != "" {
			f.asking = text
		}
		if !isClosed(f.asked) {
			close(f.asked)
		}
		return true
	}
	c.act = nil
	f.done <- Outcome{Kind: "answered", Failed: state == "failed", Text: text}
	return true
}
