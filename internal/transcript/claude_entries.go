package transcript

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// One transcript line into timeline entries (parseSingleEntry and its helpers in the previous runtime).

const (
	claudeInterruptPrefix  = "[Request interrupted by user"
	claudeDeliveryNotice   = "[Cross-session delivery notice]"
	claudeShellCommand     = "!"
	claudeBashNoOutput     = "(Bash completed with no output)"
	claudeSkillBodyPrefix  = "Base directory for this skill:"
	claudeCommandOutputMax = 20_000
	claudeCommandLookback  = 5
)

// claudeNow is Date.now(); tests fix it.
var claudeNow = func() float64 { return Now() }

// js turns a JavaScript regexp's \s into what JavaScript means by it.
func js(re string) *regexp.Regexp {
	re = strings.ReplaceAll(re, `[\s\S]`, `(?s:.)`)
	return regexp.MustCompile(strings.ReplaceAll(re, `\s`, jscompat.SpaceClass))
}

var (
	peerMessageRE  = regexp.MustCompile(`<cross-session-message\b[\s\S]*?</cross-session-message>|<agent-message\b[\s\S]*?</agent-message>`)
	peerStartRE    = js(`^\s*<(?:cross-session-message|agent-message)\b`)
	hopChainRE     = js(`^(\s*<cross-session-message\b[^>]*?)\s+hop-chain="[^"]*"`)
	protocolTagRE  = regexp.MustCompile(`<(?:` + claudeProtocolTags + `)[^>]*>[\s\S]*?</(?:` + claudeProtocolTags + `)>`)
	commandNameRE  = js(`<command-name>\s*([^\n\r\x{2028}\x{2029}]*?)\s*</command-name>`)
	bashInputRE    = js(`^\s*<bash-input>([\s\S]*?)</bash-input>\s*$`)
	commandArgsRE  = regexp.MustCompile(`<command-args>([\s\S]*?)</command-args>`)
	commandRunRE   = regexp.MustCompile(`<command-message>[^<]*is running…</command-message>`)
	bashOpenRE     = regexp.MustCompile(`<bash-(stdout|stderr)>`)
	localOpenRE    = regexp.MustCompile(`<local-command-(stdout|stderr)>`)
	taskNotifRE    = regexp.MustCompile(`<task-notification>([\s\S]*?)</task-notification>`)
	ansiRE         = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	crlfRE         = regexp.MustCompile(`\r\n?`)
	leadingLFRE    = regexp.MustCompile(`^\n+`)
	pasteTagRE     = regexp.MustCompile(`</?pasted_content\b[^>]*>`)
	imageTokenRE   = regexp.MustCompile(`\[Image #\d+\]`)
	spacesRE       = js(`\s+`)
	taskNumberRE   = regexp.MustCompile(`^Task #(\d+)`)
	linesSummaryRE = regexp.MustCompile(`^(\d+) lines$`)
	tagREs         = map[string]*regexp.Regexp{}
)

const claudeProtocolTags = "local-command-caveat|local-command-stdout|local-command-stderr|command-name|command-message|" +
	"command-args|user-prompt-submit-hook|system-reminder"

func init() {
	for _, tag := range []string{"task-id", "event", "status", "summary", "result", "total_tokens", "tool_uses", "duration_ms"} {
		tagREs[tag] = regexp.MustCompile(`<` + tag + `>([\s\S]*?)</` + tag + `>`)
	}
}

func isPeerMessage(text string) bool { return peerStartRE.MatchString(text) }

func dropHopChain(text string) string { return hopChainRE.ReplaceAllString(text, "${1}") }

func stripProtocolTags(text any) string {
	s, ok := text.(string)
	if !ok {
		claudeFail("text.replace is not a function")
	}
	return jscompat.Trim(protocolTagRE.ReplaceAllString(s, ""))
}

