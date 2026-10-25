package peers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ChatState is what a chat's agent is doing, for an agent choosing whom to write to.
type ChatState string

const (
	ChatBusy ChatState = "busy" // working, or waiting for its person's answer
	ChatIdle ChatState = "idle"
	ChatGone ChatState = "gone" // no agent: a message would not be read
)

// Chat is a conversation agents may write to.
type Chat struct {
	// Ref is how a message names it: "<machine>/<conversation id>".
	Ref         string    `json:"ref"`
	Conv        string    `json:"conversationId"`
	Title       string    `json:"title"`
	Machine     string    `json:"machine"`
	MachineName string    `json:"machineName"`
	Owner       string    `json:"owner"`
	State       ChatState `json:"state"`
}

// Local is what the service needs of this machine's conversations.
type Local interface {
	// Chats is every conversation of the machine (Conv, Title, State; the service fills the rest).
	Chats(ctx context.Context) ([]Chat, error)
	// Deliver puts text into conv's queue under clientID: at the agent's next step when steer and it works, at the
	// head of the line otherwise. A refusal is the conversation's own code.
	Deliver(ctx context.Context, conv, clientID, text string, steer bool) (Refusal, string)
}

// Relay is the way to the other machines of the fleet (through its gateway); nil when the machine has none.
type Relay interface {
	Send(ctx context.Context, o Outbound) Result
	Directory(ctx context.Context) ([]Chat, error)
}

// Outbound is a message this machine sends to another machine's chat.
type Outbound struct {
	ID        string `json:"id"`
	To        string `json:"to"` // the ref of the chat
	FromConv  string `json:"fromConversationId"`
	FromTitle string `json:"fromTitle"`
	Text      string `json:"text"`
	Via       string `json:"via"`
}

// Inbound is a message for one of this machine's chats; its sender comes from elsewhere (identity.go).
type Inbound struct {
	ID        string `json:"id"`
	To        string `json:"conversationId"`
	FromConv  string `json:"fromConversationId"`
	FromTitle string `json:"fromTitle"`
	Text      string `json:"text"`
	Via       string `json:"via"`
}

// Result is what came of a message: delivered (in the chat's queue or turn) or refused with a reason the sender sees.
type Result struct {
	ID     string  `json:"id"`
	State  State   `json:"state"`
	Reason Refusal `json:"reason,omitempty"`
	// Detail is the receiving conversation's own code when it did not take the message.
	Detail string `json:"detail,omitempty"`
	To     *Chat  `json:"to,omitempty"`
}

func refused(id string, why Refusal, detail string) Result {
	return Result{ID: id, State: StateRefused, Reason: why, Detail: detail}
}

const (
	// MaxText is the longest message (the outbox takes longer ones, an agent's note does not need them).
	MaxText = 16_000
	// RatePerMinute is how many messages one sender may get in here a minute.
	RatePerMinute = 20
	// rememberedResults: a message sent again under the same id gets the first answer, not a second delivery.
	rememberedResults = 500
)

// Service is the agents' mail of one runtime.
type Service struct {
	// Self is this machine and its owner: the sender of what goes out, the machine chats here belong to.
	Self     Sender
	Settings *Store
	Audit    *Audit
	Local    Local
	Relay    Relay
	Now      func() time.Time

	mu      sync.Mutex
	sent    map[string][]time.Time
	results map[string]Result
	order   []string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var (
	convID  = regexp.MustCompile(`^tab-[A-Za-z0-9_-]+$`)
	msgID   = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)
	localID = "local"
)

// NewID is a message id.
func NewID() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return "pm_" + hex.EncodeToString(b)
}

// Ref is a chat's ref on this machine.
func (s *Service) Ref(conv string) string { return s.Self.Machine + "/" + conv }

