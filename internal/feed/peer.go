package feed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// What Claude Code types into a session when another session of it (a teammate) writes to it: a lead-in line, one
// or more <teammate-message> blocks, and a paragraph of its own about what a peer may ask. Only the blocks are the
// sender's; the rest is the harness talking to the agent and is not shown.
const peerLeadIn = "Another Claude session sent a message:"

var (
	teammateRE = regexp.MustCompile(`<teammate-message\b([^>]*)>([\s\S]*?)</teammate-message>`)
	// agentMessageRE: Claude 2.1.310 hands a teammate's message the lead takes mid-turn over as a queued prompt of
	// its own, «<agent-message from="scout">…</agent-message>», not as a teammate-message block.
	agentMessageRE = regexp.MustCompile(`^<agent-message\b([^>]*)>([\s\S]*?)</agent-message>\s*$`)
	// agentBlockRE: the same block in a delivery under the lead-in line, beside the harness's own paragraph.
	agentBlockRE = regexp.MustCompile(`<agent-message\b([^>]*)>([\s\S]*?)</agent-message>`)
	// subagentIDRE: the id of a background subagent, not the name of a teammate.
	subagentIDRE = regexp.MustCompile(`^[0-9a-f]{12,}$`)
	peerAttrRE   = regexp.MustCompile(`([A-Za-z_][\w-]*)="([^"]*)"`)
)

// peerTextFields are the fields of a JSON notice of an unknown type that hold its words, by preference.
var peerTextFields = []string{"result", "message", "text", "content", "reason", "summary", "planContent", "plan"}

// quietEvents are the notices of the team's protocol whose fields besides their words are ids and handles.
var quietEvents = map[string]bool{"shutdown_request": true, "shutdown_approved": true, "shutdown_rejected": true}

// textField is a notice's words by preference, "" without any.
func textField(fields map[string]any) string {
	for _, k := range peerTextFields {
		if s := strings.TrimSpace(stringField(fields, k)); s != "" {
			return s
		}
	}
	return ""
}

// peerMetaFields are the fields of a JSON notice the note carries elsewhere, not in its body.
var peerMetaFields = map[string]bool{"type": true, "from": true, "timestamp": true}

// peerNotes reads the messages of other sessions out of a user line; nil when the line is not such a delivery.
func peerNotes(text string) []wire.PeerNote {
	head := strings.TrimLeft(text, " \t\r\n")
	if m := agentMessageRE.FindStringSubmatch(head); m != nil {
		n, ok := agentNote(m[1], m[2])
		if !ok {
			return nil
		}
		return []wire.PeerNote{n}
	}
	if !strings.HasPrefix(head, peerLeadIn) && !strings.HasPrefix(head, "<teammate-message") {
		return nil
	}
	var notes []wire.PeerNote
	for _, m := range teammateRE.FindAllStringSubmatch(head, -1) {
		notes = append(notes, peerNote(peerAttrs(m[1]), strings.TrimSpace(m[2])))
	}
	if !strings.HasPrefix(head, peerLeadIn) {
		return notes
	}
	for _, m := range agentBlockRE.FindAllStringSubmatch(head, -1) {
		if n, ok := agentNote(m[1], m[2]); ok {
			notes = append(notes, n)
		}
	}
	return notes
}

// handBackLead opens what a subagent hands back: the harness's frame, then the report with every line indented.
const (
	handBackLead   = "[Subagent hand-back]"
	handBackReport = "The report follows:"
)

// agentNote is an «<agent-message from=…>» block: a teammate's words, or a background subagent's report under the
// harness's frame, which is left out with the report's indent.
func agentNote(rawAttrs, body string) (wire.PeerNote, bool) {
	attrs := peerAttrs(rawAttrs)
	if attrs["from"] == "" {
		return wire.PeerNote{}, false
	}
	attrs["teammate_id"] = attrs["from"]
	body = strings.TrimSpace(body)
	var agentID *string
	if subagentIDRE.MatchString(attrs["from"]) {
		agentID = optStr(attrs["from"])
	}
	if strings.HasPrefix(body, handBackLead) {
		_, report, _ := strings.Cut(body, handBackReport)
		lines := strings.Split(strings.Trim(report, "\r\n"), "\n")
		for i, l := range lines {
			lines[i] = strings.TrimPrefix(l, "  ")
		}
		n := wire.PeerNote{From: attrs["from"], Kind: wire.PeerNoteDone, AgentID: agentID, MD: strings.TrimSpace(strings.Join(lines, "\n"))}
		return n, true
	}
	n := peerNote(attrs, body)
	n.AgentID = agentID
	return n, true
}

func peerAttrs(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range peerAttrRE.FindAllStringSubmatch(raw, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func peerNote(attrs map[string]string, body string) wire.PeerNote {
	n := wire.PeerNote{From: attrs["teammate_id"], Kind: wire.PeerNoteMessage, Color: optStr(attrs["color"]),
		Summary: optStr(attrs["summary"]), MD: body}
	fields, ok := jsonNotice(body)
	if !ok {
		return n
	}
	typ, _ := fields["type"].(string)
	if from, _ := fields["from"].(string); from != "" {
		n.From = from
	}
	if ts, _ := fields["timestamp"].(string); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			ms := wire.Ms(t.UnixMilli())
			n.SentAt = &ms
		}
	}
	if typ == "idle_notification" {
		n.MD = strings.TrimSpace(stringField(fields, "result"))
		n.Kind = wire.PeerNoteIdle
		if n.MD != "" {
			n.Kind = wire.PeerNoteDone
		}
		return n
	}
	n.Kind, n.Event, n.MD = wire.PeerNoteEvent, &typ, noticeBody(fields)
	if quietEvents[typ] {
		// A notice of the team's protocol says it all by its type: its other fields are ids, not words.
		n.MD = textField(fields)
	}
	return n
}

// jsonNotice is the fields of a body that is a JSON notice: an object that names its type.
func jsonNotice(body string) (map[string]any, bool) {
	if !strings.HasPrefix(body, "{") {
		return nil, false
	}
	var fields map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(body)))
	if err := dec.Decode(&fields); err != nil || dec.More() {
		return nil, false
	}
	typ, _ := fields["type"].(string)
	return fields, typ != ""
}

func stringField(fields map[string]any, k string) string {
	s, _ := fields[k].(string)
	return s
}

// noticeBody is what a notice of a type the feed does not know says: its words, else its other plain fields as a
// list, so nothing it said is lost and no field is taken for markup.
func noticeBody(fields map[string]any) string {
	for _, k := range peerTextFields {
		if s := strings.TrimSpace(stringField(fields, k)); s != "" {
			return s
		}
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if !peerMetaFields[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		switch v := fields[k].(type) {
		case string, float64, bool:
			fmt.Fprintf(&b, "- %s: `%s`\n", strings.ReplaceAll(k, "`", "'"), strings.ReplaceAll(fmt.Sprint(v), "`", "'"))
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
