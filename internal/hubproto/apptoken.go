package hubproto

import (
	"crypto/ed25519"
	"time"
)

// The platform's token for an app's backend (docs/build-apps/10-auth.md): the runtime asks its machine agent, the agent
// signs the request with the machine's key and posts it to AppTokenPath, and the hub mints a short JWT for the machine's
// owner with its own key (published at /.well-known/jwks.json), or trades it at the organization's identity provider for
// the provider's token (RFC 8693). The token comes back sealed to the machine's X25519 key, like a secret.
const (
	// AppTokenDomain is the first line of the message signed for a token request.
	AppTokenDomain = "harnessly-agent-app-token-v1"
	// AppTokenPath is where the hub answers it.
	AppTokenPath = "/agent/app-token"
	// MaxAppTokenBody bounds the request.
	MaxAppTokenBody = 4 << 10
)

// The kinds of token a request asks for.
const (
	// AppTokenPlatform is the hub's own JWT for the audience.
	AppTokenPlatform = "platform"
	// AppTokenOIDC is the organization's identity provider's token for the audience, got by token exchange.
	AppTokenOIDC = "oidc"
)

// The refusals of the hub, as the error code of its answer.
const (
	// AppTokenOff: the hub mints no tokens for apps (no key configured), or no exchange for oidc.
	AppTokenOff = "app_token_off"
	// AppTokenDenied: the identity provider refused the exchange.
	AppTokenDenied = "app_token_denied"
	// AppTokenIdPUnreachable: the identity provider did not answer.
	AppTokenIdPUnreachable = "idp_unreachable"
)

// AppTokenRequest is one token request: for the app and its server, good for the audience (the backend).
type AppTokenRequest struct {
	Kind     string `json:"kind"`
	App      string `json:"app"`
	Server   string `json:"server,omitempty"`
	Audience string `json:"audience"`
	// Origin is the origin of the server the token is sent to. A platform token's audience is it; an oidc audience must
	// be one the hub's admin allowed for it.
	Origin string `json:"origin"`
	Scope  string `json:"scope,omitempty"`
}

// AppTokenAnswer is the token sealed to the machine, and when it expires (unix seconds).
type AppTokenAnswer struct {
	Sealed    []Envelope `json:"sealed"`
	ExpiresAt int64      `json:"expiresAt"`
}

// SignAppToken makes the Authorization header value of a token request with this body.
func SignAppToken(priv ed25519.PrivateKey, machineID string, now time.Time, host string, body []byte) (string, error) {
	return signHeader(AppTokenDomain, priv, machineID, now, secretsHost(host, body))
}

// VerifyAppToken is VerifyDial for the header SignAppToken made over body.
func VerifyAppToken(pub ed25519.PublicKey, a DialAuth, host string, now time.Time, body []byte) error {
	return verifyHeader(AppTokenDomain, pub, a, secretsHost(host, body), now)
}
