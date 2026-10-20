package push

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/status"
)

const (
	titleMax   = 80
	detailMax  = 180
	tailBytes  = 512 << 10
	titleFirst = 256 << 10
	titleMaxRd = 8 << 20
)

// Message is what the page's service worker and its in-app toast render; the keys are in the order the page's writer
// put them.
type Message struct {
	Kind              string  `json:"kind"`
	Title             string  `json:"title"`
	Body              string  `json:"body"`
	ConversationTitle string  `json:"conversationTitle"`
	Detail            string  `json:"detail"`
	Provider          string  `json:"provider"`
	TabID             string  `json:"tabId"`
	WorkspaceID       string  `json:"workspaceId"`
	AgentSessionID    *string `json:"agentSessionId"`
	Tag               string  `json:"tag"`
	At                int64   `json:"at"`
	// URL is where a tap leads (a pushed design board); absent for the others.
	URL string `json:"url,omitempty"`
}

// Clip flattens whitespace and cuts text to max UTF-16 units with an ellipsis.
func Clip(text string, max int) string {
	flat := jscompat.Trim(spaces.ReplaceAllString(text, " "))
	if jscompat.Len(flat) <= max {
		return flat
	}
	return strings.TrimRightFunc(jscompat.Slice(flat, max-1), jscompat.IsSpace) + "…"
}

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

// entry is one item of a transcript's tail as the timeline shows it, as far as a notification needs.
type entry struct {
	kind     string // user, answer, ask, tool, approval
	text     string // the answer's markdown, the question, the tool call's summary
	toolID   string
	finished bool
}

// isHumanText: a prompt the person typed; a leading tag is a command echo or a wrapper (an annotation chip is not).
func isHumanText(s string) bool {
	if jscompat.Trim(s) == "" {
		return false
	}
	start := strings.TrimLeftFunc(s, jscompat.IsSpace)
	return !strings.HasPrefix(start, "<") || strings.HasPrefix(start, "<annotation ")
}

// claudeSkipped are tool calls the timeline shows as something else (a plan, task progress).
var claudeSkipped = map[string]bool{"ExitPlanMode": true, "TaskCreate": true, "TaskUpdate": true}

func readTailLines(path string, n int64) [][]byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	raw, err := agents.ReadTail(f, st.Size(), n)
	if err != nil {
		return nil
	}
	lines := agents.Lines(raw)
	if st.Size() > int64(len(raw)) && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines
}

func claudeEntries(path string) []entry {
	var out []entry
	tools := map[string]int{}
	for _, line := range readTailLines(path, tailBytes) {
		e := agents.DecodeObj(line)
		if e == nil || agents.Truthy(e.Val("isSidechain")) {
			continue
		}
		msg := agents.AsObj(e.Val("message"))
		switch e.Val("type") {
		case "user":
			if agents.Truthy(e.Val("isMeta")) || agents.Truthy(e.Val("isCompactSummary")) {
				continue
			}
			human := false
			switch c := msg.Val("content").(type) {
			case string:
				human = isHumanText(c)
			case []any:
				for _, b := range c {
					blk := agents.AsObj(b)
					switch blk.Val("type") {
					case "tool_result":
						if i, ok := tools[agents.AsStr(blk.Val("tool_use_id"))]; ok {
							out[i].finished = true
						}
					case "text":
						if isHumanText(agents.AsStr(blk.Val("text"))) {
							human = true
						}
					}
				}
			}
			if human {
				out = append(out, entry{kind: "user"})
			}
		case "assistant":
			content, _ := msg.Val("content").([]any)
			for _, b := range content {
				blk := agents.AsObj(b)
				switch blk.Val("type") {
				case "text":
					if t := agents.AsStr(blk.Val("text")); jscompat.Trim(t) != "" {
						out = append(out, entry{kind: "answer", text: t})
					}
				case "tool_use":
					name := agents.AsStr(blk.Val("name"))
					input := blk.Val("input")
					id := agents.AsStr(blk.Val("id"))
					if claudeSkipped[name] {
						continue
					}
					if qs, ok := agents.AsObj(input).Val("questions").([]any); ok && name == "AskUserQuestion" {
						text := ""
						if len(qs) > 0 {
							q := agents.AsObj(qs[0])
							text = agents.JSString(orEmpty(q.Val("question")))
							if text == "" {
								text = agents.JSString(orEmpty(q.Val("header")))
							}
						}
						tools[id] = len(out)
						out = append(out, entry{kind: "ask", text: text, toolID: id})
						continue
					}
					if input == nil {
						input = agents.NewObj()
					}
					tools[id] = len(out)
					out = append(out, entry{kind: "tool", text: claude.SummarizeToolCall(name, input), toolID: id})
				}
			}
		}
	}
	return out
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

var codexApprovals = map[string]bool{
	"exec_approval_request": true, "ExecApprovalRequest": true, "apply_patch_approval_request": true,
	"ApplyPatchApprovalRequest": true, "request_permissions": true, "RequestPermissions": true,
}

func codexEntries(path string) []entry {
	var out []entry
	calls := map[string]int{}
	for _, line := range readTailLines(path, tailBytes) {
		item := agents.DecodeObj(line)
		if item == nil {
			continue
		}
		payload := agents.AsObj(item.Val("payload"))
		ptype := agents.AsStr(payload.Val("type"))
		if item.Val("type") == "event_msg" && codexApprovals[ptype] {
			out = append(out, entry{kind: "approval"})
			continue
		}
		if item.Val("type") == "response_item" {
			callID := agents.AsStr(payload.Val("call_id"))
			switch ptype {
			case "function_call", "custom_tool_call":
				name := agents.AsStr(payload.Val("name"))
				if callID == "" || name == "" {
					continue
				}
				args := payload.Val("arguments")
				if ptype == "custom_tool_call" {
					args = payload.Val("input")
				}
				calls[callID] = len(out)
				out = append(out, entry{kind: "tool", text: codex.SummarizeFunctionCall(name, args), toolID: callID})
				continue
			case "function_call_output", "custom_tool_call_output":
				if i, ok := calls[callID]; ok {
					out[i].finished = true
				}
				continue
			}
		}
		if m := codex.ReadMessage(item); m != nil && m.Source == "event_msg" {
			if m.Role == "user" {
				out = append(out, entry{kind: "user"})
			} else {
				out = append(out, entry{kind: "answer", text: m.Text})
			}
		}
	}
	return out
}

func questionFrom(entries []entry) (string, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		switch e := entries[i]; e.kind {
		case "ask":
			return e.text, e.text != ""
		case "approval":
			return "", false
		case "tool":
			if !e.finished {
				return "Разрешить: " + e.text, e.text != ""
			}
		case "user":
			return "", false
		}
	}
	return "", false
}

