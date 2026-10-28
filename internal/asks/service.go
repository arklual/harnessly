package asks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/notifybots"
)

// Limits of what an agent sends.
const (
	MaxQuestion = 1500
	MaxDetails  = 2000
	MaxOptions  = 6
	MaxOption   = 40
	MaxLink     = 500
	// MaxReplies is how many messages one ask keeps.
	MaxReplies = 30
	// HourlyCap is how many asks the whole machine may send in an hour, whatever the conversations.
	HourlyCap = 60
)

// Reasons of a refusal, as the agents and the page are told them.
const (
	ReasonDisabled    = "disabled"       // the person turned asks off
	ReasonNoBot       = "no-bot"         // no bot with a chat may carry asks
	ReasonBotNotFound = "bot-not-found"  // the named bot is not one of them
	ReasonUnknownTo   = "unknown-person" // not a contact, and only contacts may be tagged
	ReasonBadTo       = "bad-person"     // neither a contact nor an @username
	ReasonEmpty       = "empty"          // no question
	ReasonTooLong     = "too-long"       // question, details, an option or the link too long
	ReasonRateLimited = "rate-limited"   // too many asks in the last hour
	ReasonNotFound    = "not-found"      // no ask of that id
	ReasonClosed      = "closed"         // the ask is closed already
	ReasonBadChoice   = "bad-choice"     // not a choice of this ask
	ReasonFailed      = "failed"         // the file could not be read or written
)

// Refusal is a reason the caller is told, with Telegram's words when it has them.
type Refusal struct {
	Reason string
	Detail string
}

func (r *Refusal) Error() string { return "asks: " + r.Reason }

func refuse(reason string) error { return &Refusal{Reason: reason} }

// RefusalOf is what to tell the caller of any error of the service.
func RefusalOf(err error) Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return *r
	}
	if rr := notifybots.RefusalOf(err); rr.Reason != notifybots.ReasonFailed {
		return Refusal{Reason: rr.Reason, Detail: rr.Detail}
	}
	if errors.Is(err, ErrNotFound) {
		return Refusal{Reason: ReasonNotFound}
	}
	var se *SettingsError
	if errors.As(err, &se) {
		return Refusal{Reason: "bad-settings", Detail: se.Field}
	}
	return Refusal{Reason: ReasonFailed}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Service is the asks of this runtime.
