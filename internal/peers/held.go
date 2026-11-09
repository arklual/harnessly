package peers

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// The owner's inbox: with Settings.Approve = others, a message of anyone but the machine's owner does not reach the
// agent at once. It waits here until the owner accepts it on the machine's page (then it is delivered as it came),
// rejects it, the sender takes it back, or its time runs out. The sender learns each outcome from the answer to the
// same message id: a resend never delivers twice. The inbox lives in the runtime's memory: a restart empties it, and
// a message sent again is held again.

const (
	// HeldKept is how many messages may wait at once; past it a new one is refused as rate-limited.
	HeldKept = 100
	// HeldTTL is how long a message waits when its sender says nothing; MaxHeldTTL the longest it may ask for.
	HeldTTL    = 24 * time.Hour
	MaxHeldTTL = 7 * 24 * time.Hour
)

// Held is a message waiting for the owner, as the page shows it.
type Held struct {
	ID      string `json:"id"`
	At      int64  `json:"at"`
	Expires int64  `json:"expires"`
	From    Party  `json:"from"`
	To      Party  `json:"to"`
	Text    string `json:"text"`
	Via     string `json:"via"`

	key     string
	sender  Sender
	conv    string
	wrapped string
	steer   bool
	target  Chat
}

var (
	// ErrNotHeld: no message of that id waits (it was decided on, taken back or expired).
	ErrNotHeld = errors.New("no such held message")
)

// needsApproval says whether a message of sender waits for the owner.
func (s *Service) needsApproval(sender Sender) bool {
	if s.Settings.Get().Approve != ApproveOthers {
		return false
	}
	return sender.Person == "" || !strings.EqualFold(sender.Person, s.Self.Person)
}

func heldTTL(seconds int64) time.Duration {
	if seconds <= 0 {
		return HeldTTL
	}
	if d := time.Duration(seconds) * time.Second; d < MaxHeldTTL {
		return d
	}
	return MaxHeldTTL
}

// hold keeps h; false when the inbox is full.
func (s *Service) hold(h *Held) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil {
		s.held = map[string]*Held{}
	}
	if len(s.held) >= HeldKept {
		return false
	}
	s.held[h.key] = h
	return true
}

// close ends a held message: the audit gets the outcome with who decided, and a resend gets the same answer.
func (s *Service) close(h *Held, r Result, by string) Result {
	rec := Record{ID: h.ID, At: s.now().UnixMilli(), Dir: DirIn, From: h.From, To: h.To, Text: h.Text, Via: h.Via,
		State: r.State, Reason: r.Reason, By: by}
	s.Audit.Add(rec)
	s.remember(h.key, r)
	return r
}

// expire ends every held message past its time.
func (s *Service) expire() {
	now := s.now().UnixMilli()
	s.mu.Lock()
	var gone []*Held
	for k, h := range s.held {
		if h.Expires <= now {
			gone = append(gone, h)
			delete(s.held, k)
		}
	}
	s.mu.Unlock()
	for _, h := range gone {
		r := refused(h.ID, RefuseExpired, "")
		r.To = &h.target
		s.close(h, r, "expired")
	}
}

// HeldList is the messages waiting for the owner, the oldest first.
func (s *Service) HeldList() []Held {
	s.expire()
	s.mu.Lock()
	out := make([]Held, 0, len(s.held))
	for _, h := range s.held {
		out = append(out, *h)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Decide is the owner's word on a held message: accept delivers it as it came, reject refuses it. by is the owner as
// the page session names them; it goes to the audit beside the sender and the chat that runs the message.
func (s *Service) Decide(ctx context.Context, id string, accept bool, by string) (Result, error) {
	s.expire()
	var h *Held
	s.mu.Lock()
	for k, c := range s.held {
		if c.ID == id {
			h = c
			delete(s.held, k)
			break
		}
	}
	s.mu.Unlock()
	if h == nil {
		return Result{}, ErrNotHeld
	}
	if by == "" {
		by = s.Self.Name()
	}
	if !accept {
		r := refused(h.ID, RefuseRejected, "")
		r.To = &h.target
		return s.close(h, r, by), nil
	}
	why, detail := s.Local.Deliver(ctx, h.conv, "peer-"+h.ID, h.wrapped, h.steer)
	if why != "" {
		r := refused(h.ID, why, detail)
		r.To = &h.target
		return s.close(h, r, by), nil
	}
	return s.close(h, Result{ID: h.ID, State: StateDelivered, To: &h.target}, by), nil
}

// revoke is the sender taking back its own held message; a message no longer held keeps its answer.
func (s *Service) revoke(sender Sender, id string) Result {
	key := sender.Machine + "/" + id
	s.mu.Lock()
	h, ok := s.held[key]
	if ok && strings.EqualFold(h.sender.Person, sender.Person) {
		delete(s.held, key)
	} else {
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		if r, found := s.remembered(key); found {
			return r
		}
		return refused(id, RefuseUnknownChat, "no held message of that id")
	}
	r := refused(h.ID, RefuseRevoked, "")
	r.To = &h.target
	return s.close(h, r, "sender")
}