func cleanCommandOutput(text string) string {
	plain := ansiRE.ReplaceAllString(text, "")
	plain = crlfRE.ReplaceAllString(plain, "\n")
	plain = trimEnd(leadingLFRE.ReplaceAllString(plain, ""))
	if jscompat.Len(plain) > claudeCommandOutputMax {
		return sliceJS(plain, claudeCommandOutputMax-1) + "…"
	}
	return plain
}

// pendingMatchKey is what two texts of one message have in common (timeline-pending).
func pendingMatchKey(text string) string {
	s := pasteTagRE.ReplaceAllString(text, " ")
	s = imageTokenRE.ReplaceAllString(s, " ")
	return jscompat.Trim(spacesRE.ReplaceAllString(s, " "))
}

func isHarnessNotice(text string) bool {
	head := trimStart(text)
	return strings.HasPrefix(head, "[Cross-session idle notice]") || strings.HasPrefix(head, claudeDeliveryNotice) ||
		strings.HasPrefix(head, "Another Claude session sent a message:")
}

type taggedMatch struct{ kind, body string }

// matchTagged is matchAll of /<prefix(stdout|stderr)>([\s\S]*?)<\/prefix\1>/g, which Go's regexp cannot spell.
func matchTagged(text string, open *regexp.Regexp, prefix string) []taggedMatch {
	var out []taggedMatch
	for at := 0; at < len(text); {
		loc := open.FindStringSubmatchIndex(text[at:])
		if loc == nil {
			break
		}
		start, end := at+loc[0], at+loc[1]
		kind := text[at+loc[2] : at+loc[3]]
		closing := "</" + prefix + kind + ">"
		if i := strings.Index(text[end:], closing); i >= 0 {
			out = append(out, taggedMatch{kind, text[end : end+i]})
			at = end + i + len(closing)
		} else {
			at = start + 1
		}
	}
	return out
}

func stamp(s string) float64 {
	if s == "" {
		return claudeNow()
	}
	return jscompat.ParseDate(s)
}

func (b *claudeBase) stamp() float64 { return stamp(b.timestamp) }

func obj(pairs ...any) *jsvalue.Object { return jsvalue.NewObject(pairs...) }

// partTexts is content.map((part) => part.text).filter(string).join('\n').
func partTexts(parts []any) string {
	var texts []string
	for _, p := range parts {
		if s, ok := get(p, "text").(string); ok {
			texts = append(texts, s)
		}
	}
	return strings.Join(texts, "\n")
}

func deliveredPeerText(raw any) (string, bool) {
	if !isStr(opt(opt(raw, "origin"), "kind"), "peer") {
		return "", false
	}
	text := ""
	switch c := opt(opt(raw, "message"), "content").(type) {
	case string:
		text = c
	case []any:
		text = partTexts(c)
	}
	m := peerMessageRE.FindString(text)
	if m == "" {
		return "", false
	}
	return dropHopChain(m), true
}

// systemNotice is the context-compacted or error-notice a system line shows, without id and timestamp: nil for none.
func systemNotice(raw *jsvalue.Object) []any {
	if v, _ := raw.Get("isSidechain"); v == true {
		return nil
	}
	if isStr(get(raw, "subtype"), "compact_boundary") {
		meta := coalesce(get(raw, "compactMetadata"), jsvalue.NewObject())
		out := []any{"type", "context-compacted"}
		if n, ok := get(meta, "preTokens").(float64); ok {
			out = append(out, "beforeTokens", n)
		}
		if n, ok := get(meta, "postTokens").(float64); ok {
			out = append(out, "afterTokens", n)
		}
		return out
	}
	level := get(raw, "level")
	if content, ok := get(raw, "content").(string); ok && (isStr(level, "warning") || isStr(level, "error")) && jscompat.Trim(content) != "" {
		return []any{"type", "error-notice", "severity", level, "message", jscompat.Trim(content)}
	}
	return nil
}

