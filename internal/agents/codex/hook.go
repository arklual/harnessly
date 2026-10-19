package codex

import (
	"regexp"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

// Hook events the runtime registers with Codex.
var HookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "PermissionRequest"}

// summaryLimit is how much of a prompt becomes the conversation's live title.
const summaryLimit = 80

// PatchEntry is one file of a patch Codex asks to apply.
type PatchEntry struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Diff      string `json:"diff,omitempty"`
	Content   string `json:"content,omitempty"`
}

// PermissionRequest is what Codex asks the person to approve: a command (ExecApprovalRequest), a patch
// (ApplyPatchApprovalRequest) or extra permissions (RequestPermissions).
type PermissionRequest struct {
	Type        string            `json:"type"`
	Command     string            `json:"command,omitempty"`
	Patches     []PatchEntry      `json:"patches,omitempty"`
	Permissions []string          `json:"permissions,omitempty"`
	CallID      string            `json:"callId,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// Meta is what a hook payload changes on the tab.
type Meta struct {
	// SessionID and JSONLPath are nil when the payload names none (the stored ones stay).
	SessionID *string
	JSONLPath *string
	// LastUserMessage and AgentSummary come with a prompt.
	LastUserMessage *string
	AgentSummary    *string
	// ClearMessages: the session was cleared (/clear), the old summary and messages go.
	ClearMessages bool
	// SetPermission: Permission replaces the pending request (nil clears it).
	SetPermission bool
	Permission    *PermissionRequest
}

// Translation is a Codex hook payload in the runtime's terms.
type Translation struct {
	Meta Meta
	// Event is the work-state event, nil for a hook that names none.
	Event *agents.Event
	// SessionInfo is set by SessionStart, for the session watchers; ClearSession when it was a /clear.
	SessionInfo  *agents.SessionInfo
	ClearSession bool
}

func str(p map[string]any, k string) (string, bool) {
	s, ok := p[k].(string)
	return s, ok
}

// TranslateEvent is the work-state event of a payload's hook_event_name.
func TranslateEvent(payload map[string]any) *agents.Event {
	name, _ := str(payload, "hook_event_name")
	switch name {
	case "SessionStart":
		return &agents.Event{Kind: agents.SessionStart}
	case "UserPromptSubmit", "PreToolUse", "PostToolUse":
		return &agents.Event{Kind: agents.PromptSubmit}
	case "Stop":
		return &agents.Event{Kind: agents.Stop}
	case "PermissionRequest":
		return &agents.Event{Kind: agents.Notification, NotificationType: "permission_prompt"}
	}
	return nil
}

// ProcessHook translates a Codex hook payload (the body Codex pipes to the hook script, passed through verbatim).
func ProcessHook(payload map[string]any) Translation {
	var t Translation
	name, _ := str(payload, "hook_event_name")
	if id, ok := str(payload, "session_id"); ok {
		t.Meta.SessionID = &id
	}
	if p, ok := str(payload, "transcript_path"); ok && p != "" {
		t.Meta.JSONLPath = &p
	}
	if prompt, ok := str(payload, "prompt"); ok && prompt != "" && name == "UserPromptSubmit" {
		summary := jscompat.Slice(prompt, summaryLimit)
		t.Meta.LastUserMessage, t.Meta.AgentSummary = &prompt, &summary
	}
	switch name {
	case "PermissionRequest":
		t.Meta.SetPermission, t.Meta.Permission = true, ParsePermissionRequest(payload)
	case "Stop":
		t.Meta.SetPermission = true
	}
	source, _ := str(payload, "source")
	isClear := name == "SessionStart" && source == "clear"
	t.Meta.ClearMessages = isClear
	if name == "SessionStart" {
		info := agents.SessionInfo{Status: agents.Running}
		if t.Meta.SessionID != nil {
			info.SessionID = *t.Meta.SessionID
		}
		info.JSONLPath, _ = str(payload, "transcript_path")
		info.Cwd, _ = str(payload, "cwd")
		t.SessionInfo = &info
		t.ClearSession = isClear
	}
	t.Event = TranslateEvent(payload)
	return t
}

// ShouldEmit: a SessionStart that is not a /clear only moves a tab that is inactive or unknown (a resumed Codex
// mid-turn is not reset to idle); every other event always goes on.
func ShouldEmit(payload map[string]any, cliState string) bool {
	ev := TranslateEvent(payload)
	if ev == nil {
		return false
	}
	if ev.Kind == agents.SessionStart {
		source, _ := str(payload, "source")
		return source == "clear" || cliState == "inactive" || cliState == "unknown"
	}
	return true
}

func asRecord(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func isObject(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// firstString is `asString(a) ?? asString(b)`: the first that is a string, even an empty one.
func firstString(a, b any) string {
	if s, ok := a.(string); ok {
		return s
	}
	s, _ := b.(string)
	return s
}

func normalizeTool(v any) string {
	s, _ := v.(string)
	return strings.ToLower(jscompat.Trim(s))
}

var fileHeader = regexp.MustCompile(`(?i)^\*\*\*` + jscompat.SpaceClass + `+(Add|Update|Delete)` + jscompat.SpaceClass + `+File:` + jscompat.SpaceClass + `+(.+?)` + jscompat.SpaceClass + `*$`)

func patchOperation(op string) string {
	switch strings.ToLower(op) {
	case "add":
		return "create"
	case "delete":
		return "delete"
	}
	return "modify"
}

func parseApplyPatchCommand(command string) []PatchEntry {
	var out []PatchEntry
	for _, line := range strings.Split(command, "\n") {
		if m := fileHeader.FindStringSubmatch(line); m != nil {
			out = append(out, PatchEntry{Operation: patchOperation(m[1]), Path: m[2], Diff: command})
		}
	}
	if len(out) == 0 {
		return []PatchEntry{{Operation: "modify", Path: "apply_patch", Diff: command}}
	}
	return out
}

func parseExec(p map[string]any) *PermissionRequest {
	ec := asRecord(p["exec_command"])
	command, _ := str(ec, "command")
	if command == "" {
		return nil
	}
	r := &PermissionRequest{Type: "ExecApprovalRequest", Command: command}
	r.CallID = firstString(p["call_id"], ec["call_id"])
	r.Cwd = firstString(ec["cwd"], p["cwd"])
	if env := asRecord(ec["env"]); env != nil {
		out := map[string]string{}
		for k, v := range env {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		if len(out) > 0 {
			r.Env = out
		}
	}
	return r
}

func parseToolExec(p map[string]any) *PermissionRequest {
	tool, input := normalizeTool(p["tool_name"]), asRecord(p["tool_input"])
	if input == nil || (tool != "bash" && tool != "exec_command") {
		return nil
	}
	command, _ := str(input, "command")
	if command == "" {
		return nil
	}
	cwd, _ := str(p, "cwd")
	return &PermissionRequest{Type: "ExecApprovalRequest", Command: command, Cwd: cwd}
}

var patchOperations = map[string]bool{"modify": true, "create": true, "delete": true}

func parsePatchEntry(v any) *PatchEntry {
	o := asRecord(v)
	path, _ := str(o, "path")
	op, _ := str(o, "operation")
	if path == "" || !patchOperations[op] {
		return nil
	}
	e := &PatchEntry{Path: path, Operation: op}
	e.Diff, _ = str(o, "diff")
	e.Content, _ = str(o, "content")
	return e
}

func parseApplyPatch(p map[string]any) *PermissionRequest {
	ap := asRecord(p["apply_patch"])
	raw, ok := ap["patches"].([]any)
	if !ok {
		return nil
	}
	var patches []PatchEntry
	for _, v := range raw {
		if e := parsePatchEntry(v); e != nil {
			patches = append(patches, *e)
		}
	}
	if len(patches) == 0 {
		return nil
	}
	return &PermissionRequest{Type: "ApplyPatchApprovalRequest", Patches: patches, CallID: firstString(p["call_id"], ap["call_id"])}
}

func parseToolApplyPatch(p map[string]any) *PermissionRequest {
	input := asRecord(p["tool_input"])
	if normalizeTool(p["tool_name"]) != "apply_patch" || input == nil {
		return nil
	}
	command, _ := str(input, "command")
	if command == "" {
		return nil
	}
	return &PermissionRequest{Type: "ApplyPatchApprovalRequest", Patches: parseApplyPatchCommand(command)}
}

func parseRequestPermissions(p map[string]any) *PermissionRequest {
	rp, ok := p["requested_permissions"].([]any)
	if !ok {
		return nil
	}
	var perms []string
	for _, v := range rp {
		if s, ok := v.(string); ok && s != "" {
			perms = append(perms, s)
		}
	}
	if len(perms) == 0 {
		return nil
	}
	callID, _ := str(p, "call_id")
	return &PermissionRequest{Type: "RequestPermissions", Permissions: perms, CallID: callID}
}

// ParsePermissionRequest reads what a PermissionRequest hook asks to approve; nil when it cannot be told.
func ParsePermissionRequest(p map[string]any) *PermissionRequest {
	if name, _ := str(p, "hook_event_name"); name != "PermissionRequest" {
		return nil
	}
	explicit, _ := str(p, "request_type")
	execCmd, applyPatch := jscompat.Truthy(p["exec_command"]) || isObject(p["exec_command"]), jscompat.Truthy(p["apply_patch"]) || isObject(p["apply_patch"])
	if explicit == "RequestPermissions" || (explicit == "" && !execCmd && !applyPatch) {
		if r := parseRequestPermissions(p); r != nil {
			return r
		}
	}
	if explicit == "ApplyPatchApprovalRequest" || applyPatch {
		if r := parseApplyPatch(p); r != nil {
			return r
		}
	}
	if explicit == "ExecApprovalRequest" || execCmd {
		if r := parseExec(p); r != nil {
			return r
		}
	}
	if r := parseToolApplyPatch(p); r != nil {
		return r
	}
	return parseToolExec(p)
}
