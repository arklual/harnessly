package share

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// A snapshot is a whitelist projection of a conversation's timeline: tool results, images, attachments, approval
// prompts and command output never get in; thinking and diffs only when asked for. Values are kept as JavaScript
// objects, so a snapshot is written to disk with the keys in the order the previous runtime wrote them.

const (
	entryChars         = 20_000
	snapshotBytes      = 5 * 1024 * 1024
	actionSummaryChars = 400
	noteChars          = 600
	truncatedMark      = "\n\n…[обрезано: запись длиннее 20 000 символов]"
)

// Options are what the owner chose to include.
type Options struct {
	Tools                      string // "summary" or "none"
	Thinking, Diffs, Economics bool
}

func (o Options) value() *jsvalue.Object {
	return jsvalue.NewObject("tools", o.Tools, "thinking", o.Thinking, "diffs", o.Diffs, "economics", o.Economics)
}

// ParseOptions is parseShareOptions: false for anything but {tools: summary|none, thinking, diffs, economics: bool}.
func ParseOptions(v any) (Options, bool) {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return Options{}, false
	}
	tools, _ := o.Get("tools")
	if tools != "summary" && tools != "none" {
		return Options{}, false
	}
	var flags [3]bool
	for i, k := range []string{"thinking", "diffs", "economics"} {
		b, ok := get(o, k).(bool)
		if !ok {
			return Options{}, false
		}
		flags[i] = b
	}
	return Options{Tools: tools.(string), Thinking: flags[0], Diffs: flags[1], Economics: flags[2]}, true
}

func get(o *jsvalue.Object, k string) any {
	v, ok := o.Get(k)
	if !ok {
		return jsvalue.Undefined
	}
	return v
}

// template is `${v}`.
func template(v any) string {
	if v == jsvalue.Undefined {
		return "undefined"
	}
	return jscompat.String(v)
}

var (
	droppedBlocks = regexp2.MustCompile(`<(system-reminder|cc-memory|local-command-stdout|local-command-stderr|local-command-caveat)(?![A-Za-z0-9_])[^>]*>[\s\S]*?</\1>`,
		regexp2.ECMAScript)
	wrapperTag = regexp.MustCompile(`</?(pasted_content|cross-session-message|command-[a-z-]+)\b[^>]*>`)
	spaceRun   = regexp.MustCompile(jscompat.SpaceClass + `+`)
)

func cleanUserText(text string) string {
	if strings.Contains(text, "<") {
		if out, err := droppedBlocks.Replace(text, "", -1, -1); err == nil {
			text = out
		}
		text = wrapperTag.ReplaceAllString(text, "")
	}
	return jscompat.Trim(text)
}

func oneLine(text string, max int) string {
	line := jscompat.Trim(spaceRun.ReplaceAllString(text, " "))
	if jscompat.Len(line) > max {
		return jscompat.Slice(line, max-1) + "…"
	}
	return line
}

func jsLength(v any) float64 {
	switch x := v.(type) {
	case []any:
		return float64(len(x))
	case string:
		return float64(jscompat.Len(x))
	}
	return 0
}

