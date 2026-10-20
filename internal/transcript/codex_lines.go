package transcript

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/codemode"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Codex transcript lines into timeline entries (codex-lines in the previous runtime).

const (
	codexStdoutLimit  = 1_048_576
	codexSummaryLimit = 100
)

// Now is Date.now() for the parsers; tests pin it.
var Now = func() float64 { return float64(time.Now().UnixMilli()) }

type codexInFlight struct {
	kind   string // exec, web-search, mcp, patch
	callID string

	command       string
	parsedCommand any
	cwd           any
	stdout        strings.Builder
	stdoutLen     int // in UTF-16 units
	truncated     bool

	query any

	server, tool string
	argsSummary  any

	files []any
	diff  any
}

type codexMessage struct {
	role, text, source, phase string
}

// codexState is what a parse carries from line to line: calls still waiting for their end, the shell calls whose output
// is not shown, and the last message read (Codex writes a message twice, as an event and as a response item).
type codexState struct {
	imageURL      func(string) string
	inFlight      map[string]*codexInFlight
	inFlightOrder []string
	staleWarnings map[string]bool
	suppressed    map[string]bool
	pending       *codexMessage
}

func newCodexState(imageURL func(string) string) *codexState {
	return &codexState{
		imageURL:      imageURL,
		inFlight:      map[string]*codexInFlight{},
		staleWarnings: map[string]bool{},
		suppressed:    map[string]bool{},
	}
}

// setInFlight is Map.set: a reused call id keeps its place.
func (s *codexState) setInFlight(id string, e *codexInFlight) {
	if _, ok := s.inFlight[id]; !ok {
		s.inFlightOrder = append(s.inFlightOrder, id)
	}
	s.inFlight[id] = e
}

func (s *codexState) deleteInFlight(id string) {
	if _, ok := s.inFlight[id]; !ok {
		return
	}
	delete(s.inFlight, id)
	for i, k := range s.inFlightOrder {
		if k == id {
			s.inFlightOrder = append(s.inFlightOrder[:i:i], s.inFlightOrder[i+1:]...)
			break
		}
	}
}

func (s *codexState) clearInFlight() {
	s.inFlight = map[string]*codexInFlight{}
	s.inFlightOrder = nil
}

func (s *codexState) inFlightKind(id string, kind string) *codexInFlight {
	if e := s.inFlight[id]; e != nil && e.kind == kind {
		return e
	}
	return nil
}

func isCodexUserText(text string) bool {
	t := jscompat.Trim(text)
	return t != "" && !strings.HasPrefix(t, "# AGENTS.md instructions") &&
		!(strings.HasPrefix(t, "<") && (strings.Contains(t, "environment_context") || strings.Contains(t, "user_instructions")))
}

// readCodexMessage is readCodexMessage of the previous runtime's Codex provider.
func readCodexMessage(rtype string, payload *jsvalue.Object) *codexMessage {
	if payload == nil {
		return nil
	}
	ptype := prop(payload, "type")
	var m codexMessage
	switch {
	case rtype == "event_msg" && (ptype == "user_message" || ptype == "agent_message"):
		m.role = "assistant"
		if ptype == "user_message" {
			m.role = "user"
		}
		m.text = safeString(prop(payload, "message"))
		if m.role == "user" && !isCodexUserText(m.text) {
			return nil
		}
	case rtype == "response_item" && ptype == "message" && (prop(payload, "role") == "user" || prop(payload, "role") == "assistant"):
		m.role = prop(payload, "role").(string)
		content, ok := prop(payload, "content").([]any)
		if !ok {
			return nil
		}
		want := "output_text"
		if m.role == "user" {
			want = "input_text"
		}
		var b strings.Builder
		for _, part := range content {
			text, isStr := prop(part, "text").(string)
			if prop(part, "type") != want || !isStr {
				continue
			}
			if m.role == "user" && !isCodexUserText(text) {
				continue
			}
			b.WriteString(text)
		}
		m.text = b.String()
	default:
		return nil
	}
	if jscompat.Trim(m.text) == "" {
		return nil
	}
	m.source = rtype
	m.phase = safeString(prop(payload, "phase"))
	return &m
}

var codexTurnBoundaries = map[string]bool{"task_started": true, "task_complete": true, "TurnComplete": true, "turn_aborted": true, "TurnAborted": true}

