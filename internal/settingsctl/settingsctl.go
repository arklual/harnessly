// Package settingsctl lets an agent change harnessly's settings when a person asks it to. It has three parts:
//
//   - the catalog: every setting an agent may read and propose a change of, with a title, a description, its kind, the
//     values it takes and the value it has now;
//   - the proposals: an agent of the Settings window's own chat changes a setting at once; an agent of any other chat
//     only proposes, and the change is made when the person says yes on the card the chat shows;
//   - the journal: every change (by a person or by an agent) with the old and the new value, who made it and when, so
//     that the Settings window can show what changed.
//
// A value is a string everywhere: an enum's value, "on"/"off" for a switch, digits for a number. The catalog holds no
// secret: a key of a provider is no setting.
package settingsctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Kind is what values a setting takes.
type Kind string

const (
	// KindEnum: one of Options.
	KindEnum Kind = "enum"
	// KindBool: "on" or "off".
	KindBool Kind = "bool"
	// KindNumber: a whole number from Min to Max.
	KindNumber Kind = "number"
	// KindText: a text the setting itself checks (Free), such as a description in JSON.
	KindText Kind = "text"
)

// The values of a switch.
const (
	On  = "on"
	Off = "off"
)

// Option is one value of an enum, with its words for the person.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Setting is a setting as the catalog lists it.
type Setting struct {
	Key string `json:"key"`
	// Section is the part of Settings it belongs to, in words.
	Section     string   `json:"section"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Kind        Kind     `json:"kind"`
	Options     []Option `json:"options,omitempty"`
	Min         *int     `json:"min,omitempty"`
	Max         *int     `json:"max,omitempty"`
	// Value is what the setting is now; ValueLabel the same in words.
	Value      string `json:"value"`
	ValueLabel string `json:"valueLabel"`
}

// Item is a setting with the way to read and to change it.
type Item struct {
	Key, Section, Title, Description string
	Kind                             Kind
	// Options are the values of an enum, asked each time (the providers and their models change).
	Options  func() []Option
	Min, Max int
	Get      func() (string, error)
	// Set is given a value that passed the check of its kind.
	Set func(value string) error
	// SetBy, when set, is called in place of Set and is also told who asks (a change that is made elsewhere, in the
	// hub, says the chat it comes from). Its note, when not empty, says what became of the value in place of a
	// change made here: it was passed on and waits for someone else's yes.
	SetBy func(value string, by Actor) (note string, err error)
	// Free lets an enum take a value that is none of Options when it says so (a model the provider does not list);
	// for a text it is the whole check.
	Free func(value string) bool
	// Why says in words why a value was not taken, when the setting can tell; nil or "": the common words.
	Why func(value string) string
	// Label says a text's value in words for the person (the card of a change, the journal); nil: the value itself.
	Label func(value string) string
	// Replaces says what a value that changes a part of a text takes the place of, in words ("" when it only adds);
	// part is false when the value is the whole text. nil: every value is the whole.
	Replaces func(value string) (label string, part bool)
	// Shown says whether the catalog has the setting now (one that a rollout opens while the process runs); nil: always.
	Shown func() bool
}

func (it Item) shown() bool { return it.Shown == nil || it.Shown() }

func boolLabel(v string) string {
	if v == On {
		return locale.Pick("on", "включено")
	}
	return locale.Pick("off", "выключено")
}

// label is a value in words.
func (it Item) label(v string) string {
	if it.Label != nil {
		return it.Label(v)
	}
	switch it.Kind {
	case KindBool:
		return boolLabel(v)
	case KindEnum:
		if it.Options != nil {
			for _, o := range it.Options() {
				if o.Value == v {
					return o.Label
				}
			}
		}
	}
	return v
}

// check says whether the setting takes the value.
func (it Item) check(v string) bool {
	switch it.Kind {
	case KindBool:
		return v == On || v == Off
	case KindNumber:
		n, err := strconv.Atoi(v)
		return err == nil && n >= it.Min && n <= it.Max && strconv.Itoa(n) == v
	case KindEnum:
		if it.Options != nil && slices.ContainsFunc(it.Options(), func(o Option) bool { return o.Value == v }) {
			return true
		}
		return it.Free != nil && it.Free(v)
	case KindText:
		return it.Free != nil && it.Free(v)
	}
	return false
}

func (it Item) view() (Setting, error) {
	v, err := it.Get()
	if err != nil {
		return Setting{}, err
	}
	s := Setting{Key: it.Key, Section: it.Section, Title: it.Title, Description: it.Description, Kind: it.Kind, Value: v, ValueLabel: it.label(v)}
	switch it.Kind {
	case KindEnum:
		s.Options = []Option{}
		if it.Options != nil {
			s.Options = it.Options()
		}
	case KindBool:
		s.Options = []Option{{Value: On, Label: boolLabel(On)}, {Value: Off, Label: boolLabel(Off)}}
	case KindNumber:
		lo, hi := it.Min, it.Max
		s.Min, s.Max = &lo, &hi
	}
	return s, nil
}

// Reasons of a refusal.
const (
	ReasonUnknownKey  = "unknown-setting" // no setting of that key
	ReasonBadValue    = "bad-value"       // the setting does not take the value
	ReasonSame        = "same"            // the setting already has the value
	ReasonNotFound    = "not-found"       // no proposal of that id
	ReasonClosed      = "closed"          // the proposal is answered already
	ReasonTooMany     = "too-many"        // too many proposals wait for an answer
	ReasonRateLimited = "rate-limited"    // too many proposals from one chat
	ReasonFailed      = "failed"
)

// Refusal is a reason the caller is told; Setting is the setting it is about when there is one, so that the caller can
// be shown what values it takes.
type Refusal struct {
	Reason  string
	Setting *Setting
	Err     error
	// Message is the setting's own words for the refusal, when it has some.
	Message string
}

func (r *Refusal) Error() string { return "settingsctl: " + r.Reason }

// Said is an error of a setting's Set whose words are for the person and the agent: a change that was refused where it
// is made (the hub said the person has no right to it).
type Said struct{ Message string }

func (e *Said) Error() string    { return e.Message }
func (r *Refusal) Unwrap() error { return r.Err }

// RefusalOf is the refusal of any error of the service.
func RefusalOf(err error) *Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return r
	}
	return &Refusal{Reason: ReasonFailed, Err: err}
}

// Who made a change.
const (
	ByPerson = "person"
	ByAgent  = "agent"
)

// How a change was made.
const (
	// ViaPage: the person changed it on the Settings screen.
	ViaPage = "page"
	// ViaSettingsChat: the agent of the Settings window's chat changed it at once.
	ViaSettingsChat = "settings-chat"
	// ViaConfirmed: an agent of another chat proposed it and the person said yes.
	ViaConfirmed = "confirmed"
)

// Actor is who made a change: a person, or the agent of a chat.
type Actor struct {
	Kind string `json:"kind"`
	// Conv and Title are the agent's chat (its title as it was then); empty for a person.
	Conv  string `json:"conv,omitempty"`
	Title string `json:"title,omitempty"`
}

// Change is one line of the journal.
type Change struct {
	ID    string `json:"id"`
	At    int64  `json:"at"`
	Key   string `json:"key"`
	Title string `json:"title"`
	// Old and New are the values; OldLabel and NewLabel the same in words.
	Old      string `json:"old"`
	New      string `json:"new"`
	OldLabel string `json:"oldLabel"`
	NewLabel string `json:"newLabel"`
	By       Actor  `json:"by"`
	Via      string `json:"via"`
	// Why is the agent's one line of why it changed the setting.
	Why string `json:"why,omitempty"`
	// Note, when not empty, says that the value was not set here but passed on, and what it waits for.
	Note string `json:"note,omitempty"`
}

// The states of a proposal.
const (
	StateWaiting  = "waiting"
	StateApplied  = "applied"
	StateDeclined = "declined"
	StateExpired  = "expired"
)

// Proposal is a change an agent asks the person to confirm: the card of the chat.
type Proposal struct {
	ID       string `json:"id"`
	At       int64  `json:"at"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Old      string `json:"old"`
	New      string `json:"new"`
	OldLabel string `json:"oldLabel"`
	NewLabel string `json:"newLabel"`
	Why      string `json:"why,omitempty"`
	// Conv and ConvTitle are the chat whose agent asks; the card is shown there.
	Conv      string `json:"conv"`
	ConvTitle string `json:"convTitle"`
	State     string `json:"state"`
	// Failed says why a change the person agreed to was not made, in the setting's words; "" for one that was not
	// tried or was made. Such a proposal is declined: nothing changed.
	Failed string `json:"failed,omitempty"`
}

