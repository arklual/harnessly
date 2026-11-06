// Package vaultkv is a small client of the key/value engine of a Vault-compatible server (OpenBao or HashiCorp
// Vault): read, write, list and delete one path of a mount, KV version 1 or 2, logged in with a token file, AppRole,
// Kubernetes or JWT. It is what the safe needs and nothing more; it never puts a value into an error or a log line.
//
// What it speaks, so that any server of the family answers it the same way:
//
//	GET    /v1/sys/internal/ui/mounts/<mount>        the engine's version (falls back to Config.KVVersion, then 2)
//	GET    /v1/<mount>/<path>                        v1 read      GET  /v1/<mount>/data/<path>      v2 read
//	PUT    /v1/<mount>/<path>                        v1 write     PUT  /v1/<mount>/data/<path>      v2 write {data}
//	DELETE /v1/<mount>/<path>                        v1 delete    DELETE /v1/<mount>/metadata/<path> v2 delete
//	GET    /v1/<mount>/<path>/?list=true             v1 list      GET  /v1/<mount>/metadata/<path>/?list=true
//	POST   /v1/auth/<auth mount>/login               approle {role_id, secret_id}; kubernetes and jwt {role, jwt}
//
// with X-Vault-Token and, when set, X-Vault-Namespace. A 403 logs in again once (the token may have expired or been
// revoked) and then is denied; a 5xx or a network error is unavailable; a 404 is not-found.
package vaultkv

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// bodyMax is the largest answer read from the server.
const bodyMax = 1 << 20

// Config is where the server is and how to log in.
type Config struct {
	// Addr is the server's base URL, such as https://openbao.example.com:8200.
	Addr string
	// Mount is the KV engine's mount, "secret" when empty.
	Mount string
	// Namespace goes into X-Vault-Namespace when set (Vault Enterprise and OpenBao namespaces).
	Namespace string
	// KVVersion is the engine's version when the server does not say it (0: ask, then 2).
	KVVersion int
	Auth      Auth
	// HTTP is the client to use; nil builds one with a 15 s timeout (and CACert, when set).
	HTTP *http.Client
	// CACert is a PEM file of the server's CA, for a private one.
	CACert string
	Now    func() time.Time
}

// Auth logs in and gives a token with its lifetime (0: as long as it works).
type Auth interface {
	Login(ctx context.Context, c *Client) (token string, ttl time.Duration, err error)
	// Method names the way in for the page and the log: token-file, approle, kubernetes, jwt.
	Method() string
}

// Client is one server and its token.
type Client struct {
	cfg  Config
	http *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
	version int
}

var segmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidPath says whether p may be used as a path in a mount: segments of letters, digits, '.', '_' and '-' joined by
// '/', no empty, "." or ".." segment, 512 bytes at most.
func ValidPath(p string) bool {
	if p == "" || len(p) > 512 {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "." || s == ".." || !segmentRe.MatchString(s) {
			return false
		}
	}
	return true
}

// New checks the config and builds the client; it does not reach the server.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(cfg.Addr, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("vault address must be an http(s) URL")
	}
	cfg.Addr = u.String()
	cfg.Mount = strings.Trim(cfg.Mount, "/")
	if cfg.Mount == "" {
		cfg.Mount = "secret"
	}
	if !ValidPath(cfg.Mount) {
		return nil, errors.New("vault mount is not a path")
	}
	if cfg.Auth == nil {
		return nil, errors.New("vault auth is not set")
	}
	if cfg.KVVersion != 0 && cfg.KVVersion != 1 && cfg.KVVersion != 2 {
		return nil, errors.New("vault kv version must be 1 or 2")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	hc := cfg.HTTP
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.CACert != "" {
			pem, err := os.ReadFile(cfg.CACert)
			if err != nil {
				return nil, fmt.Errorf("vault CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("vault CA: no certificate in the file")
			}
			tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
		hc = &http.Client{Timeout: 15 * time.Second, Transport: tr}
	}
	return &Client{cfg: cfg, http: hc}, nil
}

// Addr is the server's address, Mount the engine's mount and Method the way in: for the page's "where".
func (c *Client) Addr() string   { return c.cfg.Addr }
func (c *Client) Mount() string  { return c.cfg.Mount }
func (c *Client) Method() string { return c.cfg.Auth.Method() }

func refuse(reason string, format string, args ...any) error {
	return &secrets.Refusal{Reason: reason, Err: fmt.Errorf(format, args...)}
}

// statusError is an answer the server refused, with its status and its own words (which never hold a value we sent).
type statusError struct {
	status int
	words  string
}

func (e *statusError) Error() string {
	if e.words == "" {
		return fmt.Sprintf("vault answered %d", e.status)
	}
	return fmt.Sprintf("vault answered %d: %s", e.status, e.words)
}

func reasonOfStatus(status int) string {
	switch {
	case status == http.StatusNotFound:
		return secrets.ReasonNotFound
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		return secrets.ReasonDenied
	case status == http.StatusTooManyRequests || status >= 500:
		return secrets.ReasonUnavailable
	default:
		return secrets.ReasonFailed
	}
}

// do sends one request with token (none for a login) and decodes a JSON answer into out (nil: drop it).
func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return refuse(secrets.ReasonFailed, "encode: %v", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Addr+"/v1/"+path, rd)
	if err != nil {
		return refuse(secrets.ReasonFailed, "request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if c.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.cfg.Namespace)
	}
	req.Header.Set("X-Vault-Request", "true")
	resp, err := c.http.Do(req)
	if err != nil {
		// A url.Error names the method and the URL (no value), and why.
		return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyMax+1))
	if err != nil {
		return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: err}
	}
	if len(raw) > bodyMax {
		return refuse(secrets.ReasonFailed, "vault answer over %d bytes", bodyMax)
	}
	if resp.StatusCode == http.StatusNoContent || (resp.StatusCode < 300 && out == nil) {
		return nil
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		words := strings.Join(e.Errors, "; ")
		if len(words) > 300 {
			words = words[:300]
		}
		return &secrets.Refusal{Reason: reasonOfStatus(resp.StatusCode), Err: &statusError{status: resp.StatusCode, words: words}}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return refuse(secrets.ReasonFailed, "decode %s: %v", strings.SplitN(path, "/", 2)[0], err)
	}
	return nil
}