// readMessage is createCodexMessageReader: a message that repeats the one just read from the other source is dropped.
func (s *codexState) readMessage(rtype string, payload *jsvalue.Object) *codexMessage {
	if rtype == "turn_context" || rtype == "event_msg" && codexTurnBoundaries[mustString(prop(payload, "type"))] {
		s.pending = nil
	}
	m := readCodexMessage(rtype, payload)
	if m == nil {
		return nil
	}
	if p := s.pending; p != nil && p.role == m.role && p.source != m.source && p.text == m.text &&
		(p.phase == "" || m.phase == "" || p.phase == m.phase) {
		s.pending = nil
		return nil
	}
	s.pending = m
	return m
}

func codexEntry(typ string, ts float64, pairs ...any) *jsvalue.Object {
	o := jsvalue.NewObject("id", NewID(), "type", typ, "timestamp", ts)
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func summarizeCodexCall(name string, args any) string {
	if !isObjectLike(args) {
		return name
	}
	pick := func(a, b string) string { return safeString(codexCoalesce(prop(args, a), prop(args, b))) }
	switch name {
	case "exec_command":
		if cmd := firstLine(pick("cmd", "command")); cmd != "" {
			return "$ " + cmd
		}
		return "$ "
	case "shell", "bash":
		if cmd := firstLine(pick("command", "cmd")); cmd != "" {
			return "$ " + cmd
		}
		return "$ "
	case "read", "read_file":
		if fp := pick("path", "file_path"); fp != "" {
			return "Read " + fp
		}
		return "Read"
	case "list_files", "list":
		if cmd := pick("cmd", "path"); cmd != "" {
			return "List " + cmd
		}
		return "List"
	case "search", "grep":
		if p := pick("pattern", "query"); p != "" {
			return `Search "` + p + `"`
		}
		return "Search"
	case "write", "write_file":
		if fp := pick("path", "file_path"); fp != "" {
			return "Write " + fp
		}
		return "Write"
	}
	var v any
	switch x := args.(type) {
	case *jsvalue.Object:
		keys := x.Keys()
		if len(keys) == 0 || keys[0] == "" {
			return name
		}
		v, _ = x.Get(keys[0])
	case []any:
		if len(x) == 0 {
			return name
		}
		v = x[0]
	}
	preview, ok := v.(string)
	if !ok {
		preview = stringify(v)
	}
	return name + " " + oneLineSummary(preview, 60)
}

func summarizeCodexOutput(output string) string {
	if output == "" {
		return ""
	}
	trimmed := jscompat.Trim(output)
	if n := strings.Count(trimmed, "\n") + 1; n > 1 {
		return strconv.Itoa(n) + " lines"
	}
	return oneLineSummary(trimmed, codexSummaryLimit)
}

var (
	codexNotLineEnd    = `[^\n\r\x{2028}\x{2029}]`
	codexPatchHeaderRE = regexp.MustCompile(`^\*\*\*` + jscompat.SpaceClass + `+(?i:(Add|Update|Delete))` + jscompat.SpaceClass +
		`+(?i:File):` + jscompat.SpaceClass + `+(` + codexNotLineEnd + `+?)` + jscompat.SpaceClass + `*$`)
	codexLinesRE = regexp.MustCompile(`^([0-9]+) lines$`)
)

func parseApplyPatchInput(input string) (files []any, diff any) {
	files = []any{}
	if input == "" {
		return files, jsvalue.Undefined
	}
	for _, line := range strings.Split(input, "\n") {
		m := codexPatchHeaderRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		files = append(files, jsvalue.NewObject("path", m[2], "status", strings.ToLower(m[1])))
	}
	return files, input
}

func errorSeverity(t string) string {
	switch t {
	case "error", "Error":
		return "error"
	case "warning", "Warning":
		return "warning"
	case "stream_error", "StreamError":
		return "stream-error"
	case "guardian_warning", "GuardianWarning":
		return "guardian-warning"
	}
	return ""
}

func approvalKind(t string) string {
	switch t {
	case "exec_approval_request", "ExecApprovalRequest":
		return "exec"
	case "apply_patch_approval_request", "ApplyPatchApprovalRequest":
		return "apply-patch"
	case "request_permissions", "RequestPermissions":
		return "permissions"
	}
	return ""
}

func taskStatus(v any) string {
	switch v {
	case "in_progress", "completed", "blocked":
		return v.(string)
	}
	return "pending"
}

var codexSuppressedNames = map[string]bool{"exec_command": true, "shell": true, "bash": true, "write_stdin": true}

func buildExecEntry(f *codexInFlight, ts float64, exitCode, durationMs, stderr any, status string) *jsvalue.Object {
	if s, ok := stderr.(string); !ok || s == "" {
		stderr = jsvalue.Undefined
	}
	return codexEntry("exec-command-stream", ts,
		"callId", f.callID,
		"command", f.command,
		"parsedCommand", f.parsedCommand,
		"cwd", f.cwd,
		"stdout", f.stdout.String(),
		"stderr", stderr,
		"exitCode", exitCode,
		"durationMs", durationMs,
		"truncated", f.truncated,
		"status", status,
	)
}

func (f *codexInFlight) collectStdout(chunk string) {
	if f.truncated {
		return
	}
	n := jscompat.Len(chunk)
	if f.stdoutLen+n <= codexStdoutLimit {
		f.stdout.WriteString(chunk)
		f.stdoutLen += n
		return
	}
	if remaining := codexStdoutLimit - f.stdoutLen; remaining > 0 {
		f.stdout.WriteString(jscompat.Slice(chunk, remaining))
		f.stdoutLen += remaining
	}
	f.truncated = true
	suffix := "\n[... truncated, total " + strconv.Itoa(f.stdoutLen+n) + " bytes]"
	f.stdout.WriteString(suffix)
	f.stdoutLen += jscompat.Len(suffix)
}

// flushStale ends every call still in flight as failed, each once (flushStaleInFlight).
func (s *codexState) flushStale(ts float64) []*jsvalue.Object {
	var out []*jsvalue.Object
	if len(s.inFlight) == 0 {
		return out
	}
	for _, id := range s.inFlightOrder {
		f := s.inFlight[id]
		if s.staleWarnings[f.callID] {
			continue
		}
		s.staleWarnings[f.callID] = true
		out = append(out, codexEntry("error-notice", ts, "severity", "warning",
			"message", "In-flight "+f.kind+" call "+f.callID+" ended without completion"))
		switch f.kind {
		case "exec":
			out = append(out, buildExecEntry(f, ts, jsvalue.Undefined, jsvalue.Undefined, jsvalue.Undefined, "error"))
		case "web-search":
			out = append(out, codexEntry("web-search", ts, "callId", f.callID, "query", f.query, "status", "error"))
		case "mcp":
			out = append(out, codexEntry("mcp-tool-call", ts, "callId", f.callID, "server", f.server, "tool", f.tool,
				"argumentsSummary", f.argsSummary, "status", "error"))
		case "patch":
			out = append(out, codexEntry("patch-apply", ts, "callId", f.callID, "files", f.files, "diff", f.diff,
				"success", false, "status", "error"))
		}
	}
	s.clearInFlight()
	return out
}

func toolStatus(status string) string {
	switch status {
	case "completed":
		return "success"
	case "failed":
		return "error"
	}
	return "pending"
}

func (s *codexState) responseItem(p *jsvalue.Object, ts float64) []*jsvalue.Object {
	switch safeString(prop(p, "type")) {
	case "reasoning":
		summary := []any{}
		raw, _ := prop(p, "summary").([]any)
		for _, item := range raw {
			if str, ok := item.(string); ok {
				if jscompat.Trim(str) != "" {
					summary = append(summary, str)
				}
				continue
			}
			if isObjectLike(item) {
				if text := safeString(prop(item, "text")); jscompat.Trim(text) != "" {
					summary = append(summary, text)
				}
			}
		}
		enc, _ := prop(p, "encrypted_content").(string)
		hasEnc := enc != ""
		if len(summary) == 0 && !hasEnc {
			return nil
		}
		return []*jsvalue.Object{codexEntry("reasoning-summary", ts, "summary", summary, "hasEncryptedContent", hasEnc)}
	case "function_call":
		callID, name := safeString(prop(p, "call_id")), safeString(prop(p, "name"))
		if callID == "" || name == "" {
			return nil
		}
		if codexSuppressedNames[name] {
			s.suppressed[callID] = true
			return nil
		}
		args := prop(p, "arguments")
		if str, ok := args.(string); ok {
			if v, ok := parseJSON(str); ok && v != nil {
				args = v
			}
		}
		return []*jsvalue.Object{codexEntry("tool-call", ts, "toolUseId", callID, "toolName", name,
			"summary", summarizeCodexCall(name, args), "status", "pending")}
	case "function_call_output", "custom_tool_call_output":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		if s.suppressed[callID] {
			delete(s.suppressed, callID)
			return nil
		}
		return []*jsvalue.Object{codexEntry("tool-result", ts, "toolUseId", callID, "isError", false,
			"summary", summarizeCodexOutput(safeString(prop(p, "output"))))}
	case "custom_tool_call":
		callID, name := safeString(prop(p, "call_id")), safeString(prop(p, "name"))
		if callID == "" || name == "" {
			return nil
		}
		if name == "apply_patch" {
			files, diff := parseApplyPatchInput(safeString(prop(p, "input")))
			status := toolStatus(safeString(prop(p, "status")))
			s.suppressed[callID] = true
			return []*jsvalue.Object{codexEntry("patch-apply", ts, "callId", callID, "files", files, "diff", diff,
				"success", status == "success", "status", status)}
		}
		status := "pending"
		if safeString(prop(p, "status")) == "completed" {
			status = "success"
		}
		summary := summarizeCodexCall(name, prop(p, "input"))
		if input, ok := prop(p, "input").(string); ok && name == "exec" {
			// Code mode: the script calls the real tools, so the entry says which ones.
			d := codemode.Describe(input)
			name, summary = d.Tool, d.Summary
		}
		return []*jsvalue.Object{codexEntry("tool-call", ts, "toolUseId", callID, "toolName", name,
			"summary", summary, "status", status)}
	case "web_search_call":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			callID = NewID()
		}
		e := codexEntry("web-search", ts, "callId", callID, "query", orUndefined(safeString(prop(p, "query"))),
			"status", toolStatus(safeString(prop(p, "status"))))
		s.deleteInFlight(callID)
		return []*jsvalue.Object{e}
	}
	return nil
}

