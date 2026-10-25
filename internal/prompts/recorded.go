package prompts

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// A message of the queue can already be with the agent while it is still listed: it was steered or sent now,
// typed into the agent's own prompt box, or the runtime restarted before the removal reached the disk. When the agent
// stopped, the queue typed it again and the same message stood twice in the conversation.
//
// Claude Code's transcript is the witness. A message reaches it as one of: a `user` prompt (a turn started with it), an
// `attachment` of type `queued_command` (it was injected into the running turn, with a `queue-operation remove` and no
// `user` record), or a `queue-operation enqueue` nothing has taken yet (it waits in Claude's own queue). Each record
// belongs to one queued message, at or after the moment the message was queued, matched by text the way the timeline
// matches its pending rows (paste wrappers, picture tokens and spacing do not count).

// recordSlackMs: Claude stamps an injected message with the moment the person pressed Enter, a little before the
// platform's own clock reads the queue entry as made.
const recordSlackMs = 500

var (
	pasteTagRE = regexp.MustCompile(`</?pasted_content\b[^>]*>`)
	imageTokRE = regexp.MustCompile(`\[Image #\d+\]`)
)

// matchKey is what two texts of one message have in common: the paste wrapper, picture tokens and spacing do not count.
func matchKey(text string) string {
	return collapse(imageTokRE.ReplaceAllString(pasteTagRE.ReplaceAllString(text, " "), " "))
}

type recordedMsg struct {
	key string
	at  float64
}

// stampMs is a record's time in ms; +Inf when it has none, so a record without a time never counts as earlier.
func stampMs(s string) float64 {
	if s == "" {
		return math.Inf(1)
	}
	if at := jscompat.ParseDate(s); !math.IsNaN(at) {
		return at
	}
	return math.Inf(1)
}

// promptOf is the text of a prompt that is a string or text blocks.
func promptOf(raw json.RawMessage) (string, bool) {
	if s, ok := jsonString(raw); ok {
		return s, true
	}
	if len(raw) == 0 || raw[0] != '[' {
		return "", false
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return "", false
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != nil {
			texts = append(texts, *b.Text)
		}
	}
	return strings.Join(texts, " "), len(texts) > 0
}

// recordedMessages is what the transcript holds of the person's messages: the delivered ones, and those still waiting
// in Claude's own queue, oldest first.
func recordedMessages(lines []string) []recordedMsg {
	var delivered, waiting []recordedMsg
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e transcriptEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Type {
		case "queue-operation":
			content, isString := jsonString(e.Content)
			switch {
			case e.Operation == "enqueue" && isString:
				waiting = append(waiting, recordedMsg{matchKey(content), stampMs(e.Timestamp)})
			case e.Operation == "dequeue":
				if len(waiting) > 0 {
					waiting = waiting[1:]
				}
			case e.Operation == "remove" && isString:
				if at := slices.IndexFunc(waiting, func(r recordedMsg) bool { return r.key == matchKey(content) }); at >= 0 {
					waiting = slices.Delete(waiting, at, at+1)
				}
			}
		case "user":
			if e.Message == nil || hasToolResult(e.blocks()) {
				continue
			}
			if text, ok := e.promptText(); ok {
				delivered = append(delivered, recordedMsg{matchKey(text), stampMs(e.Timestamp)})
			}
		case "attachment":
			if e.Attachment == nil || e.Attachment.Type != "queued_command" || e.Attachment.CommandMode != "prompt" {
				continue
			}
			if text, ok := promptOf(e.Attachment.Prompt); ok {
				stamp := e.Attachment.Timestamp
				if stamp == "" {
					stamp = e.Timestamp
				}
				delivered = append(delivered, recordedMsg{matchKey(text), stampMs(stamp)})
			}
		}
	}
	all := slices.DeleteFunc(append(delivered, waiting...), func(r recordedMsg) bool { return r.key == "" })
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })
	return all
}

func hasToolResult(blocks []contentBlock) bool {
	return slices.ContainsFunc(blocks, func(b contentBlock) bool { return b.Type == "tool_result" })
}

// recordedItemIDs says which of the queue's messages the transcript already holds. One record answers for one message,
// the earliest it can belong to, so two identical messages need two records. A message of pictures only has no text to
// look for.
func recordedItemIDs(items []Item, lines []string) []string {
	type candidate struct {
		id, key string
		at      float64
	}
	cands := make([]candidate, 0, len(items))
	for _, it := range items {
		cands = append(cands, candidate{it.ID, matchKey(it.Text), float64(it.CreatedAt)})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].at < cands[j].at })
	var matched []string
	for _, r := range recordedMessages(lines) {
		i := slices.IndexFunc(cands, func(c candidate) bool { return c.key == r.key && c.key != "" && r.at >= c.at-recordSlackMs })
		if i < 0 {
			continue
		}
		matched = append(matched, cands[i].id)
		cands = slices.Delete(cands, i, i+1)
	}
	return matched
}

// transcriptOf reads the tail of the tab's transcript; false when the tab has none or it cannot be read.
func (s *Service) transcriptOf(tabID string) ([]string, bool) {
	tab, ok := s.cfg.Tabs.Tab(tabID)
	if !ok || tab.TranscriptPath == "" {
		return nil, false
	}
	return s.cfg.ReadTranscript(tab.TranscriptPath)
}

// isRecorded says whether the transcript already holds this one message.
func (s *Service) isRecorded(tabID string, item Item) bool {
	lines, ok := s.transcriptOf(tabID)
	return ok && len(recordedItemIDs([]Item{item}, lines)) > 0
}

// dropRecorded takes the messages the agent's transcript already holds out of the tab's queue; they are not typed again.
// Returns how many left.
func (s *Service) dropRecorded(tabID string) int {
	items := s.queue.List(tabID)
	if len(items) == 0 {
		return 0
	}
	lines, ok := s.transcriptOf(tabID)
	if !ok {
		return 0
	}
	dropped := 0
	for _, id := range recordedItemIDs(items, lines) {
		if _, found := s.queue.Take(tabID, id); found {
			dropped++
		}
	}
	if dropped > 0 {
		s.info("queued prompts already with the agent left the queue", "tabId", tabID, "count", dropped)
	}
	return dropped
}
