package prompts

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Probe is what Claude Code's transcript says about a moment: the tools called and not answered, and where a message
// typed during the turn stands.
type Probe struct {
	// Running are the names of the tools called and not answered yet, in call order.
	Running []string
	// Waiting: the message was queued (queue-operation enqueue) and neither injected (remove) nor started a turn
	// (dequeue).
	Waiting bool
	// Delivered: the message is in the transcript as the person's prompt, or as a `queued_command` attachment (injected
	// into the running turn, no `user` record), written at or after since. Claude Code 2.1.280 records no enqueue for
	// text typed during a turn and shows no queue; this is how its arrival is seen.
	Delivered bool
}

type transcriptEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Operation string          `json:"operation"`
	Content   json.RawMessage `json:"content"`
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment *struct {
		Type        string          `json:"type"`
		CommandMode string          `json:"commandMode"`
		Prompt      json.RawMessage `json:"prompt"`
		Timestamp   string          `json:"timestamp"`
	} `json:"attachment"`
}

type contentBlock struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ToolUseID string  `json:"tool_use_id"`
	Text      *string `json:"text"`
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func (e *transcriptEntry) blocks() []contentBlock {
	if e.Message == nil || len(e.Message.Content) == 0 || e.Message.Content[0] != '[' {
		return nil
	}
	var raw []json.RawMessage
	if json.Unmarshal(e.Message.Content, &raw) != nil {
		return nil
	}
	out := make([]contentBlock, 0, len(raw))
	for _, r := range raw {
		var b contentBlock
		_ = json.Unmarshal(r, &b)
		out = append(out, b)
	}
	return out
}

// promptText is a user record's prompt: its string content, or its text blocks joined by a space.
func (e *transcriptEntry) promptText() (string, bool) {
	if e.Message == nil {
		return "", false
	}
	if s, ok := jsonString(e.Message.Content); ok {
		return s, true
	}
	var texts []string
	for _, b := range e.blocks() {
		if b.Type == "text" && b.Text != nil {
			texts = append(texts, *b.Text)
		}
	}
	return strings.Join(texts, " "), len(texts) > 0
}

// ProbeTranscript reads JSONL records in file order for text (compared with whitespace collapsed); sinceMs is when the
// platform typed it, 0 for any time. False when the newest record is not whole JSON (being written, or bigger than the
// tail read): nothing can be said then. A broken record elsewhere is skipped.
func ProbeTranscript(lines []string, text string, sinceMs float64) (Probe, bool) {
	var records []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			records = append(records, l)
		}
	}
	type call struct{ id, name string }
	var running []call
	var queue []string
	wanted := collapse(text)
	delivered := false

	for i, rec := range records {
		if !json.Valid([]byte(rec)) {
			if i == len(records)-1 {
				return Probe{}, false
			}
			continue
		}
		// Whole JSON of another shape (an array, a field of another type) is read as far as it fits.
		var e transcriptEntry
		_ = json.Unmarshal([]byte(rec), &e)
		switch e.Type {
		case "queue-operation":
			content, isString := jsonString(e.Content)
			switch {
			case e.Operation == "enqueue" && isString:
				queue = append(queue, collapse(content))
			case e.Operation == "dequeue":
				if len(queue) > 0 {
					queue = queue[1:]
				}
			case e.Operation == "remove" && isString:
				if at := slices.Index(queue, collapse(content)); at >= 0 {
					queue = slices.Delete(queue, at, at+1)
				}
			}
		case "attachment":
			// A message that reached the running turn: Claude stamps it with the moment Enter was pressed, a little
			// before the platform's clock read it as typed.
			if a := e.Attachment; a != nil && a.Type == "queued_command" && a.CommandMode == "prompt" && wanted != "" {
				stamp := a.Timestamp
				if stamp == "" {
					stamp = e.Timestamp
				}
				prompt, ok := promptOf(a.Prompt)
				at := math.NaN()
				if stamp != "" {
					at = jscompat.ParseDate(stamp)
				}
				if ok && strings.Contains(collapse(prompt), wanted) && (math.IsNaN(at) || at >= sinceMs-recordSlackMs) {
					delivered = true
				}
			}
		case "assistant":
			for _, b := range e.blocks() {
				if b.Type == "tool_use" && b.ID != "" {
					if at := slices.IndexFunc(running, func(c call) bool { return c.id == b.ID }); at >= 0 {
						running[at].name = b.Name
					} else {
						running = append(running, call{b.ID, b.Name})
					}
				}
			}
		case "user":
			answers := 0
			for _, b := range e.blocks() {
				if b.Type != "tool_result" {
					continue
				}
				answers++
				if b.ToolUseID != "" {
					running = slices.DeleteFunc(running, func(c call) bool { return c.id == b.ToolUseID })
				}
			}
			// A prompt or a notice after a call that never got its answer (the session was cut): that call is not running.
			if answers == 0 {
				running = nil
				prompt, ok := e.promptText()
				at := math.NaN()
				if e.Timestamp != "" {
					at = jscompat.ParseDate(e.Timestamp)
				}
				if ok && wanted != "" && strings.Contains(collapse(prompt), wanted) && (math.IsNaN(at) || at >= sinceMs) {
					delivered = true
				}
			}
		}
	}
	names := make([]string, len(running))
	for i, c := range running {
		names[i] = c.name
	}
	return Probe{Running: names, Waiting: wanted != "" && slices.Contains(queue, wanted), Delivered: delivered}, true
}

const transcriptTailBytes = 1 << 20

// ReadTranscriptTail is the last megabyte of a transcript as lines, the first (possibly cut) one dropped when the file is
// longer; false when it cannot be read.
func ReadTranscriptTail(path string) ([]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false
	}
	size := st.Size()
	length := min(size, transcriptTailBytes)
	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, size-length); err != nil && err != io.EOF {
		return nil, false
	}
	lines := strings.Split(string(buf), "\n")
	if size > length {
		lines = lines[1:]
	}
	return lines, true
}
