package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// AppTokenRoute is the loopback route the runtime asks the hub for an app's token through (docs/build-apps/10-auth.md).
const AppTokenRoute = "/apps/token"

const appTokenTimeout = 20 * time.Second

// AppToken is a token for an app's backend, opened here, and when it expires.
type AppToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// AppTokenError is a refusal with the hub's code (hubproto.AppToken*) or "unavailable" when the hub was not reached.
type AppTokenError struct {
	Code string
	Err  error
}

func (e *AppTokenError) Error() string { return "app token: " + e.Code + ": " + e.Err.Error() }

// AppTokenUnavailable: the hub could not be asked (not reached, not this machine's). AppTokenNoHub: this machine has no
// hub (or no machine agent) to ask; the runtime signs a platform token with its own key then.
const (
	AppTokenUnavailable = "unavailable"
	AppTokenNoHub       = "no_hub"
)

// RequestAppToken asks the hub for a token (POST /agent/app-token on the host the dial URL points at), signed with the
// machine's key over the body, and opens the sealed answer.
func RequestAppToken(ctx context.Context, m *MachineFile, client *http.Client, now time.Time, req hubproto.AppTokenRequest) (AppToken, error) {
	unavailable := func(err error) (AppToken, error) {
		return AppToken{}, &AppTokenError{Code: AppTokenUnavailable, Err: err}
	}
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return unavailable(err)
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return unavailable(err)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = hubproto.AppTokenPath, ""
	body, _ := json.Marshal(req)
	auth, err := hubproto.SignAppToken(m.SigningKey(), m.MachineID, now, host, body)
	if err != nil {
		return unavailable(err)
	}
	signed, _ := hubproto.ParseDialHeader(auth)
	ctx, cancel := context.WithTimeout(ctx, appTokenTimeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return unavailable(err)
	}
	hreq.Header.Set("Authorization", auth)
	hreq.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = &http.Client{Timeout: appTokenTimeout}
	}
	res, err := client.Do(hreq)
	if err != nil {
		return unavailable(fmt.Errorf("could not reach the hub at %s: %w", m.HubURL, err))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		switch e.Error {
		case hubproto.AppTokenOff, hubproto.AppTokenDenied, hubproto.AppTokenIdPUnreachable:
			return AppToken{}, &AppTokenError{Code: e.Error, Err: errors.New(e.Message)}
		}
		if res.StatusCode == http.StatusNotFound {
			return AppToken{}, &AppTokenError{Code: hubproto.AppTokenOff, Err: errors.New("the hub does not mint app tokens (an older hub)")}
		}
		return unavailable(fmt.Errorf("the hub answered HTTP %d %s", res.StatusCode, e.Error))
	}
	var ans hubproto.AppTokenAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return unavailable(errors.New("the hub gave an answer that is no token"))
	}
	plain, err := hubproto.OpenSecret(m.SealingKey(), m.MachineID, signed.Nonce, ans.Sealed)
	if err != nil {
		return unavailable(errors.New("the hub's answer does not open with this machine's key"))
	}
	return AppToken{Token: string(plain), ExpiresAt: time.Unix(ans.ExpiresAt, 0)}, nil
}

// appTokenRoute is POST /apps/token on the loopback: the runtime of this machine asks the hub for an app's token.
func (a *Agent) appTokenRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	m := a.safeMachine()
	if m == nil {
		reply(w, 409, map[string]string{"error": "this machine has no hub that mints app tokens", "code": AppTokenNoHub})
		return
	}
	var req hubproto.AppTokenRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, hubproto.MaxAppTokenBody)).Decode(&req); err != nil {
		reply(w, 400, map[string]string{"error": "not a token request", "code": AppTokenUnavailable})
		return
	}
	tok, err := RequestAppToken(r.Context(), m, nil, time.Now(), req)
	if err != nil {
		code := AppTokenUnavailable
		var te *AppTokenError
		if errors.As(err, &te) {
			code = te.Code
		}
		reply(w, 502, map[string]string{"error": err.Error(), "code": code})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, 200, tok)
}

// LocalAppTokens asks the hub for app tokens through the machine agent whose agent.json lies in DataDir.
type LocalAppTokens struct {
	DataDir, TokenHeader string
	HTTP                 *http.Client
}

// Token is one token, or an *AppTokenError.
func (l LocalAppTokens) Token(req hubproto.AppTokenRequest) (AppToken, error) {
	status, raw, err := CallLocal(l.HTTP, l.DataDir, l.TokenHeader, AppTokenRoute, req, 2*appTokenTimeout)
	if errors.Is(err, ErrNoAgent) {
		return AppToken{}, &AppTokenError{Code: AppTokenNoHub, Err: err}
	}
	if err != nil {
		return AppToken{}, &AppTokenError{Code: AppTokenUnavailable, Err: fmt.Errorf("the machine agent: %w", err)}
	}
	if status != http.StatusOK {
		var e struct{ Error, Code string }
		_ = json.Unmarshal(raw, &e)
		if e.Code == "" {
			e.Code = AppTokenUnavailable
		}
		return AppToken{}, &AppTokenError{Code: e.Code, Err: errors.New(e.Error)}
	}
	var tok AppToken
	if err := json.Unmarshal(raw, &tok); err != nil || tok.Token == "" {
		return AppToken{}, &AppTokenError{Code: AppTokenUnavailable, Err: errors.New("the machine agent gave no token")}
	}
	return tok, nil
}