type proposal struct {
	Proposal
	settled time.Time
	done    chan struct{}
}

const (
	// proposalTTL is how long a proposal waits for the person.
	proposalTTL = 30 * time.Minute
	// settledKept is how long an answered proposal still answers the agent that waits on it.
	settledKept  = 5 * time.Minute
	maxWaiting   = 16
	perConv      = 8
	perConvEvery = 10 * time.Minute
	// MaxWhy is the longest reason an agent gives.
	MaxWhy = 300
)

// Service is the catalog with its proposals and its journal.
type Service struct {
	// Items are the settings, in the order the catalog lists them.
	Items   []Item
	Journal *Journal
	// SettingsChat says whether a chat is the Settings window's own: its agent changes a setting at once.
	SettingsChat func(conv string) bool
	Now          func() time.Time

	mu     sync.Mutex
	list   map[string]*proposal
	recent map[string][]time.Time
	// apply serializes the changes: the old value of a journal line is the one the change replaced.
	apply sync.Mutex

	watch watch
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// CleanWhy is an agent's reason as one line.
func CleanWhy(why string) string {
	why = strings.Join(strings.Fields(why), " ")
	if utf8.RuneCountInString(why) > MaxWhy {
		why = string([]rune(why)[:MaxWhy])
	}
	return why
}

func (s *Service) item(key string) (Item, bool) {
	for _, it := range s.Items {
		if it.Key == key && it.shown() {
			return it, true
		}
	}
	return Item{}, false
}

// List is the catalog with the values of now. A setting that cannot be read is left out.
func (s *Service) List() []Setting {
	out := []Setting{}
	for _, it := range s.Items {
		if !it.shown() {
			continue
		}
		if v, err := it.view(); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// Get is one setting of the catalog.
func (s *Service) Get(key string) (Setting, error) {
	it, ok := s.item(key)
	if !ok {
		return Setting{}, &Refusal{Reason: ReasonUnknownKey}
	}
	v, err := it.view()
	if err != nil {
		return Setting{}, &Refusal{Reason: ReasonFailed, Err: err}
	}
	return v, nil
}

// checked is the setting and its item when it takes the value and does not have it yet.
func (s *Service) checked(key, value string) (Item, Setting, error) {
	it, ok := s.item(key)
	if !ok {
		return Item{}, Setting{}, &Refusal{Reason: ReasonUnknownKey}
	}
	cur, err := it.view()
	if err != nil {
		return Item{}, Setting{}, &Refusal{Reason: ReasonFailed, Err: err}
	}
	if !it.check(value) {
		r := &Refusal{Reason: ReasonBadValue, Setting: &cur}
		if it.Why != nil {
			r.Message = it.Why(value)
		}
		return Item{}, Setting{}, r
	}
	if cur.Value == value {
		return Item{}, Setting{}, &Refusal{Reason: ReasonSame, Setting: &cur}
	}
	return it, cur, nil
}

// Apply changes a setting now and writes the change into the journal.
func (s *Service) Apply(key, value string, by Actor, via, why string) (Change, error) {
	s.apply.Lock()
	defer s.apply.Unlock()
	it, cur, err := s.checked(key, value)
	if err != nil {
		return Change{}, err
	}
	set := it.Set
	note := ""
	if it.SetBy != nil {
		set = func(v string) (err error) { note, err = it.SetBy(v, by); return err }
	}
	if err := set(value); err != nil {
		r := &Refusal{Reason: ReasonFailed, Setting: &cur, Err: err}
		var said *Said
		if errors.As(err, &said) {
			r.Message = said.Message
		}
		return Change{}, r
	}
	said := it.label(value)
	if it.Kind == KindText {
		// A text may be a change of a part (one account of the list): the journal keeps what the setting became, so
		// that «Вернуть» brings back the whole of what it was.
		if now, err := it.Get(); err == nil {
			value = now
		}
	}
	ch := Change{ID: newID("sc_"), At: s.now().UnixMilli(), Key: key, Title: it.Title, Old: cur.Value, New: value,
		OldLabel: cur.ValueLabel, NewLabel: said, By: by, Via: via, Why: CleanWhy(why), Note: note}
	if s.Journal != nil {
		_ = s.Journal.Append(ch)
	}
	s.bump()
	return ch, nil
}

// Record writes into the journal a change that was made past the catalog (a person saved the Settings screen): the
// values are told, nothing is set.
func (s *Service) Record(key, old, value string, by Actor, via string) {
	it, ok := s.item(key)
	if !ok || old == value || s.Journal == nil {
		return
	}
	_ = s.Journal.Append(Change{ID: newID("sc_"), At: s.now().UnixMilli(), Key: key, Title: it.Title, Old: old, New: value,
		OldLabel: it.label(old), NewLabel: it.label(value), By: by, Via: via})
	s.bump()
}

// prune closes what waited too long and drops what was answered long ago; the caller holds mu.
func (s *Service) prune(now time.Time) {
	for id, p := range s.list {
		switch {
		case p.State == StateWaiting && now.Sub(time.UnixMilli(p.At)) >= proposalTTL:
			p.State, p.settled = StateExpired, now
			close(p.done)
			s.bump()
		// A yes being applied (Approve between its two locks) has no settled time yet: it stays.
		case p.State != StateWaiting && !p.settled.IsZero() && now.Sub(p.settled) >= settledKept:
			delete(s.list, id)
		}
	}
}

// Outcome is what an agent's proposal came to.
type Outcome struct {
	// State: applied (the setting is changed), waiting (the person has not answered yet), declined, expired.
	State string `json:"status"`
	// Change is the journal line of an applied change.
	Change *Change `json:"change,omitempty"`
	// Proposal is the card the person answers; absent for a change of the Settings window's chat, made at once.
	Proposal *Proposal `json:"proposal,omitempty"`
}

// Propose is an agent's change of a setting. The agent of the Settings window's chat changes it at once. Any other
// agent leaves a proposal for the person: the one that waits already for the same change of the same chat, else a new
// one.
func (s *Service) Propose(key, value, why string, by Actor) (Outcome, error) {
	by.Kind = ByAgent
	if by.Conv != "" && s.SettingsChat != nil && s.SettingsChat(by.Conv) {
		ch, err := s.Apply(key, value, by, ViaSettingsChat, why)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{State: StateApplied, Change: &ch}, nil
	}
	it, cur, err := s.checked(key, value)
	if err != nil {
		return Outcome{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	waiting := 0
	for _, p := range s.list {
		if p.State != StateWaiting {
			continue
		}
		if p.Key == key && p.New == value && p.Conv == by.Conv {
			out := p.Proposal
			return Outcome{State: StateWaiting, Proposal: &out}, nil
		}
		waiting++
	}
	if waiting >= maxWaiting {
		return Outcome{}, &Refusal{Reason: ReasonTooMany}
	}
	fresh := s.recent[by.Conv][:0]
	for _, t := range s.recent[by.Conv] {
		if now.Sub(t) < perConvEvery {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= perConv {
		return Outcome{}, &Refusal{Reason: ReasonRateLimited}
	}
	if s.recent == nil {
		s.recent = map[string][]time.Time{}
	}
	s.recent[by.Conv] = append(fresh, now)
	was := cur.ValueLabel
	if it.Replaces != nil {
		// The card strikes out only what the change takes away: a new account does not replace the others.
		if label, part := it.Replaces(value); part {
			was = label
		}
	}
	p := &proposal{Proposal: Proposal{ID: newID("sp_"), At: now.UnixMilli(), Key: key, Title: it.Title, Old: cur.Value, New: value,
		OldLabel: was, NewLabel: it.label(value), Why: CleanWhy(why), Conv: by.Conv, ConvTitle: by.Title, State: StateWaiting},
		done: make(chan struct{})}
	if s.list == nil {
		s.list = map[string]*proposal{}
	}
	s.list[p.ID] = p
	s.bump()
	out := p.Proposal
	return Outcome{State: StateWaiting, Proposal: &out}, nil
}

// Wait is what the proposal came to after at most d (or ctx's end).
func (s *Service) Wait(ctx context.Context, id string, d time.Duration) Outcome {
	s.mu.Lock()
	s.prune(s.now())
	p := s.list[id]
	s.mu.Unlock()
	if p == nil {
		return Outcome{State: StateExpired}
	}
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	out := p.Proposal
	return Outcome{State: out.State, Proposal: &out}
}

// Pending are the proposals that wait for the person, the oldest first.
func (s *Service) Pending() []Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	out := []Proposal{}
	for _, p := range s.list {
		if p.State == StateWaiting {
			out = append(out, p.Proposal)
		}
	}
	slices.SortFunc(out, func(a, b Proposal) int {
		if a.At != b.At {
			return int(a.At - b.At)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Approve is the person's yes: the setting is changed as the agent of the proposal's chat, and the proposal is
// applied. A change the setting no longer takes (it has the value already, the value is gone) closes the proposal as
// declined and is told.
func (s *Service) Approve(id string) (Outcome, error) {
	s.mu.Lock()
	s.prune(s.now())
	p := s.list[id]
	if p == nil {
		s.mu.Unlock()
		return Outcome{}, &Refusal{Reason: ReasonNotFound}
	}
	if p.State != StateWaiting {
		s.mu.Unlock()
		return Outcome{}, &Refusal{Reason: ReasonClosed}
	}
	// Taken out of the waiting ones before the change, so that two yeses make one change. Settled now too: prune drops
	// an answered proposal by its settled time, and a zero one would drop it while the change is still being made.
	p.State, p.settled = StateApplied, s.now()
	req := p.Proposal
	s.mu.Unlock()

	ch, err := s.Apply(req.Key, req.New, Actor{Kind: ByAgent, Conv: req.Conv, Title: req.ConvTitle}, ViaConfirmed, req.Why)
	s.mu.Lock()
	defer s.mu.Unlock()
	p.settled = s.now()
	if err != nil {
		p.State = StateDeclined
		if p.Failed = RefusalOf(err).Message; p.Failed == "" {
			p.Failed = "the change was not made"
		}
		close(p.done)
		s.bump()
		return Outcome{}, err
	}
	close(p.done)
	s.bump()
	out := p.Proposal
	return Outcome{State: StateApplied, Change: &ch, Proposal: &out}, nil
}

// Decline is the person's no.
func (s *Service) Decline(id string) (Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	p := s.list[id]
	if p == nil {
		return Outcome{}, &Refusal{Reason: ReasonNotFound}
	}
	if p.State != StateWaiting {
		return Outcome{}, &Refusal{Reason: ReasonClosed}
	}
	p.State, p.settled = StateDeclined, s.now()
	close(p.done)
	s.bump()
	out := p.Proposal
	return Outcome{State: StateDeclined, Proposal: &out}, nil
}