// tokenFor is the token to use, logged in when there is none, it has expired or fresh says so.
func (c *Client) tokenFor(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if !fresh && c.token != "" && (c.expires.IsZero() || now.Before(c.expires)) {
		return c.token, nil
	}
	token, ttl, err := c.cfg.Auth.Login(ctx, c)
	if err != nil {
		c.token = ""
		return "", err
	}
	c.token = token
	c.expires = time.Time{}
	if ttl > 0 {
		// Log in again a little before the token ends.
		c.expires = now.Add(ttl - ttl/10)
	}
	return token, nil
}

// call sends a request with the token, and once more with a new one after a 403.
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.tokenFor(ctx, false)
	if err != nil {
		return err
	}
	err = c.do(ctx, method, path, token, body, out)
	if secrets.ReasonOf(err) != secrets.ReasonDenied {
		return err
	}
	if token, err = c.tokenFor(ctx, true); err != nil {
		return err
	}
	return c.do(ctx, method, path, token, body, out)
}

// kvVersion is the engine's version: asked once, then remembered.
func (c *Client) kvVersion(ctx context.Context) int {
	c.mu.Lock()
	v := c.version
	c.mu.Unlock()
	if v != 0 {
		return v
	}
	var out struct {
		Data struct {
			Type    string            `json:"type"`
			Options map[string]string `json:"options"`
		} `json:"data"`
	}
	// Not call: a narrow policy often forbids this path, and a 403 here is no reason to log in again.
	token, err := c.tokenFor(ctx, false)
	if err == nil {
		err = c.do(ctx, http.MethodGet, "sys/internal/ui/mounts/"+c.cfg.Mount, token, nil, &out)
	}
	switch {
	case err == nil && out.Data.Options["version"] == "2":
		v = 2
	case err == nil && (out.Data.Type == "kv" || out.Data.Type == "generic"):
		v = 1
	case c.cfg.KVVersion != 0:
		// The server would not say (a policy without that path): the configured one.
		v = c.cfg.KVVersion
	case secrets.ReasonOf(err) == secrets.ReasonUnavailable:
		// Do not remember a guess made while the server is away.
		return 2
	default:
		v = 2
	}
	c.mu.Lock()
	c.version = v
	c.mu.Unlock()
	return v
}

func (c *Client) checkPath(p string) error {
	if !ValidPath(p) {
		return refuse(secrets.ReasonBadName, "not a path")
	}
	return nil
}

// Read is the key/value pairs at p; not-found when there is none. Only string values are returned: a number or an
// object a person put by hand comes back as its JSON.
func (c *Client) Read(ctx context.Context, p string) (map[string]string, error) {
	if err := c.checkPath(p); err != nil {
		return nil, err
	}
	var data map[string]any
	if c.kvVersion(ctx) == 2 {
		var out struct {
			Data struct {
				Data map[string]any `json:"data"`
			} `json:"data"`
		}
		if err := c.call(ctx, http.MethodGet, c.cfg.Mount+"/data/"+p, nil, &out); err != nil {
			return nil, err
		}
		data = out.Data.Data
	} else {
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := c.call(ctx, http.MethodGet, c.cfg.Mount+"/"+p, nil, &out); err != nil {
			return nil, err
		}
		data = out.Data
	}
	if data == nil {
		// v2 answers 200 with null data for a version that was deleted.
		return nil, refuse(secrets.ReasonNotFound, "deleted")
	}
	res := make(map[string]string, len(data))
	for k, v := range data {
		if s, ok := v.(string); ok {
			res[k] = s
			continue
		}
		raw, _ := json.Marshal(v)
		res[k] = string(raw)
	}
	return res, nil
}

