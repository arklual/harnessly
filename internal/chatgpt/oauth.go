package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	// AgentName is the app's name OpenAI shows the person when they register it; the same on every installation.
	AgentName = "harnessly"
	// resource is the audience the tokens are asked for: the public API, whatever address a test serves it on.
	resource      = "https://api.openai.com/v1"
	dynamicClient = "dynamic_agent_client"
	scopePlan     = "chatgpt.tokens.use.direct"
	scopes        = "openid profile email offline_access resource.invoke " + scopePlan
	callbackPath  = "/auth/callback"
	// callbackPort is the port the callback listens on when it is free; only the port may vary between sign-ins.
	callbackPort = 1455
	// loginTTL is how long a started sign-in waits for its callback.
	loginTTL = 15 * time.Minute
	// loginsMax is how many sign-ins may wait at once.
	loginsMax = 4
	// refreshAhead is how long before its expiry an access token is renewed.
	refreshAhead = 2 * time.Minute
	httpBodyMax  = 1 << 20
)

// Endpoints are where OpenAI answers. Auth is the issuer and serves the authorize, token and discovery routes; API is
// the base of the Responses and models routes. A test points them at a fake server.
type Endpoints struct {
	Auth string
	API  string
}

// DefaultEndpoints are OpenAI's own.
func DefaultEndpoints() Endpoints {
	return Endpoints{Auth: "https://auth.openai.com", API: "https://api.openai.com/v1"}
}

// Error is a failure the page tells the person about: Code is stable and machine-readable, Detail is diagnostic text
// from OpenAI (never a token).
type Error struct {
	Code   string
	Status int
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return "chatgpt: " + e.Code + ": " + e.Detail
	}
	return "chatgpt: " + e.Code
}

func fail(code string, status int, detail string) *Error {
	if len(detail) > 300 {
		detail = detail[:300]
	}
	return &Error{Code: code, Status: status, Detail: detail}
}

// Codes of Error.
const (
	CodeDenied          = "denied"           // the person declined in the browser
	CodeState           = "state"            // no sign-in waits for this callback (old, used or forged)
	CodeBadCallback     = "bad-callback"     // the pasted address is not a callback
	CodeClient          = "client"           // the callback's client does not match the registration
	CodeExchange        = "exchange"         // the token endpoint refused the code
	CodeIDToken         = "id-token"         // the ID token did not check out
	CodeAccountMismatch = "account-mismatch" // a sign-in again came back as another account
	CodeSignedOut       = "signed-out"       // no usable session: sign in again
	CodeNoAccount       = "no-account"
	CodeNoPlan          = "no-plan"     // plan use not allowed for this app
	CodeUsageLimit      = "usage-limit" // the plan's (or the app's) limit is reached
	CodeNotEligible     = "not-eligible"
	CodeUnavailable     = "unavailable"
	CodeUnsupported     = "unsupported"
	CodeUpstream        = "upstream"
	CodeNetwork         = "network"
	CodeBusy            = "busy"
	CodeTooMany         = "too-many"
	CodeBadRequest      = "bad-request"
	CodeNotFound        = "not-found"
	CodeIncomplete      = "incomplete"
)

// login is one sign-in attempt waiting for its callback.
type login struct {
	state, verifier, nonce string
	redirect               string
	clientID               string // dynamic_agent_client for a new registration, else the issued one
	account                string // the account signing in again, "" for a new one
	subject                string // that account's identity, which the new ID token must match
	at                     time.Time
}

// LoginStart is what the page opens: the authorization address and the callback it ends on.
type LoginStart struct {
	URL      string `json:"url"`
	Redirect string `json:"redirect"`
}

// oauth runs the sign-ins of one runtime.
type oauth struct {
	ep   Endpoints
	http *http.Client
	accs *accounts
	now  func() time.Time
	// listen opens the loopback callback's socket; tests replace it.
	listen func(port int) (net.Listener, error)
	// onDone is told when a sign-in through the loopback listener ended (the page polls the state anyway).
	onDone func()

	mu     sync.Mutex
	logins map[string]*login
	cb     *callbackServer

	discMu sync.Mutex
	disc   *discovery
	keys   *oidc.RemoteKeySet
}