func apiErrorNotice(raw any, ts float64) *jsvalue.Object {
	if get(raw, "isApiErrorMessage") != true {
		return nil
	}
	text := ""
	switch c := opt(get(raw, "message"), "content").(type) {
	case []any:
		text = jscompat.Trim(partTexts(c))
	case string:
		text = jscompat.Trim(c)
	}
	code, _ := get(raw, "error").(string)
	severity := "error"
	if code == "rate_limit" {
		severity = "warning"
	}
	message := text
	if message == "" {
		message = "API error"
	}
	e := obj("id", NewID(), "type", "error-notice", "timestamp", ts, "severity", severity, "message", message)
	if code != "" {
		e.Set("errorCode", code)
	}
	return e
}

func summarizeToolCall(name any, input any) string {
	switch {
	case isStr(name, "Read"):
		return "Read " + jsString(coalesce(coalesce(get(input, "file_path"), get(input, "path")), ""))
	case isStr(name, "Edit"):
		fp := jsString(coalesce(get(input, "file_path"), ""))
		oldStr := jsString(coalesce(get(input, "old_string"), ""))
		newStr := jsString(coalesce(get(input, "new_string"), ""))
		verb := "Create"
		if oldStr != "" {
			verb = "Update"
		}
		added, removed := claudeDiffCount(oldStr, newStr)
		return fmt.Sprintf("%s %s (+%d, -%d)", verb, fp, added, removed)
	case isStr(name, "Write"):
		return "Write " + jsString(coalesce(get(input, "file_path"), ""))
	case isStr(name, "Bash"):
		cmd, _, _ := strings.Cut(jsString(coalesce(get(input, "command"), "")), "\n")
		return "$ " + cmd
	case isStr(name, "Grep"), isStr(name, "Glob"):
		return jsString(name) + ` "` + jsString(coalesce(get(input, "pattern"), "")) + `"`
	}
	firstVal := ""
	if ks := keys(input); len(ks) > 0 && ks[0] != "" {
		firstVal = sliceJS(jsString(index(input, ks[0])), 60)
	}
	if firstVal == "" {
		return jsString(name)
	}
	return jsString(name) + " " + firstVal
}

func firstLineSummary(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		return strconv.Itoa(len(lines)) + " lines"
	}
	return sliceJS(lines[0], 100)
}

func summarizeToolResult(content any, isError any) string {
	if truthy(isError) {
		return "error"
	}
	switch c := content.(type) {
	case string:
		return firstLineSummary(c)
	case []any:
		for _, item := range c {
			if o, ok := item.(*jsvalue.Object); ok && isStr(get(o, "type"), "text") {
				return firstLineSummary(jsString(coalesce(get(o, "text"), "")))
			}
		}
	}
	return ""
}

type claudePrompt struct {
	text   string
	images []any
}

func splitPromptContent(content []any) claudePrompt {
	var texts []string
	images := []any{}
	for _, item := range content {
		switch item.(type) {
		case *jsvalue.Object, []any:
		default:
			continue
		}
		typ := get(item, "type")
		if text, ok := get(item, "text").(string); ok && isStr(typ, "text") {
			if cleaned := stripProtocolTags(text); cleaned != "" {
				texts = append(texts, cleaned)
			}
		} else if isStr(typ, "image") && isStr(opt(get(item, "source"), "type"), "base64") && truthy(get(get(item, "source"), "data")) {
			source := get(item, "source")
			images = append(images, "data:"+jsString(coalesce(get(source, "media_type"), "image/png"))+";base64,"+jsString(get(source, "data")))
		}
	}
	return claudePrompt{strings.Join(texts, "\n"), images}
}

func slashCommand(ts float64, name, args string) *jsvalue.Object {
	return obj("id", NewID(), "type", "slash-command", "timestamp", ts, "name", name, "args", args, "origin", "transcript")
}