// ParseRef splits a ref into its machine ("" for this one) and conversation.
func (s *Service) ParseRef(ref string) (machine, conv string, ok bool) {
	ref = strings.TrimSpace(ref)
	machine, conv, found := strings.Cut(ref, "/")
	if !found {
		machine, conv = "", ref
	}
	if machine == localID || strings.EqualFold(machine, s.Self.Machine) {
		machine = ""
	}
	if !convID.MatchString(conv) || machine != "" && !machineID.MatchString(machine) {
		return "", "", false
	}
	return machine, conv, true
}

func (s *Service) localChats(ctx context.Context) ([]Chat, error) {
	chats, err := s.Local.Chats(ctx)
	if err != nil {
		return nil, err
	}
	for i := range chats {
		chats[i].Ref = s.Ref(chats[i].Conv)
		chats[i].Machine, chats[i].MachineName, chats[i].Owner = s.Self.Machine, s.Self.MachineLabel(), s.Self.Name()
	}
	return chats, nil
}

func (s *Service) chat(ctx context.Context, conv string) (Chat, bool) {
	chats, err := s.localChats(ctx)
	if err != nil {
		return Chat{}, false
	}
	for _, c := range chats {
		if c.Conv == conv {
			return c, true
		}
	}
	return Chat{}, false
}

// remembered is the answer already given to a message id, if any.
func (s *Service) remembered(key string) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[key]
	return r, ok
}

func (s *Service) remember(key string, r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = map[string]Result{}
	}
	if _, ok := s.results[key]; !ok {
		s.order = append(s.order, key)
	}
	s.results[key] = r
	for len(s.order) > rememberedResults {
		delete(s.results, s.order[0])
		s.order = s.order[1:]
	}
}

// allowRate counts a message of sender; false past RatePerMinute in the last minute.
func (s *Service) allowRate(sender Sender, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent == nil {
		s.sent = map[string][]time.Time{}
	}
	key := strings.ToLower(sender.Machine + "\x00" + sender.Person)
	kept := s.sent[key][:0]
	for _, at := range s.sent[key] {
		if now.Sub(at) < time.Minute {
			kept = append(kept, at)
		}
	}
	if len(kept) >= RatePerMinute {
		s.sent[key] = kept
		return false
	}
	s.sent[key] = append(kept, now)
	return true
}

func checkText(text string) Refusal {
	switch {
	case strings.TrimSpace(text) == "":
		return RefuseEmpty
	case len([]rune(text)) > MaxText:
		return RefuseTooLong
	}
	return ""
}

func via(v string) string {
	if v == "person" {
		return v
	}
	return "agent"
}

// Receive takes a message from sender (already verified) for one of this machine's chats: the allow-list, the rate,
// the chat, then the chat's queue. Every outcome goes to the audit; the answer is what the sender sees.
func (s *Service) Receive(ctx context.Context, sender Sender, in Inbound) Result {
	now := s.now()
	if !msgID.MatchString(in.ID) {
		in.ID = NewID()
	}
	key := sender.Machine + "/" + in.ID
	if r, ok := s.remembered(key); ok {
		return r
	}
	// "person" is the owner typing in the card of this machine's own page (Send says so itself); another machine's
	// word for it is no proof, whatever the token vouches for, so what comes from there is the agent's.
	in.Via = via(in.Via)
	if !strings.EqualFold(sender.Machine, s.Self.Machine) {
		in.Via = "agent"
	}
	if !convID.MatchString(in.FromConv) {
		in.FromConv = ""
	}
	if !convID.MatchString(in.To) {
		in.To = ""
	}
	in.FromTitle = clip(strings.TrimSpace(in.FromTitle), 200)
	rec := Record{ID: in.ID, At: now.UnixMilli(), Dir: DirIn, From: partyOf(sender, in.FromConv, in.FromTitle),
		To: partyOf(s.Self, in.To, ""), Text: in.Text, Via: in.Via}
	finish := func(r Result) Result {
		rec.State, rec.Reason = r.State, r.Reason
		if r.To != nil {
			rec.To.Title = r.To.Title
		}
		s.Audit.Add(rec)
		s.remember(key, r)
		return r
	}
	// The rate comes first: refused messages are audited too, and must not flood the audit.
	if !s.allowRate(sender, now) {
		return finish(refused(in.ID, RefuseRateLimited, ""))
	}
	if ok, why := s.Settings.Get().Decide(sender); !ok {
		return finish(refused(in.ID, why, ""))
	}
	if why := checkText(in.Text); why != "" {
		return finish(refused(in.ID, why, ""))
	}
	target, ok := s.chat(ctx, in.To)
	if !ok {
		return finish(refused(in.ID, RefuseUnknownChat, ""))
	}
	if target.State == ChatGone {
		r := refused(in.ID, RefuseAgentGone, "")
		r.To = &target
		return finish(r)
	}
	env := Envelope{ID: in.ID, Person: sender.Name(), Machine: sender.MachineLabel(), Chat: in.FromTitle, Via: in.Via,
		Text: in.Text}
	if in.FromConv != "" {
		env.Ref = sender.Machine + "/" + in.FromConv
	}
	steer := s.Settings.Get().Delivery == DeliveryStep
	why, detail := s.Local.Deliver(ctx, target.Conv, "peer-"+in.ID, Wrap(env), steer)
	if why != "" {
		r := refused(in.ID, why, detail)
		r.To = &target
		return finish(r)
	}
	return finish(Result{ID: in.ID, State: StateDelivered, To: &target})
}

