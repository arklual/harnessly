package wire

import "slices"

// PermissionMode is how an agent asks before it acts, as the chat's picker offers it (HRN-945):
//
//   - plan: it plans and changes nothing until the plan is accepted (Claude Code's `plan`, Codex's Plan mode);
//   - ask: it asks before edits and commands (Claude Code's `default`, Codex's `untrusted` approvals);
//   - bypass: it asks nothing (Claude Code's `bypassPermissions`, Codex's `--yolo`).
type PermissionMode string

const (
	PermissionPlan   PermissionMode = "plan"
	PermissionAsk    PermissionMode = "ask"
	PermissionBypass PermissionMode = "bypass"
)

// PermissionModes are the modes in the picker's order.
var PermissionModes = []PermissionMode{PermissionPlan, PermissionAsk, PermissionBypass}

// Known: one of PermissionModes.
func (p PermissionMode) Known() bool { return slices.Contains(PermissionModes, p) }