func readCommand(p *jsvalue.Object) string {
	switch c := prop(p, "command").(type) {
	case string:
		return c
	case []any:
		arr := jsvalue.Strings(c)
		if len(arr) >= 3 && (arr[0] == "/bin/zsh" || arr[0] == "/bin/bash") && arr[1] == "-lc" {
			return strings.Join(arr[2:], " ")
		}
		return strings.Join(arr, " ")
	}
	return ""
}

func readParsedCommand(p *jsvalue.Object) any {
	arr, _ := prop(p, "parsed_cmd").([]any)
	if len(arr) == 0 || !isObjectLike(arr[0]) {
		return jsvalue.Undefined
	}
	return optString(prop(arr[0], "cmd"))
}

func parseDurationMs(raw any) any {
	switch x := raw.(type) {
	case float64:
		return x
	case *jsvalue.Object, []any:
		secs, ok := safeNumber(prop(x, "secs"))
		if !ok {
			secs = 0
		}
		nanos, ok := safeNumber(prop(x, "nanos"))
		if !ok {
			nanos = 0
		}
		return float64(secs*1000) + float64(nanos/1_000_000)
	}
	return jsvalue.Undefined
}

func numberOrUndefined(v any) any {
	if f, ok := safeNumber(v); ok {
		return f
	}
	return jsvalue.Undefined
}