type discovery struct {
	Issuer     string `json:"issuer"`
	JWKS       string `json:"jwks_uri"`
	Revocation string `json:"revocation_endpoint"`
	at         time.Time
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// start opens a sign-in: a new registration (account "") or a sign-in again to a kept one.
func (o *oauth) start(account string) (LoginStart, error) {
	host, err := o.accs.hostID()
	if err != nil {
		return LoginStart{}, err
	}
	l := &login{state: randomToken(32), verifier: randomToken(48), nonce: randomToken(32), clientID: dynamicClient, at: o.now()}
	var acct Account
	if account != "" {
		if acct, err = o.accs.get(account); err != nil {
			return LoginStart{}, fail(CodeNoAccount, http.StatusNotFound, "")
		}
		l.clientID, l.account, l.subject = acct.ClientID, acct.Key, acct.Subject
	}
	port, err := o.ensureCallback()
	if err != nil {
		return LoginStart{}, err
	}
	l.redirect = "http://127.0.0.1:" + strconv.Itoa(port) + callbackPath

	o.mu.Lock()
	o.pruneLocked()
	if len(o.logins) >= loginsMax {
		o.mu.Unlock()
		return LoginStart{}, fail(CodeTooMany, http.StatusTooManyRequests, "")
	}
	o.logins[l.state] = l
	o.mu.Unlock()
	time.AfterFunc(loginTTL+time.Minute, o.closeCallbackIfIdle)

	q := url.Values{}
	q.Set("client_id", l.clientID)
	q.Set("ext_agent_host_id", host)
	if account == "" {
		q.Set("agent_name_hint", AgentName)
	} else if acct.Label != "" {
		// No id_token_hint: the address goes through the page and the browser's history, and an ID token is not to
		// lie there. The e-mail preselects the account; the subject check below refuses another one.
		q.Set("login_hint", acct.Label)
	}
	if account != "" && !acct.PlanUse() {
		// The person asked to allow plan use again after declining it: ask for consent once more.
		q.Set("prompt", "consent")
	}
	q.Set("response_type", "code")
	q.Set("redirect_uri", l.redirect)
	q.Set("scope", scopes)
	q.Set("resource", resource)
	q.Set("state", l.state)
	q.Set("nonce", l.nonce)
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", challengeOf(l.verifier))
	return LoginStart{URL: strings.TrimRight(o.ep.Auth, "/") + "/api/accounts/authorize?" + q.Encode(), Redirect: l.redirect}, nil
}

// pending says whether a sign-in waits.
func (o *oauth) pending() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneLocked()
	return len(o.logins) > 0
}

// cancel drops every waiting sign-in.
func (o *oauth) cancel() {
	o.mu.Lock()
	o.logins = map[string]*login{}
	o.mu.Unlock()
	o.closeCallbackIfIdle()
}

func (o *oauth) pruneLocked() {
	for k, l := range o.logins {
		if o.now().Sub(l.at) > loginTTL {
			delete(o.logins, k)
		}
	}
}

// take removes and returns the sign-in a callback's state names: each attempt is used once. fits, when given, must
// accept the attempt first: a callback that does not fit it leaves the attempt waiting.
func (o *oauth) take(state string, fits func(*login) bool) (*login, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneLocked()
	for k, l := range o.logins {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			if fits != nil && !fits(l) {
				return nil, false
			}
			delete(o.logins, k)
			return l, true
		}
	}
	return nil, true
}

// completePasted ends a sign-in from the callback's address the person pasted from their browser.
func (o *oauth) completePasted(ctx context.Context, raw string) (AccountView, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != callbackPath {
		return AccountView{}, fail(CodeBadCallback, http.StatusBadRequest, "")
	}
	q := u.Query()
	// The address must be the one this attempt sent: same port, so the exchange's redirect_uri matches.
	l, fits := o.take(q.Get("state"), func(l *login) bool { return "http://"+u.Host+u.Path == l.redirect })
	if !fits {
		return AccountView{}, fail(CodeBadCallback, http.StatusBadRequest, "")
	}
	if l == nil {
		return AccountView{}, fail(CodeState, http.StatusConflict, "")
	}
	defer o.closeCallbackIfIdle()
	return o.finish(ctx, l, q)
}

// completeQuery ends a sign-in from a callback the loopback listener received.
func (o *oauth) completeQuery(ctx context.Context, q url.Values) (AccountView, error) {
	l, _ := o.take(q.Get("state"), nil)
	if l == nil {
		return AccountView{}, fail(CodeState, http.StatusConflict, "")
	}
	return o.finish(ctx, l, q)
}

