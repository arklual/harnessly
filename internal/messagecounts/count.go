// Package messagecounts counts the messages of a Claude Code or Codex transcript the way the previous runtime's
// timeline/message-counts route did: user, assistant and tool messages, and the tools by name.
package messagecounts

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Counts is the answer. Breakdown values are numbers, or strings where a tool is named after a property every
// JavaScript object inherits (the previous runtime added 1 to the inherited function).
type Counts struct {
	User, Assistant, Tool int
	Breakdown             *jsvalue.Object
}

// Value is the answer as the previous runtime wrote it.
func (c Counts) Value() *jsvalue.Object {
	b := c.Breakdown
	if b == nil {
		b = jsvalue.NewObject()
	}
	return jsvalue.NewObject("userCount", c.User, "assistantCount", c.Assistant, "toolCount", c.Tool, "toolBreakdown", b)
}

// Zero is the answer for a transcript that cannot be read.
func Zero() Counts { return Counts{Breakdown: jsvalue.NewObject()} }

type codexMessage struct {
	role, text, source, phase string
}

// counter is the running count over the lines read so far.
type counter struct {
	claudeUser, tool, codexUser, codexAssistant int
	ids                                         map[string]struct{}
	// uniqueIDs: ids that are objects or arrays, each one distinct in a JavaScript Set.
	uniqueIDs int
	breakdown *jsvalue.Object
	pending   *codexMessage
	// threw: a line was null; the previous runtime's count threw on it and the route answered zeros.
	threw bool
}

func newCounter() *counter {
	return &counter{ids: map[string]struct{}{}, breakdown: jsvalue.NewObject()}
}

func (c *counter) clone() *counter {
	n := *c
	n.ids = make(map[string]struct{}, len(c.ids))
	for k := range c.ids {
		n.ids[k] = struct{}{}
	}
	n.breakdown = c.breakdown.Copy()
	if c.pending != nil {
		p := *c.pending
		n.pending = &p
	}
	return &n
}

func (c *counter) counts() Counts {
	if c.threw {
		return Zero()
	}
	return Counts{User: c.claudeUser + c.codexUser, Assistant: len(c.ids) + c.uniqueIDs + c.codexAssistant, Tool: c.tool,
		Breakdown: c.breakdown.Copy()}
}

// lines feeds text split as readline splits it (\n, \r\n and a lone \r); blank lines count for nothing either way.
func (c *counter) lines(text []byte) {
	for len(text) > 0 && !c.threw {
		i := bytes.IndexAny(text, "\r\n")
		line := text
		if i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = nil
		}
		if len(line) > 0 {
			c.line(line)
		}
	}
}

type object = map[string]json.RawMessage

// asObject is the fields of an object in text already checked as JSON.
func asObject(raw json.RawMessage) (object, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	o := object{}
	i := skipWS(raw, 1)
	for i < len(raw) && raw[i] == '"' {
		end := skipString(raw, i)
		key := raw[i:end]
		i = skipWS(raw, skipWS(raw, end)+1)
		vEnd := skip(raw, i)
		o[str(key)] = raw[i:vEnd]
		i = skipWS(raw, vEnd)
		if i < len(raw) && raw[i] == ',' {
			i = skipWS(raw, i+1)
		}
	}
	return o, true
}

func asString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	return str(raw), true
}

func asArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 || raw[0] != '[' {
		return nil, false
	}
	var out []json.RawMessage
	for _, e := range elements(raw) {
		out = append(out, e)
	}
	return out, true
}

// value decodes a JSON value into the jsvalue forms (nil, bool, float64, string, []any, *Object).
func value(raw json.RawMessage) any {
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	return v
}

func isString(raw json.RawMessage, want string) bool {
	s, ok := asString(raw)
	return ok && s == want
}

func isTrue(raw json.RawMessage) bool { return string(raw) == "true" }

// nullish: absent or null, what ?? and ?. skip.
func nullish(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }

// truthy is JavaScript truthiness of a present JSON value.
func truthy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case 'n', 'f':
		return false
	case '"':
		return len(raw) > 2
	case '{', '[', 't':
		return true
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	return err != nil || f != 0
}

func (c *counter) line(line []byte) {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) == 0 {
		return
	}
	if !valid(line) {
		return
	}
	if trimmed[0] != '{' {
		if trimmed[0] == 'n' {
			c.threw = true // obj.isMeta of null
		}
		return // an array, a string, a number: no fields, nothing to count
	}
	o, _ := asObject(bytes.TrimRight(trimmed, " \t\r\n"))
	if isTrue(o["isMeta"]) || isTrue(o["isSidechain"]) {
		return
	}
	if m := c.readCodex(o); m != nil {
		if m.role == "user" {
			c.codexUser++
		} else {
			c.codexAssistant++
		}
	}
	switch {
	case isString(o["type"], "assistant"):
		id := o["requestId"]
		if nullish(id) {
			id = o["uuid"]
		}
		if truthy(id) {
			c.addID(id)
		}
		msg, _ := asObject(o["message"])
		content, _ := asArray(msg["content"])
		for _, item := range content {
			it, ok := asObject(item)
			if !ok || !isString(it["type"], "tool_use") {
				continue
			}
			c.tool++
			if name := it["name"]; truthy(name) {
				c.bump(propertyKey(name))
			}
		}
	case isString(o["type"], "user") && realUserMessage(o):
		c.claudeUser++
	case isString(o["type"], "response_item"):
		p, _ := asObject(o["payload"])
		t, _ := asString(p["type"])
		switch t {
		case "function_call", "custom_tool_call", "web_search_call":
			name := "web_search"
			if t != "web_search_call" {
				raw := p["name"]
				if !truthy(raw) {
					return
				}
				if s, ok := asString(raw); ok && s == "write_stdin" {
					return
				}
				name = propertyKey(raw)
			}
			c.tool++
			c.bump(name)
		case "local_shell_call":
			c.tool++
			c.bump("shell")
		}
	}
}

