// Package eventbus is the hub's journal of what happened to people and the subscriptions that read it
// (docs/hub/event-bus.md). Every source — a connector of a machine, an outside webhook, a schedule that fired,
// later a poll of an MCP server or an app through the SDK — appends one envelope without text (Event v1) to the
// journal; every consumer — the rules of automations, the digest, notifications, widgets, apps — reads it from a cursor
// of its own. A new consumer does not touch the sources, and a new source does not touch the consumers.
package eventbus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Version is the version of the envelope this hub writes.
const Version = 1

// The ways an event can reach the bus. After a merge an event lists every way it came by.
const (
	ViaConnector = "connector" // a connector of the owner's machine, over the machine's link
	ViaWebhook   = "webhook"   // an outside service called the hub
	ViaPoll      = "poll"      // the hub asked a service (an MCP recipe)
	ViaSubscribe = "subscribe" // an MCP server told the hub over a subscription
	ViaSchedule  = "schedule"  // the hub's clock: a schedule or a wait fired
	ViaHarnessly = "harnessly" // harnessly itself: a chat ended, an agent waits
	ViaSDK       = "sdk"       // an app or a plugin emitted it
)

var vias = []string{ViaConnector, ViaWebhook, ViaPoll, ViaSubscribe, ViaSchedule, ViaHarnessly, ViaSDK}

// Who may see an event beyond its owner.
const (
	VisPerson = "person"
	VisTeam   = "team"
)

// personalDomains are the domains of a person's own correspondence: even an envelope without text says who wrote to
// whom and when, so such an event never leaves its owner, whatever a source asks.
var personalDomains = []string{"mail", "chat", "message", "calendar", "meeting", "note", "file", "contact", "call", "dm"}

// VisibilityOf is the visibility an event of this type gets: VisPerson for the domains of correspondence, else VisTeam.
func VisibilityOf(typ string) string {
	domain, _, _ := strings.Cut(typ, ".")
	if slices.Contains(personalDomains, domain) {
		return VisPerson
	}
	return VisTeam
}

// Event is the envelope v1. It says that something happened and where to read it, never what it says: no title, no
// body, no excerpt. Whoever needs the text reads it by Ref on the owner's machine, or opens URL.
type Event struct {
	V   int    `json:"v"`
	Seq int64  `json:"seq"` // the place in the journal: set by the hub, read by cursors
	ID  string `json:"id"`  // the event's id in the journal: consumers are idempotent by it
	// Owner is the hub user the event belongs to; "" is the deployment's own (the shared /hook), seen by its admins and
	// by the hub's own subscriptions only.
	Owner string `json:"owner"`
	Org   string `json:"org,omitempty"`
	// Space is the space (tenant) the event belongs to: the space of the machine it came from, or of the connection.
	Space string `json:"space,omitempty"`
	// Visibility is who may see the event beyond its owner: VisPerson (nobody; mail, chats, calendar) or VisTeam (the
	// members of its space and the admins of its organization). Empty on Append: chosen by the type's domain.
	Visibility string `json:"visibility"`
	// Type is "<domain>.<what>": chat.message, mail.received, issue.assigned, mr.review_requested, schedule.fired.
	Type string `json:"type"`
	// Source is where it came from: a connector's id (telegram-account, imap), a service (gitlab), "mcp:<server>",
	// "harnessly", "app:<id>".
	Source     string `json:"source"`
	Connection string `json:"connection,omitempty"` // the owner's connection it came through
	Ext        string `json:"ext,omitempty"`        // the source's own id of the thing
	// Fingerprint makes the event unique for its owner: the same thing that came by a webhook and by a poll is one
	// event. Empty on Append: made from Source and Ext.
	Fingerprint string            `json:"fingerprint"`
	At          time.Time         `json:"at"`       // when it happened at the source
	Received    time.Time         `json:"received"` // when the hub wrote it
	Actor       string            `json:"actor,omitempty"`
	URL         string            `json:"url,omitempty"`
	Thread      string            `json:"thread,omitempty"`
	Ref         string            `json:"ref,omitempty"`
	Mine        bool              `json:"mine,omitempty"`
	Facts       map[string]string `json:"facts,omitempty"`
	Via         []string          `json:"via"`
	Machine     string            `json:"machine,omitempty"` // the machine that keeps the event's text, if any
}

