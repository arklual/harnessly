// API NOTE: the contract's names and shapes are kept; the differences are additions. Calls that reach the network take
// a ctx first (Add, StartSignIn, FinishSignIn, SetHeader, Check). FinishSignIn answers the server (with its ID) even
// with an error once the state was known, so the callback page can name it. Reauthorize(ctx, id) forces a token
// refresh for the proxy's "401 → refresh once and retry". Get(id) reads one server. Reason(err) maps errors to the
// route reasons. Enabled() leaves out servers waiting for a sign-in.

package mcpconn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
)

// Secrets is where credentials are kept: the safe, by name (the shape of connections.Secrets).
type Secrets interface {
	Put(name, value string) error
	Reveal(name string) (string, error)
	Remove(name string) error
}

type lockT = sync.Mutex

// Service keeps the MCP servers of one runtime.
type Service struct {
	file    string
	secrets Secrets
	http    *connections.HTTP
	now     func() time.Time

	// mu guards the file.
	mu sync.Mutex

	flowMu sync.Mutex
	flows  map[string]flow

	lockMu sync.Mutex
	locks  map[string]*lockT
}

// New keeps servers in dir/mcp-connections.json and their credentials in secrets. h nil is connections.NewHTTP(nil):
// it refuses link-local (cloud metadata) addresses and caps answers.
func New(dir string, secrets Secrets, h *connections.HTTP) *Service {
	if h == nil {
		h = connections.NewHTTP(nil)
	}
	return &Service{
		file:    filepath.Join(dir, "mcp-connections.json"),
		secrets: secrets,
		http:    h,
		now:     time.Now,
		flows:   map[string]flow{},
		locks:   map[string]*lockT{},
	}
}

type stored struct {
	Version int            `json:"version"`
	Servers []Server       `json:"servers"`
	Clients []registration `json:"clients,omitempty"`
}

func (s *Service) readAll() (stored, error) {
	raw, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return stored{}, nil
	}
	if err != nil {
		return stored{}, err
	}
	var f stored
	if err := json.Unmarshal(raw, &f); err != nil {
		return stored{}, fmt.Errorf("mcp-connections.json: %w", err)
	}
	return f, nil
}

func (s *Service) writeAll(f stored) error {
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	f.Version = 1
	if f.Servers == nil {
		f.Servers = []Server{}
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.file), ".mcp-connections-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.file)
}

// List is every server, in the order they were added.
func (s *Service) List() ([]Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return nil, err
	}
	if f.Servers == nil {
		return []Server{}, nil
	}
	return f.Servers, nil
}

// Get is one server by id.
func (s *Service) Get(id string) (Server, error) {
	list, err := s.List()
	if err != nil {
		return Server{}, err
	}
	for _, srv := range list {
		if srv.ID == id {
			return srv, nil
		}
	}
	return Server{}, fmt.Errorf("%w: server %s", ErrNotFound, id)
}

// Enabled is every server agents get: turned on and not waiting for a sign-in.
func (s *Service) Enabled() []Server {
	list, err := s.List()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(list, func(srv Server) bool { return !srv.Enabled || srv.State == StateSignIn || srv.Owner != "" })
}

// Catalog is the well-known servers the page offers.
func (s *Service) Catalog() []CatalogEntry { return slices.Clone(catalog) }

// Probe finds how a pasted URL speaks MCP and signs in.
func (s *Service) Probe(ctx context.Context, rawURL string) (*ProbeResult, error) {
	return s.probe(ctx, rawURL, "")
}

// HeaderInput is a typed credential: a header name (default Authorization) and its value.
type HeaderInput struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// AddInput is what the page sends to connect a server.
type AddInput struct {
	URL       string `json:"url"`
	Name      string `json:"name,omitempty"`
	CatalogID string `json:"catalogId,omitempty"`
	// Transport is a hint: SSE tries the legacy transport first. The probe decides.
	Transport Transport    `json:"transport,omitempty"`
	Header    *HeaderInput `json:"header,omitempty"`
	// Owner makes the server one app's own sign-in (Server.Owner); the runtime sets it, a request body cannot.
	Owner string `json:"-"`
}

