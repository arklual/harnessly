package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Personal tokens of a person's own apps (docs/hub/client-api.md). An app on the person's computer (a
// launcher, a menu-bar counter, a script) asks for a token with a short code, the person confirms the code in the hub
// where they are signed in, and the app gets a token that works with the client API only. The person sees and revokes
// the tokens in the hub's settings. The whole thing is behind the rollout switch ClientAPISwitch, per person.

// ClientAPISwitch is the rollout switch of the client API and its tokens (docs/hub/rollout-switches.md).
const ClientAPISwitch = "client-api"

const (
	// TokenPrefix starts every personal token, so a leaked one is easy to recognize.
	TokenPrefix   = "hlp_"
	tokenTTL      = 90 * 24 * time.Hour
	tokenTouch    = time.Minute
	deviceTTL     = 10 * time.Minute
	devicePoll    = 3 // seconds the app waits between two asks for its token
	deviceKind    = "device"
	maxClientName = 60
	// userCodeAlphabet has no letters or digits that are easy to mix up when read off a screen (0/O, 1/I/L, U/V).
	userCodeAlphabet = "BCDFGHJKMNPQRSTWXZ23456789"
)

// ScopeRead and ScopePrompt are what a token may do: read the person's chats, and write to them.
const (
	ScopeRead   = "read"
	ScopePrompt = "prompt"
	// ScopeEvents lets a token read the journal of events at every level its person may (docs/hub/event-bus.md).
	ScopeEvents = "events"
)

// SwitchGate answers whether a rollout switch is on for a person (machine "" is any machine).
type SwitchGate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

// SetSwitches gives the service the rollout switches; without them the personal tokens are off.
func (s *Service) SetSwitches(g SwitchGate) { s.switches = g }

// ClientAPIOn is whether the client API is on for the person.
func (s *Service) ClientAPIOn(ctx context.Context, u store.User) bool {
	return s.switches != nil && s.switches.On(ctx, ClientAPISwitch, strings.ToLower(u.Email), "")
}

// deviceData is what a device challenge carries between the app's start and the person's answer.
type deviceData struct {
	Client string   `json:"client"`
	Device string   `json:"device"`
	State  string   `json:"state"` // pending | approved | denied
	UserID string   `json:"userId,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

func (s *Service) mountTokens(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/device/start", httpx.Handler(s.deviceStart))
	mux.Handle("POST /api/auth/device/token", httpx.Handler(s.deviceToken))
	mux.Handle("GET /api/auth/device", s.Require(s.deviceShow))
	mux.Handle("POST /api/auth/device/approve", s.Require(s.deviceApprove))
	mux.Handle("GET /api/me/tokens", s.Require(s.listTokens))
	mux.Handle("DELETE /api/me/tokens/{id}", s.Require(s.revokeToken))
}

// cleanName keeps a name an app gives itself printable and short.
func cleanName(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if r := []rune(v); len(r) > maxClientName {
		v = string(r[:maxClientName])
	}
	return v
}

// newUserCode is the code the person reads off the app and confirms in the hub: XXXX-XXXX, about 37 bits.
func newUserCode() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	out := make([]byte, 0, 9)
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(c)%len(userCodeAlphabet)])
	}
	return string(out)
}

// normalUserCode is a typed code as stored: upper case, without the dash or spaces.
func normalUserCode(v string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(v) {
		if r != '-' && !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Service) deviceStart(w http.ResponseWriter, r *http.Request) error {
	if ok, wait := s.lim.Allow("device-start:"+ratelimit.IPKey(s.ClientIP(r)), 10, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	var in struct {
		Client string `json:"client"`
		Device string `json:"device"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Client, in.Device = cleanName(in.Client), cleanName(in.Device)
	if in.Client == "" {
		return httpx.Invalid("client", "Назовите приложение")
	}
	deviceCode, userCode := newToken(), newUserCode()
	data, err := json.Marshal(deviceData{Client: in.Client, Device: in.Device, State: "pending"})
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.db.S().CreateChallenge(r.Context(), store.Challenge{
		ID: store.HashToken(deviceCode), Kind: deviceKind, Email: store.HashToken(normalUserCode(userCode)),
		Data: string(data), ExpiresAt: now.Add(deviceTTL), CreatedAt: now, IP: s.ClientIP(r),
	}); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"deviceCode": deviceCode,
		"userCode":   userCode,
		"verifyUrl":  strings.TrimRight(s.cfg.PublicURL, "/") + "/connect?code=" + url.QueryEscape(userCode),
		"interval":   devicePoll,
		"expiresIn":  int(deviceTTL / time.Second),
	})
	return nil
}