func (s *codexState) eventMsg(p *jsvalue.Object, ts float64) []*jsvalue.Object {
	typ := safeString(prop(p, "type"))
	one := func(e *jsvalue.Object) []*jsvalue.Object { return []*jsvalue.Object{e} }

	if sev := errorSeverity(typ); sev != "" {
		msg := safeString(prop(p, "message"))
		if msg == "" {
			msg = typ
		}
		return one(codexEntry("error-notice", ts, "severity", sev, "message", msg,
			"retryStatus", optString(prop(p, "retry_status")), "errorCode", optString(prop(p, "codex_error_info"))))
	}

	if kind := approvalKind(typ); kind != "" {
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			callID = NewID()
		}
		var patches, permissions any = jsvalue.Undefined, jsvalue.Undefined
		if raw, ok := prop(p, "patches").([]any); ok {
			list := []any{}
			for _, e := range raw {
				if isObjectLike(e) {
					list = append(list, jsvalue.NewObject("path", safeString(prop(e, "path")), "status", optString(prop(e, "status"))))
				}
			}
			patches = list
		}
		if raw, ok := prop(p, "permissions").([]any); ok {
			permissions = jsvalue.StringArray(jsvalue.Strings(raw))
		}
		id := NewID()
		e := jsvalue.NewObject("id", id, "type", "approval-request", "timestamp", ts, "approvalKind", kind, "callId", callID,
			"command", optString(prop(p, "command")), "cwd", optString(prop(p, "cwd")), "patches", patches,
			"permissions", permissions, "status", "pending")
		return one(e)
	}

	switch typ {
	case "user_message":
		text := safeString(prop(p, "message"))
		if text == "" {
			return nil
		}
		images := []any{}
		if raw, ok := prop(p, "images").([]any); ok {
			for _, v := range raw {
				if str, ok := v.(string); ok {
					images = append(images, str)
				}
			}
		}
		if raw, ok := prop(p, "local_images").([]any); ok {
			for _, v := range raw {
				if str, ok := v.(string); ok && s.imageURL != nil {
					if u := s.imageURL(str); u != "" {
						images = append(images, u)
					}
				}
			}
		}
		e := codexEntry("user-message", ts, "text", text)
		if len(images) > 0 {
			e.Set("images", images)
		}
		return one(e)
	case "task_complete", "TurnComplete":
		return one(codexEntry("turn-end", ts))
	case "turn_aborted", "TurnAborted":
		return one(codexEntry("interrupt", ts))
	case "shutdown_complete", "ShutdownComplete":
		return one(codexEntry("session-exit", ts))
	case "plan_update", "PlanUpdate":
		raw, _ := prop(p, "plan").([]any)
		var out []*jsvalue.Object
		for _, item := range raw {
			if !isObjectLike(item) {
				continue
			}
			taskID := safeString(codexCoalesce(prop(item, "task_id"), prop(item, "id")))
			status := taskStatus(prop(item, "status"))
			subject := safeString(codexCoalesce(codexCoalesce(prop(item, "subject"), prop(item, "title")), prop(item, "description")))
			if taskID == "" && subject == "" {
				continue
			}
			if taskID == "" {
				taskID = subject
			}
			out = append(out, codexEntry("task-progress", ts, "action", "update", "taskId", taskID, "subject", subject,
				"description", optString(prop(item, "description")), "status", status))
		}
		return out
	case "entered_review_mode", "EnteredReviewMode":
		md := safeString(prop(p, "description"))
		if md == "" {
			md = "Entered review mode"
		}
		return one(codexEntry("plan", ts, "toolUseId", "", "markdown", md, "status", "pending"))
	case "exited_review_mode", "ExitedReviewMode":
		outcome := safeString(prop(p, "outcome"))
		status := "pending"
		switch outcome {
		case "approved":
			status = "success"
		case "rejected":
			status = "error"
		}
		md := "Exited review mode"
		if outcome != "" {
			md = "Review " + outcome
		}
		return one(codexEntry("plan", ts, "toolUseId", "", "markdown", md, "status", status))
	case "request_user_input", "RequestUserInput":
		question := safeString(prop(p, "question"))
		if question == "" {
			return nil
		}
		e := codexEntry("ask-user-question", ts)
		toolUseID := safeString(prop(p, "call_id"))
		if toolUseID == "" {
			toolUseID = NewID()
		}
		e.Set("toolUseId", toolUseID)
		e.Set("questions", []any{jsvalue.NewObject("question", question, "header", "", "options", []any{},
			"multiSelect", false, "preview", false)})
		e.Set("status", "pending")
		return one(e)
	case "item_completed":
		if prop(prop(p, "item"), "type") != "ContextCompaction" {
			return nil
		}
		return one(codexEntry("context-compacted", ts))
	case "context_compacted", "ContextCompacted":
		return one(codexEntry("context-compacted", ts, "beforeTokens", numberOrUndefined(prop(p, "before_tokens")),
			"afterTokens", numberOrUndefined(prop(p, "after_tokens"))))
	case "exec_command_begin", "ExecCommandBegin":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		command := safeString(prop(p, "command"))
		if command == "" {
			if arr, ok := prop(p, "command").([]any); ok {
				command = joinSpace(arr)
			}
		}
		s.setInFlight(callID, &codexInFlight{kind: "exec", callID: callID, command: command,
			parsedCommand: jsvalue.Undefined, cwd: optString(prop(p, "cwd"))})
		return nil
	case "exec_command_delta", "ExecCommandDelta":
		callID := safeString(prop(p, "call_id"))
		chunk := safeString(codexCoalesce(prop(p, "chunk"), prop(p, "data")))
		if callID == "" || chunk == "" {
			return nil
		}
		if f := s.inFlightKind(callID, "exec"); f != nil {
			f.collectStdout(chunk)
		}
		return nil
	case "exec_command_end", "ExecCommandEnd":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		exitCode := numberOrUndefined(prop(p, "exit_code"))
		status := "error"
		if exitCode == 0.0 {
			status = "success"
		}
		stderr := optString(prop(p, "stderr"))
		durationMs := parseDurationMs(prop(p, "duration"))
		if f := s.inFlightKind(callID, "exec"); f != nil {
			aggregated, _ := prop(p, "aggregated_output").(string)
			stdout, isStr := prop(p, "stdout").(string)
			if f.stdoutLen == 0 && aggregated != "" {
				f.collectStdout(aggregated)
			} else if isStr && stdout != "" && f.stdoutLen == 0 {
				f.collectStdout(stdout)
			}
			e := buildExecEntry(f, ts, exitCode, durationMs, stderr, status)
			s.deleteInFlight(callID)
			return one(e)
		}
		aggregated, ok := prop(p, "aggregated_output").(string)
		if !ok {
			aggregated = safeString(prop(p, "stdout"))
		}
		f := &codexInFlight{kind: "exec", callID: callID, command: readCommand(p), parsedCommand: readParsedCommand(p),
			cwd: optString(prop(p, "cwd"))}
		if aggregated != "" {
			f.collectStdout(aggregated)
		}
		return one(buildExecEntry(f, ts, exitCode, durationMs, stderr, status))
	case "web_search_begin", "WebSearchBegin":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		s.setInFlight(callID, &codexInFlight{kind: "web-search", callID: callID, query: orUndefined(safeString(prop(p, "query")))})
		return nil
	case "web_search_end", "WebSearchEnd":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		var query any
		if f := s.inFlightKind(callID, "web-search"); f != nil {
			query = f.query
		} else {
			query = orUndefined(safeString(prop(p, "query")))
		}
		results, _ := prop(p, "results").([]any)
		var resultsSummary, resultCount any = orUndefined(safeString(prop(p, "summary"))), jsvalue.Undefined
		if len(results) > 0 {
			resultsSummary = strconv.Itoa(len(results)) + " results"
			resultCount = len(results)
		}
		s.deleteInFlight(callID)
		return one(codexEntry("web-search", ts, "callId", callID, "query", query, "resultsSummary", resultsSummary,
			"resultCount", resultCount, "status", "success"))
	case "mcp_tool_call_begin", "McpToolCallBegin":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		args := codexCoalesce(prop(p, "arguments"), jsvalue.NewObject())
		s.setInFlight(callID, &codexInFlight{kind: "mcp", callID: callID, server: safeString(prop(p, "server")),
			tool: safeString(prop(p, "tool")), argsSummary: oneLineSummary(stringify(args), 80)})
		return nil
	case "mcp_tool_call_end", "McpToolCallEnd":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		server, tool := safeString(prop(p, "server")), safeString(prop(p, "tool"))
		var argsSummary any = jsvalue.Undefined
		if f := s.inFlightKind(callID, "mcp"); f != nil {
			server, tool, argsSummary = f.server, f.tool, f.argsSummary
		}
		s.deleteInFlight(callID)
		result := stringify(codexCoalesce(prop(p, "result"), ""))
		return one(codexEntry("mcp-tool-call", ts, "callId", callID, "server", server, "tool", tool,
			"argumentsSummary", argsSummary, "resultSummary", oneLineSummary(result, 120), "status", "success"))
	case "patch_apply_begin", "PatchApplyBegin":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		s.setInFlight(callID, &codexInFlight{kind: "patch", callID: callID, files: []any{}, diff: jsvalue.Undefined})
		return nil
	case "patch_apply_updated", "PatchApplyUpdated":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		f := s.inFlightKind(callID, "patch")
		if f == nil {
			return nil
		}
		if path := safeString(prop(p, "path")); path != "" {
			f.files = append(f.files, jsvalue.NewObject("path", path, "status", optString(prop(p, "status"))))
		}
		return nil
	case "patch_apply_end", "PatchApplyEnd":
		callID := safeString(prop(p, "call_id"))
		if callID == "" {
			return nil
		}
		success := prop(p, "success") != false
		files := []any{}
		if f := s.inFlightKind(callID, "patch"); f != nil {
			files = f.files
		}
		s.deleteInFlight(callID)
		status := "error"
		if success {
			status = "success"
		}
		return one(codexEntry("patch-apply", ts, "callId", callID, "files", files, "success", success, "status", status))
	}
	return nil
}

