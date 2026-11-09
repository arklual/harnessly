package apptoken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Token exchange (RFC 8693): the hub hands the corporate identity provider a token that says who the person is and
// gets back the provider's own access token for the app's backend (its audience). The backend then sees the bank's
// token, as if the person had signed in to it directly, and checks it with the provider's keys as it already does.
//
// The subject token is the platform's own JWT (Type, signed by the hub): the provider trusts the hub as an external
// issuer by its JWKS (Keycloak: an identity provider of type "JWT"/"OIDC" with token exchange; others: a "trusted
// issuer"). The hub keeps no token of the person's own sign-in.

// The grant and token types of RFC 8693.
const (
	GrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	TokenTypeJWT       = "urn:ietf:params:oauth:token-type:jwt"
	TokenTypeAccess    = "urn:ietf:params:oauth:token-type:access_token"
)

// Errors of Exchange: the provider refused (no new sign-in on our side mends it), or did not answer.
var (
	ErrExchangeDenied     = errors.New("apptoken: the identity provider refused the exchange")
	ErrIdPUnreachable     = errors.New("apptoken: the identity provider did not answer")
	ErrExchangeMisconfig  = errors.New("apptoken: the token exchange is not set up")
	errExchangeNotAnOAuth = errors.New("apptoken: the identity provider answered something that is no token")
)

// ExchangeConfig is the hub's client at the corporate provider.
type ExchangeConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	// SubjectTokenType is how the provider is told what the subject token is; TokenTypeJWT when empty.
	SubjectTokenType string
}

// Exchanged is the provider's answer.
type Exchanged struct {
	AccessToken     string
	IssuedTokenType string
	ExpiresAt       time.Time
}

// Exchange trades subject (the platform's JWT) for the provider's token for audience. scope may be empty.
func Exchange(ctx context.Context, client *http.Client, cfg ExchangeConfig, subject, audience, scope string, now time.Time) (Exchanged, error) {
	if cfg.TokenURL == "" || cfg.ClientID == "" {
		return Exchanged{}, ErrExchangeMisconfig
	}
	if audience == "" {
		return Exchanged{}, ErrNoAudience
	}
	typ := cfg.SubjectTokenType
	if typ == "" {
		typ = TokenTypeJWT
	}
	form := url.Values{
		"grant_type":           {GrantTokenExchange},
		"subject_token":        {subject},
		"subject_token_type":   {typ},
		"requested_token_type": {TokenTypeAccess},
		"audience":             {audience},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Exchanged{}, fmt.Errorf("%w: %v", ErrExchangeMisconfig, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return Exchanged{}, fmt.Errorf("%w: %v", ErrIdPUnreachable, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var ans struct {
		AccessToken     string `json:"access_token"`
		IssuedTokenType string `json:"issued_token_type"`
		ExpiresIn       int64  `json:"expires_in"`
		Error           string `json:"error"`
		Description     string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &ans)
	switch {
	case res.StatusCode >= 500:
		return Exchanged{}, fmt.Errorf("%w: HTTP %d", ErrIdPUnreachable, res.StatusCode)
	case res.StatusCode != http.StatusOK || ans.Error != "":
		// invalid_grant, invalid_target, unauthorized_client, access_denied, invalid_client: the provider said no.
		return Exchanged{}, fmt.Errorf("%w: HTTP %d %s %s", ErrExchangeDenied, res.StatusCode, ans.Error, clip(ans.Description))
	case ans.AccessToken == "":
		return Exchanged{}, errExchangeNotAnOAuth
	}
	out := Exchanged{AccessToken: ans.AccessToken, IssuedTokenType: ans.IssuedTokenType}
	if ans.ExpiresIn > 0 {
		out.ExpiresAt = now.Add(time.Duration(ans.ExpiresIn) * time.Second)
	}
	return out, nil
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