// finish checks a callback of a known attempt, exchanges its code and keeps the account.
func (o *oauth) finish(ctx context.Context, l *login, q url.Values) (AccountView, error) {
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return AccountView{}, fail(CodeDenied, http.StatusForbidden, "")
		}
		return AccountView{}, fail(CodeExchange, http.StatusBadGateway, e)
	}
	code := q.Get("code")
	if code == "" {
		return AccountView{}, fail(CodeBadCallback, http.StatusBadRequest, "")
	}
	clientID := l.clientID
	issued := q.Get("client_id")
	switch {
	case l.clientID == dynamicClient:
		// A new registration must name the client it issued; dynamic_agent_client is the entry point, never kept.
		if issued == "" || issued == dynamicClient {
			return AccountView{}, fail(CodeClient, http.StatusBadGateway, "the callback names no issued client")
		}
		clientID = issued
	case issued != "" && issued != l.clientID:
		return AccountView{}, fail(CodeClient, http.StatusBadGateway, "the callback names another client")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", l.verifier)
	form.Set("redirect_uri", l.redirect)
	form.Set("resource", resource)
	tok, err := o.tokenCall(ctx, form)
	if err != nil {
		return AccountView{}, err
	}
	id, err := o.verifyID(ctx, tok.IDToken, clientID, l.nonce)
	if err != nil {
		return AccountView{}, err
	}
	if l.subject != "" && id.Subject != l.subject {
		return AccountView{}, fail(CodeAccountMismatch, http.StatusConflict, "")
	}
	key := l.account
	if key == "" {
		if same, ok := o.accs.byClient(clientID, id.Subject); ok {
			key = same.Key
		} else {
			key = newKey()
		}
	}
	// A refresh of the same account must not write its tokens over these, nor use the old refresh token after them.
	lock := o.accs.lockOf(key)
	lock.Lock()
	defer lock.Unlock()
	acct, err := o.accs.get(key)
	switch {
	case err == nil:
	case l.account == "":
		acct = Account{Key: key}
	default:
		return AccountView{}, fail(CodeNoAccount, http.StatusNotFound, "")
	}
	acct.ClientID, acct.Subject = clientID, id.Subject
	if id.Email != "" {
		acct.Label = id.Email
	}
	if id.Plan != "" {
		acct.Plan = id.Plan
	}
	acct.IDToken = tok.IDToken
	o.applyTokens(&acct, tok)
	if err := o.accs.put(acct, true); err != nil {
		return AccountView{}, err
	}
	return AccountView{Key: acct.Key, Label: acct.Label, Plan: acct.Plan, SignedIn: true, PlanUse: acct.PlanUse(), Active: true}, nil
}

// tokenResponse is the token endpoint's answer.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (o *oauth) applyTokens(a *Account, t *tokenResponse) {
	now := o.now()
	a.AccessToken = t.AccessToken
	if t.RefreshToken != "" {
		a.RefreshToken = t.RefreshToken
	}
	exp := t.ExpiresIn
	if exp <= 0 {
		exp = 3600
	}
	a.ExpiresAt = nowMs(now.Add(time.Duration(exp) * time.Second))
	if t.Scope != "" {
		a.Scopes = strings.Fields(t.Scope)
	}
	a.SavedAt = nowMs(now)
}

// oauthFailure is the token endpoint's error body.
type oauthFailure struct {
	Error       any    `json:"error"`
	Description string `json:"error_description"`
	Code        string `json:"code"`
}

func (f oauthFailure) code() string {
	switch e := f.Error.(type) {
	case string:
		return e
	case map[string]any:
		if c, ok := e["code"].(string); ok {
			return c
		}
	}
	return f.Code
}

// tokenCall posts a grant to the token endpoint.
func (o *oauth) tokenCall(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.ep.Auth, "/")+"/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := o.http.Do(req)
	if err != nil {
		return nil, fail(CodeNetwork, http.StatusBadGateway, "")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, httpBodyMax))
	if res.StatusCode != http.StatusOK {
		var f oauthFailure
		_ = json.Unmarshal(raw, &f)
		c := f.code()
		switch {
		case res.StatusCode >= 500:
			return nil, fail(CodeUnavailable, http.StatusBadGateway, c)
		case unusableRefresh(c) && form.Get("grant_type") == "refresh_token", c == "invalid_client":
			return nil, fail(CodeSignedOut, http.StatusUnauthorized, c)
		}
		return nil, fail(CodeExchange, http.StatusBadGateway, c)
	}
	var t tokenResponse
	if json.Unmarshal(raw, &t) != nil || t.AccessToken == "" {
		return nil, fail(CodeExchange, http.StatusBadGateway, "the token endpoint answered no access token")
	}
	if form.Get("grant_type") == "authorization_code" && t.IDToken == "" {
		return nil, fail(CodeIDToken, http.StatusBadGateway, "no ID token")
	}
	return &t, nil
}