// tsToMillis is Date.parse of a record's timestamp, now when it has none or it does not parse.
func tsToMillis(raw any) float64 {
	s, _ := raw.(string)
	if s == "" {
		return Now()
	}
	ms := jscompat.ParseDate(s)
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return Now()
	}
	return ms
}

// codexItem is a line that passed the rollout item schema: an object with a string type, an optional string timestamp
// and an optional object payload.
type codexItem struct {
	timestamp any
	typ       string
	payload   *jsvalue.Object
}

func validateCodexItem(v any) (codexItem, bool) {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return codexItem{}, false
	}
	var it codexItem
	ts, has := o.Get("timestamp")
	if has {
		if _, ok := ts.(string); !ok {
			return codexItem{}, false
		}
		it.timestamp = ts
	} else {
		it.timestamp = codexUndefined
	}
	t, ok := prop(o, "type").(string)
	if !ok {
		return codexItem{}, false
	}
	it.typ = t
	if p, has := o.Get("payload"); has {
		po, ok := p.(*jsvalue.Object)
		if !ok {
			return codexItem{}, false
		}
		if po.Has("__proto__") {
			po = po.Copy()
			po.Delete("__proto__")
		}
		it.payload = po
	}
	return it, true
}

func (s *codexState) processItem(it codexItem) []*jsvalue.Object {
	ts := tsToMillis(it.timestamp)
	payload := it.payload
	if payload == nil {
		payload = jsvalue.NewObject()
	}
	msg := s.readMessage(it.typ, it.payload)
	if msg != nil && msg.role == "assistant" {
		return []*jsvalue.Object{codexEntry("assistant-message", ts, "markdown", msg.text)}
	}
	if it.typ == "response_item" && msg != nil && msg.role == "user" {
		return []*jsvalue.Object{codexEntry("user-message", ts, "text", msg.text)}
	}
	if it.typ == "event_msg" && prop(payload, "type") == "user_message" && msg == nil {
		return nil
	}
	switch it.typ {
	case "response_item":
		return s.responseItem(payload, ts)
	case "event_msg":
		return s.eventMsg(payload, ts)
	}
	return nil
}