func answerFrom(entries []entry) (string, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		switch e := entries[i]; e.kind {
		case "answer":
			if t := StripMarkdown(e.text); t != "" {
				return t, true
			}
		case "user":
			return "", false
		}
	}
	return "", false
}

var aiTitle = regexp.MustCompile(`"type":"ai-title","aiTitle":"((?:[^"\\]|\\.)*)"`)

// sessionTitle is the last title Claude gave the conversation, looked for in growing windows from the end.
func sessionTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	for n := int64(titleFirst); ; n *= 4 {
		raw, err := agents.ReadTail(f, st.Size(), min(n, titleMaxRd))
		if err != nil {
			return ""
		}
		if ms := aiTitle.FindAllSubmatch(raw, -1); len(ms) > 0 {
			var title string
			if json.Unmarshal([]byte(`"`+string(ms[len(ms)-1][1])+`"`), &title) == nil {
				return title
			}
		}
		if n >= titleMaxRd || n >= st.Size() {
			return ""
		}
	}
}

// permissionText is what a Codex permission request asks, as the notification says it.
func permissionText(raw []byte) (string, bool) {
	var r struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Patches []struct {
			Path string `json:"path"`
		} `json:"patches"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &r) != nil {
		return "", false
	}
	switch r.Type {
	case "ExecApprovalRequest":
		return "Разрешить команду: " + r.Command, true
	case "ApplyPatchApprovalRequest":
		names := make([]string, len(r.Patches))
		for i, p := range r.Patches {
			names[i] = p.Path[strings.LastIndexByte(p.Path, '/')+1:]
		}
		return "Разрешить правку: " + strings.Join(names, ", "), true
	}
	return "Просит разрешение", true
}

// BuildMessage writes the notification of a finished turn ("done") or a question ("question"): the conversation's
// title, what the person has to do, and the words of the answer or the question from the transcript's tail.
func BuildMessage(tabID string, e status.Entry, kind, tagPrefix string, now time.Time) Message {
	codexTab := e.AgentProviderID.Value() == agents.Codex
	var entries []entry
	if e.JSONLPath != "" {
		if codexTab {
			entries = codexEntries(e.JSONLPath)
		} else {
			entries = claudeEntries(e.JSONLPath)
		}
	}
	title := ""
	if e.JSONLPath != "" && !codexTab {
		title = sessionTitle(e.JSONLPath)
	}
	for _, alt := range []string{e.AgentSummary.Value(), e.LastUserMessage.Value(), e.TabName, "Разговор"} {
		if title != "" {
			break
		}
		title = alt
	}
	title = Clip(title, titleMax)

	var question string
	asked := true
	if kind == "question" {
		var ok bool
		if e.PermissionRequest.Has() {
			question, ok = permissionText(*e.PermissionRequest.Val)
		}
		if !ok {
			question, ok = questionFrom(entries)
		}
		asked = !ok || strings.HasPrefix(question, "Разрешить")
		if !ok {
			question = ""
		}
	}
	failed := e.LastEvent.Has() && e.LastEvent.Val.Name == "stop-failure"
	var detail string
	switch {
	case kind == "question" && question != "":
		detail = question
	case kind == "question":
		detail = "Агент остановился и ждёт твоего решения"
	default:
		if a, ok := answerFrom(entries); ok {
			detail = a
		} else if e.LastAssistantMessage.Has() {
			detail = e.LastAssistantMessage.Value()
		} else if failed {
			detail = "Ход закончился ошибкой"
		} else {
			detail = "Агент закончил работу"
		}
	}
	body := detail
	if kind == "question" {
		if !asked {
			body = "Вопрос: " + detail
		}
	} else if failed {
		body = "Ошибка — " + detail
	} else {
		body = "Готово — " + detail
	}
	provider := agents.Claude
	if codexTab {
		provider = agents.Codex
	}
	var sid *string
	if e.AgentSessionID.Has() {
		v := e.AgentSessionID.Value()
		sid = &v
	}
	return Message{
		Kind:              kind,
		Title:             title,
		Body:              Clip(body, detailMax),
		ConversationTitle: title,
		Detail:            Clip(detail, detailMax),
		Provider:          provider,
		TabID:             tabID,
		WorkspaceID:       e.WorkspaceID,
		AgentSessionID:    sid,
		Tag:               tagPrefix + tabID,
		At:                now.UnixMilli(),
	}
}
