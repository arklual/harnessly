package mcpconn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
)

// flowTTL is how long a started sign-in waits for its callback.
const flowTTL = 10 * time.Minute

// refreshSkew: a token that expires sooner than this is refreshed before use.
const refreshSkew = 60 * time.Second

// Token endpoint client authentication methods.
const (
	authNone  = "none"
	authPost  = "client_secret_post"
	authBasic = "client_secret_basic"
)

// cred is what the safe keeps under mcp-<id>.
type cred struct {
	Header string     `json:"header,omitempty"`
	OAuth  *oauthCred `json:"oauth,omitempty"`
}

type oauthCred struct {
	ClientID      string    `json:"clientId"`
	ClientSecret  string    `json:"clientSecret,omitempty"`
	AuthMethod    string    `json:"authMethod,omitempty"`
	AccessToken   string    `json:"accessToken"`
	RefreshToken  string    `json:"refreshToken,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt,omitzero"`
	TokenEndpoint string    `json:"tokenEndpoint"`
	Resource      string    `json:"resource"`
	Scope         string    `json:"scope,omitempty"`
	AuthServer    string    `json:"authServer"`
}

// registration is a dynamically registered client, cached per authorization server and redirect URI. Its secret, when
// one was issued, is in the safe under clientSecretName.
type registration struct {
	AuthServer  string    `json:"authServer"`
	RedirectURI string    `json:"redirectUri"`
	ClientID    string    `json:"clientId"`
	AuthMethod  string    `json:"authMethod"`
	HasSecret   bool      `json:"hasSecret,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt,omitzero"`
}

func clientSecretName(authServer, redirectURI string) string {
	sum := sha256.Sum256([]byte(authServer + "\n" + redirectURI))
	return "mcp-client-" + hex.EncodeToString(sum[:8])
}

// client is a registered client with its secret.
type client struct {
	ID     string
	Secret string
	Method string
}

