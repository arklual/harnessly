package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

// The choices an agent puts to the person through the app.ask host tool. They live in memory only: an ask is a question
// of the moment, and the agent's call that waits for it is gone with a restart too.

const (
	// AskTTL is how long a choice stays open; past it the agent's call ends as cancelled.
	AskTTL = 10 * time.Minute
	// MaxAsksPerChat bounds the open choices of one chat.
	MaxAsksPerChat = 5
	MaxAskOptions  = 8
	MaxAskOption   = 80
	MaxAskQuestion = 300
)

var (
	// ErrAskArgs: a question or options past the limits.
	ErrAskArgs = errors.New("apps: bad ask")
	// ErrAskBusy: the chat has too many open choices.
	ErrAskBusy = errors.New("apps: too many open choices in this chat")
	// ErrAskGone: no such open choice.
	ErrAskGone = errors.New("apps: no such choice")
	// ErrAskAnswer: the answer is none of the options.
	ErrAskAnswer = errors.New("apps: the answer is not one of the options")
)

// PendingAsk is an open choice as the page lists it.
type PendingAsk struct {
	ID        string   `json:"id"`
	App       string   `json:"app"`
	AppName   string   `json:"appName"`
	Question  string   `json:"question"`
	Options   []string `json:"options"`
	CreatedAt int64    `json:"createdAt"`
}

type openAsk struct {
	PendingAsk
	conv   string
	result chan *string
}

// Asks is the open choices of the machine.
type Asks struct {
	Now func() time.Time
	// TTL replaces AskTTL (tests).
	TTL time.Duration

	mu   sync.Mutex
	open map[string]*openAsk
}

func NewAsks() *Asks { return &Asks{open: map[string]*openAsk{}} }

func (a *Asks) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Asks) ttl() time.Duration {
	if a.TTL > 0 {
		return a.TTL
	}
	return AskTTL
}

// Ask puts a choice to the person of the chat and waits for the answer: the option picked, or cancelled=true when the
// person cancelled, the time ran out or ctx ended.
func (a *Asks) Ask(ctx context.Context, conv, app, appName, question string, options []string) (answer string, cancelled bool, err error) {
	if question == "" || len(question) > MaxAskQuestion || len(options) < 2 || len(options) > MaxAskOptions {
		return "", false, ErrAskArgs
	}
	for _, o := range options {
		if o == "" || len(o) > MaxAskOption {
			return "", false, ErrAskArgs
		}
	}
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	ask := &openAsk{
		PendingAsk: PendingAsk{
			ID: "ask_" + hex.EncodeToString(raw[:]), App: app, AppName: appName, Question: question,
			Options: append([]string(nil), options...), CreatedAt: a.now().UnixMilli(),
		},
		conv: conv, result: make(chan *string, 1),
	}
	a.mu.Lock()
	n := 0
	for _, o := range a.open {
		if o.conv == conv {
			n++
		}
	}
	if n >= MaxAsksPerChat {
		a.mu.Unlock()
		return "", false, ErrAskBusy
	}
	a.open[ask.ID] = ask
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.open, ask.ID)
		a.mu.Unlock()
	}()

	timer := time.NewTimer(a.ttl())
	defer timer.Stop()
	select {
	case r := <-ask.result:
		if r == nil {
			return "", true, nil
		}
		return *r, false, nil
	case <-timer.C:
	case <-ctx.Done():
	}
	return "", true, nil
}

// Pending are the open choices of a chat, oldest first.
func (a *Asks) Pending(conv string) []PendingAsk {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []PendingAsk{}
	for _, o := range a.open {
		if o.conv == conv {
			out = append(out, o.PendingAsk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// Answer closes a choice with one of its options; nil cancels it.
func (a *Asks) Answer(id string, answer *string) error {
	a.mu.Lock()
	o := a.open[id]
	if o == nil {
		a.mu.Unlock()
		return ErrAskGone
	}
	if answer != nil {
		ok := false
		for _, opt := range o.Options {
			ok = ok || opt == *answer
		}
		if !ok {
			a.mu.Unlock()
			return ErrAskAnswer
		}
	}
	delete(a.open, id)
	a.mu.Unlock()
	o.result <- answer
	return nil
}