// AuthStart says what the page asks next after Add: an OAuth sign-in (StartSignIn) or a header (SetHeader).
type AuthStart struct {
	Auth             Auth   `json:"auth"`
	HeaderName       string `json:"headerName,omitempty"`
	OAuthUnavailable bool   `json:"oauthUnavailable,omitempty"`
	TokenURL         string `json:"tokenUrl,omitempty"`
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "m" + hex.EncodeToString(b)
}

func secretName(id string) string { return "mcp-" + id }

// authStart is the next step a server waits for; nil when it is connected.
func authStart(srv Server) *AuthStart {
	if srv.State != StateSignIn {
		return nil
	}
	a := &AuthStart{Auth: srv.Auth, HeaderName: srv.HeaderName, OAuthUnavailable: srv.OAuthUnavailable}
	if e, ok := catalogEntry(srv.CatalogID); ok {
		a.TokenURL = e.TokenURL
	}
	return a
}

// ByOwner is the sign-in kept for one app's server (Server.Owner), if any.
func (s *Service) ByOwner(owner string) (Server, bool) {
	if owner == "" {
		return Server{}, false
	}
	list, err := s.List()
	if err != nil {
		return Server{}, false
	}
	for _, srv := range list {
		if srv.Owner == owner {
			return srv, true
		}
	}
	return Server{}, false
}

