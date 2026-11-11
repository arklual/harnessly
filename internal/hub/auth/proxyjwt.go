package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// proxyProvider is the identity provider name of people a gateway vouches for (config.ProxyJWT).
const proxyProvider = "proxy"

// proxyAccountTTL is how long an assertion's address stays mapped to its account before the account rules run again.
const proxyAccountTTL = 5 * time.Minute

// proxySigningAlgs are the asymmetric algorithms a gateway may sign with: Teleport signs its assertions with ES256.
var proxySigningAlgs = []string{oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512, oidc.EdDSA}

// proxyGate checks the assertions of the gateway in front of the hub: the keys are fetched once and refreshed when a
// token names a key the hub has not seen.
type proxyGate struct {
	verifier *oidc.IDTokenVerifier
	mu       sync.Mutex
	accounts map[string]proxyAccount
}

type proxyAccount struct {
	userID string
	at     time.Time
}

func (s *Service) initProxyJWT() {
	p := s.cfg.ProxyJWT
	if !p.On() {
		return
	}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), s.client), p.JWKSURL)
	s.proxy = &proxyGate{
		verifier: oidc.NewVerifier(p.Issuer, keys, &oidc.Config{ClientID: p.Audience, SkipIssuerCheck: p.Issuer == "", Now: s.now, SupportedSigningAlgs: proxySigningAlgs}),
		accounts: map[string]proxyAccount{},
	}
}

// ProxiedHost says whether r came through the gateway that vouches for its people: on that address the hub signs
// nobody in by a cookie and sets none.
func (s *Service) ProxiedHost(r *http.Request) bool {
	return s.proxy != nil && strings.EqualFold(r.Host, s.cfg.ProxyJWT.Host)
}

// proxyInAuthorization says whether the gateway's assertion travels in the Authorization header of r: the gateway adds
// it to every request of the browser, a page of another site's among them, so it proves nothing about who made it.
func (s *Service) proxyInAuthorization(r *http.Request) bool {
	return s.ProxiedHost(r) && strings.EqualFold(s.cfg.ProxyJWT.Header, "authorization")
}

// errProxyAssertion is an assertion that is missing, forged, expired or names no address.
var errProxyAssertion = errors.New("the gateway's assertion does not hold")

// proxyPrincipal is the person the gateway's assertion names. A request without a valid one is nobody: the gateway
// lets no one through without it, so a missing assertion means the request did not come through it.
func (s *Service) proxyPrincipal(r *http.Request) (*Principal, error) {
	raw := r.Header.Get(s.cfg.ProxyJWT.Header)
	if raw == "" {
		return nil, nil
	}
	ctx := r.Context()
	email, err := s.proxyEmail(ctx, raw)
	if err != nil {
		s.log.Debug("proxy assertion refused", "error", err)
		return nil, nil
	}
	u, err := s.proxyUser(ctx, r, email)
	if err != nil || u == nil {
		return nil, err
	}
	if u.DisabledAt != nil {
		return nil, nil
	}
	admin, err := s.adminFor(ctx, s.db.S(), *u)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	return &Principal{User: *u, Session: store.Session{Kind: KindHub, UserID: u.ID, CreatedAt: now, ExpiresAt: now, LastSeenAt: now, IP: s.ClientIP(r)}, Admin: admin}, nil
}

func (s *Service) proxyEmail(ctx context.Context, raw string) (string, error) {
	tok, err := s.proxy.verifier.Verify(oidc.ClientContext(ctx, s.client), raw)
	if err != nil {
		return "", err
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return "", err
	}
	if nbf, ok := claims["nbf"].(float64); ok && s.now().Before(time.Unix(int64(nbf), 0).Add(-time.Minute)) {
		return "", errProxyAssertion
	}
	value, _ := claims[s.cfg.ProxyJWT.EmailClaim].(string)
	email, ok := NormalizeEmail(value)
	if !ok {
		return "", errProxyAssertion
	}
	return email, nil
}

// proxyUser is the account of the address the gateway vouches for, made by the hub's own account rules on first sight
// (HUB_SIGNUP applies: in invite mode a stranger gets no account). The mapping is kept for a few minutes so that a page
// of requests runs the rules once.
func (s *Service) proxyUser(ctx context.Context, r *http.Request, email string) (*store.User, error) {
	now := s.now()
	s.proxy.mu.Lock()
	acc, ok := s.proxy.accounts[email]
	s.proxy.mu.Unlock()
	if ok && now.Sub(acc.at) < proxyAccountTTL {
		u, err := s.db.S().UserByID(ctx, acc.userID)
		if err == nil && u.Email == email {
			return &u, nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	u, isNew, err := s.resolve(ctx, identityInfo{Provider: proxyProvider, Subject: email, Email: email, EmailVerified: true}, resolveOpts{})
	if err != nil {
		var refused *httpx.Error
		if errors.As(err, &refused) {
			s.log.Info("proxy sign-in refused", "error", err)
			return nil, nil
		}
		return nil, err
	}
	s.proxy.mu.Lock()
	if len(s.proxy.accounts) > 10_000 {
		clear(s.proxy.accounts)
	}
	s.proxy.accounts[email] = proxyAccount{userID: u.ID, at: now}
	s.proxy.mu.Unlock()
	if isNew || !ok {
		_ = s.db.S().TouchLogin(ctx, u.ID, now.UTC())
		s.Audit(ctx, u.ID, "login", r, proxyProvider)
	}
	return &u, nil
}