// Limits of the envelope: it stays a short envelope however a source fills it.
const (
	typeMax   = 64
	sourceMax = 64
	extMax    = 400
	fpMax     = 500
	actorMax  = 120
	urlMax    = 2000
	threadMax = 200
	refMax    = 500
	factsMax  = 12
	factKey   = 40
	factMax   = 200
	idMax     = 64
)

var (
	typeRe   = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)+$`)
	sourceRe = regexp.MustCompile(`^[a-z][a-z0-9_.-]*(:[a-z0-9_.-]+)?$`)
	factRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

// ErrInvalid is wrapped by every refusal of an envelope.
var ErrInvalid = errors.New("eventbus: invalid event")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// normalize checks an envelope before it is written and cuts what may be cut: a long actor or fact is clipped, a URL
// that is not http(s) is dropped, an unknown way is refused. It never adds text.
func (e Event) normalize(now time.Time) (Event, error) {
	e.V = Version
	e.Type, e.Source = strings.TrimSpace(e.Type), strings.TrimSpace(e.Source)
	if len(e.Type) > typeMax || !typeRe.MatchString(e.Type) {
		return e, invalid("type %q: <domain>.<what>, lower case", e.Type)
	}
	if len(e.Source) > sourceMax || !sourceRe.MatchString(e.Source) {
		return e, invalid("source %q", e.Source)
	}
	switch {
	case e.Visibility == "" || VisibilityOf(e.Type) == VisPerson:
		// A source may narrow an event to its owner, never widen correspondence to a team.
		if e.Visibility != VisPerson {
			e.Visibility = VisibilityOf(e.Type)
		}
	case e.Visibility != VisPerson && e.Visibility != VisTeam:
		return e, invalid("visibility %q", e.Visibility)
	}
	if e.Owner == "" {
		e.Visibility = VisTeam // the deployment's own events have no person to keep them for
	}
	if len(e.Owner) > idMax || len(e.Org) > idMax || len(e.Space) > idMax || len(e.Connection) > idMax || len(e.Machine) > idMax {
		return e, invalid("an id is too long")
	}
	e.Ext = strings.TrimSpace(e.Ext)
	if len(e.Ext) > extMax {
		return e, invalid("ext is longer than %d", extMax)
	}
	if e.Fingerprint == "" {
		if e.Ext == "" {
			return e, invalid("ext or fingerprint is required")
		}
		e.Fingerprint = Fingerprint(e.Source, e.Connection, e.Ext)
	}
	if len(e.Fingerprint) > fpMax {
		return e, invalid("fingerprint is longer than %d", fpMax)
	}
	if e.At.IsZero() || e.At.After(now.Add(24*time.Hour)) {
		e.At = now
	}
	e.At = e.At.UTC().Truncate(time.Millisecond)
	e.Actor = clip(e.Actor, actorMax)
	if e.URL != "" {
		u, err := url.Parse(e.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(e.URL) > urlMax {
			e.URL = ""
		}
	}
	if len(e.Thread) > threadMax || len(e.Ref) > refMax {
		return e, invalid("thread or ref is too long")
	}
	if len(e.Facts) > factsMax {
		return e, invalid("more than %d facts", factsMax)
	}
	facts := map[string]string{}
	for k, v := range e.Facts {
		if len(k) > factKey || !factRe.MatchString(k) {
			return e, invalid("fact key %q", k)
		}
		facts[k] = clip(v, factMax)
	}
	e.Facts = facts
	if len(e.Via) == 0 {
		return e, invalid("via is required")
	}
	for _, v := range e.Via {
		if !slices.Contains(vias, v) {
			return e, invalid("via %q", v)
		}
	}
	slices.Sort(e.Via)
	e.Via = slices.Compact(e.Via)
	return e, nil
}

// Fingerprint is the default fingerprint of an event: its source, its connection and the source's own id.
func Fingerprint(source, connection, ext string) string {
	h := sha256.Sum256([]byte(source + "\x00" + connection + "\x00" + ext))
	return hex.EncodeToString(h[:16])
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
