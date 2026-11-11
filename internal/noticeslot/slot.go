// Package noticeslot is the smart notice's open slot: an app (through its SDK) or an agent (through the
// `harnessly notice` command or the notice_ask MCP tool) shows the person a ready element in the black plate — a
// status, a progress, a choice, a confirmation, a short field, code cells or a view of its own — and gets the answer
// back. One description serves the three ways in. The page lists what is open and answers it.
//
// What the slot never does: ask for a password, a key or a sign-in. There is no password field in any kind, a view of
// its own that brings one is refused, a field worded as a password or key is refused, and a typed answer that looks
// like a key is turned back with a pointer to the safe. Secrets go through the safe and the site sign-in.
//
// It lives in memory: a notice is a question of the moment, and the call that waits for it is gone with a restart too.
// Only the sources the person turned off are kept (Muted), by the caller.
package noticeslot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Kind is what the plate shows.
type Kind string

const (
	KindStatus   Kind = "status"
	KindProgress Kind = "progress"
	KindChoice   Kind = "choice"
	KindConfirm  Kind = "confirm"
	KindField    Kind = "field"
	KindCode     Kind = "code"
	KindCustom   Kind = "custom"
)

// Waits tells a kind that waits for the person's answer (the others only tell).
func (k Kind) Waits() bool { return k != KindStatus && k != KindProgress }

func (k Kind) known() bool {
	switch k {
	case KindStatus, KindProgress, KindChoice, KindConfirm, KindField, KindCode, KindCustom:
		return true
	}
	return false
}

const (
	SourceApp   = "app"
	SourceAgent = "agent"
)

// Source is who asks: an app (ID is its id, Name its name from the manifest) or the agent of a chat (ID is the chat).
// The page draws it as the label over the plate, which the caller cannot change.
type Source struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Key names the source for limits and muting.
func (s Source) Key() string { return s.Kind + ":" + s.ID }

// Spec is what a caller asks to show.
type Spec struct {
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	// Icon is a harnessly icon name for the plate; empty for the kind's own.
	Icon string `json:"icon,omitempty"`
	// Options are a choice's 2…6 answers; Own adds a line for the person's own words.
	Options []string `json:"options,omitempty"`
	Own     bool     `json:"own,omitempty"`
	// Yes and No name a confirmation's buttons; Danger marks an action that cannot be undone.
	Yes    string `json:"yes,omitempty"`
	No     string `json:"no,omitempty"`
	Danger bool   `json:"danger,omitempty"`
	// Placeholder is a field's hint while it is empty (a field is one line).
	Placeholder string `json:"placeholder,omitempty"`
	// Digits is a code's length, 4…8.
	Digits int `json:"digits,omitempty"`
	// Progress is a progress's share done, 0…1; nil while it is not known.
	Progress *float64 `json:"progress,omitempty"`
	// View is a view of its own: an HTML fragment of layout and buttons (data-answer), drawn with the plate's tokens and kit.
	View string `json:"view,omitempty"`
	// TimeoutSec is how long it waits; 0 for the default, at most a day.
	TimeoutSec int `json:"timeoutSec,omitempty"`
}