// unusableRefresh: a refresh error after which only a new sign-in helps.
func unusableRefresh(code string) bool {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	}
	return false
}

// discover reads OpenAI's OpenID configuration (keys and revocation endpoint), kept for an hour.
func (o *oauth) discover(ctx context.Context) (*discovery, *oidc.RemoteKeySet, error) {
	o.discMu.Lock()
	defer o.discMu.Unlock()
	if o.disc != nil && o.now().Sub(o.disc.at) < time.Hour {
		return o.disc, o.keys, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(o.ep.Auth, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := o.http.Do(req)
	if err != nil {
		return nil, nil, fail(CodeNetwork, http.StatusBadGateway, "")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, httpBodyMax))
	var d discovery
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &d) != nil || d.JWKS == "" {
		return nil, nil, fail(CodeUnavailable, http.StatusBadGateway, "OpenID configuration")
	}
	if strings.TrimRight(d.Issuer, "/") != strings.TrimRight(o.ep.Auth, "/") {
		return nil, nil, fail(CodeIDToken, http.StatusBadGateway, "the OpenID configuration names another issuer")
	}
	if !o.openAIAddress(d.JWKS) {
		return nil, nil, fail(CodeIDToken, http.StatusBadGateway, "the keys' address is not OpenAI's")
	}
	if d.Revocation != "" && !o.openAIAddress(d.Revocation) {
		d.Revocation = ""
	}
	d.at = o.now()
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), o.http), d.JWKS)
	if o.disc == nil || o.disc.JWKS != d.JWKS {
		o.keys = keys
	}
	o.disc = &d
	return o.disc, o.keys, nil
}

// openAIAddress: an address a token or a key may be fetched from or sent to — https on the issuer's host or another
// openai.com host, or the issuer's own loopback test server.
func (o *oauth) openAIAddress(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	auth, err := url.Parse(o.ep.Auth)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if u.Scheme == "https" {
		return host == auth.Hostname() || strings.HasSuffix(host, ".openai.com")
	}
	ip := net.ParseIP(host)
	return u.Scheme == "http" && ip != nil && ip.IsLoopback() && u.Host == auth.Host
}

// idClaims is what the account takes from a checked ID token.
type idClaims struct {
	Subject string
	Email   string
	Plan    string
}