// Add probes a server and keeps it. A server without auth (or with the header given) is checked at once; an OAuth one
// is kept in state "signin" and AuthStart says so. A typed header that the server refuses is ErrDenied and nothing is
// kept. Adding a URL that is already connected answers that server.
func (s *Service) Add(ctx context.Context, in AddInput) (Server, *AuthStart, error) {
	entry, fromCatalog := catalogEntry(in.CatalogID)
	if in.CatalogID != "" && !fromCatalog {
		return Server{}, nil, fmt.Errorf("%w: unknown catalog entry", ErrBadRequest)
	}
	if in.URL == "" && fromCatalog {
		in.URL = entry.URL
	}
	if in.Transport == "" && fromCatalog {
		in.Transport = entry.Transport
	}
	canon, _, err := canonical(in.URL)
	if err != nil {
		return Server{}, nil, err
	}
	if list, err := s.List(); err == nil {
		for _, srv := range list {
			if srv.URL == canon && srv.Owner == in.Owner {
				return srv, authStart(srv), nil
			}
		}
	}
	p, err := s.probe(ctx, in.URL, in.Transport)
	if err != nil {
		return Server{}, nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" && fromCatalog {
		name = entry.Name
	}
	if name == "" {
		name = p.Name
	}
	srv := Server{
		ID:               newID(),
		Name:             oneLine(name),
		URL:              p.URL,
		Transport:        p.Transport,
		Auth:             p.Auth,
		HeaderName:       p.HeaderName,
		OAuthUnavailable: p.OAuthUnavailable,
		CatalogID:        in.CatalogID,
		Owner:            in.Owner,
		Enabled:          in.Owner == "",
		State:            StateSignIn,
		CreatedAt:        s.now().UTC(),
	}
	if srv.Auth == AuthHeader && fromCatalog && entry.HeaderName != "" {
		srv.HeaderName = entry.HeaderName
	}

	if in.Header != nil && strings.TrimSpace(in.Header.Value) != "" {
		hname, value, err := headerOf(in.Header.Name, in.Header.Value)
		if err != nil {
			return Server{}, nil, err
		}
		srv.Auth, srv.HeaderName = AuthHeader, hname
		tools, err := s.inspect(ctx, srv, http.Header{hname: {value}})
		if errors.Is(err, ErrDenied) {
			return Server{}, nil, err
		}
		s.applyCheck(&srv, tools, err)
		if err := s.writeCred(srv.ID, cred{Header: value}); err != nil {
			return Server{}, nil, err
		}
	} else if srv.Auth == AuthNone {
		tools, err := s.inspect(ctx, srv, nil)
		s.applyCheck(&srv, tools, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Server{}, nil, err
	}
	srv.Slug = uniqueSlug(slugOf(srv.Name), f.Servers, srv.ID)
	f.Servers = append(f.Servers, srv)
	if err := s.writeAll(f); err != nil {
		return Server{}, nil, err
	}
	return srv, authStart(srv), nil
}

// StartSignIn starts the OAuth sign-in of a server: it rediscovers the server's authorization server, registers the
// runtime as a client for redirectURI when it is not yet, and answers the URL the person's browser opens.
func (s *Service) StartSignIn(ctx context.Context, id, redirectURI string) (string, error) {
	ru, err := url.Parse(redirectURI)
	if err != nil || (ru.Scheme != "https" && ru.Scheme != "http") || ru.Host == "" || ru.Fragment != "" {
		return "", fmt.Errorf("%w: bad redirect URI", ErrBadRequest)
	}
	srv, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if srv.Auth != AuthOAuth {
		return "", fmt.Errorf("%w: the server does not sign in with OAuth", ErrBadRequest)
	}
	p, err := s.probe(ctx, srv.URL, srv.Transport)
	if err != nil {
		return "", err
	}
	if p.oauth == nil {
		return "", fmt.Errorf("%w: the server no longer offers an OAuth sign-in", ErrOAuthFailed)
	}
	c, err := s.register(ctx, p.oauth, redirectURI)
	if err != nil {
		return "", err
	}
	return s.startFlow(id, redirectURI, c, *p.oauth)
}

// FinishSignIn completes a sign-in from the callback's state, code and error parameters: it exchanges the code (with
// the PKCE verifier and the resource), keeps the tokens and checks the server. Once the state is known the server is
// answered even with an error. An unknown, reused or expired state is ErrBadRequest; a refusal at the service is
// ErrDenied (access_denied) or ErrOAuthFailed.
func (s *Service) FinishSignIn(ctx context.Context, state, code, errParam string) (Server, error) {
	fl, ok := s.takeFlow(state)
	if state == "" || !ok {
		return Server{}, fmt.Errorf("%w: unknown or expired sign-in", ErrBadRequest)
	}
	srv, err := s.Get(fl.serverID)
	if err != nil {
		return Server{}, err
	}
	switch {
	case errParam == "access_denied":
		return srv, fmt.Errorf("%w: the sign-in was declined", ErrDenied)
	case errParam != "":
		return srv, fmt.Errorf("%w: the service said %s", ErrOAuthFailed, safeWord(errParam))
	case code == "":
		return srv, fmt.Errorf("%w: no code", ErrBadRequest)
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {fl.redirectURI},
		"code_verifier": {fl.verifier},
		"resource":      {fl.d.Resource},
	}
	ans, err := s.token(ctx, fl.d.Token, fl.client, form)
	if errors.Is(err, errInvalidClient) {
		s.forgetClient(fl.d.AuthServer, fl.redirectURI)
	}
	if errors.Is(err, errInvalidGrant) {
		err = fmt.Errorf("%w: the code was refused", ErrOAuthFailed)
	}
	if err != nil {
		return srv, err
	}
	o := &oauthCred{
		ClientID:      fl.client.ID,
		ClientSecret:  fl.client.Secret,
		AuthMethod:    fl.client.Method,
		AccessToken:   ans.AccessToken,
		RefreshToken:  ans.RefreshToken,
		ExpiresAt:     s.expiry(ans.ExpiresIn),
		TokenEndpoint: fl.d.Token,
		Resource:      fl.d.Resource,
		Scope:         ans.Scope,
		AuthServer:    fl.d.AuthServer,
	}
	if o.Scope == "" {
		o.Scope = fl.d.Scope
	}
	lock := s.refreshLock(srv.ID)
	lock.Lock()
	err = s.writeCred(srv.ID, cred{OAuth: o})
	lock.Unlock()
	if err != nil {
		return srv, err
	}
	tools, cerr := s.inspect(ctx, srv, http.Header{"Authorization": {"Bearer " + o.AccessToken}})
	return s.update(srv.ID, func(x *Server) error {
		s.applyCheck(x, tools, cerr)
		return nil
	})
}

// SetHeader sets the typed credential of a server: name (default Authorization; a bare token there gets "Bearer ")
// and value. The server is checked with it first; a refusal is ErrDenied and nothing changes.
func (s *Service) SetHeader(ctx context.Context, id, name, value string) (Server, error) {
	srv, err := s.Get(id)
	if err != nil {
		return Server{}, err
	}
	hname, hvalue, err := headerOf(name, value)
	if err != nil {
		return Server{}, err
	}
	probe := srv
	probe.Auth, probe.HeaderName = AuthHeader, hname
	tools, cerr := s.inspect(ctx, probe, http.Header{hname: {hvalue}})
	if errors.Is(cerr, ErrDenied) {
		return Server{}, cerr
	}
	if err := s.writeCred(id, cred{Header: hvalue}); err != nil {
		return Server{}, err
	}
	s.dropFlows(id)
	return s.update(id, func(x *Server) error {
		x.Auth, x.HeaderName = AuthHeader, hname
		s.applyCheck(x, tools, cerr)
		return nil
	})
}

// Check signs in to a server as agents would, runs initialize and tools/list, and keeps the state and tools it
// found. The answer's state says how it went; the error is for a missing server or a broken store.
func (s *Service) Check(ctx context.Context, id string) (Server, error) {
	srv, err := s.Get(id)
	if err != nil {
		return Server{}, err
	}
	header, err := s.Authorize(ctx, id)
	if err != nil && !errors.Is(err, ErrSignInRequired) {
		return s.update(id, func(x *Server) error {
			s.applyCheck(x, nil, err)
			return nil
		})
	}
	if err != nil {
		return s.Get(id)
	}
	tools, cerr := s.inspect(ctx, srv, header)
	if errors.Is(cerr, ErrDenied) && srv.Auth == AuthOAuth {
		if header, err = s.Reauthorize(ctx, id); err == nil {
			tools, cerr = s.inspect(ctx, srv, header)
		} else if errors.Is(err, ErrSignInRequired) {
			return s.Get(id)
		}
	}
	return s.update(id, func(x *Server) error {
		s.applyCheck(x, tools, cerr)
		return nil
	})
}

// inspect runs initialize and tools/list.
func (s *Service) inspect(ctx context.Context, srv Server, header http.Header) ([]Tool, error) {
	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	sess, err := open(ctx, s.http.Client, srv.Transport, srv.URL, header)
	if err != nil {
		return nil, err
	}
	defer sess.close()
	if _, err := handshake(ctx, sess); err != nil {
		return nil, err
	}
	return listTools(ctx, sess)
}

// applyCheck writes the outcome of inspect into srv.
func (s *Service) applyCheck(srv *Server, tools []Tool, err error) {
	srv.CheckedAt = s.now().UTC()
	switch {
	case err == nil:
		srv.State = StateOK
		srv.Tools = tools
	case errors.Is(err, ErrSignInRequired):
		srv.State = StateSignIn
	case errors.Is(err, ErrDenied) && srv.Auth == AuthOAuth:
		srv.State = StateSignIn
	case errors.Is(err, ErrDenied):
		srv.State = StateDenied
	default:
		srv.State = StateUnreachable
	}
}

// Authorize is the headers that sign a request to the server in, fresh: an OAuth token is refreshed when it is about
// to expire. ErrSignInRequired when the person has to sign in (again).
func (s *Service) Authorize(ctx context.Context, id string) (http.Header, error) {
	srv, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return s.authorize(ctx, srv, false)
}

// Reauthorize is Authorize with an OAuth token refreshed whatever its expiry: for a request the server refused.
func (s *Service) Reauthorize(ctx context.Context, id string) (http.Header, error) {
	srv, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return s.authorize(ctx, srv, true)
}

func (s *Service) authorize(ctx context.Context, srv Server, force bool) (http.Header, error) {
	switch srv.Auth {
	case AuthNone:
		return http.Header{}, nil
	case AuthHeader:
		cr, err := s.readCred(srv.ID)
		if err != nil {
			return nil, err
		}
		if cr.Header == "" || srv.HeaderName == "" {
			return nil, fmt.Errorf("%w: no credential", ErrSignInRequired)
		}
		return http.Header{textproto.CanonicalMIMEHeaderKey(srv.HeaderName): {cr.Header}}, nil
	case AuthOAuth:
		token, err := s.accessToken(ctx, srv.ID, force)
		if err != nil {
			return nil, err
		}
		return http.Header{"Authorization": {"Bearer " + token}}, nil
	}
	return nil, fmt.Errorf("%w: unknown auth", ErrBadRequest)
}

// Rename names a server anew; its slug follows the name.
func (s *Service) Rename(id, name string) (Server, error) {
	name = oneLine(name)
	if name == "" {
		return Server{}, fmt.Errorf("%w: empty name", ErrBadRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Server{}, err
	}
	i := slices.IndexFunc(f.Servers, func(x Server) bool { return x.ID == id })
	if i < 0 {
		return Server{}, fmt.Errorf("%w: server %s", ErrNotFound, id)
	}
	f.Servers[i].Name = name
	f.Servers[i].Slug = uniqueSlug(slugOf(name), f.Servers, id)
	return f.Servers[i], s.writeAll(f)
}

// SetEnabled turns a server on or off for agents.
func (s *Service) SetEnabled(id string, enabled bool) (Server, error) {
	return s.update(id, func(x *Server) error {
		x.Enabled = enabled
		return nil
	})
}

// Remove forgets a server and its credentials.
func (s *Service) Remove(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	lock := s.refreshLock(id)
	lock.Lock()
	err := s.secrets.Remove(secretName(id))
	lock.Unlock()
	if err != nil && !errors.Is(err, connections.ErrNotFound) && !errors.Is(err, ErrNotFound) {
		return err
	}
	s.dropFlows(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return err
	}
	f.Servers = slices.DeleteFunc(f.Servers, func(x Server) bool { return x.ID == id })
	return s.writeAll(f)
}

// update changes one server in the file.
func (s *Service) update(id string, change func(*Server) error) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Server{}, err
	}
	i := slices.IndexFunc(f.Servers, func(x Server) bool { return x.ID == id })
	if i < 0 {
		return Server{}, fmt.Errorf("%w: server %s", ErrNotFound, id)
	}
	if err := change(&f.Servers[i]); err != nil {
		return Server{}, err
	}
	return f.Servers[i], s.writeAll(f)
}

