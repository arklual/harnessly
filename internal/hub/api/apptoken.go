package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
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

func newAppTokens(cfg config.AppToken, issuer string, client *http.Client, shared *sharedKey) (*appTokens, error) {
	if !cfg.On() {
		return nil, nil
	}
	key, err := loadAppTokenKey(cfg.KeyFile, shared)
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

// sharedKey is what the replicas of a hub (HUB_REPLICAS=on) must agree on: they sign and seal for each other, and a
// key a replica made for itself would be one the others do not know.
type sharedKey struct {
	// secret is HUB_SECRET_KEY; empty when the hub keeps its key in a file.
	secret string
}

// loadAppTokenKey is the key the hub signs apps' tokens with: the file when it is there; otherwise, for a replica, one
// derived from HUB_SECRET_KEY (the same on every replica), and for a single hub a new one written to the file.
func loadAppTokenKey(path string, shared *sharedKey) (ed25519.PrivateKey, error) {
	if shared == nil {
		return apptoken.LoadOrCreateKey(path)
	}
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return apptoken.LoadOrCreateKey(path)
	}
	if shared.secret == "" {
		return nil, fmt.Errorf("%s is missing: with HUB_REPLICAS=on every replica must sign with the same key; give each the same file (a Secret) or set HUB_SECRET_KEY", path)
	}
	seed := sha256.Sum256([]byte("harnessly-hub/app-token/v1\x00" + shared.secret))
	return ed25519.NewKeyFromSeed(seed[:]), nil
}

// replicaKeyCheck refuses to start a replica that would seal with a key of its own: without HUB_SECRET_KEY the key is
// the file secret_key of HUB_DATA_DIR, and a replica without that file would make a new one (docs/hub/replicas.md).
func replicaKeyCheck(cfg *config.Config) error {
	if cfg.SecretKey != "" {
		return nil
	}
	path := filepath.Join(cfg.DataDir, sshinstall.SecretKeyFile)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("HUB_REPLICAS=on without HUB_SECRET_KEY needs %s, the same on every replica (a Secret mounted there): %w", path, err)
	}
	return nil
}

// appTokenSubject checks a platform token of a person the hub signed for an app's backend at audience and answers the
// person's id: what lets the documents and boards services act for the person who opened them (docs/manage.go).
func (h *Hub) appTokenSubject(token, audience string) (string, error) {
	if h.appTokens == nil {
		return "", errors.New("app tokens are off")
	}
	c, err := apptoken.Verify(token, h.appTokens.signer.JWKS(), h.appTokens.signer.Issuer(), audience, time.Now())
	if err != nil {
		return "", err
	}
	return c.Subject, nil
}
