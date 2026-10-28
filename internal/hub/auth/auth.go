// Package auth is the hub's accounts: sessions and cookies, the CSRF guard, email codes, passkeys, OAuth sign-in with
// GitHub, Yandex ID and OpenID Connect, and the rules that decide which sign-in belongs to which account.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	sessionAbsolute = 30 * 24 * time.Hour
	sessionIdle     = 14 * 24 * time.Hour
	sessionTouch    = time.Minute
	// RunSessionTTL is how long a machine's own origin keeps a person signed in (subdomain mode).
	RunSessionTTL = 12 * time.Hour

	KindHub = "hub"
	KindRun = "run"
)

// Deps are what the service is built from.
type Deps struct {
	Config  *config.Config
	DB      *store.DB
	Mailer  mail.Mailer
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// HTTPClient is used for the calls to GitHub, Yandex and OpenID providers; nil means a client with a timeout.
	HTTPClient *http.Client
	// Endpoints moves the fixed providers' addresses; tests only.
	Endpoints Endpoints
}

// Service is the account side of the hub.
type Service struct {
	cfg     *config.Config
	db      *store.DB
	mailer  mail.Mailer
	lim     *ratelimit.Limiter
	log     *slog.Logger
	now     func() time.Time
	client  *http.Client
	wa      *webauthn.WebAuthn
	oauth   map[string]*oauthProvider
	oauthOr []string // provider routes in display order
	ep      Endpoints
}

// New builds the service: it prepares WebAuthn when passkeys are on and the OAuth providers that are configured.
func New(d Deps) (*Service, error) {
	s := &Service{cfg: d.Config, db: d.DB, mailer: d.Mailer, lim: d.Limiter, log: d.Log, now: d.Now, client: d.HTTPClient, oauth: map[string]*oauthProvider{}, ep: d.Endpoints}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.lim == nil {
		s.lim = ratelimit.New()
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 15 * time.Second}
	}
	if s.cfg.Passkey {
		wa, err := webauthn.New(&webauthn.Config{
			RPID:          s.cfg.RPID,
			RPDisplayName: s.cfg.RPName,
			RPOrigins:     []string{s.cfg.PublicURL},
		})
		if err != nil {
			// WebAuthn needs a domain name for the RP ID: a hub reached by an IP address has no passkeys.
			s.log.Warn("passkeys are off", "rpId", s.cfg.RPID, "reason", err)
		} else {
			s.wa = wa
		}
	}
	s.initOAuth()
	return s, nil
}

// CookieName is the session cookie of the hub's origin.
func (s *Service) CookieName() string {
	if s.cfg.Secure {
		return "__Host-hl_session"
	}
	return "hl_session"
}

// RunCookieName is the cookie of a machine's own origin (subdomain mode). On https it is `__Host-hl_run`: a browser
// accepts a cookie with that prefix only from the host itself, Secure, with Path=/ and no Domain, so a script on a
// sibling machine's origin (they share the registrable domain unless HUB_RUNTIME_DOMAIN is a domain of its own) cannot
// plant one for this machine, which would sign the person in as someone else there, or lock them out. A cookie
// of the plain name is never read on https. Without https there is no prefix to have.
func (s *Service) RunCookieName() string {
	if s.cfg.Secure {
		return "__Host-hl_run"
	}
	return "hl_run"
}

// Principal is the signed-in person behind a request.
type Principal struct {
	User    store.User
	Session store.Session
	// Admin is whether the person administers this installation (a proven address on HUB_ADMIN_EMAILS, or the first
	// account).
	Admin bool
}

// UserView is a user as the API sends it.
type UserView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	IsAdmin   bool   `json:"isAdmin"`
	CreatedAt string `json:"createdAt"`
}

// View is the API form of a user; admin is what adminFor said about them.
func (s *Service) View(u store.User, admin bool) UserView {
	return UserView{ID: u.ID, Email: u.Email, Name: u.Name, IsAdmin: admin, CreatedAt: httpx.Time(u.CreatedAt)}
}

// ClientIP is the address of the person behind r, honouring the trusted proxies.
func (s *Service) ClientIP(r *http.Request) string { return httpx.ClientIP(r, s.cfg.TrustedProxies) }

func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// startSession signs a person in: a new session row, the cookie, an audit line. method is "email", "passkey",
// "github", …
func (s *Service) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u store.User, method string) error {
	token := newToken()
	now := s.now().UTC()
	sess := store.Session{
		TokenHash: store.HashToken(token), Kind: KindHub, UserID: u.ID,
		CreatedAt: now, ExpiresAt: now.Add(sessionAbsolute), LastSeenAt: now,
		IP: s.ClientIP(r), UserAgent: truncate(r.UserAgent(), 200),
	}
	st := s.db.S()
	if err := st.CreateSession(ctx, sess); err != nil {
		return err
	}
	_ = st.TouchLogin(ctx, u.ID, now)
	s.Audit(ctx, u.ID, "login", r, method)
	http.SetCookie(w, &http.Cookie{
		Name: s.CookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionAbsolute.Seconds()),
	})
	return nil
}