// Write replaces the key/value pairs at p.
func (c *Client) Write(ctx context.Context, p string, data map[string]string) error {
	if err := c.checkPath(p); err != nil {
		return err
	}
	if c.kvVersion(ctx) == 2 {
		return c.call(ctx, http.MethodPut, c.cfg.Mount+"/data/"+p, map[string]any{"data": data}, nil)
	}
	return c.call(ctx, http.MethodPut, c.cfg.Mount+"/"+p, data, nil)
}

// Delete removes p: in KV v2 every version with its metadata, or, when the policy does not allow that, the latest
// version.
func (c *Client) Delete(ctx context.Context, p string) error {
	if err := c.checkPath(p); err != nil {
		return err
	}
	if c.kvVersion(ctx) == 2 {
		err := c.call(ctx, http.MethodDelete, c.cfg.Mount+"/metadata/"+p, nil, nil)
		if secrets.ReasonOf(err) == secrets.ReasonDenied {
			return c.call(ctx, http.MethodDelete, c.cfg.Mount+"/data/"+p, nil, nil)
		}
		return err
	}
	return c.call(ctx, http.MethodDelete, c.cfg.Mount+"/"+p, nil, nil)
}

// List is the keys right under p ("" for the mount's root); a folder ends with '/'. A path with nothing under it is
// an empty list.
func (c *Client) List(ctx context.Context, p string) ([]string, error) {
	if p != "" {
		if err := c.checkPath(p); err != nil {
			return nil, err
		}
		p += "/"
	}
	path := c.cfg.Mount + "/" + p
	if c.kvVersion(ctx) == 2 {
		path = c.cfg.Mount + "/metadata/" + p
	}
	var out struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	err := c.call(ctx, http.MethodGet, path+"?list=true", nil, &out)
	if secrets.ReasonOf(err) == secrets.ReasonNotFound {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return out.Data.Keys, nil
}

// loginAnswer is what every auth method answers.
type loginAnswer struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int64  `json:"lease_duration"`
	} `json:"auth"`
}

func (c *Client) login(ctx context.Context, mount string, body map[string]string) (string, time.Duration, error) {
	var out loginAnswer
	if err := c.do(ctx, http.MethodPost, "auth/"+strings.Trim(mount, "/")+"/login", "", body, &out); err != nil {
		var r *secrets.Refusal
		var st *statusError
		if errors.As(err, &r) && (r.Reason == secrets.ReasonNotFound ||
			(r.Reason == secrets.ReasonFailed && errors.As(err, &st))) {
			// No such auth mount or role, or credentials the server turns down (a 400 for a wrong secret ID or
			// JWT): a login that cannot work, the same as a 403 for the person reading the page.
			r.Reason = secrets.ReasonDenied
		}
		return "", 0, err
	}
	if out.Auth.ClientToken == "" {
		return "", 0, refuse(secrets.ReasonDenied, "login gave no token")
	}
	return out.Auth.ClientToken, time.Duration(out.Auth.LeaseDuration) * time.Second, nil
}

func readSecretFile(path, what string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", refuse(secrets.ReasonDenied, "%s: %v", what, err)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return "", refuse(secrets.ReasonDenied, "%s: the file is empty", what)
	}
	return v, nil
}

// TokenFile reads a token from a file at every login, so a token an agent of the server (bao agent, vault agent)
// renews in the file is picked up after a 403.
type TokenFile struct{ Path string }

func (a TokenFile) Method() string { return "token-file" }

func (a TokenFile) Login(context.Context, *Client) (string, time.Duration, error) {
	t, err := readSecretFile(a.Path, "token file")
	return t, 0, err
}

// AppRole logs in with a role id and a secret id read from a file.
type AppRole struct {
	Mount, RoleID, SecretIDFile string
}

func (a AppRole) Method() string { return "approle" }

func (a AppRole) Login(ctx context.Context, c *Client) (string, time.Duration, error) {
	secretID, err := readSecretFile(a.SecretIDFile, "approle secret id")
	if err != nil {
		return "", 0, err
	}
	return c.login(ctx, orDefault(a.Mount, "approle"), map[string]string{"role_id": a.RoleID, "secret_id": secretID})
}

// JWT logs in with a role and a token read from a file: the kubernetes method with the pod's service account token,
// or the jwt method with any OIDC token the server trusts.
type JWT struct {
	// Kind is "kubernetes" or "jwt": the method's name and its default mount.
	Kind, Mount, Role, TokenFile string
}

func (a JWT) Method() string { return a.Kind }

func (a JWT) Login(ctx context.Context, c *Client) (string, time.Duration, error) {
	jwt, err := readSecretFile(a.TokenFile, a.Kind+" token")
	if err != nil {
		return "", 0, err
	}
	return c.login(ctx, orDefault(a.Mount, a.Kind), map[string]string{"role": a.Role, "jwt": jwt})
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return strings.TrimSpace(v)
}