type Service struct {
	Store    *Store
	Settings *SettingsStore
	Bots     *notifybots.Store
	Telegram *notifybots.Client
	Now      func() time.Time
	// Changed is told of every change of an ask (new, answered, closed): the browser notification hangs here.
	Changed func(before *Ask, after Ask)
	// PollWait is the long poll of a bot's poller (25 s when zero); tests shorten it.
	PollWait time.Duration

	mu      sync.Mutex
	waiters map[string][]chan struct{}
	pollers map[string]context.CancelFunc
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Request is what an agent asks.
type Request struct {
	Kind     Kind     `json:"kind"`
	Question string   `json:"question"`
	Details  string   `json:"details"`
	Link     string   `json:"link"`
	Options  []string `json:"options"`
	// To is a contact's name or id, or a Telegram @username; "" asks the chat as a whole.
	To string `json:"to"`
	// Bot is a bot's name or id; "" is the default one.
	Bot string `json:"bot"`
	// TimeoutSec is how long the agent waits; 0 is the person's default.
	TimeoutSec int `json:"timeoutSec"`

	Session      string `json:"session"`
	Conversation string `json:"-"`
	Workspace    string `json:"-"`
	Agent        string `json:"-"`
}

func (s *Service) pickBot(set Settings, ref string) (notifybots.Bot, error) {
	bots, err := s.Bots.List()
	if err != nil {
		return notifybots.Bot{}, err
	}
	allowed := func(b notifybots.Bot) bool {
		if b.Chat == nil {
			return false
		}
		if len(set.Bots) == 0 {
			return true
		}
		for _, id := range set.Bots {
			if id == b.ID {
				return true
			}
		}
		return false
	}
	ref = strings.TrimSpace(ref)
	if ref != "" {
		for _, b := range bots {
			if (b.ID == ref || strings.EqualFold(b.Name, ref) || strings.EqualFold(b.Username, strings.TrimPrefix(ref, "@"))) && allowed(b) {
				return b, nil
			}
		}
		return notifybots.Bot{}, refuse(ReasonBotNotFound)
	}
	for _, b := range bots {
		if b.ID == set.DefaultBot && allowed(b) {
			return b, nil
		}
	}
	for _, b := range bots {
		if allowed(b) {
			return b, nil
		}
	}
	return notifybots.Bot{}, refuse(ReasonNoBot)
}

func resolveTo(set Settings, ref string) (*Person, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if c, ok := set.FindContact(ref); ok {
		p := c.Person()
		return &p, nil
	}
	if set.OnlyContacts {
		return nil, refuse(ReasonUnknownTo)
	}
	if strings.HasPrefix(ref, "@") || strings.Contains(ref, "t.me/") {
		if u := CleanUsername(ref); u != "" {
			return &Person{Username: u}, nil
		}
	}
	return nil, refuse(ReasonBadTo)
}

func (s *Service) checkRate(set Settings, session string) error {
	asks, err := s.Store.List()
	if err != nil {
		return err
	}
	hourAgo := s.now().Add(-time.Hour).UnixMilli()
	mine, all := 0, 0
	for _, a := range asks {
		if a.CreatedAt < hourAgo || a.Status == StatusFailed {
			continue
		}
		all++
		if a.Session == session {
			mine++
		}
	}
	if mine >= set.PerHour || all >= HourlyCap {
		return refuse(ReasonRateLimited)
	}
	return nil
}

func checkRequest(r *Request) error {
	r.Question = strings.TrimSpace(r.Question)
	r.Details = strings.TrimSpace(r.Details)
	r.Link = strings.TrimSpace(r.Link)
	if r.Kind == "" {
		r.Kind = KindQuestion
	}
	if r.Kind != KindQuestion && r.Kind != KindApproval {
		return refuse("bad-kind")
	}
	if r.Question == "" {
		return refuse(ReasonEmpty)
	}
	if utf8.RuneCountInString(r.Question) > MaxQuestion || utf8.RuneCountInString(r.Details) > MaxDetails ||
		len(r.Link) > MaxLink || len(r.Options) > MaxOptions {
		return refuse(ReasonTooLong)
	}
	opts := make([]string, 0, len(r.Options))
	for _, o := range r.Options {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			continue
		}
		if utf8.RuneCountInString(o) > MaxOption {
			return refuse(ReasonTooLong)
		}
		opts = append(opts, o)
	}
	r.Options = opts
	return nil
}

// Create sends an ask and stores it; the agent then waits on its id.
func (s *Service) Create(ctx context.Context, r Request) (Ask, error) {
	set, err := s.Settings.Get()
	if err != nil {
		return Ask{}, err
	}
	if !set.Enabled {
		return Ask{}, refuse(ReasonDisabled)
	}
	if err := checkRequest(&r); err != nil {
		return Ask{}, err
	}
	bot, err := s.pickBot(set, r.Bot)
	if err != nil {
		return Ask{}, err
	}
	to, err := resolveTo(set, r.To)
	if err != nil {
		return Ask{}, err
	}
	if err := s.checkRate(set, r.Session); err != nil {
		return Ask{}, err
	}
	timeout := time.Duration(r.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(set.DefaultTimeoutMin) * time.Minute
	}
	if max := time.Duration(set.MaxTimeoutMin) * time.Minute; timeout > max {
		timeout = max
	}
	if timeout < time.Minute {
		timeout = time.Minute
	}
	now := s.now()
	a := Ask{ID: "ask_" + randomHex(6), Kind: r.Kind, Status: StatusWaiting, Question: r.Question, Details: r.Details,
		Link: r.Link, Options: r.Options, To: to, BotID: bot.ID, BotName: bot.Name, ChatTitle: bot.Chat.Title,
		ChatID: bot.Chat.ID, Silent: set.Quiet.InQuiet(now), Session: r.Session, Conversation: r.Conversation,
		Workspace: r.Workspace, Agent: r.Agent, CreatedAt: now.UnixMilli(), Deadline: now.Add(timeout).UnixMilli()}
	source := ""
	if set.ShowSource && r.Agent != "" {
		source = "«" + r.Agent + "»"
	}
	a.HTML = messageHTML(a, source)
	id, err := s.Telegram.SendHTML(ctx, bot.Token, a.ChatID, a.HTML, buttons(a), a.Silent)
	if err != nil {
		return Ask{}, err
	}
	a.MessageID = id
	if err := s.Store.Add(a); err != nil {
		return Ask{}, err
	}
	s.changed(nil, a)
	s.ensurePoller(bot.ID)
	return a, nil
}