// Send is a message from one of this machine's chats (fromConv, "" for none) to the chat ref names: delivered here when
// the chat is on this machine, through the relay when it is on another.
func (s *Service) Send(ctx context.Context, fromConv, ref, text, by string) Result {
	id := NewID()
	machine, conv, ok := s.ParseRef(ref)
	if !ok {
		return refused(id, RefuseBadRef, "")
	}
	title := ""
	if from, found := s.chat(ctx, fromConv); found {
		title = from.Title
	} else {
		fromConv = ""
	}
	if machine == "" {
		return s.Receive(ctx, s.Self, Inbound{ID: id, To: conv, FromConv: fromConv, FromTitle: title, Text: text, Via: by})
	}
	rec := Record{ID: id, At: s.now().UnixMilli(), Dir: DirOut, From: partyOf(s.Self, fromConv, title),
		To: Party{Machine: machine, Conv: conv}, Text: text, Via: via(by)}
	var r Result
	switch {
	case checkText(text) != "":
		r = refused(id, checkText(text), "")
	case s.Relay == nil:
		r = refused(id, RefuseNoRelay, "")
	default:
		r = s.Relay.Send(ctx, Outbound{ID: id, To: machine + "/" + conv, FromConv: fromConv, FromTitle: title, Text: text,
			Via: via(by)})
		if r.ID == "" {
			r.ID = id
		}
	}
	if r.To != nil {
		rec.To = Party{Person: "", PersonName: r.To.Owner, Machine: machine, MachineName: r.To.MachineName, Conv: conv,
			Title: r.To.Title}
	}
	rec.State, rec.Reason = r.State, r.Reason
	s.Audit.Add(rec)
	return r
}

// Directory is the chats a viewer may write to: this machine's (none when the allow-list refuses the viewer), and with
// remote, the other machines' through the relay. errs names what could not be listed.
func (s *Service) Directory(ctx context.Context, viewer Sender, remote bool) (chats []Chat, errs []string) {
	chats = []Chat{}
	if ok, why := s.Settings.Get().Decide(viewer); ok {
		local, err := s.localChats(ctx)
		if err != nil {
			errs = append(errs, s.Self.Machine+": "+err.Error())
		}
		chats = append(chats, local...)
	} else {
		errs = append(errs, s.Self.Machine+": "+string(why))
	}
	if !remote {
		return chats, errs
	}
	if s.Relay == nil {
		return chats, append(errs, string(RefuseNoRelay))
	}
	more, err := s.Relay.Directory(ctx)
	if err != nil {
		return chats, append(errs, string(RefuseUnreachable)+": "+err.Error())
	}
	for _, c := range more {
		if c.Machine != s.Self.Machine {
			chats = append(chats, c)
		}
	}
	return chats, errs
}
