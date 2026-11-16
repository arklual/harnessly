package connectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is the one envelope of every connector's event. It says that something happened and where to read it, never
// what it says: no body, no excerpt, no text of a letter or a message. The agent reads the thing itself on the machine
// by Ref, with the connector's read tool, where it comes marked as data, not instructions.
type Event struct {
	// Type is "<domain>.<what>": mail.received, calendar.starting, chat.message, feed.item, webhook.received,
	// file.changed. A connector emits only the types its Spec.Events names.
	Type string `json:"type"`
	// ID is the source's own stable id of the thing (a Message-ID, a feed item's guid, a file's path and mtime). The
	// runtime makes it unique across connections (Fingerprint): the same ID is delivered once.
	ID string `json:"id"`
	// At is when it happened at the source; zero is the time it was seen.
	At time.Time `json:"at"`
	// Actor is who did it: "Ann <ann@example.com>", "@ann", a feed's author. Short.
	Actor string `json:"actor,omitempty"`
	// Title is the subject or the headline, at most TitleMax runes. Never the body.
	Title string `json:"title,omitempty"`
	// URL is where the person opens it (a web link); empty when there is none.
	URL string `json:"url,omitempty"`
	// Thread groups events of one conversation (a mail thread, a chat, a meeting): the inbox's item and the wait key
	// of the hub's rules. Empty: the event is a thing of its own.
	Thread string `json:"thread,omitempty"`
	// Ref is what the agent reads the whole thing by: the arguments of the connector's read tool, as
	// "<tool> key=value…" ("item id=https://example.com/p/1"). The runtime prefixes the command and the connection.
	Ref string `json:"ref,omitempty"`
	// Mine says the person did it themselves (their own letter, their own message).
	Mine bool `json:"mine,omitempty"`
	// Facts are short facts a rule can filter on: {"hasAttachment": "true", "list": "true", "startsAt": "…"}. At most
	// FactsMax, each value at most FactMax runes. Never text of the thing.
	Facts map[string]string `json:"facts,omitempty"`
}

const (
	TitleMax = 200
	FactsMax = 12
	FactMax  = 200
	actorMax = 120
	urlMax   = 2000
	idMax    = 400
)

// Envelope is an event as the runtime keeps and delivers it: the connector's event with where it came from.
type Envelope struct {
	Event
	// Fingerprint is unique per connection and thing: "<connector>:<connection>:<event id>".
	Fingerprint string `json:"fingerprint"`
	// Connector is the Spec.ID, Kind its kind, Connection the instance's id, Account its label.
	Connector  string `json:"connector"`
	Kind       Kind   `json:"kind"`
	Connection string `json:"connection"`
	Account    string `json:"account,omitempty"`
	// Read is the whole command the agent runs to read it: "harnessly connector read <connection> <ref>".
	Read string `json:"read,omitempty"`
}

// ReadCommand is the command an agent runs to read a thing by its ref.
func ReadCommand(connection, ref string) string {
	if ref == "" {
		return ""
	}
	return "harnessly connector read " + connection + " " + ref
}

// seal checks an event against the connector's Spec and makes its envelope: types it does not name, empty ids and
// overlong fields are refused or cut here, once, for every connector.
func seal(sp Spec, in Instance, account string, ev Event, now time.Time) (Envelope, error) {
	known := false
	for _, t := range sp.Events {
		known = known || t == ev.Type
	}
	if !known {
		return Envelope{}, fmt.Errorf("event type %q is not in the spec of %s", ev.Type, sp.ID)
	}
	if ev.ID == "" || len(ev.ID) > idMax {
		return Envelope{}, errors.New("an event needs an id of at most 400 bytes")
	}
	if ev.At.IsZero() {
		ev.At = now
	}
	ev.At = ev.At.UTC()
	ev.Title = clip(oneLine(ev.Title), TitleMax)
	ev.Actor = clip(oneLine(ev.Actor), actorMax)
	if len(ev.URL) > urlMax || (ev.URL != "" && !strings.HasPrefix(ev.URL, "https://") && !strings.HasPrefix(ev.URL, "http://")) {
		ev.URL = ""
	}
	ev.Thread = clip(oneLine(ev.Thread), idMax)
	ev.Ref = clip(oneLine(ev.Ref), idMax)
	if len(ev.Facts) > 0 {
		facts := map[string]string{}
		for _, k := range sortedKeys(ev.Facts) {
			if len(facts) == FactsMax {
				break
			}
			if !fieldRe.MatchString(k) {
				continue
			}
			facts[k] = clip(oneLine(ev.Facts[k]), FactMax)
		}
		ev.Facts = facts
	}
	return Envelope{
		Event:       ev,
		Fingerprint: sp.ID + ":" + in.ID + ":" + ev.ID,
		Connector:   sp.ID,
		Kind:        sp.Kind,
		Connection:  in.ID,
		Account:     account,
		Read:        ReadCommand(in.ID, ev.Ref),
	}, nil
}

// HubEvent is the envelope as the hub's rules take it (docs/hub/automations.md): source is the kind ("mail", "feed"),
// fingerprint the envelope's, and the event its facts without title or any text. A rule filters on type, connector,
// connection, account, actor, mine and facts.
func (e Envelope) HubEvent() (source, fingerprint string, event json.RawMessage) {
	m := map[string]any{
		"type":       e.Type,
		"connector":  e.Connector,
		"connection": e.Connection,
		"account":    e.Account,
		"id":         e.ID,
		"at":         e.At.Format(time.RFC3339),
	}
	if e.Actor != "" {
		m["actor"] = e.Actor
	}
	if e.Thread != "" {
		m["thread"] = e.Thread
	}
	if e.Read != "" {
		m["ref"] = e.Read
	}
	if e.Mine {
		m["mine"] = true
	}
	if len(e.Facts) > 0 {
		m["facts"] = maps.Clone(e.Facts)
	}
	raw, _ := json.Marshal(m)
	return string(e.Kind), e.Fingerprint, raw
}

// InboxSignal is the envelope as the inbox's POST /api/ingest takes a signal (harnessly-inbox store.Signal), without
// an excerpt: a connector sends no text of the thing. The inbox prefixes the keys with the source's kind itself.
type InboxSignal struct {
	Fingerprint string    `json:"fingerprint"`
	ItemKey     string    `json:"itemKey"`
	Kind        string    `json:"kind"`
	Actor       string    `json:"actor"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	At          time.Time `json:"at"`
	Mine        bool      `json:"mine"`
}

// Signal is the envelope as an inbox signal.
func (e Envelope) Signal() InboxSignal {
	item := e.Thread
	if item == "" {
		item = e.ID
	}
	return InboxSignal{
		Fingerprint: e.Fingerprint,
		ItemKey:     e.Connection + ":" + item,
		Kind:        e.Type,
		Actor:       e.Actor,
		Title:       e.Title,
		URL:         e.URL,
		At:          e.At,
		Mine:        e.Mine,
	}
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