// parseCommandProtocol is a slash command record or its output; ok is false when the text is not command protocol.
func parseCommandProtocol(text string, ts float64) (entries []*jsvalue.Object, ok bool) {
	if m := bashInputRE.FindStringSubmatch(text); m != nil {
		return []*jsvalue.Object{slashCommand(ts, claudeShellCommand, jscompat.Trim(m[1]))}, true
	}
	if outs := matchTagged(text, bashOpenRE, "bash-"); len(outs) > 0 {
		var stdout, stderr []string
		for _, m := range outs {
			if m.kind == "stdout" {
				stdout = append(stdout, m.body)
			} else {
				stderr = append(stderr, m.body)
			}
		}
		out, errText := strings.Join(stdout, "\n"), strings.Join(stderr, "\n")
		if jscompat.Trim(out) == claudeBashNoOutput {
			out = ""
		}
		var parts []string
		for _, p := range []string{out, errText} {
			if jscompat.Trim(p) != "" {
				parts = append(parts, p)
			}
		}
		output := cleanCommandOutput(strings.Join(parts, "\n"))
		return []*jsvalue.Object{obj("id", NewID(), "type", "command-output", "timestamp", ts, "output", output,
			"isError", jscompat.Trim(errText) != "")}, true
	}
	if m := commandNameRE.FindStringSubmatch(text); m != nil {
		name := strings.TrimPrefix(m[1], "/")
		if name == "exit" {
			return []*jsvalue.Object{obj("id", NewID(), "type", "session-exit", "timestamp", ts)}, true
		}
		args := ""
		if a := commandArgsRE.FindStringSubmatch(text); a != nil {
			args = jscompat.Trim(a[1])
		}
		return []*jsvalue.Object{slashCommand(ts, name, args)}, true
	}
	outs := matchTagged(text, localOpenRE, "local-command-")
	if len(outs) == 0 {
		return nil, false
	}
	bodies := make([]string, len(outs))
	isError := false
	for i, m := range outs {
		bodies[i] = m.body
		isError = isError || m.kind == "stderr"
	}
	output := cleanCommandOutput(strings.Join(bodies, "\n"))
	if output == "" {
		return []*jsvalue.Object{}, true
	}
	return []*jsvalue.Object{obj("id", NewID(), "type", "command-output", "timestamp", ts, "output", output, "isError", isError)}, true
}

type claudeRunningCommand struct {
	uuid          any
	expandsPrompt bool
}

func isExpandedCommandPrompt(raw any, b *claudeBase, cmd *claudeRunningCommand) bool {
	if nullish(b.parentUUID) || !sameString(b.parentUUID, cmd.uuid) {
		return false
	}
	switch c := opt(opt(raw, "message"), "content").(type) {
	case string:
		return cmd.expandsPrompt && !strings.Contains(c, "<local-command-")
	case []any:
		if len(c) == 0 {
			return false
		}
		for _, item := range c {
			if !isStr(get(item, "type"), "text") {
				return false
			}
		}
		return cmd.expandsPrompt || strings.HasPrefix(jsString(coalesce(get(c[0], "text"), "")), claudeSkillBodyPrefix)
	}
	return false
}

func sameString(a, b any) bool {
	x, ok := a.(string)
	return ok && isStr(b, x)
}

func extractTag(xml, tag string) (string, bool) {
	m := tagREs[tag].FindStringSubmatch(xml)
	if m == nil {
		return "", false
	}
	return jscompat.Trim(m[1]), true
}

func parseTaskNotification(text string, ts float64) *jsvalue.Object {
	m := taskNotifRE.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	xml := m[1]
	taskID, _ := extractTag(xml, "task-id")
	event, hasEvent := extractTag(xml, "event")
	status, hasStatus := extractTag(xml, "status")
	if !hasStatus && hasEvent {
		status = "event"
	}
	summary, _ := extractTag(xml, "summary")
	if taskID == "" || status == "" || summary == "" {
		return nil
	}
	result, hasResult := extractTag(xml, "result")
	if !hasResult && hasEvent {
		result, hasResult = event, true
	}
	e := obj("id", NewID(), "type", "task-notification", "timestamp", ts, "taskId", taskID, "status", status, "summary", summary)
	if hasResult {
		e.Set("result", result)
	}
	total, _ := extractTag(xml, "total_tokens")
	uses, _ := extractTag(xml, "tool_uses")
	duration, _ := extractTag(xml, "duration_ms")
	if total != "" || uses != "" || duration != "" {
		usage := obj()
		for _, kv := range [][2]string{{"totalTokens", total}, {"toolUses", uses}, {"durationMs", duration}} {
			if kv[1] != "" {
				usage.Set(kv[0], jsNumber(kv[1]))
			}
		}
		e.Set("usage", usage)
	}
	return e
}

