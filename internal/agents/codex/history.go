package codex

import (
	"math"
	"os"
	"regexp"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

// Message is a prompt or an answer read from a rollout line.
type Message struct {
	Role string // "user" or "assistant"
	Text string
	// Source is the record type it came from (event_msg or response_item), Phase Codex's phase tag.
	Source string
	Phase  string
}

// isUserText: text Codex injects as a user message (AGENTS.md, environment and instruction blocks) is not a prompt.
func isUserText(text string) bool {
	t := jscompat.Trim(text)
	return t != "" && !strings.HasPrefix(t, "# AGENTS.md instructions") &&
		!(strings.HasPrefix(t, "<") && (strings.Contains(t, "environment_context") || strings.Contains(t, "user_instructions")))
}

// ReadMessage reads a prompt or answer from a rollout record (event_msg user_message/agent_message or a response_item
// message); nil for anything else or an empty text.
func ReadMessage(record *agents.Obj) *Message {
	payload := agents.AsObj(record.Val("payload"))
	if payload == nil {
		return nil
	}
	var m Message
	rtype, ptype := record.Val("type"), payload.Val("type")
	switch {
	case rtype == "event_msg" && (ptype == "user_message" || ptype == "agent_message"):
		m.Role = "assistant"
		if ptype == "user_message" {
			m.Role = "user"
		}
		m.Text = agents.AsStr(payload.Val("message"))
		if m.Role == "user" && !isUserText(m.Text) {
			return nil
		}
	case rtype == "response_item" && ptype == "message" && (payload.Val("role") == "user" || payload.Val("role") == "assistant"):
		m.Role = payload.Val("role").(string)
		content, ok := payload.Val("content").([]any)
		if !ok {
			return nil
		}
		want := "output_text"
		if m.Role == "user" {
			want = "input_text"
		}
		var b strings.Builder
		for _, part := range content {
			blk := agents.AsObj(part)
			text, isStr := blk.Str("text")
			if blk == nil || blk.Val("type") != want || !isStr {
				continue
			}
			if m.Role == "user" && !isUserText(text) {
				continue
			}
			b.WriteString(text)
		}
		m.Text = b.String()
	default:
		return nil
	}
	if jscompat.Trim(m.Text) == "" {
		return nil
	}
	m.Source = rtype.(string)
	m.Phase = agents.AsStr(payload.Val("phase"))
	return &m
}

func parseDuration(v any) *float64 {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return &x
	case *agents.Obj:
		finite := func(k string) float64 {
			n, ok := x.Val(k).(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
				return 0
			}
			return n
		}
		ms := finite("secs")*1000 + finite("nanos")/1_000_000
		return &ms
	case []any:
		ms := 0.0
		return &ms
	}
	return nil
}

var patchFileHeader = regexp.MustCompile(`(?i)^\*\*\*` + jscompat.SpaceClass + `+(?:Add|Update|Delete)` + jscompat.SpaceClass + `+File:` + jscompat.SpaceClass + `+(.+?)` + jscompat.SpaceClass + `*$`)

// ReadHistoryStats reads a finished turn from the rollout's last 128 KiB, newest line first: prompt and answer, their
// times, the turn's duration, the tools used and the files a patch or an edit touched.
func ReadHistoryStats(path string) agents.HistoryStats {
	out := agents.EmptyHistoryStats()
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	raw, err := agents.ReadTail(f, st.Size(), tailSize)
	if err != nil {
		return out
	}
	lines := agents.Lines(raw)
	touched := map[string]bool{}
	touch := func(p string) {
		if p != "" && !touched[p] {
			touched[p] = true
			out.TouchedFiles = append(out.TouchedFiles, p)
		}
	}
	var turnEnd *float64
	for i := len(lines) - 1; i >= 0; i-- {
		item := agents.DecodeObj(lines[i])
		if item == nil {
			continue
		}
		payload := agents.AsObj(item.Val("payload"))
		var ts *float64
		if t := item.Val("timestamp"); agents.Truthy(t) {
			if ms := jscompat.ParseDate(agents.JSString(t)); !math.IsNaN(ms) && !math.IsInf(ms, 0) {
				ts = &ms
			}
		}
		if msg := ReadMessage(item); msg != nil {
			text := msg.Text
			if msg.Role == "user" {
				if ts != nil {
					out.FirstUserTs = ts
				}
				if out.LastUserText == nil {
					out.LastUserText = &text
				}
			} else {
				if out.LastAssistantTs == nil {
					out.LastAssistantTs = ts
				}
				if out.LastAssistantText == nil {
					out.LastAssistantText = &text
				}
			}
			continue
		}
		switch item.Val("type") {
		case "event_msg":
			switch agents.AsStr(payload.Val("type")) {
			case "task_complete", "TurnComplete":
				if ts != nil && *ts != 0 && turnEnd == nil {
					turnEnd = ts
				}
			case "exec_command_begin", "ExecCommandBegin":
				out.ToolUsage.Add("Bash")
			case "exec_command_end", "ExecCommandEnd":
				if out.TurnDurationMs == nil || *out.TurnDurationMs == 0 {
					out.TurnDurationMs = parseDuration(payload.Val("duration"))
				}
			}
		case "response_item":
			rt := agents.AsStr(payload.Val("type"))
			if rt != "function_call" && rt != "custom_tool_call" {
				continue
			}
			name := agents.AsStr(payload.Val("name"))
			if name == "" || name == "write_stdin" {
				continue
			}
			out.ToolUsage.Add(name)
			switch name {
			case "apply_patch":
				for _, line := range strings.Split(agents.AsStr(payload.Val("input")), "\n") {
					if m := patchFileHeader.FindStringSubmatch(line); m != nil {
						touch(m[1])
					}
				}
			case "Edit", "Write", "edit", "write":
				args := payload.Val("arguments")
				if args == nil {
					args = payload.Val("input")
				}
				if s, ok := args.(string); ok {
					v, err := agents.Decode([]byte(s))
					if err != nil {
						continue
					}
					args = v
				}
				obj := agents.AsObj(args)
				fp := obj.Val("file_path")
				if fp == nil {
					fp = obj.Val("path")
				}
				touch(agents.AsStr(fp))
			}
		}
	}
	if out.LastAssistantTs == nil {
		out.LastAssistantTs = turnEnd
	}
	return out
}