// Project turns timeline entries into snapshot entries. Pure; redaction and limits come after.
func Project(timeline []*jsvalue.Object, o Options) []*jsvalue.Object {
	var out []*jsvalue.Object
	withTools := o.Tools == "summary"
	action := func(at any, name any, summary string, status any, diff *jsvalue.Object) {
		if !withTools {
			return
		}
		e := jsvalue.NewObject("t", "action", "at", at, "name", name, "summary", oneLine(summary, actionSummaryChars), "status", status)
		if diff != nil {
			e.Set("diff", diff)
		}
		out = append(out, e)
	}
	note := func(at any, text string) {
		out = append(out, jsvalue.NewObject("t", "note", "at", at, "text", oneLine(text, noteChars)))
	}
	for _, e := range timeline {
		at := get(e, "timestamp")
		switch e.String("type") {
		case "user-message":
			if truthy(get(e, "pending")) || truthy(get(e, "attachmentPlaceholder")) {
				break
			}
			text := cleanUserText(e.String("text"))
			images := jsLength(get(e, "images"))
			if text != "" || images > 0 {
				out = append(out, jsvalue.NewObject("t", "user", "at", at, "text", text, "images", images))
			}
		case "assistant-message":
			if md := e.String("markdown"); jscompat.Trim(md) != "" {
				model := get(e, "model")
				if model == jsvalue.Undefined {
					model = nil
				}
				out = append(out, jsvalue.NewObject("t", "assistant", "at", at, "markdown", md, "model", model))
			}
		case "thinking":
			if t := e.String("thinking"); o.Thinking && jscompat.Trim(t) != "" {
				out = append(out, jsvalue.NewObject("t", "thinking", "at", at, "text", t))
			}
		case "reasoning-summary":
			if parts := e.Array("summary"); o.Thinking && len(parts) > 0 {
				ss := make([]string, len(parts))
				for i, p := range parts {
					if p != nil {
						ss[i] = jscompat.String(p)
					}
				}
				out = append(out, jsvalue.NewObject("t", "thinking", "at", at, "text", strings.Join(ss, "\n\n")))
			}
		case "tool-call":
			var diff *jsvalue.Object
			if d := e.Object("diff"); o.Diffs && d != nil {
				diff = jsvalue.NewObject("kind", "replace", "file", get(d, "filePath"), "old", get(d, "oldString"), "new", get(d, "newString"))
			}
			action(at, get(e, "toolName"), e.String("summary"), get(e, "status"), diff)
		case "agent-group":
			var parts []string
			for _, k := range []string{"agentType", "description"} {
				if v := get(e, k); truthy(v) {
					parts = append(parts, jscompat.String(v))
				}
			}
			action(at, "Agent", strings.Join(parts, ": "), "success", nil)
		case "task-notification":
			status := any("pending")
			switch get(e, "status") {
			case "completed":
				status = "success"
			case "failed":
				status = "error"
			}
			action(at, "Task", e.String("summary"), status, nil)
		case "exec-command-stream":
			action(at, "exec", "$ "+template(get(e, "command")), get(e, "status"), nil)
		case "web-search":
			action(at, "WebSearch", nullishString(get(e, "query")), get(e, "status"), nil)
		case "mcp-tool-call":
			action(at, template(get(e, "server"))+"."+template(get(e, "tool")), nullishString(get(e, "argumentsSummary")), get(e, "status"), nil)
		case "patch-apply":
			var diff *jsvalue.Object
			if d := get(e, "diff"); o.Diffs && truthy(d) {
				diff = jsvalue.NewObject("kind", "patch", "text", d)
			}
			var paths []string
			for _, f := range e.Array("files") {
				p := ""
				if fo, ok := f.(*jsvalue.Object); ok {
					if v, ok := fo.Get("path"); ok && v != nil {
						p = jscompat.String(v)
					}
				}
				paths = append(paths, p)
			}
			action(at, "apply_patch", strings.Join(paths, ", "), get(e, "status"), diff)
		case "plan":
			if md := e.String("markdown"); jscompat.Trim(md) != "" {
				out = append(out, jsvalue.NewObject("t", "plan", "at", at, "markdown", md))
			}
		case "ask-user-question":
			var qs []string
			for _, q := range e.Array("questions") {
				s := ""
				if qo, ok := q.(*jsvalue.Object); ok {
					if v, ok := qo.Get("question"); ok && v != nil {
						s = jscompat.String(v)
					}
				}
				qs = append(qs, s)
			}
			questions := strings.Join(qs, " · ")
			if answer := get(e, "answer"); truthy(answer) {
				note(at, "Вопрос агента: "+questions+" — ответ: "+template(answer))
			} else {
				note(at, "Вопрос агента: "+questions)
			}
		case "interrupt":
			note(at, "Ход прерван")
		case "context-compacted":
			note(at, "Контекст диалога сжат")
		case "error-notice":
			if get(e, "severity") == "error" {
				note(at, "Ошибка: "+template(get(e, "message")))
			}
		case "slash-command":
			if !truthy(get(e, "pending")) {
				args := ""
				if a := get(e, "args"); truthy(a) {
					args = " " + template(a)
				}
				note(at, "/"+template(get(e, "name"))+args)
			}
		}
	}
	return out
}

