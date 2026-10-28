package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const oauthTTL = 10 * time.Minute

// oauthCookieName is the cookie that ties an OAuth flow to the browser that started it. On https it is
// `__Host-hl_oauth` (Secure, Path=/, no Domain), for the reason of RunCookieName: a page of another origin of the same
// site, a machine's, could otherwise plant its own state in a person's browser, send them to the callback of a flow it
// started, and sign them in to its account (login CSRF). The flow's address is bound by the state row in the database,
// not by the cookie's path, which the prefix does not allow to narrow.
func (s *Service) oauthCookieName() string {
	if s.cfg.Secure {
		return "__Host-hl_oauth"
	}
	return "hl_oauth"
}

// oauthProvider is one way to sign in through someone else's login page. The OAuth plumbing (state, PKCE, the
// cookie, the exchange) is shared; what differs is where the endpoints are and how the person is read.
type oauthProvider struct {
	// route is the name in the URL: github, yandex, oidc-<id>.
	route string
	// config returns the OAuth client with its endpoints; OpenID providers find them by discovery, lazily.
	config func(ctx context.Context) (*oauth2.Config, error)
	// authOptions are extra parameters of the authorization URL (the OpenID nonce).
	authOptions func(nonce string) []oauth2.AuthCodeOption
	// identity reads the person once the code is exchanged.
	identity func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token, nonce string) (identityInfo, error)
}

// Endpoints overrides where the fixed providers live; the zero value is the real ones. Tests point it at fakes.
type Endpoints struct {
	YandexAuth, YandexToken, YandexInfo string
}

func (s *Service) initOAuth() {
	if s.cfg.GitHubOn() {
		s.addOAuth(s.githubProvider())
	}
	if s.cfg.YandexOn() {
		s.addOAuth(s.yandexProvider())
	}
	for _, p := range s.cfg.OIDC {
		s.addOAuth(s.oidcProvider(p.ID))
	}
}

func (s *Service) addOAuth(p *oauthProvider) {
	s.oauth[p.route] = p
	s.oauthOr = append(s.oauthOr, p.route)
}

func (s *Service) callbackURL(route string) string {
	return s.cfg.PublicURL + "/api/auth/oauth/" + route + "/callback"
}

type oauthState struct {
	Provider string `json:"provider"`
	Intent   string `json:"intent"`
	Next     string `json:"next,omitempty"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce,omitempty"`
	UserID   string `json:"userId,omitempty"`
}

// redirectError sends the browser back to the page that started the flow with an error code to show.
func (s *Service) redirectError(w http.ResponseWriter, r *http.Request, intent, code string) {
	target := "/signin"
	if intent == "link" {
		target = "/settings"
	}
	http.Redirect(w, r, target+"?error="+url.QueryEscape(code), http.StatusFound)
}

func (s *Service) clearOAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.oauthCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// oauthStart sends the browser to the provider.
func (s *Service) oauthStart(w http.ResponseWriter, r *http.Request) {
	route := r.PathValue("provider")
	intent := r.URL.Query().Get("intent")
	switch intent {
	case "signin", "signup", "link":
	case "":
		intent = "signin"
	default:
		s.redirectError(w, r, "", "oauth_failed")
		return
	}
	p := s.oauth[route]
	if p == nil {
		s.redirectError(w, r, intent, "provider_off")
		return
	}
	if err := s.limitSignin(r); err != nil {
		s.redirectError(w, r, intent, "rate_limited")
		return
	}
	ctx := r.Context()
	st := oauthState{Provider: route, Intent: intent, Next: SafeNext(r.URL.Query().Get("next")), Verifier: oauth2.GenerateVerifier()}
	if intent == "link" {
		pr, err := s.Authenticate(r)
		if err != nil || pr == nil {
			s.redirectError(w, r, intent, "expired")
			return
		}
		st.UserID = pr.User.ID
		if st.Next == "" {
			st.Next = "/settings"
		}
	}
	cfg, err := p.config(s.httpCtx(ctx))
	if err != nil {
		s.log.Warn("oauth provider not reachable", "provider", route, "error", err)
		s.redirectError(w, r, intent, "oauth_failed")
		return
	}
	if p.authOptions != nil {
		st.Nonce = newToken()
	}
	state := newToken()
	raw, _ := json.Marshal(st)
	now := s.now().UTC()
	if err := s.db.S().CreateChallenge(ctx, store.Challenge{ID: store.HashToken(state), Kind: "oauth", Data: string(raw), ExpiresAt: now.Add(oauthTTL), CreatedAt: now, IP: s.ClientIP(r)}); err != nil {
		s.log.Error("oauth state not stored", "error", err)
		s.redirectError(w, r, intent, "oauth_failed")
		return
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(st.Verifier)}
	if p.authOptions != nil {
		opts = append(opts, p.authOptions(st.Nonce)...)
	}
	http.SetCookie(w, &http.Cookie{Name: s.oauthCookieName(), Value: state, Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(oauthTTL.Seconds())})
	http.Redirect(w, r, cfg.AuthCodeURL(state, opts...), http.StatusFound)
}