// addID adds an assistant message id to the set, compared as a JavaScript Set compares (objects only to themselves).
func (c *counter) addID(raw json.RawMessage) {
	switch raw[0] {
	case '{', '[':
		c.uniqueIDs++
	case '"':
		s, _ := asString(raw)
		c.ids["s"+s] = struct{}{}
	case 't':
		c.ids["true"] = struct{}{}
	default:
		f, _ := strconv.ParseFloat(string(raw), 64)
		c.ids["n"+jscompat.Number(f)] = struct{}{}
	}
}

// propertyKey is the property name a value becomes (String of it).
func propertyKey(raw json.RawMessage) string {
	if s, ok := asString(raw); ok {
		return s
	}
	return jscompat.String(value(raw))
}

// inherited are the properties every object has from Object.prototype, read where the breakdown has no own key yet.
var inherited = map[string]bool{"constructor": true, "__defineGetter__": true, "__defineSetter__": true, "hasOwnProperty": true,
	"__lookupGetter__": true, "__lookupSetter__": true, "isPrototypeOf": true, "propertyIsEnumerable": true, "toString": true,
	"valueOf": true, "toLocaleString": true}

// bump is toolBreakdown[name] = (toolBreakdown[name] ?? 0) + 1.
func (c *counter) bump(name string) {
	if name == "__proto__" {
		return // the setter ignores the string it is given
	}
	switch v, ok := c.breakdown.Get(name); {
	case !ok && inherited[name]:
		fn := name
		if name == "constructor" {
			fn = "Object"
		}
		c.breakdown.Set(name, "function "+fn+"() { [native code] }1")
	case !ok:
		c.breakdown.Set(name, 1.0)
	default:
		if f, isNum := v.(float64); isNum {
			c.breakdown.Set(name, f+1)
		} else {
			c.breakdown.Set(name, jscompat.String(v)+"1")
		}
	}
}

func realUserMessage(o object) bool {
	msg, _ := asObject(o["message"])
	content := msg["content"]
	if s, ok := asString(content); ok {
		return s != ""
	}
	items, ok := asArray(content)
	if !ok {
		return false
	}
	for _, it := range items {
		if len(it) == 0 || (it[0] != '{' && it[0] != '[') {
			continue
		}
		if it[0] == '[' {
			return true // an array is an object without a type
		}
		obj, _ := asObject(it)
		if !isString(obj["type"], "tool_result") {
			return true
		}
	}
	return false
}

var turnBoundaries = map[string]bool{"task_started": true, "task_complete": true, "TurnComplete": true, "turn_aborted": true, "TurnAborted": true}

// readCodex is the previous runtime's Codex message reader: a logical message, or nil for a mirror of the one before.
func (c *counter) readCodex(o object) *codexMessage {
	typ, _ := asString(o["type"])
	payloadRaw := o["payload"]
	payload, isObj := asObject(payloadRaw)
	if typ == "turn_context" {
		c.pending = nil
	} else if typ == "event_msg" {
		pt := "undefined"
		if isObj {
			if raw, ok := payload["type"]; ok {
				pt = jscompat.String(value(raw))
			}
		}
		if turnBoundaries[pt] {
			c.pending = nil
		}
	}
	m := readCodexMessage(typ, payloadRaw, payload, isObj)
	if m == nil {
		return nil
	}
	if p := c.pending; p != nil && p.role == m.role && p.source != m.source && p.text == m.text &&
		(p.phase == "" || m.phase == "" || p.phase == m.phase) {
		c.pending = nil
		return nil
	}
	c.pending = m
	return m
}

func userText(text string) bool {
	t := jscompat.Trim(text)
	return t != "" && !strings.HasPrefix(t, "# AGENTS.md instructions") &&
		!(strings.HasPrefix(t, "<") && (strings.Contains(t, "environment_context") || strings.Contains(t, "user_instructions")))
}

func readCodexMessage(typ string, payloadRaw json.RawMessage, payload object, isObj bool) *codexMessage {
	if !truthy(payloadRaw) || !isObj {
		return nil
	}
	pt, _ := asString(payload["type"])
	var role, text string
	switch {
	case typ == "event_msg" && (pt == "user_message" || pt == "agent_message"):
		role = "assistant"
		if pt == "user_message" {
			role = "user"
		}
		text, _ = asString(payload["message"])
		if role == "user" && !userText(text) {
			return nil
		}
	case typ == "response_item" && pt == "message" && (isString(payload["role"], "user") || isString(payload["role"], "assistant")):
		role, _ = asString(payload["role"])
		parts, ok := asArray(payload["content"])
		if !ok {
			return nil
		}
		want := "output_text"
		if role == "user" {
			want = "input_text"
		}
		var b strings.Builder
		for _, part := range parts {
			block, ok := asObject(part)
			if !ok || !isString(block["type"], want) {
				continue
			}
			t, ok := asString(block["text"])
			if !ok || role == "user" && !userText(t) {
				continue
			}
			b.WriteString(t)
		}
		text = b.String()
	default:
		return nil
	}
	if jscompat.Trim(text) == "" {
		return nil
	}
	phase, _ := asString(payload["phase"])
	return &codexMessage{role: role, text: text, source: typ, phase: phase}
}