// deviceByUserCode is the waiting challenge of a code the person typed or followed.
func (s *Service) deviceByUserCode(ctx context.Context, code string) (store.Challenge, deviceData, error) {
	c, err := s.db.S().LatestChallenge(ctx, deviceKind, store.HashToken(normalUserCode(code)))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !s.now().Before(c.ExpiresAt)) {
		return store.Challenge{}, deviceData{}, httpx.New(http.StatusNotFound, "code_unknown", "Код не найден или устарел: начните подключение в приложении заново")
	}
	if err != nil {
		return store.Challenge{}, deviceData{}, err
	}
	var d deviceData
	if err := json.Unmarshal([]byte(c.Data), &d); err != nil {
		return store.Challenge{}, deviceData{}, err
	}
	if d.State != "pending" {
		return store.Challenge{}, deviceData{}, httpx.Conflict("code_used", "Этот код уже подтверждён или отклонён")
	}
	return c, d, nil
}

// codeTries limits how many codes one person may look up, so a code cannot be guessed from a signed-in account.
func (s *Service) codeTries(p *Principal) error {
	if ok, wait := s.lim.Allow("device-code:"+p.User.ID, 20, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	return nil
}

func errClientAPIOff() error {
	return httpx.New(http.StatusNotFound, "client_api_off", "Подключение приложений для вас пока не включено")
}

func (s *Service) deviceShow(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if !s.ClientAPIOn(r.Context(), p.User) {
		return errClientAPIOff()
	}
	if err := s.codeTries(p); err != nil {
		return err
	}
	c, d, err := s.deviceByUserCode(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"client": d.Client, "device": d.Device, "ip": c.IP, "expiresAt": httpx.Time(c.ExpiresAt),
		"scopes": []string{ScopeRead, ScopePrompt, ScopeEvents},
	})
	return nil
}

