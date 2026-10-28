package handoff

import (
	"encoding/json"
	"io"
	"strings"
)

// canonicalHistory writes provider-neutral semantic JSONL one source record at a time. It deliberately omits raw
// transcript envelopes: those identify the CLI, account/session, model and machine even when their obvious fields are
// removed. One decoded record is the memory bound; a whole transcript is never retained.
func canonicalHistory(dst io.Writer, src io.Reader) (int, int64, error) {
	dec := json.NewDecoder(src)
	dec.UseNumber()
	w := &countWriter{w: dst}
	c := historyCanonicalizer{out: w}
	for {
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				return c.records, w.n, nil
			}
			return 0, w.n, err
		}
		if err := c.take(raw); err != nil {
			return 0, w.n, err
		}
	}
}

type countWriter struct {
	w io.Writer
	n int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

type historyCanonicalizer struct {
	out     io.Writer
	records int
	last    struct{ role, text, source string }
}

func (c *historyCanonicalizer) take(raw map[string]any) error {
	typ := textField(raw, "type")
	if typ == "user" || typ == "assistant" { // Claude transcript message.
		message, _ := raw["message"].(map[string]any)
		role := textField(message, "role")
		if role != "user" && role != "assistant" {
			role = typ
		}
		return c.message(role, message["content"], "claude")
	}
	// System records describe the source CLI/runtime rather than the person's
	// conversation, so they are intentionally not part of portable history.
	if typ == "system" {
		return nil
	}
	payload, _ := raw["payload"].(map[string]any)
	ptype := textField(payload, "type")
	switch typ {
	case "event_msg":
		switch ptype {
		case "user_message":
			text := textField(payload, "message")
			if privateCodexContext(text) {
				return nil
			}
			return c.message("user", text, "event")
		case "agent_message":
			return c.message("assistant", textField(payload, "message"), "event")
		case "exec_command_begin":
			return c.tool("shell", payload["command"], textField(payload, "call_id"))
		case "exec_command_end":
			return c.toolResult(payload["aggregated_output"], textField(payload, "call_id"), false)
		}
	case "response_item":
		switch ptype {
		case "message":
			role := textField(payload, "role")
			if role == "user" || role == "assistant" {
				return c.message(role, payload["content"], "response")
			}
		case "function_call", "custom_tool_call":
			input := payload["arguments"]
			if input == nil {
				input = payload["input"]
			}
			return c.tool(textField(payload, "name"), decodedJSON(input), textField(payload, "call_id"))
		case "function_call_output", "custom_tool_call_output":
			return c.toolResult(decodedJSON(payload["output"]), textField(payload, "call_id"), false)
		case "web_search_call":
			return c.tool("web_search", payload["query"], textField(payload, "call_id"))
		}
	}
	return nil
}

func (c *historyCanonicalizer) message(role string, content any, source string) error {
	if role != "user" && role != "assistant" {
		return nil
	}
	if text, ok := content.(string); ok {
		return c.textMessage(role, text, source)
	}
	parts, ok := content.([]any)
	if !ok {
		return nil
	}
	var text strings.Builder
	flush := func() error {
		if text.Len() == 0 {
			return nil
		}
		err := c.textMessage(role, text.String(), source)
		text.Reset()
		return err
	}
	for _, part := range parts {
		p, _ := part.(map[string]any)
		switch textField(p, "type") {
		case "text", "input_text", "output_text":
			text.WriteString(textField(p, "text"))
		case "tool_use":
			if err := flush(); err != nil {
				return err
			}
			if err := c.tool(textField(p, "name"), p["input"], textField(p, "id")); err != nil {
				return err
			}
		case "tool_result":
			if err := flush(); err != nil {
				return err
			}
			isError, _ := p["is_error"].(bool)
			if err := c.toolResult(p["content"], textField(p, "tool_use_id"), isError); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (c *historyCanonicalizer) textMessage(role, text, source string) error {
	if strings.TrimSpace(text) == "" || role == "user" && privateCodexContext(text) {
		return nil
	}
	// Codex commonly writes the same message in event_msg and response_item. Keep the first semantic occurrence only.
	if c.last.role == role && c.last.text == text && c.last.source != source {
		c.last = struct{ role, text, source string }{}
		return nil
	}
	c.last.role, c.last.text, c.last.source = role, text, source
	return c.write(map[string]any{"kind": "message", "role": role, "content": text})
}

func (c *historyCanonicalizer) tool(name string, input any, callID string) error {
	record := map[string]any{"kind": "tool", "name": name, "input": sanitizeHistory(input)}
	if callID != "" {
		record["callId"] = callID
	}
	return c.write(record)
}

func (c *historyCanonicalizer) toolResult(output any, callID string, isError bool) error {
	record := map[string]any{"kind": "tool_result", "output": sanitizeHistory(output)}
	if callID != "" {
		record["callId"] = callID
	}
	if isError {
		record["error"] = true
	}
	return c.write(record)
}

func (c *historyCanonicalizer) write(record map[string]any) error {
	raw, err := json.Marshal(sanitizeHistory(record))
	if err != nil {
		return err
	}
	if _, err := c.out.Write(append(raw, '\n')); err != nil {
		return err
	}
	c.records++
	return nil
}

func textField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func decodedJSON(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	var decoded any
	if json.Unmarshal([]byte(s), &decoded) == nil {
		return decoded
	}
	return s
}

func privateCodexContext(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "# AGENTS.md instructions") ||
		strings.Contains(trimmed, "<environment_context>") || strings.Contains(trimmed, "<user_instructions>")
}

var privateHistoryKeys = map[string]bool{
	"account": true, "accountid": true, "cli": true, "cliversion": true, "cwd": true, "entrypoint": true,
	"host": true, "hostname": true, "machine": true, "machineid": true, "model": true, "modelprovider": true,
	"organization": true, "organizationid": true, "orgid": true, "provider": true, "session": true,
	"sessionid": true, "usertype": true, "version": true,
}

func sanitizeHistory(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
			if privateHistoryKeys[normalized] {
				delete(v, key)
				continue
			}
			v[key] = sanitizeHistory(child)
		}
		return v
	case []any:
		for i := range v {
			v[i] = sanitizeHistory(v[i])
		}
		return v
	default:
		return value
	}
}
