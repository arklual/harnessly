package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The platform's token for apps' backends (docs/build-apps/10-auth.md). Why the hub signs, not the machine: the hub
// knows who the person is (the machine's owner, signed in by the organization's SSO) and which organization and team
// they are in; one key per deployment means a backend trusts one JWKS, not one per machine; and the hub holds the
// client at the identity provider that token exchange needs. A machine never holds the key.

// appTokens is the hub's signer and its exchange client; nil when HUB_APP_TOKEN=off.
type appTokens struct {
	signer   *apptoken.Signer
	cfg      config.AppToken
	client   *http.Client
	audience string
}

func newAppTokens(cfg config.AppToken, issuer string, client *http.Client) (*appTokens, error) {
	if !cfg.On() {
		return nil, nil
	}
	key, err := apptoken.LoadOrCreateKey(cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("HUB_APP_TOKEN_KEY_FILE: %w", err)
	}
	var previous []ed25519.PublicKey
	for _, f := range cfg.PreviousKeys {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("HUB_APP_TOKEN_PREVIOUS_KEYS: %w", err)
		}
		pub, err := apptoken.ParsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("HUB_APP_TOKEN_PREVIOUS_KEYS %s: %w", f, err)
		}
		previous = append(previous, pub)
	}
	s, err := apptoken.NewSigner(issuer, key, previous...)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &appTokens{signer: s, cfg: cfg, client: client, audience: cfg.ExchangeAudience}, nil
}

// jwks is GET /.well-known/jwks.json: the public keys a backend checks the platform's tokens with. Public, cached.
func (h *Hub) jwks(w http.ResponseWriter, _ *http.Request) {
	if h.appTokens == nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "application/jwk-set+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(h.appTokens.signer.JWKS())
}

// discovery is GET /.well-known/openid-configuration: the issuer and its keys, for a backend that checks the platform's
// tokens with an OIDC library. The hub signs no ID tokens for sign-in; the document only names the issuer, the keys and
// the one algorithm. Public, cached; 404 while the hub issues no tokens.
func (h *Hub) discovery(w http.ResponseWriter, _ *http.Request) {
	if h.appTokens == nil {
		http.NotFound(w, nil)
		return
	}
	iss := h.appTokens.signer.Issuer()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                iss,
		"jwks_uri":                              iss + apptoken.JWKSPath,
		"id_token_signing_alg_values_supported": []string{"EdDSA"},
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
	})
}

var (
	errAppTokenOff = httpx.New(http.StatusConflict, hubproto.AppTokenOff, "Хаб не выдаёт токены приложениям")
	errExchangeOff = httpx.New(http.StatusConflict, hubproto.AppTokenOff, "Обмен токена на токен провайдера входа не настроен")
)

// agentAppToken is POST /agent/app-token: a machine asks a token for its owner, for an app's backend. The answer is
// sealed to the machine's key; the journal says who got a token for which app and audience, never the token.
func (h *Hub) agentAppToken(w http.ResponseWriter, r *http.Request) error {
	if h.appTokens == nil {
		return errAppTokenOff
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, hubproto.MaxAppTokenBody+1))
	if err != nil || len(body) > hubproto.MaxAppTokenBody {
		return httpx.Invalid("body", "Некорректный запрос")
	}
	m, err := h.Dialer.AdmitAppToken(r, body)
	if err != nil {
		return err
	}
	var req hubproto.AppTokenRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Audience == "" || req.App == "" || req.Origin == "" {
		return httpx.Invalid("body", "Нужны приложение, аудитория и адрес сервера")
	}
	if req.Kind != hubproto.AppTokenPlatform && req.Kind != hubproto.AppTokenOIDC {
		return httpx.Invalid("kind", "Вид токена: platform или oidc")
	}
	ctx := r.Context()
	u, err := h.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil {
		return err
	}
	sp, err := h.db.S().SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	at := h.appTokens
	claims := apptoken.Claims{Subject: u.ID, Email: u.Email, Name: u.Name, Org: sp.OrgID, Space: sp.ID, App: req.App,
		Server: req.Server, Machine: m.ID}
	var token string
	var expires time.Time
	switch req.Kind {
	case hubproto.AppTokenPlatform:
		// A platform token is good for the server it is sent to, and nothing else.
		if !strings.EqualFold(req.Audience, req.Origin) {
			return httpx.Invalid("audience", "Аудитория токена платформы — адрес самого сервера")
		}
		claims.Audience = req.Origin
		var c apptoken.Claims
		if token, c, err = at.signer.Mint(claims, apptoken.TTL); err != nil {
			return err
		}
		expires = c.Expiry
	case hubproto.AppTokenOIDC:
		if at.cfg.Exchange.TokenURL == "" {
			return errExchangeOff
		}
		// The admin lists which provider audience may go to which backend: an app cannot ask the bank's token for one
		// backend and have the host send it to its own address.
		if allowed, ok := at.cfg.ExchangeAudiences[req.Audience]; !ok || !strings.EqualFold(allowed, req.Origin) {
			h.log.Info("app token: an audience the admin did not allow", "machine", m.ID, "app", req.App, "audience", req.Audience, "origin", req.Origin)
			return httpx.New(http.StatusForbidden, hubproto.AppTokenDenied, "Администратор хаба не разрешил эту аудиторию для этого сервера")
		}
		// The provider is handed the platform's token for itself, a minute long, and answers its own for the backend.
		claims.Audience = at.audience
		subject, _, err := at.signer.Mint(claims, time.Minute)
		if err != nil {
			return err
		}
		scope := req.Scope
		if scope == "" {
			scope = at.cfg.ExchangeScope
		}
		x, err := apptoken.Exchange(ctx, at.client, at.cfg.Exchange, subject, req.Audience, scope, time.Now())
		switch {
		case errors.Is(err, apptoken.ErrExchangeDenied):
			h.log.Info("app token: the identity provider refused the exchange", "machine", m.ID, "app", req.App, "audience", req.Audience, "err", err)
			return httpx.New(http.StatusForbidden, hubproto.AppTokenDenied, "Провайдер входа отказал в токене для этого приложения")
		case err != nil && !errors.Is(err, apptoken.ErrExchangeMisconfig):
			h.log.Warn("app token: the identity provider did not answer", "machine", m.ID, "err", err)
			return httpx.New(http.StatusBadGateway, hubproto.AppTokenIdPUnreachable, "Провайдер входа не ответил")
		case err != nil:
			return errExchangeOff
		}
		token, expires = x.AccessToken, x.ExpiresAt
		if expires.IsZero() {
			expires = time.Now().Add(apptoken.TTL)
		}
	}
	h.log.Info("app token", "machine", m.ID, "user", u.ID, "kind", req.Kind, "app", req.App, "audience", req.Audience)
	encPub, err := base64.StdEncoding.DecodeString(m.EncPub)
	if err != nil {
		return httpx.New(http.StatusConflict, "no_key", "У машины нет ключа для запечатывания")
	}
	auth, _ := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
	sealed, err := hubproto.SealSecret(encPub, m.ID, auth.Nonce, []byte(token))
	if err != nil {
		return httpx.New(http.StatusRequestEntityTooLarge, "too_large", "Токен не помещается")
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, hubproto.AppTokenAnswer{Sealed: sealed, ExpiresAt: expires.Unix()})
	return nil
}
