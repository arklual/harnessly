// Package peers is how agents of different conversations — on this machine or on another machine of the fleet — write
// to each other: who may write here (the allow-list), how a message from another agent reaches a conversation (typed
// into its agent as a <peer-message> envelope, never as the person's own words), the list of chats an agent may write
// to, and the audit of what went in and out.
//
// The sender's identity is never taken from the message: on this machine it is the runtime itself; from another machine
// it is a token the fleet's gateway signed with this runtime's own secret (the machine auth the gateway already uses
// for page sessions), see identity.go.
package peers

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// Tag is the envelope's element: what the agent reads, and what the feed recognizes as another agent's message.
const Tag = "peer-message"

// Envelope is a message from another agent as it is typed into the receiving agent.
type Envelope struct {
	ID string
	// Person is the sender's owner as people call them (Иван), Machine the sender's machine (dev-department), Chat the
	// sender's chat title.
	Person  string
	Machine string
	Chat    string
	// Ref is where an answer goes: "<machine>/<conversation id>" (see ValidRef). The envelope carries no ready-made
	// command: the agent builds its answer from the ref itself.
	Ref string
	// Via is who wrote it: "agent", or "person" for a reply the owner typed in the card.
	Via  string
	Text string
}

var (
	// envelopeRE is one envelope; a text is envelopes and nothing else when only blank space is left between them. A
	// body cannot hold the closing tag (Wrap neutralizes it), so the shortest match is the whole envelope.
	envelopeRE = regexp.MustCompile(`(?s)<peer-message\b([^>]*)>\n?(.*?)\n?</peer-message>`)
	attrRE     = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
	// pastedRE is a paste as Claude Code records it; a long message typed by the outbox comes back wrapped in one.
	pastedRE = regexp.MustCompile(`(?s)^\s*<pasted_content id="([^"]+)">\n?(.*?)\n?</pasted_content id="([^"]+)">\s*$`)
	// tagRE is the envelope's tag in the text, opening or closing, however spaced or cased: `<peer-message`,
	// `</peer-message>`, `< / Peer-Message`.
	tagRE = regexp.MustCompile(`(?i)<\s*(/?)\s*peer-message`)
	refRE = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

// maxAttr is the longest attribute value; the sender's chat title is the one that can be long.
const maxAttr = 200

// ValidRef: a ref is "<machine>/<conversation id>" of letters, digits, dot, underscore and dash, and neither half is
// a dot path.
func ValidRef(ref string) bool {
	if !refRE.MatchString(ref) {
		return false
	}
	machine, conv, _ := strings.Cut(ref, "/")
	return strings.Trim(machine, ".") != "" && strings.Trim(conv, ".") != ""
}

// attr is an attribute value that can neither close the attribute nor the tag: control and format characters (line
// breaks, escapes, zero-width and bidi marks) become a space or are dropped, and & " ' < > are escaped.
func attr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return html.EscapeString(clip(strings.TrimSpace(b.String()), maxAttr))
}

// body is the message text that cannot pass for the envelope: its own opening or closing tag gets a zero-width space
// inside the name (the feed shows it back as typed), and control characters other than line breaks and tabs, which
// would reach the agent's terminal, are dropped.
func body(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	return tagRE.ReplaceAllString(s, "<${1}peer-message\u200b")
}

// Wrap is the text typed into the receiving agent. The attributes are escaped and the ref must be a ref, else it is left
// out; an opening or closing tag inside the text is neutralized, so a message cannot end its own envelope, nor start
// a second one with attributes of its own, and pass the rest off as the person's words.
func Wrap(e Envelope) string {
	var b strings.Builder
	b.WriteString("<" + Tag)
	via := "agent"
	if e.Via == "person" {
		via = "person"
	}
	ref := ""
	if ValidRef(e.Ref) {
		ref = e.Ref
	}
	for _, kv := range [][2]string{{"id", e.ID}, {"person", e.Person}, {"machine", e.Machine}, {"chat", e.Chat},
		{"ref", ref}, {"via", via}} {
		if v := attr(kv[1]); v != "" {
			b.WriteString(" " + kv[0] + `="` + v + `"`)
		}
	}
	b.WriteString(">\n")
	b.WriteString(body(e.Text))
	b.WriteString("\n</" + Tag + ">")
	return b.String()
}

// Parse reads an envelope back from a user entry's text (unwrapping the paste Claude Code may have recorded it as).
// Several agents' messages that went in as one (ParseAll) read as the first of them with the words of all.
func Parse(text string) (Envelope, bool) {
	all := ParseAll(text)
	if len(all) == 0 {
		return Envelope{}, false
	}
	e := all[0]
	for _, next := range all[1:] {
		e.Text += "\n\n" + next.Text
	}
	return e, true
}

// ParseAll reads the envelopes a user entry's text is made of: one, or the several that waited in the line and were
// typed as one message, in their order. A text with anything but envelopes in it is not another agent's message: nil.
func ParseAll(text string) []Envelope {
	if m := pastedRE.FindStringSubmatch(text); m != nil && m[1] == m[3] {
		text = m[2]
	}
	found := envelopeRE.FindAllStringSubmatchIndex(text, -1)
	if len(found) == 0 {
		return nil
	}
	at := 0
	out := make([]Envelope, 0, len(found))
	for _, m := range found {
		if strings.TrimSpace(text[at:m[0]]) != "" {
			return nil
		}
		at = m[1]
		out = append(out, envelopeOf(text[m[2]:m[3]], text[m[4]:m[5]]))
	}
	if strings.TrimSpace(text[at:]) != "" {
		return nil
	}
	return out
}

func envelopeOf(attrs, body string) Envelope {
	e := Envelope{Text: body}
	for _, a := range attrRE.FindAllStringSubmatch(attrs, -1) {
		v := html.UnescapeString(a[2])
		switch a[1] {
		case "id":
			e.ID = v
		case "person":
			e.Person = v
		case "machine":
			e.Machine = v
		case "chat":
			e.Chat = v
		case "ref":
			if ValidRef(v) {
				e.Ref = v
			}
		case "via":
			e.Via = v
		}
	}
	return e
}

// plainTagRE is an envelope's tag, whole or cut by a snippet's edge, for Plain to drop from a text it cannot parse.
var plainTagRE = regexp.MustCompile(`(?i)<\s*/?\s*peer-message\b[^>]*>?`)

// Plain is how a message of another agent reads outside the chat itself — the ⌘K preview, a search hit, a tab's
// snippet, a notification: «От «<chat>»: <text>», one line per sender, without the envelope. A text that is not
// another agent's message comes back as it is, save for a stray envelope tag (a snippet cut out of one).
func Plain(text string) string {
	all := ParseAll(text)
	if len(all) == 0 {
		return strings.TrimSpace(plainTagRE.ReplaceAllString(text, ""))
	}
	parts := make([]string, 0, len(all))
	for _, e := range all {
		parts = append(parts, "От «"+plainSender(e)+"»: "+strings.TrimSpace(e.Text))
	}
	return strings.Join(parts, "\n\n")
}

// plainSender is what a reader knows the sender by: the chat's title, else its owner, else the plain word.
func plainSender(e Envelope) string {
	switch {
	case e.Chat != "":
		return e.Chat
	case e.Person != "":
		return e.Person
	}
	return "другого чата"
}