// nullishString is `v ?? ”` used as a string.
func nullishString(v any) string {
	if v == nil || v == jsvalue.Undefined {
		return ""
	}
	return template(v)
}

// mapEntryStrings is an entry with every string a viewer sees passed through f.
func mapEntryStrings(e *jsvalue.Object, f func(string) string) *jsvalue.Object {
	out := e.Copy()
	apply := func(o *jsvalue.Object, k string) {
		if v, ok := o.Get(k); ok {
			if s, isStr := v.(string); isStr {
				o.Set(k, f(s))
			}
		}
	}
	switch e.String("t") {
	case "user", "thinking", "note":
		apply(out, "text")
	case "assistant", "plan":
		apply(out, "markdown")
	case "action":
		// The diff's strings first, as the previous runtime mapped them (the redactor's counters keep that order).
		if d := e.Object("diff"); d != nil {
			nd := d.Copy()
			if d.String("kind") == "patch" {
				apply(nd, "text")
			} else {
				apply(nd, "file")
				apply(nd, "old")
				apply(nd, "new")
			}
			out.Set("diff", nd)
		}
		apply(out, "name")
		apply(out, "summary")
	}
	return out
}

// RedactSnapshot passes every string a viewer can see through the redactor: title, model, entries, diffs.
func RedactSnapshot(s *jsvalue.Object, r *Redactor) *jsvalue.Object {
	out := s.Copy()
	out.Set("title", r.Redact(s.String("title")))
	if m, ok := s.Get("model"); ok && m != nil {
		if ms, isStr := m.(string); isStr {
			out.Set("model", r.Redact(ms))
		}
	}
	entries := s.Array("entries")
	mapped := make([]any, len(entries))
	for i, e := range entries {
		if eo, ok := e.(*jsvalue.Object); ok {
			mapped[i] = mapEntryStrings(eo, r.Redact)
		} else {
			mapped[i] = e
		}
	}
	out.Set("entries", mapped)
	return out
}

// CaptureInput is what a snapshot is made of.
type CaptureInput struct {
	Timeline  []*jsvalue.Object
	Provider  string
	Title     string
	OwnerSub  any // string or nil
	Options   Options
	Economics *jsvalue.Object // nil for none
	Redactor  *Redactor
	Now       float64
}

// Capture is projection → redaction → the per-entry limit (after redaction, so a cut never splits a secret) → the 5 MB
// check. It returns the snapshot and its report, or false when the snapshot is too large.
func Capture(in CaptureInput) (snapshot, report *jsvalue.Object, ok bool) {
	projected := Project(in.Timeline, in.Options)
	var model any
	for i := len(projected) - 1; i >= 0; i-- {
		e := projected[i]
		if m := get(e, "model"); e.String("t") == "assistant" && truthy(m) {
			model = m
			break
		}
	}
	var first, last any
	if len(projected) > 0 {
		first, last = nullish(get(projected[0], "at")), nullish(get(projected[len(projected)-1], "at"))
	}
	var econ any
	if in.Options.Economics && in.Economics != nil {
		econ = in.Economics
	}
	entries := make([]any, len(projected))
	for i, e := range projected {
		entries[i] = e
	}
	draft := jsvalue.NewObject("schema", 1, "title", in.Title, "provider", in.Provider, "model", model,
		"owner", jsvalue.NewObject("sub", in.OwnerSub), "capturedAt", in.Now, "firstAt", first, "lastAt", last,
		"options", in.Options.value(), "economics", econ, "entries", entries)
	redacted := RedactSnapshot(draft, in.Redactor)
	truncated := false
	cut := func(v string) string {
		if jscompat.Len(v) <= entryChars {
			return v
		}
		truncated = true
		return jscompat.Slice(v, entryChars) + truncatedMark
	}
	limited := redacted.Array("entries")
	for i, e := range limited {
		if eo, ok := e.(*jsvalue.Object); ok {
			limited[i] = mapEntryStrings(eo, cut)
		}
	}
	redacted.Set("entries", limited)
	bytes := len(jsvalue.Stringify(redacted, ""))
	if bytes > snapshotBytes {
		return nil, nil, false
	}
	report = in.Redactor.Report()
	report.Set("entries", len(limited))
	report.Set("bytes", bytes)
	report.Set("truncated", truncated)
	return redacted, report, true
}