// verifyID checks the ID token's signature against OpenAI's keys, its issuer, audience (the issued client), expiry
// and the nonce of this attempt.
func (o *oauth) verifyID(ctx context.Context, raw, clientID, nonce string) (idClaims, error) {
	_, keys, err := o.discover(ctx)
	if err != nil {
		return idClaims{}, err
	}
	v := oidc.NewVerifier(strings.TrimRight(o.ep.Auth, "/"), keys, &oidc.Config{ClientID: clientID, Now: o.now})
	tok, err := v.Verify(oidc.ClientContext(ctx, o.http), raw)
	if err != nil {
		return idClaims{}, fail(CodeIDToken, http.StatusBadGateway, err.Error())
	}
	if subtle.ConstantTimeCompare([]byte(tok.Nonce), []byte(nonce)) != 1 {
		return idClaims{}, fail(CodeIDToken, http.StatusBadGateway, "nonce")
	}
	if tok.Subject == "" {
		return idClaims{}, fail(CodeIDToken, http.StatusBadGateway, "no subject")
	}
	var c struct {
		Email string `json:"email"`
		Auth  struct {
			Plan string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	_ = tok.Claims(&c)
	return idClaims{Subject: tok.Subject, Email: c.Email, Plan: c.Auth.Plan}, nil
}

// token is a usable access token of an account, renewed first when it is about to expire. One refresh per account
// runs at a time: the refresh token rotates, and a second use of the old one ends the session.
func (o *oauth) token(ctx context.Context, key string) (string, error) {
	lock := o.accs.lockOf(key)
	lock.Lock()
	defer lock.Unlock()
	a, err := o.accs.get(key)
	if err != nil {
		return "", fail(CodeNoAccount, http.StatusNotFound, "")
	}
	if !a.SignedIn() {
		return "", fail(CodeSignedOut, http.StatusUnauthorized, "")
	}
	if !a.PlanUse() {
		return "", fail(CodeNoPlan, http.StatusForbidden, "")
	}
	if a.AccessToken != "" && o.now().Add(refreshAhead).UnixMilli() < a.ExpiresAt {
		return a.AccessToken, nil
	}
	if a.RefreshToken == "" {
		return "", fail(CodeSignedOut, http.StatusUnauthorized, "")
	}
	// The refresh runs to its end whatever happens to the turn that asked for it: a rotated refresh token that is not
	// kept ends the session.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	ctx = rctx
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", a.ClientID)
	form.Set("refresh_token", a.RefreshToken)
	form.Set("resource", resource)
	t, err := o.tokenCall(ctx, form)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == CodeSignedOut {
			_ = o.accs.update(key, func(x *Account) { x.clearTokens() })
		}
		return "", err
	}
	var access string
	err = o.accs.update(key, func(x *Account) {
		o.applyTokens(x, t)
		if t.IDToken != "" {
			x.IDToken = t.IDToken
		}
		access = x.AccessToken
	})
	if err != nil {
		return "", err
	}
	return access, nil
}

// signOut revokes the account's renewable session and forgets its tokens; the registration (client and identity)
// stays for a later sign-in. revoked says OpenAI confirmed the revocation.
func (o *oauth) signOut(ctx context.Context, key string) (revoked bool, err error) {
	lock := o.accs.lockOf(key)
	lock.Lock()
	defer lock.Unlock()
	a, err := o.accs.get(key)
	if err != nil {
		return false, fail(CodeNoAccount, http.StatusNotFound, "")
	}
	if a.RefreshToken != "" {
		revoked = o.revoke(ctx, a.ClientID, a.RefreshToken)
	} else {
		revoked = true
	}
	return revoked, o.accs.update(key, func(x *Account) { x.clearTokens() })
}

// revoke posts the refresh token to OpenAI's revocation endpoint, a few times on a network failure or a 5xx.
func (o *oauth) revoke(ctx context.Context, clientID, refresh string) bool {
	d, _, err := o.discover(ctx)
	if err != nil || d.Revocation == "" {
		return false
	}
	form := url.Values{}
	form.Set("token", refresh)
	form.Set("token_type_hint", "refresh_token")
	form.Set("client_id", clientID)
	for i := range 3 {
		if i > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Duration(i) * 500 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Revocation, strings.NewReader(form.Encode()))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := o.http.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, httpBodyMax))
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			return true
		}
		if res.StatusCode < 500 {
			return false
		}
	}
	return false
}

// --- the loopback callback ---

// callbackServer listens on 127.0.0.1 while a sign-in waits: when the person's browser runs on this machine it lands
// here and the sign-in ends by itself.
type callbackServer struct {
	ln   net.Listener
	srv  *http.Server
	port int
}

func defaultListen(port int) (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
}

// ensureCallback starts the loopback listener once; the usual port when free, any other otherwise.
func (o *oauth) ensureCallback() (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cb != nil {
		return o.cb.port, nil
	}
	ln, err := o.listen(callbackPort)
	if err != nil {
		if ln, err = o.listen(0); err != nil {
			return 0, fmt.Errorf("chatgpt: no loopback port for the callback: %w", err)
		}
	}
	cb := &callbackServer{ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, o.serveCallback)
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	o.cb = cb
	go func() { _ = cb.srv.Serve(ln) }()
	return cb.port, nil
}

func (o *oauth) serveCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := o.completeQuery(ctx, r.URL.Query())
	defer o.closeCallbackIfIdle()
	if o.onDone != nil {
		defer o.onDone()
	}
	text := "Готово: вход через ChatGPT выполнен. Вернитесь в harnessly. / Signed in with ChatGPT. Return to harnessly."
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		text = "Вход не удался — вернитесь в harnessly и попробуйте снова. / Sign-in failed: return to harnessly and try again."
	}
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>harnessly</title><body style="font:16px system-ui;margin:48px">`+text+`</body>`)
}

// closeCallbackIfIdle stops the listener once no sign-in waits.
func (o *oauth) closeCallbackIfIdle() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneLocked()
	if len(o.logins) > 0 || o.cb == nil {
		return
	}
	cb := o.cb
	o.cb = nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cb.srv.Shutdown(ctx)
	}()
}
