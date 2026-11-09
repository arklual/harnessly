package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The platform's token for an app's backend (docs/build-apps/10-auth.md): a server with auth platform or oidc gets
// "Authorization: Bearer <token>" on every call the host makes for the app — a view's hl.tools.call and the agent's
// calls through the MCP proxy. The token comes from the hub (the machine agent asks it, signed with the machine's key);
// a machine with no hub signs a platform token with its own key, whose JWKS it writes next to the key for the backend's
// admin to copy. The view and the agent never see a token.

// appTokenSkew: a cached token is renewed this long before it expires, so none reaches a backend about to run out.
const appTokenSkew = time.Minute

type hubTokens interface {
	Token(req hubproto.AppTokenRequest) (agent.AppToken, error)
}

type appTokenSource struct {
	hub     hubTokens
	dataDir string
	now     func() time.Time

	mu     sync.Mutex
	cache  map[string]agent.AppToken
	local  *apptoken.Signer
	noHub  bool
	issuer string
}

var (
	appTokensMu  sync.Mutex
	appTokensFor = map[*gate.CLIToken]*appTokenSource{}
)

// appTokensOf is the one token source of a runtime.
func appTokensOf(d deps) *appTokenSource {
	appTokensMu.Lock()
	defer appTokensMu.Unlock()
	if s := appTokensFor[d.CLI]; s != nil {
		return s
	}
	s := newAppTokenSource(agent.LocalAppTokens{DataDir: d.DataDir, TokenHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")}, d.DataDir)
	appTokensFor[d.CLI] = s
	return s
}

func newAppTokenSource(hub hubTokens, dataDir string) *appTokenSource {
	host, _ := os.Hostname()
	return &appTokenSource{hub: hub, dataDir: dataDir, now: time.Now, cache: map[string]agent.AppToken{},
		issuer: "urn:harnessly:machine:" + host}
}

// Token is the token of kind (apps.AuthPlatform, apps.AuthOIDC) for app's server and audience; fresh skips the cache
// (the backend refused the cached one). A refusal is a *apps.BridgeError with the code a view and the agent read.
func (s *appTokenSource) Token(kind, app, server, origin, audience, scope string, fresh bool) (string, error) {
	if kind == apps.AuthPlatform {
		audience = origin
	}
	if audience == "" || origin == "" {
		return "", &apps.BridgeError{Code: apps.CodeInvalidParams, Message: "the server has no audience for its token: give auth.audience or a url"}
	}
	key := kind + "\x00" + app + "\x00" + server + "\x00" + origin + "\x00" + audience + "\x00" + scope
	now := s.now()
	s.mu.Lock()
	if t, ok := s.cache[key]; ok && !fresh && t.ExpiresAt.After(now.Add(appTokenSkew)) {
		s.mu.Unlock()
		return t.Token, nil
	}
	noHub := s.noHub
	s.mu.Unlock()

	var tok agent.AppToken
	var err error
	if noHub {
		tok, err = s.mintLocal(kind, app, server, audience)
	} else {
		tok, err = s.hub.Token(hubproto.AppTokenRequest{Kind: kind, App: app, Server: server, Audience: audience, Origin: origin, Scope: scope})
		var te *agent.AppTokenError
		if errors.As(err, &te) && te.Code == agent.AppTokenNoHub {
			s.mu.Lock()
			s.noHub = true
			s.mu.Unlock()
			tok, err = s.mintLocal(kind, app, server, audience)
		}
	}
	if err != nil {
		return "", tokenRefusal(err)
	}
	s.mu.Lock()
	s.cache[key] = tok
	s.mu.Unlock()
	return tok.Token, nil
}

// mintLocal signs a platform token with the machine's own key: there is no hub to sign it or to exchange it.
func (s *appTokenSource) mintLocal(kind, app, server, audience string) (agent.AppToken, error) {
	if kind != apps.AuthPlatform {
		return agent.AppToken{}, &agent.AppTokenError{Code: hubproto.AppTokenOff, Err: errors.New("a machine without a hub exchanges no tokens")}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.local == nil {
		key, err := apptoken.LoadOrCreateKey(filepath.Join(s.dataDir, "app-token.pem"))
		if err != nil {
			return agent.AppToken{}, err
		}
		signer, err := apptoken.NewSigner(s.issuer, key)
		if err != nil {
			return agent.AppToken{}, err
		}
		// The backend's admin copies this file: the machine publishes no keys on the network.
		_ = os.WriteFile(filepath.Join(s.dataDir, "app-token.jwks.json"), signer.JWKS(), 0o644)
		signer.SetClock(s.now)
		s.local = signer
	}
	user, _ := os.UserHomeDir()
	sub := s.issuer + ":" + filepath.Base(user)
	raw, c, err := s.local.Mint(apptoken.Claims{Subject: sub, Audience: audience, App: app, Server: server}, apptoken.TTL)
	if err != nil {
		return agent.AppToken{}, err
	}
	return agent.AppToken{Token: raw, ExpiresAt: c.Expiry}, nil
}

// tokenRefusal maps a token that could not be had to the code a view reads.
func tokenRefusal(err error) error {
	var te *agent.AppTokenError
	if !errors.As(err, &te) {
		return &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: "app token: " + err.Error()}
	}
	switch te.Code {
	case hubproto.AppTokenDenied:
		return &apps.BridgeError{Code: apps.CodeAuthDenied, Message: apps.AuthDeniedMessage, Detail: te.Error()}
	case hubproto.AppTokenIdPUnreachable:
		return &apps.BridgeError{Code: apps.CodeIdPUnreachable, Message: apps.IdPUnreachableMessage, Detail: te.Error()}
	case hubproto.AppTokenOff:
		return &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "this deployment gives apps no token of that kind: its admin sets it up (HUB_APP_TOKEN_*)", Detail: te.Error()}
	}
	return &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "the hub did not answer for the app's token", Detail: te.Error()}
}

// withAppToken is the server with the platform's token of its auth in Authorization; srv unchanged when it has none.
func withAppToken(ctx context.Context, tokens *appTokenSource, app, server string, decl, srv apps.MCPServer, fresh bool) (apps.MCPServer, error) {
	_ = ctx
	a := decl.Auth
	if a == nil || (a.Type != apps.AuthPlatform && a.Type != apps.AuthOIDC) {
		return srv, nil
	}
	// The token is for the address the call goes to: srv.URL is where the host sends it.
	tok, err := tokens.Token(a.Type, app, server, srv.Origin(), decl.AudienceFor(), a.Scope, fresh)
	if err != nil {
		return srv, err
	}
	headers := make(map[string]string, len(srv.Headers)+1)
	for k, v := range srv.Headers {
		headers[k] = v
	}
	headers["Authorization"] = "Bearer " + tok
	srv.Headers, srv.Credentialed = headers, true
	return srv, nil
}

// callWithAppToken runs a tool of a server with auth platform or oidc: once with the cached token, and once more with
// a fresh one when the backend takes the cached one no more.
func callWithAppToken(ctx context.Context, tokens *appTokenSource, app, server string, decl, srv apps.MCPServer, tool string, args map[string]any) (json.RawMessage, error) {
	signed, err := withAppToken(ctx, tokens, app, server, decl, srv, false)
	if err != nil {
		return nil, err
	}
	out, err := apps.CallTool(ctx, signed, tool, args)
	var be *apps.BridgeError
	if errors.As(err, &be) && be.Code == apps.CodeAuthExpired {
		if signed, err = withAppToken(ctx, tokens, app, server, decl, srv, true); err != nil {
			return nil, err
		}
		out, err = apps.CallTool(ctx, signed, tool, args)
	}
	return out, err
}