func extractDiff(toolName any, input any) (filePath any, diff any) {
	switch {
	case isStr(toolName, "Edit"):
		fp := jsString(coalesce(get(input, "file_path"), ""))
		oldStr := jsString(coalesce(get(input, "old_string"), ""))
		newStr := jsString(coalesce(get(input, "new_string"), ""))
		if oldStr == "" && newStr == "" {
			return fp, undef
		}
		return fp, obj("filePath", fp, "oldString", oldStr, "newString", newStr)
	case isStr(toolName, "Write"):
		fp := jsString(coalesce(get(input, "file_path"), ""))
		content := jsString(coalesce(get(input, "content"), ""))
		if content == "" {
			return fp, undef
		}
		return fp, obj("filePath", fp, "oldString", "", "newString", content)
	case isStr(toolName, "Read"):
		return jsString(coalesce(coalesce(get(input, "file_path"), get(input, "path")), "")), undef
	}
	return undef, undef
}

// parseSentFiles is SendUserFile's input as the timeline shows it; nil for undefined.
func parseSentFiles(input any, cwd string) *jsvalue.Object {
	raw, _ := get(input, "files").([]any)
	var paths []any
	for _, p := range raw {
		s, ok := p.(string)
		if !ok || jscompat.Trim(s) == "" {
			continue
		}
		if !strings.HasPrefix(s, "/") && cwd != "" {
			s = posixJoin(cwd, s)
		}
		paths = append(paths, s)
	}
	if len(paths) == 0 {
		return nil
	}
	out := obj("paths", paths)
	if caption, ok := get(input, "caption").(string); ok && jscompat.Trim(caption) != "" {
		out.Set("caption", jscompat.Trim(caption))
	}
	if display := get(input, "display"); isStr(display, "render") || isStr(display, "attach") {
		out.Set("display", display)
	}
	if isStr(get(input, "status"), "proactive") {
		out.Set("proactive", true)
	}
	return out
}

func typeOf(e *jsvalue.Object) string { return e.String("type") }

func (p *claudeParse) single(raw any, b *claudeBase) []*jsvalue.Object {
	ts := b.stamp()
	switch b.typ {
	case "assistant":
		return p.assistant(raw, b, ts)
	case "user":
		return p.user(raw, ts)
	}
	return nil
}

func (p *claudeParse) assistant(raw any, b *claudeBase, ts float64) []*jsvalue.Object {
	if e := apiErrorNotice(raw, ts); e != nil {
		return []*jsvalue.Object{e}
	}
	msg, ok := zAssistantEntry(raw)
	if !ok {
		return nil
	}
	usageAttached := false
	var entries []*jsvalue.Object
	for _, c := range msg.content {
		switch {
		case isStr(get(c, "type"), "thinking"):
			entries = append(entries, obj("id", NewID(), "type", "thinking", "timestamp", ts, "thinking", coalesce(get(c, "thinking"), "")))
		case isStr(get(c, "type"), "text") && c.Has("text"):
			e := obj("id", NewID(), "type", "assistant-message", "timestamp", ts, "markdown", get(c, "text"))
			if truthy(msg.usage) && !usageAttached {
				e.Set("usage", msg.usage)
				e.Set("model", msg.model)
				usageAttached = true
			}
			entries = append(entries, e)
		case isStr(get(c, "type"), "tool_use") && c.Has("id") && c.Has("name"):
			entries = append(entries, toolUseEntry(c, b, ts))
		}
	}
	if msg.stopReason != undef {
		for i := len(entries) - 1; i >= 0; i-- {
			if typeOf(entries[i]) == "assistant-message" {
				entries[i].Set("stopReason", msg.stopReason)
				break
			}
		}
	}
	return entries
}