func (s *Service) deviceApprove(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ctx := r.Context()
	if !s.ClientAPIOn(ctx, p.User) {
		return errClientAPIOff()
	}
	if err := s.codeTries(p); err != nil {
		return err
	}
	var in struct {
		Code   string   `json:"code"`
		Allow  bool     `json:"allow"`
		Scopes []string `json:"scopes"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	c, d, err := s.deviceByUserCode(ctx, in.Code)
	if err != nil {
		return err
	}
	d.State, d.UserID = "denied", p.User.ID
	if in.Allow {
		d.State = "approved"
		d.Scopes = []string{ScopeRead}
		for _, sc := range []string{ScopePrompt, ScopeEvents} {
			if slices.Contains(in.Scopes, sc) {
				d.Scopes = append(d.Scopes, sc)
			}
		}
		if in.Scopes == nil {
			d.Scopes = []string{ScopeRead, ScopePrompt}
		}
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if ok, err := s.db.S().SetChallengeData(ctx, c.ID, string(data)); err != nil {
		return err
	} else if !ok {
		return httpx.ErrExpired
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": d.State, "client": d.Client, "device": d.Device})
	return nil
}

// deviceWait is an answer of the token route while there is no token: the app reads error and keeps asking or stops.
func deviceWait(code, message string) error {
	return httpx.New(http.StatusBadRequest, code, message)
}

func (s *Service) deviceToken(w http.ResponseWriter, r *http.Request) error {
	if ok, wait := s.lim.Allow("device-token:"+ratelimit.IPKey(s.ClientIP(r)), 120, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	var in struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	st := s.db.S()
	id := store.HashToken(in.DeviceCode)
	c, err := st.ChallengeByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && (c.Kind != deviceKind || !s.now().Before(c.ExpiresAt))) {
		return deviceWait("expired_token", "Код устарел: начните подключение заново")
	}
	if err != nil {
		return err
	}
	var d deviceData
	if err := json.Unmarshal([]byte(c.Data), &d); err != nil {
		return err
	}
	switch d.State {
	case "pending":
		return deviceWait("authorization_pending", "Ждём подтверждения в хабе")
	case "denied":
		_, _ = st.ConsumeChallenge(ctx, id)
		return deviceWait("access_denied", "Подключение отклонено")
	}
	if ok, err := st.ConsumeChallenge(ctx, id); err != nil {
		return err
	} else if !ok {
		return deviceWait("expired_token", "Код уже использован")
	}
	u, err := st.UserByID(ctx, d.UserID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.DisabledAt != nil) {
		return deviceWait("access_denied", "Аккаунт недоступен")
	}
	if err != nil {
		return err
	}
	token := TokenPrefix + newToken()
	now := s.now().UTC()
	t := store.APIToken{
		ID: store.NewID(), UserID: u.ID, TokenHash: store.HashToken(token), Client: d.Client, Device: d.Device,
		Scopes: strings.Join(d.Scopes, " "), CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(tokenTTL), IP: s.ClientIP(r),
	}
	if err := st.CreateAPIToken(ctx, t); err != nil {
		return err
	}
	s.Audit(ctx, u.ID, "device_connected", r, truncate(d.Client+" · "+d.Device, 200))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token": token, "tokenId": t.ID, "scopes": d.Scopes, "expiresAt": httpx.Time(t.ExpiresAt),
		"user": map[string]any{"id": u.ID, "email": u.Email, "name": u.Name},
	})
	return nil
}

// TokenView is a token as the settings show it.
type TokenView struct {
	ID         string   `json:"id"`
	Client     string   `json:"client"`
	Device     string   `json:"device"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt string   `json:"lastUsedAt"`
	ExpiresAt  string   `json:"expiresAt"`
	IP         string   `json:"ip"`
}

func (s *Service) listTokens(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ts, err := s.db.S().UserAPITokens(r.Context(), p.User.ID, s.now())
	if err != nil {
		return err
	}
	out := []TokenView{}
	for _, t := range ts {
		out = append(out, TokenView{ID: t.ID, Client: t.Client, Device: t.Device, Scopes: strings.Fields(t.Scopes),
			CreatedAt: httpx.Time(t.CreatedAt), LastUsedAt: httpx.Time(t.LastUsedAt), ExpiresAt: httpx.Time(t.ExpiresAt), IP: t.IP})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"on": s.ClientAPIOn(r.Context(), p.User), "tokens": out})
	return nil
}

func (s *Service) revokeToken(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ok, err := s.db.S().RevokeAPIToken(r.Context(), p.User.ID, r.PathValue("id"), s.now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrNotFound
	}
	s.Audit(r.Context(), p.User.ID, "device_revoked", r, r.PathValue("id"))
	httpx.NoContent(w)
	return nil
}

// TokenPrincipal is the person behind a personal token in the request's Authorization header: nil, nil when there is
// no personal token there, or it is revoked, expired, its person is switched off or the client API is off for them.
func (s *Service) TokenPrincipal(r *http.Request) (*Principal, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(raw, TokenPrefix) {
		return nil, nil
	}
	ctx := r.Context()
	st := s.db.S()
	t, err := st.APITokenByHash(ctx, store.HashToken(strings.TrimSpace(raw)))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if t.RevokedAt != nil || !now.Before(t.ExpiresAt) {
		return nil, nil
	}
	u, err := st.UserByID(ctx, t.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if u.DisabledAt != nil || !s.ClientAPIOn(ctx, u) {
		return nil, nil
	}
	if now.Sub(t.LastUsedAt) > tokenTouch {
		if err := st.TouchAPIToken(ctx, t.ID, now.UTC(), now.UTC().Add(tokenTTL)); err != nil {
			s.log.Warn("token not touched", "error", err)
		}
	}
	tok := t
	return &Principal{User: u, Token: &tok}, nil
}

// Can is whether the principal may do what scope names: a session may do anything, a token what it was given.
func (p *Principal) Can(scope string) bool {
	if p.Token == nil {
		return true
	}
	for _, sc := range strings.Fields(p.Token.Scopes) {
		if sc == scope {
			return true
		}
	}
	return false
}