// Notice is one open notice as the page lists it.
type Notice struct {
	ID string `json:"id"`
	Spec
	Source       Source `json:"source"`
	Conversation string `json:"conversation,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	// Queued: another notice of the same source waits in the same chat before it; the page shows it after that one.
	Queued bool `json:"queued,omitempty"`
}

// Result is what the caller gets back.
type Result struct {
	// Status: answered, dismissed (the person said no or closed it), timeout, or muted (the person turned the source off).
	Status string `json:"status"`
	// Value is the answer: a choice's index ({"choice": n}) or own words ({"text": s}), a confirmation's true/false, a
	// field's or code's text, a view's own JSON.
	Value json.RawMessage `json:"value,omitempty"`
}

const (
	StatusAnswered  = "answered"
	StatusDismissed = "dismissed"
	StatusTimeout   = "timeout"
	StatusMuted     = "muted"
)

// Limits.
const (
	DefaultTTL     = 10 * time.Minute
	MaxTTL         = 24 * time.Hour
	StatusTTL      = 8 * time.Second
	MaxShownPerSrc = 3  // waiting notices of one source shown at once on the machine
	MaxOpenPerSrc  = 12 // all open notices of one source, queued ones too
	RatePerMinute  = 5  // new notices of one source a minute
	MaxTitle       = 120
	MaxText        = 600
	MaxOption      = 80
	MinOptions     = 2
	MaxOptions     = 6
	MaxView        = 16 << 10
	MaxAnswer      = 2000
)

var (
	ErrArgs   = errors.New("noticeslot: bad notice")
	ErrSecret = errors.New("noticeslot: the slot does not take passwords, keys or sign-ins; use the safe")
	ErrBusy   = errors.New("noticeslot: this source has too many open notices")
	ErrRate   = errors.New("noticeslot: this source shows notices too often")
	ErrMuted  = errors.New("noticeslot: the person turned this source off")
	ErrGone   = errors.New("noticeslot: no such notice")
	ErrAnswer = errors.New("noticeslot: the answer does not fit the notice")
	// ErrAnswerSecret: the person typed something that looks like a key; the page points them to the safe.
	ErrAnswerSecret = errors.New("noticeslot: the answer looks like a key")
)

var iconName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

type open struct {
	Notice
	result chan Result
	done   bool
	final  Result
}

// Slot is the machine's open notices.
type Slot struct {
	Now func() time.Time
	// Muted tells a source the person turned off; nil for none.
	Muted func(Source) bool
	// Changed is told when the list changes (the page refreshes); nil for none.
	Changed func()

	mu    sync.Mutex
	open  map[string]*open
	shows map[string][]time.Time
}

func New() *Slot { return &Slot{open: map[string]*open{}, shows: map[string][]time.Time{}} }

func (s *Slot) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Slot) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

// Check validates a spec and fills its defaults; the error says what is wrong.
func Check(sp *Spec) error {
	sp.Title = strings.TrimSpace(sp.Title)
	sp.Text = strings.TrimSpace(sp.Text)
	if !sp.Kind.known() || sp.Title == "" || utf8.RuneCountInString(sp.Title) > MaxTitle || utf8.RuneCountInString(sp.Text) > MaxText {
		return ErrArgs
	}
	if sp.Icon != "" && !iconName.MatchString(sp.Icon) {
		return ErrArgs
	}
	if sp.TimeoutSec < 0 || time.Duration(sp.TimeoutSec)*time.Second > MaxTTL {
		return ErrArgs
	}
	// Nothing in the slot carries a key, whatever its kind.
	if LooksSecret(sp.Title) || LooksSecret(sp.Text) {
		return ErrSecret
	}
	switch sp.Kind {
	case KindChoice:
		if len(sp.Options) < MinOptions || len(sp.Options) > MaxOptions {
			return ErrArgs
		}
		for i, o := range sp.Options {
			sp.Options[i] = strings.TrimSpace(o)
			if sp.Options[i] == "" || utf8.RuneCountInString(sp.Options[i]) > MaxOption {
				return ErrArgs
			}
		}
	case KindConfirm:
		if utf8.RuneCountInString(sp.Yes) > MaxOption || utf8.RuneCountInString(sp.No) > MaxOption {
			return ErrArgs
		}
	case KindField:
		if AsksForSecret(sp.Title, sp.Text, sp.Placeholder) {
			return ErrSecret
		}
		if utf8.RuneCountInString(sp.Placeholder) > MaxOption {
			return ErrArgs
		}
	case KindCode:
		if sp.Digits == 0 {
			sp.Digits = 6
		}
		if sp.Digits < 4 || sp.Digits > 8 {
			return ErrArgs
		}
		if AsksForSecret(sp.Title, sp.Text) {
			return ErrSecret
		}
	case KindProgress:
		if sp.Progress != nil && (*sp.Progress < 0 || *sp.Progress > 1) {
			return ErrArgs
		}
	case KindCustom:
		if strings.TrimSpace(sp.View) == "" || len(sp.View) > MaxView {
			return ErrArgs
		}
		if HasPasswordField(sp.View) {
			return ErrSecret
		}
		if ViewIsActive(sp.View) {
			return fmt.Errorf("%w: a custom view is layout and buttons with data-answer: no script, handler, field, form, frame or link", ErrArgs)
		}
	}
	// Only what the kind uses travels to the page.
	if sp.Kind != KindChoice {
		sp.Options, sp.Own = nil, false
	}
	if sp.Kind != KindConfirm {
		sp.Yes, sp.No, sp.Danger = "", "", false
	}
	if sp.Kind != KindField {
		sp.Placeholder = ""
	}
	if sp.Kind != KindCode {
		sp.Digits = 0
	}
	if sp.Kind != KindProgress {
		sp.Progress = nil
	}
	if sp.Kind != KindCustom {
		sp.View = ""
	}
	return nil
}

func (sp Spec) ttl() time.Duration {
	switch {
	case sp.Kind == KindStatus:
		return StatusTTL
	case sp.TimeoutSec > 0:
		return time.Duration(sp.TimeoutSec) * time.Second
	}
	return DefaultTTL
}

func (sp Spec) same(o Spec) bool {
	return sp.Kind == o.Kind && sp.Title == o.Title && sp.Text == o.Text &&
		strings.Join(sp.Options, "\x00") == strings.Join(o.Options, "\x00")
}

// Open shows a notice and returns it at once; Wait gets its result. The same question from the same source in the
// same chat while the first is open joins it (its id comes back), so a caller that repeats itself shows one plate.
func (s *Slot) Open(src Source, conv string, sp Spec) (Notice, error) {
	if src.Kind != SourceApp && src.Kind != SourceAgent || src.ID == "" {
		return Notice{}, ErrArgs
	}
	if err := Check(&sp); err != nil {
		return Notice{}, err
	}
	if s.Muted != nil && s.Muted(src) {
		return Notice{}, ErrMuted
	}
	now := s.now()
	s.mu.Lock()
	s.sweep(now)
	key := src.Key()
	count := 0
	for _, o := range s.open {
		if o.Source.Key() != key {
			continue
		}
		if o.Conversation == conv && !o.done && o.Spec.same(sp) {
			n := o.Notice
			s.mu.Unlock()
			return n, nil
		}
		count++
	}
	if count >= MaxOpenPerSrc {
		s.mu.Unlock()
		return Notice{}, ErrBusy
	}
	recent := s.shows[key][:0]
	for _, t := range s.shows[key] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= RatePerMinute {
		s.shows[key] = recent
		s.mu.Unlock()
		return Notice{}, ErrRate
	}
	s.shows[key] = append(recent, now)
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	src.Name = strings.TrimSpace(src.Name)
	o := &open{
		Notice: Notice{ID: "ntc_" + hex.EncodeToString(raw[:]), Spec: sp, Source: src, Conversation: conv,
			CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(sp.ttl()).UnixMilli()},
		result: make(chan Result, 1),
	}
	s.open[o.ID] = o
	n := o.Notice
	s.mu.Unlock()
	s.changed()
	return n, nil
}

// Ask shows a notice and waits for its result (a telling kind returns answered at once).
func (s *Slot) Ask(ctx context.Context, src Source, conv string, sp Spec) (Result, error) {
	n, err := s.Open(src, conv, sp)
	if err != nil {
		return Result{}, err
	}
	if !n.Kind.Waits() {
		return Result{Status: StatusAnswered}, nil
	}
	ttl := time.Until(time.UnixMilli(n.ExpiresAt))
	wctx, cancel := context.WithTimeout(ctx, ttl)
	defer cancel()
	r, done := s.Wait(wctx, n.ID)
	if !done {
		// The caller left (or the time ran out): the plate goes.
		s.finish(n.ID, Result{Status: StatusTimeout})
		return Result{Status: StatusTimeout}, nil
	}
	return r, nil
}

// Wait waits for a notice's result until ctx ends; done=false while it is still open. A finished notice gives its
// result once more for a while, so a caller polling again after a lost answer still gets it.
func (s *Slot) Wait(ctx context.Context, id string) (Result, bool) {
	s.mu.Lock()
	now := s.now()
	s.sweep(now)
	o := s.open[id]
	if o == nil {
		s.mu.Unlock()
		return Result{Status: StatusTimeout}, true
	}
	if o.done {
		r := o.final
		s.mu.Unlock()
		return r, true
	}
	ch := o.result
	// The notice's own term ends the wait too, whether or not the page is there to look.
	left := time.UnixMilli(o.ExpiresAt).Sub(now)
	s.mu.Unlock()
	term := time.NewTimer(max(left, 0))
	defer term.Stop()
	select {
	case r := <-ch:
		ch <- r // the next waiter sees it too
		return r, true
	case <-term.C:
		s.finish(id, Result{Status: StatusTimeout})
		r := <-ch
		ch <- r
		return r, true
	case <-ctx.Done():
		return Result{}, false
	}
}

// keepDone is how long a finished notice still answers Wait.
const keepDone = 2 * time.Minute

func (s *Slot) finish(id string, r Result) bool {
	s.mu.Lock()
	o := s.open[id]
	if o == nil || o.done {
		s.mu.Unlock()
		return false
	}
	o.done, o.final = true, r
	o.ExpiresAt = s.now().Add(keepDone).UnixMilli()
	s.mu.Unlock()
	o.result <- r
	s.changed()
	return true
}

// sweep closes what ran out and forgets what finished long ago; called with mu held.
func (s *Slot) sweep(now time.Time) {
	for id, o := range s.open {
		if now.UnixMilli() < o.ExpiresAt {
			continue
		}
		if o.done || !o.Kind.Waits() {
			delete(s.open, id)
			continue
		}
		o.done, o.final = true, Result{Status: StatusTimeout}
		o.ExpiresAt = now.Add(keepDone).UnixMilli()
		o.result <- o.final
	}
}

// Pending is what the page shows: the open notices, oldest first; of one source in one chat only the first is shown,
// the rest are Queued behind it, and at most MaxShownPerSrc waiting ones of a source are shown on the machine.
func (s *Slot) Pending() []Notice {
	s.mu.Lock()
	s.sweep(s.now())
	out := make([]Notice, 0, len(s.open))
	for _, o := range s.open {
		if !o.done {
			out = append(out, o.Notice)
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt < out[j].CreatedAt || out[i].CreatedAt == out[j].CreatedAt && out[i].ID < out[j].ID
	})
	head := map[string]bool{}
	shown := map[string]int{}
	for i := range out {
		n := &out[i]
		if !n.Kind.Waits() {
			continue
		}
		k := n.Source.Key()
		chat := k + "|" + n.Conversation
		if head[chat] || shown[k] >= MaxShownPerSrc {
			n.Queued = true
			continue
		}
		head[chat] = true
		shown[k]++
	}
	return out
}

// Update changes an open progress or status: its share done and its text. Only the source that opened it may.
func (s *Slot) Update(src Source, id string, progress *float64, text *string, title *string) error {
	s.mu.Lock()
	o := s.open[id]
	if o == nil || o.done || o.Source.Key() != src.Key() {
		s.mu.Unlock()
		return ErrGone
	}
	next := o.Spec
	if progress != nil {
		p := *progress
		next.Progress = &p
	}
	if text != nil {
		next.Text = *text
	}
	if title != nil {
		next.Title = *title
	}
	if err := Check(&next); err != nil {
		s.mu.Unlock()
		return err
	}
	o.Spec = next
	if o.Kind == KindStatus {
		o.ExpiresAt = s.now().Add(StatusTTL).UnixMilli()
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

// SourceOf is who opened a notice (ok=false when there is none).
func (s *Slot) SourceOf(id string) (Source, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.open[id]
	if o == nil {
		return Source{}, false
	}
	return o.Source, true
}

// Close takes a notice away for its source (a progress that ended, a question no longer needed).
func (s *Slot) Close(src Source, id string) error {
	s.mu.Lock()
	o := s.open[id]
	ok := o != nil && o.Source.Key() == src.Key()
	s.mu.Unlock()
	if !ok {
		return ErrGone
	}
	s.finish(id, Result{Status: StatusDismissed})
	return nil
}

// Dismiss is the person closing a notice (no answer).
func (s *Slot) Dismiss(id string) error {
	if !s.finish(id, Result{Status: StatusDismissed}) {
		return ErrGone
	}
	return nil
}

// MuteSource ends every open notice of a source the person just turned off.
func (s *Slot) MuteSource(src Source) {
	s.mu.Lock()
	var ids []string
	for id, o := range s.open {
		if o.Source.Key() == src.Key() && !o.done {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.finish(id, Result{Status: StatusMuted})
	}
}

// Answer is the person's answer; it is checked against the notice's kind. A typed text that looks like a key is
// refused with ErrAnswerSecret and the notice stays open.
func (s *Slot) Answer(id string, value json.RawMessage) error {
	s.mu.Lock()
	o := s.open[id]
	if o == nil || o.done {
		s.mu.Unlock()
		return ErrGone
	}
	sp := o.Spec
	s.mu.Unlock()
	clean, err := checkAnswer(sp, value)
	if err != nil {
		return err
	}
	if !s.finish(id, Result{Status: StatusAnswered, Value: clean}) {
		return ErrGone
	}
	return nil
}

var codeDigits = regexp.MustCompile(`^[0-9]+$`)

func checkAnswer(sp Spec, value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 || len(value) > MaxAnswer*2 {
		return nil, ErrAnswer
	}
	text := func(s string, max int) error {
		if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > max {
			return ErrAnswer
		}
		if LooksSecret(s) {
			return ErrAnswerSecret
		}
		return nil
	}
	switch sp.Kind {
	case KindChoice:
		var v struct {
			Choice *int    `json:"choice"`
			Text   *string `json:"text"`
		}
		if json.Unmarshal(value, &v) != nil {
			return nil, ErrAnswer
		}
		switch {
		case v.Choice != nil && v.Text == nil:
			if *v.Choice < 0 || *v.Choice >= len(sp.Options) {
				return nil, ErrAnswer
			}
			return json.Marshal(map[string]any{"choice": *v.Choice, "option": sp.Options[*v.Choice]})
		case v.Text != nil && v.Choice == nil && sp.Own:
			if err := text(*v.Text, MaxAnswer); err != nil {
				return nil, err
			}
			return json.Marshal(map[string]string{"text": strings.TrimSpace(*v.Text)})
		}
		return nil, ErrAnswer
	case KindConfirm:
		var v bool
		if json.Unmarshal(value, &v) != nil {
			return nil, ErrAnswer
		}
		return json.Marshal(v)
	case KindField:
		var v string
		if json.Unmarshal(value, &v) != nil {
			return nil, ErrAnswer
		}
		if err := text(v, MaxAnswer); err != nil {
			return nil, err
		}
		return json.Marshal(strings.TrimSpace(v))
	case KindCode:
		var v string
		if json.Unmarshal(value, &v) != nil || len(v) != sp.Digits || !codeDigits.MatchString(v) {
			return nil, ErrAnswer
		}
		return json.Marshal(v)
	case KindCustom:
		var v any
		if json.Unmarshal(value, &v) != nil {
			return nil, ErrAnswer
		}
		if anySecret(v) {
			return nil, ErrAnswerSecret
		}
		return json.Marshal(v)
	}
	return nil, ErrAnswer
}

func anySecret(v any) bool {
	switch t := v.(type) {
	case string:
		return LooksSecret(t)
	case []any:
		for _, x := range t {
			if anySecret(x) {
				return true
			}
		}
	case map[string]any:
		for k, x := range t {
			if LooksSecret(k) || anySecret(x) {
				return true
			}
		}
	}
	return false
}