func toolUseEntry(c *jsvalue.Object, b *claudeBase, ts float64) *jsvalue.Object {
	input := any(jsvalue.NewObject())
	if c.Has("input") {
		input = get(c, "input")
	}
	toolName, toolUseID := get(c, "name"), get(c, "id")
	switch {
	case isStr(toolName, "ExitPlanMode") && !nullish(get(input, "plan")):
		plan := get(input, "plan")
		planText, ok := plan.(string)
		if !ok {
			planText = string(jsvalue.Stringify(plan, "  "))
		}
		id := NewID()
		filePath := any(undef)
		if s, ok := get(input, "planFilePath").(string); ok {
			filePath = s
		}
		allowed := any(undef)
		if list, ok := get(input, "allowedPrompts").([]any); ok {
			prompts := make([]any, len(list))
			for i, p := range list {
				prompts[i] = obj("tool", jsString(coalesce(get(p, "tool"), "")), "prompt", jsString(coalesce(get(p, "prompt"), "")))
			}
			allowed = prompts
		}
		return obj("id", id, "type", "plan", "timestamp", ts, "toolUseId", toolUseID, "markdown", planText, "filePath", filePath,
			"allowedPrompts", allowed, "status", "pending")
	case isStr(toolName, "TaskCreate"):
		subject, _ := get(input, "subject").(string)
		description := get(input, "description")
		if _, ok := description.(string); !ok {
			description = undef
		}
		return obj("id", NewID(), "type", "task-progress", "timestamp", ts, "action", "create", "taskId", "", "toolUseId", toolUseID,
			"subject", subject, "description", description, "status", "pending")
	case isStr(toolName, "TaskUpdate"):
		status := "pending"
		if s, ok := get(input, "status").(string); ok && slices.Contains([]string{"in_progress", "completed", "blocked"}, s) {
			status = s
		}
		taskID := get(input, "taskId")
		taskIDText, ok := taskID.(string)
		if !ok {
			taskIDText = jsString(coalesce(taskID, ""))
		}
		return obj("id", NewID(), "type", "task-progress", "timestamp", ts, "action", "update", "taskId", taskIDText, "status", status)
	}
	if questions, ok := askQuestions(toolName, input); ok {
		items := make([]any, len(questions))
		for i, q := range questions {
			options, _ := get(q, "options").([]any)
			question := jsString(coalesce(get(q, "question"), ""))
			header := jsString(coalesce(get(q, "header"), ""))
			opts := make([]any, len(options))
			for j, o := range options {
				opts[j] = obj("label", jsString(coalesce(get(o, "label"), "")), "description", jsString(coalesce(get(o, "description"), "")))
			}
			multi := truthy(get(q, "multiSelect"))
			preview := false
			for _, o := range options {
				if _, ok := get(o, "preview").(string); ok {
					preview = true
					break
				}
			}
			items[i] = obj("question", question, "header", header, "options", opts, "multiSelect", multi, "preview", preview)
		}
		return obj("id", NewID(), "type", "ask-user-question", "timestamp", ts, "toolUseId", toolUseID, "questions", items,
			"status", "pending")
	}
	summary := summarizeToolCall(toolName, input)
	filePath, diff := extractDiff(toolName, input)
	var peer, sent *jsvalue.Object
	if isStr(toolName, "SendMessage") {
		to, okTo := get(input, "to").(string)
		message, okMsg := get(input, "message").(string)
		if okTo && okMsg && jscompat.Trim(message) != "" {
			peer = obj("to", to, "text", message)
		}
	}
	if isStr(toolName, "SendUserFile") {
		sent = parseSentFiles(input, b.cwd)
	}
	e := obj("id", NewID(), "type", "tool-call", "timestamp", ts, "toolUseId", toolUseID, "toolName", toolName, "summary", summary,
		"filePath", filePath, "diff", diff)
	if peer != nil {
		e.Set("peerMessage", peer)
	}
	if sent != nil {
		e.Set("sentFiles", sent)
	}
	e.Set("status", "pending")
	return e
}

