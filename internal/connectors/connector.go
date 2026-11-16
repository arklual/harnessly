// Package connectors is the one contract every connector of the runtime follows: mail, calendars, folders, chat bots,
// feeds and incoming webhooks (docs/connectors.md). A connector adds three things to what harnessly already has:
//
//   - a way to connect: a Spec with the fields the person fills in, the secret ones kept in the machine's safe;
//   - tools for the agent: reads, free, and actions outward on the person's behalf, which only run once the person said
//     yes on the card of the chat that asked (Service.Propose / Service.Answer), each written to the journal of sends;
//   - a source of events: a Watcher the runtime runs with a cursor kept on the machine, whose events are short envelopes
//     (Event) without the text of a letter or a message, delivered to the hub's rules and to the inbox in the formats
//     they take today.
//
// A connector registers itself from init (Register) and knows nothing of the safe, the files or the delivery: the
// Service and the Runner do that for every connector the same way.
package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Kind is what a connector connects: the catalog groups its cards by it.
type Kind string

const (
	Mail     Kind = "mail"
	Calendar Kind = "calendar"
	Files    Kind = "files"
	Chat     Kind = "chat"
	Feed     Kind = "feed"
	Webhook  Kind = "webhook"
)

// Kinds are the kinds in the catalog's order.
var Kinds = []Kind{Mail, Calendar, Chat, Files, Feed, Webhook}

// Text is a word for the person in the page's languages. En is required; Ru falls back to En.
type Text struct {
	En string `json:"en"`
	Ru string `json:"ru,omitempty"`
}

// T is a Text in English and Russian.
func T(en, ru string) Text { return Text{En: en, Ru: ru} }

// FieldType says how the form asks a field and how it is checked.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldURL      FieldType = "url"
	FieldEmail    FieldType = "email"
	FieldNumber   FieldType = "number"
	FieldPath     FieldType = "path"
	FieldPassword FieldType = "password"
	// FieldChoice is one of Choices; the form draws it as a list of its own, not a native select.
	FieldChoice FieldType = "choice"
	// FieldChoices is several of Choices, kept as their values joined by commas; the form draws a row of toggles.
	FieldChoices FieldType = "choices"
)

// When shows a field only while another field has one of Values: a field of one way to connect (a CalDAV password)
// is hidden, not asked and not kept while the person chose the other way (a link). The other field is a choice.
type When struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

// Choice is one value of a FieldChoice.
type Choice struct {
	Value string `json:"value"`
	Label Text   `json:"label"`
}

