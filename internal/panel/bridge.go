package panel

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The agent → pane channel of one runtime: a command goes out over the sync socket to the person's pages, the page
// that shows the tab runs it and answers over HTTP, and the answer goes back to the agent that waits.

const (
	answerTimeout = 8 * time.Second
	rateLimit     = 12
	rateWindowMs  = 60_000
	maxPending    = 16
)

// Outcome is how a command ended: answered (with Result), no-ui, timeout, busy or rate-limited.
type Outcome struct {
	Kind   string
	Result Result
}

// Bridge holds the commands waiting for an answer and each tab's recent commands.
type Bridge struct {
	// Publish sends an event to every page; Subscribers counts the pages (and relays) connected.
	Publish     func(event *jsvalue.Object)
	Subscribers func() int
	Now         func() time.Time

	mu      sync.Mutex
	pending map[string]pendingCommand
	stamps  map[string][]int64
}

type pendingCommand struct {
	tabID  string
	answer chan Result
}

// NewBridge is a bridge publishing through publish.
func NewBridge(publish func(*jsvalue.Object), subscribers func() int) *Bridge {
	return &Bridge{Publish: publish, Subscribers: subscribers, Now: time.Now, pending: map[string]pendingCommand{}, stamps: map[string][]int64{}}
}

// RateAllowed counts a command of the tab against its limit (12 a minute); a refused one is not counted.
func (b *Bridge) RateAllowed(tabID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rateAllowed(tabID)
}

func (b *Bridge) rateAllowed(tabID string) bool {
	ok, stamps := RateDecision(b.stamps[tabID], b.Now().UnixMilli(), rateLimit, rateWindowMs)
	b.stamps[tabID] = stamps
	return ok
}

// Dispatch asks the page that shows the tab right now and waits for its answer; timeout 0 is the default 8 s.
func (b *Bridge) Dispatch(tabID string, cmd Command, timeout time.Duration) Outcome {
	if timeout == 0 {
		timeout = answerTimeout
	}
	if b.Subscribers() == 0 {
		return Outcome{Kind: "no-ui"}
	}
	b.mu.Lock()
	for _, p := range b.pending {
		if p.tabID == tabID {
			b.mu.Unlock()
			return Outcome{Kind: "busy"}
		}
	}
	if len(b.pending) >= maxPending {
		b.mu.Unlock()
		return Outcome{Kind: "busy"}
	}
	if !b.rateAllowed(tabID) {
		b.mu.Unlock()
		return Outcome{Kind: "rate-limited"}
	}
	id := UUID()
	answer := make(chan Result, 1)
	b.pending[id] = pendingCommand{tabID: tabID, answer: answer}
	b.mu.Unlock()

	b.Publish(jsvalue.NewObject("type", "panel-command", "id", id, "tabId", tabID, "command", cmd.JSON()))
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-answer:
		return Outcome{Kind: "answered", Result: r}
	case <-t.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		select {
		case r := <-answer: // answered at the last moment
			return Outcome{Kind: "answered", Result: r}
		default:
		}
		return Outcome{Kind: "timeout"}
	}
}

// Answer settles a command with the first answer; false for a later one or an id never issued.
func (b *Bridge) Answer(id string, r Result) bool {
	b.mu.Lock()
	p, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ok {
		p.answer <- r
	}
	return ok
}

// UUID is a random version-4 UUID in lowercase.
func UUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
