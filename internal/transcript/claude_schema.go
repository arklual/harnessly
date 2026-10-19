package transcript

import (
	"math"

	"harnessly/internal/jsvalue"
)

// The previous runtime's zod (v4) schemas of a transcript line: which lines pass and what the parsed copy holds. A
// parsed object has the schema's keys first, in the schema's order, then (for a loose object) the others in the order
// they came; strict objects drop the others; "__proto__" never survives the copy.

type claudeBase struct {
	typ         string
	uuid        any // string or undefined
	parentUUID  any // string, null or undefined
	timestamp   string
	cwd         string
	isSidechain bool
}

func zObject(v any) (*jsvalue.Object, bool) {
	o, ok := v.(*jsvalue.Object)
	return o, ok && o != nil
}

// zOptional says whether an optional key holds what check accepts (absent is fine).
func zOptional(o *jsvalue.Object, k string, check func(any) bool) bool {
	v, ok := o.Get(k)
	return !ok || check(v)
}

func zString(v any) bool { _, ok := v.(string); return ok }
func zBool(v any) bool   { _, ok := v.(bool); return ok }

func zNumber(v any) bool {
	f, ok := v.(float64)
	return ok && !math.IsInf(f, 0) && !math.IsNaN(f)
}

func zBaseEntry(raw any) (*claudeBase, bool) {
	o, ok := zObject(raw)
	if !ok {
		return nil, false
	}
	nullableString := func(v any) bool { return v == nil || zString(v) }
	external := func(v any) bool { return isStr(v, "external") }
	if !zOptional(o, "uuid", zString) || !zOptional(o, "parentUuid", nullableString) ||
		!zOptional(o, "timestamp", zString) || !zOptional(o, "sessionId", zString) || !zOptional(o, "cwd", zString) ||
		!zOptional(o, "isSidechain", zBool) || !zString(get(o, "type")) || !zOptional(o, "userType", external) ||
		!zOptional(o, "version", zString) || !zOptional(o, "gitBranch", zString) {
		return nil, false
	}
	b := &claudeBase{typ: o.String("type"), uuid: get(o, "uuid"), parentUUID: get(o, "parentUuid")}
	b.timestamp = o.String("timestamp")
	b.cwd = o.String("cwd")
	b.isSidechain, _ = get(o, "isSidechain").(bool)
	return b, true
}

// zPick copies the schema's keys present in o, in the schema's order; loose also copies the others after them.
func zPick(o *jsvalue.Object, loose bool, schemaKeys ...string) *jsvalue.Object {
	out := jsvalue.NewObject()
	known := map[string]bool{}
	for _, k := range schemaKeys {
		known[k] = true
		if v, ok := o.Get(k); ok && k != "__proto__" {
			out.Set(k, v)
		}
	}
	if loose {
		for _, k := range o.Keys() {
			if !known[k] && k != "__proto__" {
				v, _ := o.Get(k)
				out.Set(k, v)
			}
		}
	}
	return out
}

func zText(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !isStr(get(o, "type"), "text") || !zString(get(o, "text")) {
		return nil, false
	}
	return zPick(o, false, "type", "text"), true
}

func zToolUse(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !isStr(get(o, "type"), "tool_use") || !zString(get(o, "id")) || !zString(get(o, "name")) {
		return nil, false
	}
	var input *jsvalue.Object
	if raw, present := o.Get("input"); present {
		in, ok := zObject(raw)
		if !ok {
			return nil, false
		}
		input = zPick(in, true)
	}
	out := zPick(o, false, "type", "id", "name")
	if input != nil {
		out.Set("input", input)
	}
	return out, true
}

func zThinking(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !isStr(get(o, "type"), "thinking") || !zOptional(o, "thinking", zString) {
		return nil, false
	}
	return zPick(o, false, "type", "thinking"), true
}

func zToolResult(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !isStr(get(o, "type"), "tool_result") || !zString(get(o, "tool_use_id")) || !zOptional(o, "is_error", zBool) {
		return nil, false
	}
	return zPick(o, false, "type", "tool_use_id", "content", "is_error"), true
}

// zTyped is z.object({ type: z.string() }).passthrough().
func zTyped(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !zString(get(o, "type")) {
		return nil, false
	}
	return zPick(o, true, "type"), true
}

func zAssistantItem(v any) (*jsvalue.Object, bool) {
	for _, f := range []func(any) (*jsvalue.Object, bool){zText, zToolUse, zThinking, zTyped} {
		if out, ok := f(v); ok {
			return out, true
		}
	}
	return nil, false
}

func zUserItem(v any) (*jsvalue.Object, bool) {
	for _, f := range []func(any) (*jsvalue.Object, bool){zText, zToolResult, zTyped} {
		if out, ok := f(v); ok {
			return out, true
		}
	}
	return nil, false
}

func zUsage(v any) (*jsvalue.Object, bool) {
	o, ok := zObject(v)
	if !ok || !zNumber(get(o, "input_tokens")) || !zNumber(get(o, "output_tokens")) ||
		!zOptional(o, "cache_creation_input_tokens", zNumber) || !zOptional(o, "cache_read_input_tokens", zNumber) {
		return nil, false
	}
	var cache *jsvalue.Object
	if raw, present := o.Get("cache_creation"); present {
		c, ok := zObject(raw)
		if !ok || !zOptional(c, "ephemeral_5m_input_tokens", zNumber) || !zOptional(c, "ephemeral_1h_input_tokens", zNumber) {
			return nil, false
		}
		cache = zPick(c, true, "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens")
	}
	out := zPick(o, true, "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "cache_creation")
	if cache != nil {
		out.Set("cache_creation", cache)
	}
	return out, true
}

type claudeAssistantMessage struct {
	model      any
	content    []*jsvalue.Object
	stopReason any
	usage      any
}

// zAssistantEntry is AssistantEntrySchema for a line that already passed the base schema with type "assistant".
func zAssistantEntry(raw any) (*claudeAssistantMessage, bool) {
	msg, ok := zObject(get(raw, "message"))
	if !ok || !zOptional(msg, "role", func(v any) bool { return isStr(v, "assistant") }) || !zOptional(msg, "model", zString) {
		return nil, false
	}
	items, ok := get(msg, "content").([]any)
	if !ok {
		return nil, false
	}
	out := &claudeAssistantMessage{model: get(msg, "model"), stopReason: get(msg, "stop_reason"), usage: undef}
	for _, it := range items {
		parsed, ok := zAssistantItem(it)
		if !ok {
			return nil, false
		}
		out.content = append(out.content, parsed)
	}
	if !zOptional(msg, "stop_reason", func(v any) bool { return v == nil || zString(v) }) {
		return nil, false
	}
	if raw, present := msg.Get("usage"); present {
		u, ok := zUsage(raw)
		if !ok {
			return nil, false
		}
		out.usage = u
	}
	return out, true
}

// zUserEntry is UserEntrySchema's message content for a line that passed the base schema with type "user": a string or
// the parsed items.
func zUserEntry(raw any) (any, bool) {
	msg, ok := zObject(get(raw, "message"))
	if !ok || !zOptional(msg, "role", func(v any) bool { return isStr(v, "user") }) {
		return nil, false
	}
	switch c := get(msg, "content").(type) {
	case string:
		return c, true
	case []any:
		items := make([]any, 0, len(c))
		for _, it := range c {
			parsed, ok := zUserItem(it)
			if !ok {
				return nil, false
			}
			items = append(items, parsed)
		}
		return items, true
	}
	return nil, false
}