func askQuestions(toolName, input any) ([]any, bool) {
	if !isStr(toolName, "AskUserQuestion") {
		return nil, false
	}
	q, ok := get(input, "questions").([]any)
	return q, ok
}

func (p *claudeParse) user(raw any, ts float64) []*jsvalue.Object {
	if get(raw, "isCompactSummary") == true {
		return nil
	}
	if get(raw, "isMeta") == true {
		if text, ok := deliveredPeerText(raw); ok {
			return []*jsvalue.Object{obj("id", NewID(), "type", "user-message", "timestamp", ts, "text", text)}
		}
		return nil
	}
	parsed, ok := zUserEntry(raw)
	if !ok {
		return nil
	}
	if content, ok := parsed.(string); ok {
		if n := parseTaskNotification(content, ts); n != nil {
			return []*jsvalue.Object{n}
		}
		if cmd, ok := parseCommandProtocol(content, ts); ok {
			return cmd
		}
		if cleaned := stripProtocolTags(content); cleaned != "" {
			return []*jsvalue.Object{obj("id", NewID(), "type", "user-message", "timestamp", ts, "text", cleaned)}
		}
		return nil
	}
	content := parsed.([]any)
	if len(content) == 1 {
		first := content[0].(*jsvalue.Object)
		if isStr(get(first, "type"), "text") && first.Has("text") {
			text, ok := get(first, "text").(string)
			if !ok {
				claudeFail("text.startsWith is not a function")
			}
			if strings.HasPrefix(text, claudeInterruptPrefix) {
				return []*jsvalue.Object{obj("id", NewID(), "type", "interrupt", "timestamp", ts)}
			}
		}
	}
	var entries []*jsvalue.Object
	prompt := splitPromptContent(content)
	if prompt.text != "" || len(prompt.images) > 0 {
		e := obj("id", NewID(), "type", "user-message", "timestamp", ts, "text", prompt.text)
		if len(prompt.images) > 0 {
			e.Set("images", prompt.images)
		}
		entries = append(entries, e)
	}
	for _, it := range content {
		item := it.(*jsvalue.Object)
		if !isStr(get(item, "type"), "tool_result") || !item.Has("tool_use_id") {
			continue
		}
		answers := recordOfStrings(opt(get(raw, "toolUseResult"), "answers"))
		var summary string
		if answers != nil {
			values := make([]string, 0, answers.Len())
			for _, k := range answers.Keys() {
				values = append(values, answers.String(k))
			}
			summary = strings.Join(values, ", ")
		} else {
			summary = summarizeToolResult(get(item, "content"), coalesce(get(item, "is_error"), false))
		}
		e := obj("id", NewID(), "type", "tool-result", "timestamp", ts, "toolUseId", get(item, "tool_use_id"),
			"isError", coalesce(get(item, "is_error"), false), "summary", summary)
		if answers != nil {
			e.Set("answers", answers)
		}
		entries = append(entries, e)
	}
	return entries
}

func recordOfStrings(v any) *jsvalue.Object {
	o, ok := v.(*jsvalue.Object)
	if !ok || o == nil {
		return nil
	}
	out := jsvalue.NewObject()
	for _, k := range o.Keys() {
		s, ok := get(o, k).(string)
		if !ok {
			return nil
		}
		out.Set(k, s)
	}
	return out
}

