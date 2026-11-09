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
	// CodeHostNotAllowed: the server's address is no https one and no internal host the deployment lists.
	CodeHostNotAllowed = "host_not_allowed"
	// CodeTLSUntrusted: the server's certificate ends in no root the machine trusts (RUNTIME_APPS_CA_FILE adds some).
	CodeTLSUntrusted = "tls_untrusted"
	// CodeUpstreamError: the server answered, with an HTTP error or with something else than JSON-RPC.
	CodeUpstreamError   = "upstream_error"
	CodeRateLimited     = "rate_limited"
	CodeHostUnavailable = "host_unavailable"
	CodeInvalidParams   = "invalid_params"
	// CodeConflict: a storage write named the version it read (ifVersion) and the key has moved on; the error says the
	// current version and nothing was written.
	CodeConflict = "conflict"
	// CodeQuotaExceeded: a storage write would take its area past its limit.
	CodeQuotaExceeded = "quota_exceeded"
	// CodeAuthRequired: the server wants the person signed in and nobody is (no token, no connection signed in). The
	// error's SignIn says where the person signs in.
	CodeAuthRequired = "auth_required"
	// CodeAuthExpired: the person was signed in and the server takes the sign-in no more (an expired or revoked token,
	// a refresh the identity provider refused). Signing in again mends it.
	CodeAuthExpired = "auth_expired"
	// CodeAuthDenied: the person is signed in and the server (or the identity provider) refuses them this: HTTP 403, an
	// insufficient scope, access_denied. Signing in again does not mend it; an admin of the server does.
	CodeAuthDenied = "auth_denied"
	// CodeIdPUnreachable: the identity provider (its token or discovery endpoint) did not answer, so no token could be
	// had. Nothing is wrong with the sign-in; the call may be repeated later.
	CodeIdPUnreachable = "idp_unreachable"
)

// SignIn says where a refused call is mended by a sign-in: the host shows «Войти» on it. Connection is the catalog id
// of the person's connection the app's server goes through; App and Server name an app's own server that the host signs
// in to (auth: oauth). A view reads it and may not act on it by itself: the sign-in is the host's, in a top window.
type SignIn struct {
	Connection string `json:"connection,omitempty"`
	App        string `json:"app,omitempty"`
	Server     string `json:"server,omitempty"`
}

// authCodes are the codes of a refused sign-in.
var authCodes = map[string]bool{CodeAuthRequired: true, CodeAuthExpired: true, CodeAuthDenied: true, CodeIdPUnreachable: true}

// IsAuthCode says whether a code is one of a refused sign-in.
func IsAuthCode(code string) bool { return authCodes[code] }

// BridgeError is a refusal with the code a view sees.
type BridgeError struct {
	Code    string
	Message string
	// Detail is for the log only: a transport or remote error the page must not read.
	Detail string
	// SignIn, for an auth code, is where the person signs in; nil when no sign-in mends it.
	SignIn *SignIn
}

func (e *BridgeError) Error() string { return e.Code + ": " + e.Message }

func denied(format string, a ...any) error {
	return &BridgeError{Code: CodePermissionDenied, Message: fmt.Sprintf(format, a...)}
}

// methodPermission is the right a method of the bridge needs; "" is a method that needs none. It is made from the
// surface (surface.go).

// ErrUnknownMethod: the bridge has no such method.
var ErrUnknownMethod = errors.New("apps: unknown bridge method")

// RequiredPermission is the right a bridge method needs, and whether the method exists.
func RequiredPermission(method string) (string, bool) {
	p, ok := methodPermission[method]
	return p, ok
}

// Allows says whether the app holds a permission. A right with a value (files.read:docs/*, events:turn) is held when
// the app lists any value of it; which paths or kinds it reaches is for the call's own check.
func (m *Manifest) Allows(permission string) bool {
	if permission == "" {
		return true
	}
	for _, p := range m.Permissions {
		if p == permission || (paramPermissions[permission] && strings.HasPrefix(p, permission+":")) {
			return true
		}
	}
	return false
}

// EventKindsOf are the kinds of events:<kind> the app holds.
func (m *Manifest) EventKindsOf() []string {
	var out []string
	for _, p := range m.Permissions {
		if kind, ok := strings.CutPrefix(p, "events:"); ok {
			out = append(out, kind)
		}
	}
	return out
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
	// Opening a conversation (chat:<id>) shows the person's chats, which is what agents.read is for.
	if method == "harnessly/pane.open" {
		if target, _ := params["target"].(string); strings.HasPrefix(target, "chat:") && !m.Allows("agents.read") {
			return denied("%s of %s needs the permission agents.read", method, "chat:<id>")
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
