package delegation

import (
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the tools answer is text for an agent: short, the facts first, the chat's own words last.

const (
	// ReadDefault and ReadMax bound how many entries a read gives.
	ReadDefault = 40
	ReadMax     = 200
	// AnswerMax is the longest answer a wait or a wake-up carries; read_conversation gives the rest.
	AnswerMax = 6000
	// textMax and toolOutputMax bound one message and one failed tool's output in a read.
	textMax       = 4000
	toolOutputMax = 600
)

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + fmt.Sprintf("… [%d more characters]", len(rs)-max)
}

// Head is the chat's line: its id, title, agent and state.
func Head(c wire.Conversation) string {
	model := "default model"
	if c.Model != nil {
		model = c.Model.ID
		if c.Model.Effort != nil {
			model += " · " + string(*c.Model.Effort)
		}
	}
	return fmt.Sprintf("%s «%s» · %s %s · %s", c.ID, c.Title, c.Provider, model, stateWords(c))
}

func stateWords(c wire.Conversation) string {
	s := string(c.State.K)
	if c.Queue.Count > 0 {
		s += fmt.Sprintf(", %d queued", c.Queue.Count)
	}
	if c.Queue.Paused != nil {
		s += ", queue paused"
	}
	return s
}

func person(text string) string {
	if e, ok := peers.Parse(text); ok {
		from := e.Chat
		if from == "" {
			from = e.Person
		}
		return "[agent of «" + from + "»] " + clip(e.Text, textMax)
	}
	return "[person] " + clip(text, textMax)
}

// transcript is the entries of a snapshot as lines, the last limit of them, with the cursor to read on from.
func transcript(c wire.Conversation, snap wire.ConvSnapshot, limit int) string {
	if limit <= 0 {
		limit = ReadDefault
	}
	limit = min(limit, ReadMax)
	entries := snap.Entries
	skipped := 0
	if len(entries) > limit {
		skipped = len(entries) - limit
		entries = entries[skipped:]
	}
	var b strings.Builder
	b.WriteString(Head(c) + "\n")
	if skipped > 0 {
		fmt.Fprintf(&b, "(%d earlier entries left out)\n", skipped)
	}
	for _, e := range entries {
		if line := entryLine(e); line != "" {
			b.WriteString("\n" + line + "\n")
		}
	}
	queued := map[string]bool{}
	for _, id := range snap.Queue.Items {
		queued[id] = true
	}
	for _, m := range snap.Messages {
		if queued[m.ClientID] {
			b.WriteString("\n[queued] " + clip(person(m.Text), 300) + "\n")
		}
	}
	if len(snap.Entries) > 0 {
		fmt.Fprintf(&b, "\nnext: %s (pass it as since to read only what comes after)\n", snap.Entries[len(snap.Entries)-1].ID)
	}
	return b.String()
}

func entryLine(e wire.Entry) string {
	switch e.T {
	case wire.EntryUser:
		return person(e.Text)
	case wire.EntrySay:
		return "[agent] " + clip(e.MD, textMax)
	case wire.EntryTool:
		line := "[tool " + e.Tool + "] " + clip(e.Summary, 200)
		switch {
		case e.OK == nil:
			line += " (running)"
		case !*e.OK:
			line += " (failed)"
			if e.Output != nil && e.Output.Text != "" {
				line += "\n  " + strings.ReplaceAll(clip(e.Output.Text, toolOutputMax), "\n", "\n  ")
			}
		}
		return line
	case wire.EntrySubagent:
		return "[subagent " + e.AgentType + "] " + clip(e.Description, 200) + " (" + string(e.Status) + ")"
	case wire.EntryAnswers:
		parts := make([]string, len(e.Pairs))
		for i, p := range e.Pairs {
			parts[i] = p.Q + " → " + p.A
		}
		return "[person answered] " + clip(strings.Join(parts, "; "), textMax)
	case wire.EntryPeer:
		return person(e.Text)
	case wire.EntryCompaction:
		return "[context compacted]"
	case wire.EntryInterrupt:
		return "[interrupted]"
	case wire.EntryNotice:
		if e.Detail != nil {
			return "[notice] " + clip(*e.Detail, 300)
		}
		return "[notice] " + e.Code
	}
	return ""
}

// lastAnswer is the agent's last answer in a snapshot: the answer entry of its last turn that has one, else its last
// words.
func lastAnswer(snap wire.ConvSnapshot) string {
	byID := make(map[string]wire.Entry, len(snap.Entries))
	for _, e := range snap.Entries {
		byID[e.ID] = e
	}
	for i := len(snap.Turns) - 1; i >= 0; i-- {
		if a := snap.Turns[i].AnswerEntryID; a != nil {
			if e, ok := byID[*a]; ok && e.MD != "" {
				return clip(e.MD, AnswerMax)
			}
		}
	}
	for i := len(snap.Entries) - 1; i >= 0; i-- {
		if e := snap.Entries[i]; e.T == wire.EntrySay && e.MD != "" {
			return clip(e.MD, AnswerMax)
		}
	}
	return ""
}

// ResultText is a wait's answer.
func ResultText(r Result) string {
	var b strings.Builder
	b.WriteString(Head(r.Conv) + "\n")
	switch r.Outcome {
	case Done:
		b.WriteString("outcome: done — the agent finished and waits for the next message\n")
	case NeedsPerson:
		b.WriteString("outcome: needs-person — the agent waits for the person (a question, a permission or a paused queue); tell the person, or wait again\n")
	case Ended:
		b.WriteString("outcome: ended — the agent is not running; send_to_conversation starts it again\n")
	case Timeout:
		b.WriteString("outcome: timeout — the agent still works; wait again, or read_conversation to see how far it got\n")
	}
	if r.Answer != "" {
		b.WriteString("\nlast answer:\n" + r.Answer + "\n")
	}
	return b.String()
}

// wakeText is the message a finished chat wakes its parent with.
func wakeText(r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The chat you delegated, %s «%s», ", r.Conv.ID, r.Conv.Title)
	if r.Outcome == Done {
		b.WriteString("finished its turn.")
	} else {
		b.WriteString("stopped running (" + string(r.Conv.State.K) + ").")
	}
	if r.Answer != "" {
		b.WriteString("\n\nIts last answer:\n" + r.Answer)
	}
	b.WriteString("\n\nread_conversation gives the whole chat; send_to_conversation gives it more work.")
	return b.String()
}