var codexShellNames = map[string]bool{"exec_command": true, "shell": true, "bash": true}

func codexMergeToolResults(entries []*jsvalue.Object) {
	calls := map[string]*jsvalue.Object{}
	for _, e := range entries {
		switch e.String("type") {
		case "tool-call":
			calls[e.String("toolUseId")] = e
		case "tool-result":
			call := calls[e.String("toolUseId")]
			if call == nil {
				continue
			}
			isErr, _ := e.Get("isError")
			if isErr == true {
				call.Set("status", "error")
			} else {
				call.Set("status", "success")
			}
			if sum := e.String("summary"); sum != "" && isErr != true {
				if m := codexLinesRE.FindStringSubmatch(sum); m != nil && codexShellNames[call.String("toolName")] {
					call.Set("summary", call.String("summary")+" → "+m[1]+" lines")
				}
			}
		}
	}
}

type codexLinesResult struct {
	entries      []*jsvalue.Object
	entryOffsets []int64
	summary      *string
	errorCount   int
}

// parseCodexLines turns lines into entries; offsets, when given, are where each line starts.
func parseCodexLines(lines []string, s *codexState, offsets []int64) codexLinesResult {
	var r codexLinesResult
	r.entries = []*jsvalue.Object{}
	for i, raw := range lines {
		if jscompat.Trim(raw) == "" {
			continue
		}
		parsed, ok := parseJSON(raw)
		if !ok {
			r.errorCount++
			continue
		}
		it, ok := validateCodexItem(parsed)
		if !ok {
			r.errorCount++
			continue
		}
		produced := s.processItem(it)
		if r.summary == nil || *r.summary == "" {
			for _, e := range produced {
				if e.String("type") == "user-message" {
					sum := oneLineSummary(e.String("text"), codexSummaryLimit)
					r.summary = &sum
					break
				}
			}
		}
		var off int64
		if offsets != nil && i < len(offsets) {
			off = offsets[i]
		}
		for _, e := range produced {
			r.entries = append(r.entries, e)
			r.entryOffsets = append(r.entryOffsets, off)
		}
	}
	codexMergeToolResults(r.entries)
	return r
}