// oauthCallback finishes the flow: the state must be the one this browser started, and is used once.
func (s *Service) oauthCallback(w http.ResponseWriter, r *http.Request) {
	route := r.PathValue("provider")
	ctx := r.Context()
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(s.oauthCookieName())
	s.clearOAuthCookie(w)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		s.redirectError(w, r, "", "oauth_failed")
		return
	}
	st := s.db.S()
	ch, err := st.ChallengeByID(ctx, store.HashToken(state))
	if err != nil {
		s.redirectError(w, r, "", "expired")
		return
	}
	if used, err := st.ConsumeChallenge(ctx, ch.ID); err != nil || !used || ch.Kind != "oauth" || !s.now().Before(ch.ExpiresAt) {
		s.redirectError(w, r, "", "expired")
		return
	}
	var data oauthState
	if json.Unmarshal([]byte(ch.Data), &data) != nil || data.Provider != route {
		s.redirectError(w, r, "", "oauth_failed")
		return
	}
	p := s.oauth[route]
	if p == nil {
		s.redirectError(w, r, data.Intent, "provider_off")
		return
	}
	if e := r.URL.Query().Get("error"); e != "" || r.URL.Query().Get("code") == "" {
		s.redirectError(w, r, data.Intent, "oauth_failed")
		return
	}

	hctx := s.httpCtx(ctx)
	cfg, err := p.config(hctx)
	if err != nil {
		s.log.Warn("oauth provider not reachable", "provider", route, "error", err)
		s.redirectError(w, r, data.Intent, "oauth_failed")
		return
	}
	tok, err := cfg.Exchange(hctx, r.URL.Query().Get("code"), oauth2.VerifierOption(data.Verifier))
	if err != nil {
		s.log.Info("oauth code not accepted", "provider", route, "error", err)
		s.redirectError(w, r, data.Intent, "oauth_failed")
		return
	}
	info, err := p.identity(hctx, cfg, tok, data.Nonce)
	if err != nil {
		s.log.Info("oauth identity not read", "provider", route, "error", err)
		s.redirectError(w, r, data.Intent, "oauth_failed")
		return
	}

	opts := resolveOpts{Next: data.Next}
	if data.Intent == "link" {
		pr, err := s.Authenticate(r)
		if err != nil || pr == nil || pr.User.ID != data.UserID {
			s.redirectError(w, r, data.Intent, "expired")
			return
		}
		opts.LinkTo = &pr.User
	}
	u, isNew, err := s.resolve(ctx, info, opts)
	if err != nil {
		s.redirectError(w, r, data.Intent, oauthErrorCode(err))
		return
	}
	if data.Intent == "link" {
		s.Audit(ctx, u.ID, "identity_linked", r, info.Provider)
		http.Redirect(w, r, data.Next, http.StatusFound)
		return
	}
	if err := s.startSession(ctx, w, r, u, strings.TrimPrefix(route, "oidc-")); err != nil {
		s.log.Error("session not started", "error", err)
		s.redirectError(w, r, data.Intent, "oauth_failed")
		return
	}
	_ = isNew
	http.Redirect(w, r, s.nextFor(ctx, u, data.Next), http.StatusFound)
}

func oauthErrorCode(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		switch he.Code {
		case "account_exists", "signup_closed", "expired", "first_account_unverified":
			return he.Code
		case "conflict":
			return "identity_taken"
		}
	}
	return "oauth_failed"
}

// httpCtx carries the hub's HTTP client (with a timeout) to the OAuth and OpenID libraries.
func (s *Service) httpCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, s.client)
}
