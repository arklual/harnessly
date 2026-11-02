package apps

import (
	"errors"
	"fmt"
	"strings"
)

// The host↔view bridge speaks MCP Apps (JSON-RPC 2.0 over postMessage) plus the host's own methods under harnessly/.
// The page's bridge (web/app/next/features/apps/bridge.ts) holds the same table; the runtime holds it too, because the
// calls that reach the machine (storage, tools) come to its routes and are checked there once more.

// Error codes a view sees as HarnesslyError.code.
const (
	CodePermissionDenied = "permission_denied"
	CodeNotInTarget      = "not_in_target"
	CodePolicyBlocked    = "policy_blocked"
	CodeNetworkDenied    = "network_denied"
	CodeRateLimited      = "rate_limited"
	CodeHostUnavailable  = "host_unavailable"
	CodeInvalidParams    = "invalid_params"
)

// BridgeError is a refusal with the code a view sees.
type BridgeError struct {
	Code    string
	Message string
	// Detail is for the log only: a transport or remote error the page must not read.
	Detail string
}

func (e *BridgeError) Error() string { return e.Code + ": " + e.Message }

func denied(format string, a ...any) error {
	return &BridgeError{Code: CodePermissionDenied, Message: fmt.Sprintf(format, a...)}
}

// methodPermission is the right a method of the bridge needs; "" is a method that needs none.
var methodPermission = map[string]string{
	"ui/initialize":                 "",
	"ui/notifications/initialized":  "",
	"ui/notifications/size-changed": "",
	"ui/open-link":                  "",
	"tools/call":                    "",
	"ui/message":                    "chat.prompt",
	"harnessly/chat.read":           "chat.read",
	"harnessly/chat.card":           "chat.card",
	"harnessly/pane.open":           "pane.open",
	"harnessly/storage.get":         "storage",
	"harnessly/storage.set":         "storage",
	"harnessly/storage.delete":      "storage",
	"harnessly/storage.keys":        "storage",
	"harnessly/notify":              "notify",
	"harnessly/files.read":          "files.read",
	"harnessly/files.write":         "files.write",
	"harnessly/flow.status":         "",
	"harnessly/flow.go":             "",
	"harnessly/data.get":            "",
	"harnessly/data.set":            "",
	"harnessly/ui.toast":            "",
	"harnessly/ui.confirm":          "",
	"harnessly/sinopia.document":    "sinopia.read",
	"harnessly/sinopia.selection":   "sinopia.read",
	"harnessly/sinopia.node":        "sinopia.read",
	"harnessly/sinopia.query":       "sinopia.read",
	"harnessly/sinopia.fields":      "sinopia.read",
	"harnessly/sinopia.mark":        "sinopia.read",
	"harnessly/sinopia.patch":       "sinopia.write",
	"harnessly/sinopia.setFields":   "sinopia.write",
	"harnessly/sinopia.boards.add":  "sinopia.write",
	"harnessly/sinopia.render":      "sinopia.export",
	"harnessly/sinopia.comment":     "sinopia.comment",
}

// ErrUnknownMethod: the bridge has no such method.
var ErrUnknownMethod = errors.New("apps: unknown bridge method")

// RequiredPermission is the right a bridge method needs, and whether the method exists.
func RequiredPermission(method string) (string, bool) {
	p, ok := methodPermission[method]
	return p, ok
}

// Allows says whether the app holds a permission. files.read and files.write are held in the manifest with a mask
// (files.read:docs/*): the app holds the right when it lists any; which paths it reaches is FileScope's to judge.
func (m *Manifest) Allows(permission string) bool {
	if permission == "" {
		return true
	}
	for _, p := range m.Permissions {
		if p == permission || ((permission == "files.read" || permission == "files.write") && strings.HasPrefix(p, permission+":")) {
			return true
		}
	}
	return false
}

// Authorize checks a bridge call of an app's view: the method exists, the app holds its permission and the view is
// shown in a target the app declared. Storage scope "space" writes need the right of their own.
func (m *Manifest) Authorize(method, target string, params map[string]any) error {
	need, ok := methodPermission[method]
	if !ok {
		return ErrUnknownMethod
	}
	// A call that names no target is refused, not let through: the target comes from the view's session.
	if target == "" {
		return &BridgeError{Code: CodeInvalidParams, Message: "no target"}
	}
	if !m.HasTarget(target) {
		return &BridgeError{Code: CodeNotInTarget, Message: "the app does not work in " + target}
	}
	if !m.Allows(need) {
		return denied("%s needs the permission %s", method, need)
	}
	if scope, _ := params["scope"].(string); scope == string(ScopeSpace) && (method == "harnessly/storage.set" || method == "harnessly/storage.delete") {
		if !m.Allows("storage.space.write") {
			return denied("%s on the space needs the permission storage.space.write", method)
		}
	}
	return nil
}

// CanCallTool says whether a view may call a tool by its name "<server>.<tool>": only the servers of the app's own
// package, never another app's or the host's.
func (p *Package) CanCallTool(name string) (server, tool string, err error) {
	server, tool, ok := strings.Cut(name, ".")
	if !ok || server == "" || tool == "" {
		return "", "", &BridgeError{Code: CodeInvalidParams, Message: `a tool is named "<server>.<tool>"`}
	}
	if _, has := p.MCP[server]; !has {
		return "", "", &BridgeError{Code: CodePolicyBlocked, Message: "the app has no MCP server " + server}
	}
	return server, tool, nil
}