func (s *Service) changed(before *Ask, after Ask) {
	s.mu.Lock()
	chans := s.waiters[after.ID]
	delete(s.waiters, after.ID)
	s.mu.Unlock()
	for _, c := range chans {
		close(c)
	}
	if s.Changed != nil {
		s.Changed(before, after)
	}
}

// subscribe is a channel closed at the ask's next change.
func (s *Service) subscribe(id string) chan struct{} {
	c := make(chan struct{})
	s.mu.Lock()
	if s.waiters == nil {
		s.waiters = map[string][]chan struct{}{}
	}
	s.waiters[id] = append(s.waiters[id], c)
	s.mu.Unlock()
	return c
}

// expire closes an ask past its deadline.
func (s *Service) expire(a Ask) Ask {
	if a.Status.Final() || s.now().UnixMilli() < a.Deadline {
		return a
	}
	return s.close(a.ID, func(x *Ask) { x.Status = StatusTimeout })
}

// close finishes an ask once: the change runs only on a waiting ask; the chat's message gets the outcome.
func (s *Service) close(id string, change func(*Ask)) Ask {
	var before Ask
	after, err := s.Store.Update(id, func(x *Ask) error {
		if x.Status.Final() {
			return errClosed
		}
		before = *x
		change(x)
		x.ClosedAt = s.now().UnixMilli()
		return nil
	})
	if err != nil {
		return after
	}
	s.changed(&before, after)
	go s.editClosed(after)
	return after
}

var errClosed = refuse(ReasonClosed)

