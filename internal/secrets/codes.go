package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// One-time codes (2FA, SMS, e-mail confirmations): an agent's command asks for one, the person types it in the
// panel, the command takes it once and runs with it. A code is never stored: it lives in this process's memory from
// the person's answer until the asking command takes it (or two minutes pass), and no answer, list or log carries it.

// How a code ask stands.
const (
	CodeWaiting   = "waiting"
	CodeReady     = "ready"
	CodeCancelled = "cancelled"
	CodeExpired   = "expired"
	CodeGone      = "gone"
)

// ReasonBadCode: the code is not as many digits as asked; ReasonNoChat: a code is asked only from a chat.
const (
	ReasonBadCode = "bad-code"
	ReasonNoChat  = "no-chat"
)

const (
	// DefaultCodeWait and MaxCodeWait: how long an ask waits for the person.
	DefaultCodeWait = 10 * time.Minute
	MaxCodeWait     = 30 * time.Minute
	// codeKept is how long an answered code waits for its command to take it.
	codeKept = 2 * time.Minute
	// codeAlive: an ask nobody has waited on for this long has lost its command (the agent stopped it).
	codeAlive = 15 * time.Second
	// codeSettledKept is how long the end of an ask still answers its command.
	codeSettledKept = 2 * time.Minute
	MinCodeDigits   = 4
	MaxCodeDigits   = 8
	maxCodes        = 16
)

// CodeAsk is what the page shows of a code ask: what it is for, how long it waits, never the code.
type CodeAsk struct {
	ID     string `json:"id"`
	Why    string `json:"why"`
	Conv   string `json:"conv"`
	Title  string `json:"title"`
	Digits int    `json:"digits"`
	At     int64  `json:"at"`
	Until  int64  `json:"until"`
}

type codeAsk struct {
	CodeAsk
	state   string
	value   string
	settled time.Time
	waiters int
	polled  time.Time
	done    chan struct{}
}

// Codes are the open code asks of this machine.
type Codes struct {
	Now func() time.Time

	mu      sync.Mutex
	list    map[string]*codeAsk
	limiter *vault.Limiter
}

// NewCodes is the code asks with at most 6 a conversation in 10 minutes.
func NewCodes() *Codes {
	return &Codes{Now: time.Now, list: map[string]*codeAsk{},
		limiter: vault.NewLimiter(vault.Rule{Limit: 6, Window: 10 * time.Minute})}
}

func newCodeID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "sc_" + hex.EncodeToString(b)
}

// ValidCode says whether value is a code of digits digits.
func ValidCode(value string, digits int) bool {
	if len(value) != digits {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// end settles x; the caller holds mu.
func (c *Codes) end(x *codeAsk, state string, now time.Time) {
	if x.state != CodeWaiting {
		return
	}
	x.state = state
	x.settled = now
	close(x.done)
}

// prune ends what is past its time and drops what has ended long enough ago; the caller holds mu.
func (c *Codes) prune(now time.Time) {
	for id, x := range c.list {
		switch {
		case x.state == CodeWaiting && now.UnixMilli() >= x.Until:
			c.end(x, CodeExpired, now)
		case x.state == CodeWaiting && x.waiters == 0 && now.Sub(x.polled) >= codeAlive:
			c.end(x, CodeGone, now)
		case x.state == CodeReady && now.Sub(x.settled) >= codeKept:
			delete(c.list, id)
		case x.state != CodeWaiting && x.state != CodeReady && now.Sub(x.settled) >= codeSettledKept:
			delete(c.list, id)
		}
	}
}

// Open starts an ask of a code of digits digits for the chat conv, waiting at most wait.
func (c *Codes) Open(why, conv, title string, digits int, wait time.Duration) (CodeAsk, error) {
	if conv == "" {
		return CodeAsk{}, refuse(ReasonNoChat, nil)
	}
	if digits < MinCodeDigits || digits > MaxCodeDigits {
		return CodeAsk{}, refuse(ReasonBadCode, nil)
	}
	wait = min(max(wait, time.Minute), MaxCodeWait)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	c.prune(now)
	waiting := 0
	for _, x := range c.list {
		if x.state == CodeWaiting {
			waiting++
		}
	}
	if waiting >= maxCodes {
		return CodeAsk{}, refuse(ReasonTooMany, nil)
	}
	if ok, _ := c.limiter.Take("conv:"+conv, now.UnixMilli()); !ok {
		return CodeAsk{}, refuse(ReasonRateLimited, nil)
	}
	x := &codeAsk{CodeAsk: CodeAsk{ID: newCodeID(), Why: CleanWhy(why), Conv: conv, Title: jscompat.Slice(tmux.OneLineSafe(title), 200),
		Digits: digits, At: now.UnixMilli(), Until: now.Add(wait).UnixMilli()}, state: CodeWaiting, polled: now, done: make(chan struct{})}
	c.list[x.ID] = x
	return x.CodeAsk, nil
}

// Wait is how the ask id of the chat conv stands after at most d: on ready it hands the code over once and forgets it.
// Another chat's id reads as gone.
func (c *Codes) Wait(ctx context.Context, id, conv string, d time.Duration) (string, string) {
	c.mu.Lock()
	c.prune(c.Now())
	x := c.list[id]
	if x == nil || x.Conv != conv {
		c.mu.Unlock()
		return CodeGone, ""
	}
	x.waiters++
	c.mu.Unlock()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-x.done:
	case <-t.C:
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	x.waiters--
	x.polled = now
	state, value := x.state, ""
	if state == CodeReady {
		value = x.value
		x.value = ""
		delete(c.list, id)
	}
	return state, value
}

// Pending are the asks waiting for the person, oldest first.
func (c *Codes) Pending() []CodeAsk {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(c.Now())
	out := []CodeAsk{}
	for _, x := range c.list {
		if x.state == CodeWaiting {
			out = append(out, x.CodeAsk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At || (out[i].At == out[j].At && out[i].ID < out[j].ID) })
	return out
}

// Put answers the ask id with the person's code; false for an id that waits no more.
func (c *Codes) Put(id, value string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	c.prune(now)
	x := c.list[id]
	if x == nil || x.state != CodeWaiting {
		return false, nil
	}
	if !ValidCode(value, x.Digits) {
		return true, refuse(ReasonBadCode, nil)
	}
	x.value = value
	c.end(x, CodeReady, now)
	return true, nil
}

// Cancel ends the ask id without a code: the person said no, or the command gave up; false for one that waits no more.
func (c *Codes) Cancel(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	x := c.list[id]
	if x == nil || x.state != CodeWaiting {
		return false
	}
	c.end(x, CodeCancelled, c.Now())
	return true
}

// CancelOf ends the ask id only for its own chat (the command giving up).
func (c *Codes) CancelOf(id, conv string) bool {
	c.mu.Lock()
	x := c.list[id]
	c.mu.Unlock()
	if x == nil || x.Conv != conv {
		return false
	}
	return c.Cancel(id)
}