// nullish is `v ?? null`.
func nullish(v any) any {
	if v == jsvalue.Undefined {
		return nil
	}
	return v
}

func finite(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	return 0
}

// transcriptLine is one transcript line as the economics read it.
type transcriptLine struct {
	Timestamp   any             `json:"timestamp"`
	IsSidechain any             `json:"isSidechain"`
	Type        any             `json:"type"`
	Subtype     any             `json:"subtype"`
	DurationMs  any             `json:"durationMs"`
	Message     json.RawMessage `json:"message"`
	Payload     json.RawMessage `json:"payload"`
}

// TranscriptEconomics is tokens and time straight from the raw transcript. Claude writes one line per content block
// with the same message id and usage, so usage counts once per id; a Codex token_count is cumulative, the last wins.
// false where the previous runtime threw (a line that is JSON null).
func TranscriptEconomics(content, provider string) (*jsvalue.Object, bool) {
	var input, output, cacheRead, cacheWrite, activeMs, turns float64
	hasDurations := false
	var firstAt, lastAt *float64
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "null" {
			return nil, false
		}
		if !strings.HasPrefix(trimmed, "{") {
			continue // a number, string or array has no fields; invalid JSON is skipped
		}
		var item transcriptLine
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		if ts, ok := item.Timestamp.(string); ok {
			if at := jscompat.ParseDate(ts); !math.IsNaN(at) {
				if firstAt == nil {
					firstAt = &at
				}
				a := at
				lastAt = &a
			}
		}
		if provider == "claude" {
			if item.IsSidechain == true {
				continue
			}
			if item.Type == "assistant" && len(item.Message) > 0 {
				var msg struct {
					ID    any            `json:"id"`
					Usage map[string]any `json:"usage"`
				}
				var probe struct {
					Usage any `json:"usage"`
				}
				if json.Unmarshal(item.Message, &probe) == nil && truthy(probe.Usage) {
					_ = json.Unmarshal(item.Message, &msg)
					if key, ok := idKey(msg.ID); ok && (key == "" || !seen[key]) {
						seen[key] = true
						input += finite(msg.Usage["input_tokens"])
						output += finite(msg.Usage["output_tokens"])
						cacheRead += finite(msg.Usage["cache_read_input_tokens"])
						cacheWrite += finite(msg.Usage["cache_creation_input_tokens"])
					}
				}
			}
			if item.Type == "system" && item.Subtype == "turn_duration" {
				hasDurations = true
				turns++
				activeMs += finite(item.DurationMs)
			}
			continue
		}
		if item.Type != "event_msg" || len(item.Payload) == 0 {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(item.Payload, &payload) != nil {
			continue // not an object: no type
		}
		switch payload["type"] {
		case "token_count":
			if info, ok := payload["info"].(map[string]any); ok {
				if usage, ok := info["total_token_usage"]; ok && truthy(usage) {
					u, _ := usage.(map[string]any)
					cached := finite(u["cached_input_tokens"])
					input = math.Max(0, finite(u["input_tokens"])-cached)
					cacheRead = cached
					output = finite(u["output_tokens"])
				}
			}
		case "task_complete":
			turns++
			if d, ok := payload["duration_ms"].(float64); ok {
				hasDurations = true
				activeMs += d
			}
		}
	}
	tokens := jsvalue.NewObject("input", input, "output", output, "cacheRead", cacheRead, "cacheWrite", cacheWrite)
	var active any
	if hasDurations {
		active = activeMs
	}
	wall := 0.0
	if firstAt != nil && lastAt != nil {
		wall = *lastAt - *firstAt
	}
	return jsvalue.NewObject("tokens", tokens, "activeMs", active, "wallMs", wall, "turns", turns), true
}

// idKey is a message id as a Set key when it is truthy: strings and numbers apart.
func idKey(v any) (string, bool) {
	if !truthy(v) {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return "s" + x, true
	case float64:
		return "n" + jscompat.Number(x), true
	case bool:
		return "b", true
	}
	return "", true // an object or an array is its own key: never seen before
}

// truthy is JavaScript truthiness of a value, undefined included.
func truthy(v any) bool { return v != jsvalue.Undefined && jsvalue.Truthy(v) }