func (s *Service) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.CookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// Audit writes a line of the sign-in trail. A failure to write it is logged and never blocks the request.
func (s *Service) Audit(ctx context.Context, userID, kind string, r *http.Request, detail string) {
	e := store.AuthEvent{UserID: userID, Kind: kind, Detail: detail, CreatedAt: s.now().UTC()}
	if r != nil {
		e.IP, e.UserAgent = s.ClientIP(r), truncate(r.UserAgent(), 200)
	}
	if err := s.db.S().AddAuthEvent(ctx, e); err != nil {
		s.log.Warn("audit line not written", "kind", kind, "error", err)
	}
}

// Authenticate finds the person behind the request's session cookie. It returns nil, nil when there is none, or the
// session is over (absolute 30 days, idle 14 days).
func (s *Service) Authenticate(r *http.Request) (*Principal, error) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return nil, nil
	}
	return s.principalFor(r.Context(), store.HashToken(c.Value), KindHub)
}

func (s *Service) principalFor(ctx context.Context, hash, kind string) (*Principal, error) {
	st := s.db.S()
	sess, err := st.SessionByHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if sess.Kind != kind || !now.Before(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > sessionIdle {
		_ = st.DeleteSession(ctx, hash)
		return nil, nil
	}
	u, err := st.UserByID(ctx, sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if now.Sub(sess.LastSeenAt) > sessionTouch {
		if err := st.TouchSession(ctx, hash, now.UTC()); err != nil {
			s.log.Warn("session not touched", "error", err)
		}
	}
	admin, err := s.adminFor(ctx, st, u)
	if err != nil {
		return nil, err
	}
	return &Principal{User: u, Session: sess, Admin: admin}, nil
}

// Require wraps a handler that needs a signed-in person.
func (s *Service) Require(h func(w http.ResponseWriter, r *http.Request, p *Principal) error) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.Authenticate(r)
		if err != nil {
			return err
		}
		if p == nil {
			return httpx.ErrUnauthenticated
		}
		return h(w, r, p)
	}
}

// CreateRunSession starts a session for a machine's own origin (subdomain mode) and returns its token.
func (s *Service) CreateRunSession(ctx context.Context, userID, machineID string) (token string, expires time.Time, err error) {
	token = newToken()
	now := s.now().UTC()
	expires = now.Add(RunSessionTTL)
	err = s.db.S().CreateSession(ctx, store.Session{
		TokenHash: store.HashToken(token), Kind: KindRun, UserID: userID, MachineID: machineID,
		CreatedAt: now, ExpiresAt: expires, LastSeenAt: now,
	})
	return token, expires, err
}

// RunSession resolves a machine-origin cookie to the person and the machine it was made for; nil when it is not valid.
func (s *Service) RunSession(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, nil
	}
	return s.principalFor(ctx, store.HashToken(token), KindRun)
}

// OpenURL tells where a SignedIn person goes next.
func (s *Service) nextFor(ctx context.Context, u store.User, asked string) string {
	if n := SafeNext(asked); n != "" {
		return n
	}
	spaces, err := s.db.S().SpacesByUser(ctx, u.ID)
	if err != nil || len(spaces) == 0 {
		return "/new"
	}
	return "/s/" + spaces[len(spaces)-1].Slug
}

// SafeNext returns next when it is a path on this site, else "". A redirect target taken from a request must never
// leave the site.
func SafeNext(next string) string {
	if next == "" || len(next) > 512 || next[0] != '/' || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n\t") {
		return ""
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return next
}

// SignedIn is what every sign-in flow answers.
type SignedIn struct {
	User  UserView `json:"user"`
	IsNew bool     `json:"isNew"`
	Next  string   `json:"next"`
}

func (s *Service) signedIn(ctx context.Context, w http.ResponseWriter, r *http.Request, u store.User, isNew bool, method, asked string) error {
	if err := s.startSession(ctx, w, r, u, method); err != nil {
		return err
	}
	admin, err := s.adminFor(ctx, s.db.S(), u)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, SignedIn{User: s.View(u, admin), IsNew: isNew, Next: s.nextFor(ctx, u, asked)})
	return nil
}

// Provider facts the UI needs.

// OIDCProviders lists the configured generic OpenID providers for /api/config.
func (s *Service) OIDCProviders() []config.OIDC { return s.cfg.OIDC }

// PasskeyOn says whether passkeys are on.
func (s *Service) PasskeyOn() bool { return s.wa != nil }

// MailMode is "smtp" or "log".
func (s *Service) MailMode() string { return s.mailer.Mode() }

// Housekeeping removes expired sessions and challenges; run it now and then.
func (s *Service) Housekeeping(ctx context.Context) error { return s.db.S().PurgeExpired(ctx, s.now()) }