// Field is one input of the connect form.
type Field struct {
	// ID is the key in Instance.Config (or the secret's key), [a-z][a-zA-Z0-9]*.
	ID    string    `json:"id"`
	Type  FieldType `json:"type"`
	Label Text      `json:"label"`
	Hint  Text      `json:"hint,omitzero"`
	// Placeholder is an example value ("https://example.com/feed.xml").
	Placeholder string   `json:"placeholder,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
	Default     string   `json:"default,omitempty"`
	Choices     []Choice `json:"choices,omitempty"`
	// When, if set, shows the field only while another field has one of its values; a hidden field is neither
	// required nor kept.
	When *When `json:"when,omitempty"`
	// Discover says the choices come from the service, not from Choices: the form asks Discoverer.Discover with the
	// values filled in so far (the calendars of an account, the folders of a mailbox) and draws them as buttons.
	Discover bool `json:"discover,omitempty"`
	// Secret fields are kept in the machine's safe (the secret "connector-<instance id>", a JSON object of the secret
	// fields), never in connectors.json, never in an answer of the runtime and never shown to an agent.
	Secret bool `json:"secret,omitempty"`
	// Generate is a secret the runtime makes itself (a webhook's signing key): the form does not ask it, Connect fills
	// it with a random value, and the person sees it once (Conn.Shown), then again only by Service.Reissue.
	Generate bool `json:"generate,omitempty"`
}

// Arg is one argument of a tool or an action, given as key=value.
type Arg struct {
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Required bool   `json:"required,omitempty"`
}

// Tool is a read the agent may do without asking: `harnessly connector read <connection> <tool> key=value…`.
type Tool struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Args    []Arg  `json:"args,omitempty"`
}

// Action is something done outward on the person's behalf (send a letter or a message, create a meeting): the agent
// proposes it with `harnessly connector act`, and it runs only after the person's yes on the card, exactly as shown.
type Action struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Title is the card's words for the person ("Send the letter").
	Title Text  `json:"title"`
	Args  []Arg `json:"args,omitempty"`
	// Direct marks an action that reaches only the person themselves (a push to their own bound chat): it runs at
	// once, without the card, and is still a line of the journal of sends. The connector must make sure it cannot
	// reach anyone else, and limit how often it runs.
	Direct bool `json:"direct,omitempty"`
}

// Spec describes a connector: the card of the catalog, the form, the tools and the events.
type Spec struct {
	// ID is stable and short, [a-z][a-z0-9-]*: "rss", "imap", "caldav". It names the connector in every event.
	ID      string  `json:"id"`
	Kind    Kind    `json:"kind"`
	Title   Text    `json:"title"`
	Summary Text    `json:"summary"`
	Fields  []Field `json:"fields"`
	// Events are the event types its source emits ("feed.item"); empty for a connector without a source.
	Events  []string `json:"events,omitempty"`
	Tools   []Tool   `json:"tools,omitempty"`
	Actions []Action `json:"actions,omitempty"`
	// Inbox is the inbox's source kind its events go to (POST /api/ingest, "mail", "calendar", "telegram"); "" keeps
	// them out of the inbox. Only kinds the inbox takes today may be named.
	Inbox string `json:"inbox,omitempty"`
}

// Instance is one connection of a connector as a connector sees it: the non-secret values of the form, a reader of
// the secret ones, and the HTTP client the runtime gives connectors (proxy, company CA, no cloud metadata addresses).
type Instance struct {
	ID        string
	Connector string
	Name      string
	Config    map[string]string
	// Secret reads a secret field from the safe at the moment of use (a rotated password is taken at once).
	Secret func(field string) (string, error)
	HTTP   *http.Client
	// Load and Store read and write the connection's own small record on the machine (a bound chat, a pairing code,
	// the last messages a bot got): JSON in <data>/connectors/state/<id>.memo.json, never shown to an agent or the
	// page, gone with the connection. Load of a record never written leaves v as it is.
	Load  func(v any) error
	Store func(v any) error
	// Conv is the chat whose agent asked, in Act; "" elsewhere.
	Conv string
	// Answer is the person's yes or no to a proposed action, given elsewhere than the page (a button in a bound chat);
	// set only for a Relay. The connector must make sure the answer is the person's own.
	Answer func(ctx context.Context, proposal string, approve bool) (Proposal, error)
	// Dir is the connection's own folder on the machine (<data>/connectors/data/<id>) for what the connector keeps
	// beyond the cursor: a webhook's bodies, a folder's journal of changes. Made on first use by the connector, removed
	// with the connection. Empty in a check before the connection is kept.
	Dir string
}

// Account says who or what a connection is, after a check: "me@example.com", "example.com/feed.xml".
type Account struct {
	// Label is shown in the list and goes into every event as "account".
	Label string `json:"label"`
}

// Result is what a read or an action answers: Text for the agent, and Data from outside the machine (a letter, a feed
// item) which the runtime always hands over marked as data, not instructions (Untrusted).
type Result struct {
	// Text is the connector's own words ("3 items", "sent").
	Text string `json:"text,omitempty"`
	// Data is what came from outside, as text.
	Data string `json:"data,omitempty"`
	// From names where Data came from, for the mark: "rss: Example blog".
	From string `json:"from,omitempty"`
}

// Connector is the contract. Check is required; the others it implements by what it can do: Reader for Spec.Tools,
// Actor for Spec.Actions, Watcher (or Poller) for Spec.Events.
type Connector interface {
	Spec() Spec
	// Check tries the connection's values and secrets against the service and says who they sign in as. Its error is
	// shown to the person: words, never a secret.
	Check(ctx context.Context, in Instance) (Account, error)
}

// Reader runs the reads of Spec.Tools.
type Reader interface {
	Read(ctx context.Context, in Instance, tool string, args map[string]string) (Result, error)
}

// Actor runs an action of Spec.Actions. The Service calls it only after the person said yes to exactly these args.
type Actor interface {
	Act(ctx context.Context, in Instance, action string, args map[string]string) (Result, error)
}

// Step is what the person still has to do for a connection to work, shown under it on the page: "Open the link and
// press Start" with the link.
type Step struct {
	Text Text   `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Stepper is a connector that may need one more step from the person after the check (a chat bound to a bot).
// Next answers it, or false when there is none.
type Stepper interface {
	Next(ctx context.Context, in Instance) (Step, bool)
}

// Relay is a connector that shows the proposed actions of every connection to the person in another place too (a bot's
// message with «Send / Don't send»), and takes their answer there through Instance.Answer.
type Relay interface {
	// Offer shows a new proposal. It runs in the background; an error is only logged.
	Offer(ctx context.Context, in Instance, p Proposal) error
	// Settle closes the shown proposal once it was answered (anywhere) or ran out of time.
	Settle(ctx context.Context, in Instance, p Proposal) error
}

// Discoverer lists the choices of a field marked Discover from the values the person filled in so far (Instance.Secret
// reads the form's secret values, not the safe): the calendars of an account, the folders of a mailbox. Its error is
// shown to the person like Check's.
type Discoverer interface {
	Discover(ctx context.Context, in Instance, field string) ([]Choice, error)
}

// ActGate refuses an action of a connection before a card is shown, when this connection cannot do it at all (a
// calendar connected by a read-only link): the agent gets the words at once and the person no card that cannot work.
type ActGate interface {
	CanAct(in Instance, action string) error
}

// Incoming is a request a service sent to the connection's own address (a webhook), as the runtime got it: the
// headers the hub passed on, the body, and the part of the path after the connection (a secret in the path).
type Incoming struct {
	Header http.Header
	Body   []byte
	Path   string
}

// Receiver is a source that is pushed to instead of asked: a webhook. Receive checks the request (the signature, by
// the connection's secret), keeps what it needs under Instance.Dir and answers the events it makes; the runtime keeps
// them and delivers them as a Watcher's. ErrUnauthorized answers 401 without words; ErrDuplicate a repeat already
// taken (200, nothing emitted).
type Receiver interface {
	Receive(ctx context.Context, in Instance, req Incoming) ([]Event, error)
}

// Shown is a value the person sees once, after a connect or a reissue: a generated secret, the address with it.
type Shown struct {
	Label Text   `json:"label"`
	Value string `json:"value"`
}

// Revealer says what the person sees once after a connect or a reissue. Without it, they see the generated fields.
type Revealer interface {
	Reveal(in Instance) ([]Shown, error)
}

// Cursor is a source's own mark of where it stopped: an opaque string the runtime keeps on the machine.
type Cursor string

// Emit hands a batch of events with the cursor after them. The runtime writes both to the machine's disk at once
// before it answers nil; a source must not move on past the batch until Emit returned nil. Events already emitted
// (the same Event.ID) are dropped, so a source that read again after a crash does not double them.
type Emit func(events []Event, next Cursor) error

// Watcher is a source of events that runs until ctx ends: IMAP IDLE, a bot's long poll, a folder watcher. It starts
// from the cursor of the last batch the runtime kept ("" on the first run) and returns when ctx is done; an error ends
// this run, and the runtime starts it again later from the last kept cursor.
type Watcher interface {
	Watch(ctx context.Context, in Instance, from Cursor, emit Emit) error
}

// Poller is a source the runtime asks every Interval: a feed, a calendar's sync token, a mailbox's UIDs. Each poll gets
// the last kept cursor and answers the new events and the cursor after them.
type Poller interface {
	Poll(ctx context.Context, in Instance, from Cursor) ([]Event, Cursor, error)
	// Interval is how long to wait between two polls of this connection.
	Interval(in Instance) time.Duration
}

// Errors a connector returns so that the page and the agent get a reason, not a stack.
var (
	ErrUnknownTool = errors.New("unknown tool")
	ErrBadArgs     = errors.New("bad arguments")
	ErrNotFound    = errors.New("not found")
	// ErrUnauthorized: a pushed request did not prove it comes from the service (Receiver).
	ErrUnauthorized = errors.New("unauthorized")
	// ErrDuplicate: a pushed request was already taken (Receiver).
	ErrDuplicate = errors.New("duplicate")
)

// Said is an error whose message is for the person: the form shows it as it is ("The address does not answer").
type Said struct{ Message string }

func (e *Said) Error() string { return e.Message }

// Sayf is a Said.
func Sayf(format string, args ...any) error { return &Said{Message: fmt.Sprintf(format, args...)} }

var (
	regMu    sync.RWMutex
	registry = map[string]Connector{}
	idRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	fieldRe  = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,31}$`)
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	typeRe   = regexp.MustCompile(`^[a-z]+(\.[a-z]+)+$`)
)

// Register adds a connector; connectors call it from init. A Spec that breaks the contract panics at start, so a
// mistake is seen by the first test, not by a person.
func Register(c Connector) {
	sp := c.Spec()
	if err := sp.check(); err != nil {
		panic("connectors: " + sp.ID + ": " + err.Error())
	}
	if len(sp.Tools) > 0 {
		if _, ok := c.(Reader); !ok {
			panic("connectors: " + sp.ID + ": tools without Read")
		}
	}
	if len(sp.Actions) > 0 {
		if _, ok := c.(Actor); !ok {
			panic("connectors: " + sp.ID + ": actions without Act")
		}
	}
	for _, f := range sp.Fields {
		if f.Discover {
			if _, ok := c.(Discoverer); !ok {
				panic("connectors: " + sp.ID + ": a discovered field without Discover")
			}
		}
	}
	if _, push := c.(Receiver); len(sp.Events) > 0 && sourceOf(c) == nil && !push {
		panic("connectors: " + sp.ID + ": events without Watch, Poll or Receive")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[sp.ID]; dup {
		panic("connectors: " + sp.ID + " registered twice")
	}
	registry[sp.ID] = c
}

// Lookup is the connector of an id.
func Lookup(id string) (Connector, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := registry[id]
	return c, ok
}

// Catalog is every registered connector's Spec, by kind in the catalog's order, then by id.
func Catalog() []Spec {
	regMu.RLock()
	out := make([]Spec, 0, len(registry))
	for _, c := range registry {
		out = append(out, c.Spec())
	}
	regMu.RUnlock()
	order := map[Kind]int{}
	for i, k := range Kinds {
		order[k] = i
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return order[out[i].Kind] < order[out[j].Kind]
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (sp Spec) check() error {
	if !idRe.MatchString(sp.ID) {
		return errors.New("id must be [a-z][a-z0-9-]*")
	}
	known := false
	for _, k := range Kinds {
		known = known || k == sp.Kind
	}
	if !known {
		return fmt.Errorf("unknown kind %q", sp.Kind)
	}
	if sp.Title.En == "" {
		return errors.New("no title")
	}
	seen := map[string]bool{}
	for _, f := range sp.Fields {
		if !fieldRe.MatchString(f.ID) || seen[f.ID] {
			return fmt.Errorf("field %q: bad or repeated id", f.ID)
		}
		seen[f.ID] = true
		if f.Label.En == "" {
			return fmt.Errorf("field %q: no label", f.ID)
		}
		if f.Type == FieldPassword && !f.Secret {
			return fmt.Errorf("field %q: a password field must be secret", f.ID)
		}
		if f.Generate && !f.Secret {
			return fmt.Errorf("field %q: a generated field must be secret", f.ID)
		}
		if (f.Type == FieldChoice || f.Type == FieldChoices) && len(f.Choices) == 0 && !f.Discover {
			return fmt.Errorf("field %q: a choice without choices", f.ID)
		}
		if f.Discover && f.Type != FieldChoices && f.Type != FieldChoice {
			return fmt.Errorf("field %q: only a choice is discovered", f.ID)
		}
	}
	for _, f := range sp.Fields {
		if f.When == nil {
			continue
		}
		ok := false
		for _, g := range sp.Fields {
			ok = ok || (g.ID == f.When.Field && g.ID != f.ID && g.Type == FieldChoice)
		}
		if !ok || len(f.When.Values) == 0 {
			return fmt.Errorf("field %q: when names no choice field of the spec", f.ID)
		}
	}
	for _, t := range sp.Events {
		if !typeRe.MatchString(t) {
			return fmt.Errorf("event type %q: must be like feed.item", t)
		}
	}
	names := map[string]bool{}
	for _, t := range sp.Tools {
		if !nameRe.MatchString(t.Name) || names[t.Name] {
			return fmt.Errorf("tool %q: bad or repeated name", t.Name)
		}
		names[t.Name] = true
	}
	for _, a := range sp.Actions {
		if !nameRe.MatchString(a.Name) || names[a.Name] || a.Title.En == "" {
			return fmt.Errorf("action %q: bad or repeated name, or no title", a.Name)
		}
		names[a.Name] = true
	}
	switch sp.Inbox {
	case "", "mail", "calendar", "telegram":
	default:
		return fmt.Errorf("inbox %q: the inbox takes mail, calendar and telegram", sp.Inbox)
	}
	return nil
}

// sourceOf is the connector's source as a Watcher: its own, or its Poller run on a timer.
func sourceOf(c Connector) Watcher {
	if w, ok := c.(Watcher); ok {
		return w
	}
	if p, ok := c.(Poller); ok {
		return polling{p}
	}
	return nil
}

// minPoll is the shortest wait between two polls of one connection: a service is never asked more often.
var minPoll = time.Minute

// polling runs a Poller as a Watcher: a poll, the batch emitted, a wait of Interval, until ctx ends.
type polling struct{ p Poller }

func (w polling) Watch(ctx context.Context, in Instance, from Cursor, emit Emit) error {
	cur := from
	for {
		events, next, err := w.p.Poll(ctx, in, cur)
		if err != nil {
			return err
		}
		if len(events) > 0 || next != cur {
			if err := emit(events, next); err != nil {
				return err
			}
			cur = next
		}
		d := max(w.p.Interval(in), minPoll)
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}