// flow is a sign-in waiting for its callback.
type flow struct {
	serverID    string
	verifier    string
	redirectURI string
	client      client
	d           discovery
	created     time.Time
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// register finds or makes a client of the authorization server for redirectURI (RFC 7591).
func (s *Service) register(ctx context.Context, d *discovery, redirectURI string) (client, error) {
	s.mu.Lock()
	f, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return client{}, err
	}
	for _, r := range f.Clients {
		if r.AuthServer != d.AuthServer || r.RedirectURI != redirectURI {
			continue
		}
		if !r.ExpiresAt.IsZero() && !r.ExpiresAt.After(s.now().Add(refreshSkew)) {
			break
		}
		c := client{ID: r.ClientID, Method: r.AuthMethod}
		if r.HasSecret {
			secret, err := s.secrets.Reveal(clientSecretName(r.AuthServer, r.RedirectURI))
			if err != nil {
				break
			}
			c.Secret = secret
		}
		return c, nil
	}

	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	res, err := s.http.Do(ctx, connections.Request{
		Method: http.MethodPost,
		URL:    d.Register,
		Header: http.Header{"User-Agent": {userAgent}},
		Body: map[string]any{
			"client_name":                "harnessly",
			"redirect_uris":              []string{redirectURI},
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": authNone,
		},
	})
	if err != nil {
		return client{}, fmt.Errorf("%w: registration: %v", ErrOAuthFailed, err)
	}
	if res.Status < 200 || res.Status > 299 {
		return client{}, fmt.Errorf("%w: registration refused (%d)", ErrOAuthFailed, res.Status)
	}
	var out struct {
		ClientID              string `json:"client_id"`
		ClientSecret          string `json:"client_secret"`
		ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
		AuthMethod            string `json:"token_endpoint_auth_method"`
	}
	if json.Unmarshal(res.Body, &out) != nil || out.ClientID == "" {
		return client{}, fmt.Errorf("%w: registration answered without a client id", ErrOAuthFailed)
	}
	c := client{ID: out.ClientID, Secret: out.ClientSecret, Method: authNone}
	if c.Secret != "" {
		c.Method = secretMethod(out.AuthMethod, d.AuthMethods)
	}
	r := registration{AuthServer: d.AuthServer, RedirectURI: redirectURI, ClientID: c.ID, AuthMethod: c.Method, HasSecret: c.Secret != ""}
	if out.ClientSecretExpiresAt > 0 {
		r.ExpiresAt = time.Unix(out.ClientSecretExpiresAt, 0).UTC()
	}
	if r.HasSecret {
		if err := s.secrets.Put(clientSecretName(r.AuthServer, r.RedirectURI), c.Secret); err != nil {
			return client{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err = s.readAll()
	if err != nil {
		return client{}, err
	}
	f.Clients = slices.DeleteFunc(f.Clients, func(x registration) bool {
		return x.AuthServer == r.AuthServer && x.RedirectURI == r.RedirectURI
	})
	f.Clients = append(f.Clients, r)
	return c, s.writeAll(f)
}

// forgetClient drops a cached registration the authorization server no longer knows.
func (s *Service) forgetClient(authServer, redirectURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return
	}
	n := len(f.Clients)
	f.Clients = slices.DeleteFunc(f.Clients, func(x registration) bool {
		return x.AuthServer == authServer && x.RedirectURI == redirectURI
	})
	if len(f.Clients) != n {
		_ = s.writeAll(f)
		_ = s.secrets.Remove(clientSecretName(authServer, redirectURI))
	}
}

// secretMethod is how a client with a secret authenticates: what registration said, else what the server supports
// (basic first, the RFC 6749 default).
func secretMethod(registered string, supported []string) string {
	if registered == authPost || registered == authBasic {
		return registered
	}
	if len(supported) > 0 && !slices.Contains(supported, authBasic) && slices.Contains(supported, authPost) {
		return authPost
	}
	return authBasic
}

// startFlow keeps a new sign-in and answers the authorization URL.
func (s *Service) startFlow(serverID, redirectURI string, c client, d discovery) (string, error) {
	au, err := url.Parse(d.Authorize)
	if err != nil {
		return "", fmt.Errorf("%w: bad authorization endpoint", ErrOAuthFailed)
	}
	verifier := randomString(32)
	state := randomString(32)
	q := au.Query()
	q.Set("response_type", "code")
	q.Set("client_id", c.ID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challengeOf(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("resource", d.Resource)
	if d.Scope != "" {
		q.Set("scope", d.Scope)
	}
	au.RawQuery = q.Encode()

	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	s.pruneFlows()
	s.flows[state] = flow{serverID: serverID, verifier: verifier, redirectURI: redirectURI, client: c, d: d, created: s.now()}
	return au.String(), nil
}

// takeFlow removes and answers the sign-in of state; a state is good once and for flowTTL.
func (s *Service) takeFlow(state string) (flow, bool) {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	s.pruneFlows()
	f, ok := s.flows[state]
	delete(s.flows, state)
	return f, ok
}

func (s *Service) pruneFlows() {
	for k, f := range s.flows {
		if s.now().Sub(f.created) > flowTTL {
			delete(s.flows, k)
		}
	}
}

func (s *Service) dropFlows(serverID string) {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	for k, f := range s.flows {
		if f.serverID == serverID {
			delete(s.flows, k)
		}
	}
}

// errInvalidGrant: the authorization server no longer accepts the grant (a revoked or expired refresh token).
var errInvalidGrant = fmt.Errorf("%w: grant refused", ErrSignInRequired)

// errInvalidClient: the authorization server no longer knows the client.
var errInvalidClient = fmt.Errorf("%w: client refused", ErrOAuthFailed)

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

// token calls the token endpoint with form, authenticating as c.
func (s *Service) token(ctx context.Context, endpoint string, c client, form url.Values) (tokenAnswer, error) {
	header := http.Header{
		"Content-Type": {"application/x-www-form-urlencoded"},
		"Accept":       {"application/json"},
		"User-Agent":   {userAgent},
	}
	switch c.Method {
	case authBasic:
		header.Set("Authorization", connections.Basic(url.QueryEscape(c.ID), url.QueryEscape(c.Secret)))
	case authPost:
		form.Set("client_id", c.ID)
		form.Set("client_secret", c.Secret)
	default:
		form.Set("client_id", c.ID)
	}
	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	res, err := s.http.Do(ctx, connections.Request{Method: http.MethodPost, URL: endpoint, Header: header, Body: []byte(form.Encode())})
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint: %v", ErrUnreachable, err)
	}
	var out tokenAnswer
	if mediaType(res.Header) == "application/x-www-form-urlencoded" {
		v, _ := url.ParseQuery(string(res.Body))
		out = tokenAnswer{AccessToken: v.Get("access_token"), TokenType: v.Get("token_type"), RefreshToken: v.Get("refresh_token"), Scope: v.Get("scope"), Error: v.Get("error")}
		out.ExpiresIn, _ = strconv.ParseInt(v.Get("expires_in"), 10, 64)
	} else if json.Unmarshal(res.Body, &out) != nil && res.Status >= 200 && res.Status <= 299 {
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint answered no JSON", ErrOAuthFailed)
	}
	switch {
	case out.Error == "invalid_grant":
		return tokenAnswer{}, errInvalidGrant
	case out.Error == "invalid_client" || (res.Status == http.StatusUnauthorized && out.Error == ""):
		return tokenAnswer{}, errInvalidClient
	case out.Error != "":
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint said %s", ErrOAuthFailed, safeWord(out.Error))
	case res.Status >= 500:
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint status %d", ErrUnreachable, res.Status)
	case res.Status < 200 || res.Status > 299:
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint status %d", ErrOAuthFailed, res.Status)
	case out.AccessToken == "":
		return tokenAnswer{}, fmt.Errorf("%w: token endpoint gave no access token", ErrOAuthFailed)
	case out.TokenType != "" && !strings.EqualFold(out.TokenType, "bearer"):
		return tokenAnswer{}, fmt.Errorf("%w: token type is not bearer", ErrOAuthFailed)
	}
	return out, nil
}

// safeWord keeps an OAuth error code readable and short: it is echoed in messages.
func safeWord(s string) string {
	b := []byte{}
	for i := 0; i < len(s) && len(b) < 40; i++ {
		c := s[i]
		if c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b = append(b, c)
		}
	}
	return string(b)
}

func (s *Service) expiry(in int64) time.Time {
	if in <= 0 {
		return time.Time{}
	}
	return s.now().Add(time.Duration(in) * time.Second).UTC()
}

// accessToken is the server's current access token, refreshed when it expires within refreshSkew or when force is
// set. Refreshes of one server run one at a time; a refused grant turns the server to "signin".
func (s *Service) accessToken(ctx context.Context, id string, force bool) (string, error) {
	lock := s.refreshLock(id)
	lock.Lock()
	defer lock.Unlock()

	cr, err := s.readCred(id)
	if err != nil {
		return "", err
	}
	o := cr.OAuth
	if o == nil || o.AccessToken == "" {
		s.setState(id, StateSignIn)
		return "", fmt.Errorf("%w: not signed in", ErrSignInRequired)
	}
	now := s.now()
	fresh := o.ExpiresAt.IsZero() || o.ExpiresAt.After(now.Add(refreshSkew))
	if fresh && !force {
		return o.AccessToken, nil
	}
	if o.RefreshToken == "" {
		if !force && (o.ExpiresAt.IsZero() || o.ExpiresAt.After(now)) {
			return o.AccessToken, nil
		}
		s.setState(id, StateSignIn)
		return "", fmt.Errorf("%w: the sign-in expired", ErrSignInRequired)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {o.RefreshToken},
		"resource":      {o.Resource},
	}
	c := client{ID: o.ClientID, Secret: o.ClientSecret, Method: o.AuthMethod}
	ans, err := s.token(ctx, o.TokenEndpoint, c, form)
	if errors.Is(err, errInvalidGrant) || errors.Is(err, errInvalidClient) {
		s.setState(id, StateSignIn)
		return "", fmt.Errorf("%w: the sign-in expired", ErrSignInRequired)
	}
	if err != nil {
		return "", err
	}
	o.AccessToken = ans.AccessToken
	if ans.RefreshToken != "" {
		o.RefreshToken = ans.RefreshToken
	}
	o.ExpiresAt = s.expiry(ans.ExpiresIn)
	if ans.Scope != "" {
		o.Scope = ans.Scope
	}
	if err := s.writeCred(id, cr); err != nil {
		return "", err
	}
	return o.AccessToken, nil
}

func (s *Service) refreshLock(id string) *lockT {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &lockT{}
		s.locks[id] = l
	}
	return l
}
