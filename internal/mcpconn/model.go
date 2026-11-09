// Package mcpconn links the runtime to the person's remote MCP servers: a catalog of well-known ones and any server
// URL the person pastes. Probe finds how a server signs in (OAuth by the MCP authorization spec, a header the person
// types, or nothing); the OAuth client registers itself, runs the authorization code flow with PKCE and keeps tokens
// fresh. Agents reach the servers through the runtime's proxy, which asks Authorize for the headers of each call.
//
// What is kept: the servers (address, transport, sign-in kind, state, tools) in mcp-connections.json in the runtime's
// data folder, and their credentials in the safe under the name mcp-<id>. No answer and no log line carries a
// credential.
package mcpconn

import (
	"errors"
	"fmt"
	"time"
)

// Transport is how the server speaks MCP.
type Transport string

const (
	// HTTP is streamable HTTP (MCP 2025-03-26 and later).
	HTTP Transport = "http"
	// SSE is the legacy HTTP+SSE transport (MCP 2024-11-05): a GET stream announces where to POST.
	SSE Transport = "sse"
)

// Auth is how the server signs in.
type Auth string

const (
	AuthOAuth  Auth = "oauth"
	AuthHeader Auth = "header"
	AuthNone   Auth = "none"
)

// State is how the server answered last.
type State string

const (
	StateOK          State = "ok"
	StateSignIn      State = "signin"
	StateDenied      State = "denied"
	StateUnreachable State = "unreachable"
)

// Tool is one tool the server offers, as tools/list names it.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Server is one connected MCP server.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug is lowercase [a-z0-9-], unique among the servers; agents see the server as mcp-<slug>.
	Slug      string    `json:"slug"`
	URL       string    `json:"url"`
	Transport Transport `json:"transport"`
	Auth      Auth      `json:"auth"`
	// HeaderName is the header that carries the typed credential (auth "header").
	HeaderName string `json:"headerName,omitempty"`
	// OAuthUnavailable: the server signs in with OAuth but lets no new client register, so it takes a typed token.
	OAuthUnavailable bool   `json:"oauthUnavailable,omitempty"`
	CatalogID        string `json:"catalogId,omitempty"`
	// Owner is "app:<app>/<server>" for a sign-in made for one app's own server (auth: oauth): only that app's calls
	// use it; it is never given to agents and never found by its address or catalog id for anyone else.
	Owner   string `json:"owner,omitempty"`
	Enabled bool   `json:"enabled"`
	State   State  `json:"state"`
	// Me is who or what the server signs in as, when known.
	Me        string    `json:"me,omitempty"`
	Tools     []Tool    `json:"tools,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
}

// ProbeResult is what a pasted URL turned out to be.
type ProbeResult struct {
	// URL is the canonical server URL (lowercase scheme and host, no fragment).
	URL       string    `json:"url"`
	Transport Transport `json:"transport"`
	Auth      Auth      `json:"auth"`
	// Name is the server's own name (serverInfo) when it answered without auth, else one made from the host.
	Name string `json:"name,omitempty"`
	// OAuthUnavailable: the server signs in with OAuth but lets no new client register, so it takes a typed token.
	OAuthUnavailable bool `json:"oauthUnavailable,omitempty"`
	// HeaderName is the header a typed credential goes in (auth "header").
	HeaderName string `json:"headerName,omitempty"`

	oauth *discovery
}

// discovery is where a server's OAuth lives.
type discovery struct {
	// Resource is the RFC 8707 resource indicator: the canonical MCP server URL.
	Resource   string
	AuthServer string
	Authorize  string
	Token      string
	Register   string
	// AuthMethods is token_endpoint_auth_methods_supported.
	AuthMethods []string
	Scope       string
}

// Errors the service answers with; Reason maps them to the words routes answer.
var (
	// ErrDenied: the credentials are wrong or lack a permission.
	ErrDenied = errors.New("denied")
	// ErrUnreachable: the server did not answer or answered something that is not MCP.
	ErrUnreachable = errors.New("unreachable")
	// ErrNotFound: no such server.
	ErrNotFound = errors.New("not-found")
	// ErrBadRequest: the input is not acceptable (a bad URL, an unknown or expired sign-in state).
	ErrBadRequest = errors.New("bad-request")
	// ErrSignInRequired: the server needs the person to sign in (again).
	ErrSignInRequired = errors.New("signin-required")
	// ErrOAuthFailed: the authorization server refused the sign-in or answered something unexpected.
	ErrOAuthFailed = errors.New("oauth-failed")
	// ErrSignInExpired is an ErrSignInRequired of a person who was signed in: the token ran out with no refresh, or the
	// identity provider refused the refresh.
	ErrSignInExpired = fmt.Errorf("%w: the sign-in expired", ErrSignInRequired)
)

// Reason is the short word for an error.
func Reason(err error) string {
	for _, e := range []error{ErrDenied, ErrNotFound, ErrBadRequest, ErrSignInRequired, ErrOAuthFailed, ErrUnreachable} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return ErrUnreachable.Error()
}