func (s *Service) editClosed(a Ask) {
	if a.MessageID == 0 {
		return
	}
	bot, err := s.botByID(a.BotID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = s.Telegram.EditHTML(ctx, bot.Token, a.ChatID, a.MessageID, closedHTML(a))
}

func (s *Service) botByID(id string) (notifybots.Bot, error) {
	b, err := s.Bots.Get(id)
	if err != nil {
		return notifybots.Bot{}, refuse(ReasonNoBot)
	}
	return b, nil
}

// Get is one ask, closed by its deadline when it passed.
func (s *Service) Get(id string) (Ask, error) {
	a, err := s.Store.Get(id)
	if err != nil {
		return Ask{}, err
	}
	return s.expire(a), nil
}

// List is every ask, newest first, the overdue ones closed.
func (s *Service) List() ([]Ask, error) {
	asks, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	for i := range asks {
		asks[i] = s.expire(asks[i])
	}
	return asks, nil
}

// Wait returns the ask once it is closed, or as it is when ctx ends first (a client's long poll).
func (s *Service) Wait(ctx context.Context, id string) (Ask, error) {
	for {
		changed := s.subscribe(id)
		a, err := s.Get(id)
		if err != nil || a.Status.Final() {
			return a, err
		}
		left := time.Until(time.UnixMilli(a.Deadline))
		if s.Now != nil {
			left = time.UnixMilli(a.Deadline).Sub(s.now())
		}
		timer := time.NewTimer(max(left, 0) + 50*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return a, nil
		case <-changed:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// MarkSeen records that the agent read the ask's outcome and its replies so far.
func (s *Service) MarkSeen(id string) (Ask, error) {
	return s.Store.Update(id, func(x *Ask) error {
		x.SeenAt, x.SeenReplies = s.now().UnixMilli(), len(x.Replies)
		return nil
	})
}

// Cancel calls an ask off.
func (s *Service) Cancel(id string) (Ask, error) {
	a, err := s.Store.Get(id)
	if err != nil {
		return Ask{}, err
	}
	if a.Status.Final() {
		return a, refuse(ReasonClosed)
	}
	return s.close(id, func(x *Ask) { x.Status = StatusCancelled }), nil
}

// choiceOf maps a button's choice to the decision and the option's text; ok false for a choice the ask has not.
func choiceOf(a Ask, choice string) (decision, text string, ok bool) {
	switch {
	case a.Kind == KindApproval && (choice == "ok" || choice == DecisionApproved):
		return DecisionApproved, "", true
	case a.Kind == KindApproval && (choice == "fix" || choice == DecisionChanges):
		return DecisionChanges, "", true
	case strings.HasPrefix(choice, "o"):
		n, err := strconv.Atoi(choice[1:])
		if err == nil && n >= 0 && n < len(a.Options) {
			return "", a.Options[n], true
		}
	}
	for _, o := range a.Options {
		if o == choice {
			return "", o, true
		}
	}
	return "", "", false
}

// answer closes an ask with a reply: a pressed button (choice) or words.
func (s *Service) answer(id string, r Reply) (Ask, error) {
	a, err := s.Store.Get(id)
	if err != nil {
		return Ask{}, err
	}
	if a.Status.Final() {
		return a, refuse(ReasonClosed)
	}
	decision, text := "", strings.TrimSpace(r.Text)
	if r.Choice != "" {
		d, t, ok := choiceOf(a, r.Choice)
		if !ok {
			return a, refuse(ReasonBadChoice)
		}
		decision, r.Choice = d, d
		if t != "" {
			r.Choice, text = t, t
		}
	} else if text == "" {
		return a, refuse(ReasonEmpty)
	} else if a.Kind == KindApproval {
		decision = DecisionComment
	}
	r.Text = text
	out := s.close(id, func(x *Ask) {
		x.Status, x.Decision = StatusAnswered, decision
		reply := r
		x.Answer = &reply
		x.Replies = appendReply(x.Replies, r)
	})
	if !out.Status.Final() || out.Answer == nil {
		return out, refuse(ReasonClosed)
	}
	return out, nil
}

func appendReply(rs []Reply, r Reply) []Reply {
	if len(rs) >= MaxReplies {
		return rs
	}
	return append(rs, r)
}

// AnswerFromWeb is the person's answer in the browser: a choice (approved, changes, an option) or words, or both.
func (s *Service) AnswerFromWeb(id, choice, text string, by Person) (Ask, error) {
	if by.Name == "" {
		by.Name = "harnessly"
	}
	if choice != "" && strings.TrimSpace(text) != "" {
		// A decision with a comment: the comment comes as a reply after it.
		a, err := s.answer(id, Reply{From: by, Choice: choice, Via: "web", At: s.now().UnixMilli()})
		if err != nil {
			return a, err
		}
		return s.addReply(id, Reply{From: by, Text: strings.TrimSpace(text), Via: "web", At: s.now().UnixMilli()})
	}
	return s.answer(id, Reply{From: by, Choice: choice, Text: text, Via: "web", At: s.now().UnixMilli()})
}

// addReply records a message that came after the answer.
func (s *Service) addReply(id string, r Reply) (Ask, error) {
	var before Ask
	a, err := s.Store.Update(id, func(x *Ask) error {
		before = *x
		x.Replies = appendReply(x.Replies, r)
		return nil
	})
	if err == nil {
		s.changed(&before, a)
	}
	return a, err
}