func (p *claudeParse) agentGroup(group []claudeRaw, hint *claudeAgentHint) *jsvalue.Object {
	ts := group[0].base.stamp()
	var parsed []*jsvalue.Object
	for _, r := range group {
		parsed = append(parsed, p.single(r.raw, r.base)...)
	}
	entries := entryArray(mergeToolResults(parsed))
	if hint != nil {
		return obj("id", NewID(), "type", "agent-group", "timestamp", ts, "agentType", hint.agentType, "description", hint.description,
			"entryCount", len(group), "entries", entries)
	}
	description := ""
scan:
	for _, r := range group {
		if !isStr(get(r.raw, "type"), "user") || !truthy(get(r.raw, "message")) {
			continue
		}
		switch c := get(get(r.raw, "message"), "content").(type) {
		case string:
			description = sliceJS(c, 100)
			break scan
		case []any:
			for _, item := range c {
				if text, ok := get(item, "text").(string); ok && isStr(get(item, "type"), "text") {
					description = sliceJS(text, 100)
					break
				}
			}
			if description != "" {
				break scan
			}
		}
	}
	if description == "" {
		description = "Sub-agent"
	}
	return obj("id", NewID(), "type", "agent-group", "timestamp", ts, "agentType", "Unknown", "description", description,
		"entryCount", len(group), "entries", entries)
}

func entryArray(entries []*jsvalue.Object) []any {
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = e
	}
	return out
}

// mapKey is a Map key with SameValueZero equality; an empty array is a fresh key, as no other value equals it.
func mapKey(v any, fresh *int) string {
	switch x := v.(type) {
	case string:
		return "s" + x
	case float64:
		return "n" + jscompat.Number(x)
	case bool:
		return "b" + strconv.FormatBool(x)
	case nil:
		return "null"
	case *jsvalue.Object:
		return fmt.Sprintf("o%p", x)
	case []any:
		if len(x) > 0 {
			return fmt.Sprintf("a%p", &x[0])
		}
		*fresh++
		return "e" + strconv.Itoa(*fresh)
	}
	return "undefined"
}

func mergeToolResults(entries []*jsvalue.Object) []*jsvalue.Object {
	toolCalls := map[string]*jsvalue.Object{}
	questions := map[string]*jsvalue.Object{}
	plans := map[string]*jsvalue.Object{}
	tasks := map[string]*jsvalue.Object{}
	result := make([]*jsvalue.Object, 0, len(entries))
	fresh := 0
	key := func(v any) string { return mapKey(v, &fresh) }
	for _, e := range entries {
		id := get(e, "toolUseId")
		switch typeOf(e) {
		case "tool-call":
			toolCalls[key(id)] = e
		case "ask-user-question":
			questions[key(id)] = e
		case "plan":
			plans[key(id)] = e
		case "tool-result":
			k := key(id)
			status := "success"
			if truthy(get(e, "isError")) {
				status = "error"
			}
			summary := e.String("summary")
			if task := tasks[k]; task != nil && summary != "" {
				if m := taskNumberRE.FindStringSubmatch(summary); m != nil {
					task.Set("taskId", m[1])
				}
				delete(tasks, k)
			}
			if call := toolCalls[k]; call != nil {
				call.Set("status", status)
				if summary != "" && !truthy(get(e, "isError")) {
					m := linesSummaryRE.FindStringSubmatch(summary)
					name := get(call, "toolName")
					if isStr(name, "Bash") && m != nil {
						call.Set("summary", jsString(get(call, "summary"))+" → "+m[1]+" lines")
					} else if isStr(name, "Grep") || isStr(name, "Glob") {
						count := "1"
						if m != nil {
							count = m[1]
						}
						call.Set("summary", jsString(get(call, "summary"))+" → "+count+" results")
					}
				}
			} else if q := questions[k]; q != nil {
				q.Set("status", status)
				if summary != "" {
					q.Set("answer", summary)
				}
				if answers := get(e, "answers"); truthy(answers) {
					q.Set("answers", answers)
				}
			} else if plan := plans[k]; plan != nil {
				plan.Set("status", status)
			}
		case "task-progress":
			if isStr(get(e, "action"), "create") && truthy(id) {
				tasks[key(id)] = e
			}
		}
		result = append(result, e)
	}
	return result
}