func (s *Service) setState(id string, st State) {
	_, _ = s.update(id, func(x *Server) error {
		x.State = st
		return nil
	})
}

// readCred reads a server's credentials; none kept is ErrSignInRequired.
func (s *Service) readCred(id string) (cred, error) {
	raw, err := s.secrets.Reveal(secretName(id))
	if err != nil || raw == "" {
		return cred{}, fmt.Errorf("%w: no credential", ErrSignInRequired)
	}
	var c cred
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return cred{}, fmt.Errorf("%w: unreadable credential", ErrSignInRequired)
	}
	return c, nil
}

func (s *Service) writeCred(id string, c cred) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.secrets.Put(secretName(id), string(raw))
}

// forbiddenHeaders are headers a typed credential must not set: the transport and the proxy own them.
var forbiddenHeaders = []string{
	"Host", "Content-Length", "Content-Type", "Accept", "Connection", "Transfer-Encoding", "Upgrade", "Te", "Trailer",
	"Keep-Alive", "Proxy-Authorization", "Proxy-Connection", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id",
	"X-Cli-Token", "Cookie",
}

// headerOf checks a typed header: the name defaults to Authorization, where a bare token gets "Bearer ".
func headerOf(name, value string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Authorization"
	}
	for _, c := range name {
		if !(c == '-' || c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", "", fmt.Errorf("%w: bad header name", ErrBadRequest)
		}
	}
	name = textproto.CanonicalMIMEHeaderKey(name)
	if slices.Contains(forbiddenHeaders, name) {
		return "", "", fmt.Errorf("%w: header %s cannot carry a credential", ErrBadRequest, name)
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", "", fmt.Errorf("%w: bad header value", ErrBadRequest)
	}
	if name == "Authorization" && !strings.Contains(value, " ") {
		value = "Bearer " + value
	}
	return name, value, nil
}

// slugOf makes a slug of a name: lowercase [a-z0-9-], at most 32 characters.
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	if slug == "" {
		slug = "server"
	}
	return slug
}

// uniqueSlug is base, or base-2, base-3, … when another server than self has it.
func uniqueSlug(base string, list []Server, self string) string {
	taken := func(slug string) bool {
		return slices.ContainsFunc(list, func(x Server) bool { return x.ID != self && x.Slug == slug })
	}
	slug := base
	for n := 2; taken(slug); n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}
