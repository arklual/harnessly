package mcpconn

// Keyed servers: a local server (transport stdio) and an API (transport api) both sign in with a set of
// keys the person types into the fields of their catalog entry. The keys are kept in the safe as one credential
// (cred.Env); the server keeps only the fields' names.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// envOf checks the keys typed for the fields: every required field has a value (or its default), names not among the
// fields are dropped, values are one line.
func envOf(fields []EnvField, in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range fields {
		v := strings.TrimSpace(in[f.Name])
		if v == "" {
			v = f.Default
		}
		if !f.File && strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("%w: %s is not one line", ErrBadRequest, f.Name)
		}
		if v == "" {
			if f.Optional {
				continue
			}
			return nil, fmt.Errorf("%w: %s is required", ErrBadRequest, f.Name)
		}
		out[f.Name] = v
	}
	return out, nil
}

// problemOf is the few words the page shows for a keyed server that did not answer.
func problemOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoRuntime):
		return strings.TrimPrefix(err.Error(), ErrNoRuntime.Error()+": ") + " is not installed on this machine"
	case errors.Is(err, ErrDenied):
		return "the keys were refused"
	case errors.Is(err, errBroken):
		return "the server's package does not run"
	case strings.Contains(err.Error(), "answered HTTP "):
		return err.Error()[strings.Index(err.Error(), "answered HTTP "):]
	default:
		return "did not answer"
	}
}

func (s *Service) keep(srv Server) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Server{}, err
	}
	srv.Slug = uniqueSlug(slugBase(srv), f.Servers, srv.ID)
	f.Servers = append(f.Servers, srv)
	return srv, s.writeAll(f)
}

func (s *Service) newKeyed(entry CatalogEntry, in AddInput) Server {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = entry.Name
	}
	srv := Server{
		ID: newID(), Name: oneLine(name), Transport: entry.Transport, Auth: AuthEnv, CatalogID: entry.ID, Owner: in.Owner,
		Enabled: in.Owner == "", State: StateSignIn, CreatedAt: s.now().UTC(),
		Command: slices.Clone(entry.Command), Env: slices.Clone(entry.Env), FixedEnv: entry.FixedEnv,
		APIHeaders: entry.APIHeaders, APIQuery: entry.APIQuery, CheckPath: entry.CheckPath, Guide: entry.Guide,
	}
	if entry.Transport == Stdio {
		srv.URL = "stdio:" + entry.ID
	} else {
		srv.URL = entry.URL
	}
	if !slices.ContainsFunc(entry.Env, func(f EnvField) bool { return !f.Optional }) {
		srv.Auth = AuthNone
	}
	return srv
}

// addLocal connects a local server of the catalog: the keys are checked by starting it and listing its tools.
func (s *Service) addLocal(ctx context.Context, entry CatalogEntry, in AddInput) (Server, *AuthStart, error) {
	if existing, ok := s.byCatalogAny(entry.ID, in.Owner); ok && len(in.Env) == 0 {
		return existing, nil, nil
	}
	env, err := envOf(entry.Env, in.Env)
	if err != nil {
		return Server{}, nil, err
	}
	srv := s.newKeyed(entry, in)
	tools, cerr := s.inspectLocal(ctx, srv, env)
	s.applyCheck(&srv, tools, cerr)
	srv.Problem = problemOf(cerr)
	if len(env) > 0 {
		if err := s.writeCred(srv.ID, cred{Env: env}); err != nil {
			return Server{}, nil, err
		}
	}
	srv, err = s.keep(srv)
	return srv, nil, err
}

// addAPI connects an API of the catalog: the keys are checked by its CheckPath.
func (s *Service) addAPI(ctx context.Context, entry CatalogEntry, in AddInput) (Server, *AuthStart, error) {
	if existing, ok := s.byCatalogAny(entry.ID, in.Owner); ok && len(in.Env) == 0 {
		return existing, nil, nil
	}
	env, err := envOf(entry.Env, in.Env)
	if err != nil {
		return Server{}, nil, err
	}
	srv := s.newKeyed(entry, in)
	cerr := s.checkAPI(ctx, srv, env)
	if errors.Is(cerr, ErrDenied) {
		return Server{}, nil, cerr
	}
	s.applyCheck(&srv, apiTools(srv), cerr)
	srv.Problem = problemOf(cerr)
	if len(env) > 0 {
		if err := s.writeCred(srv.ID, cred{Env: env}); err != nil {
			return Server{}, nil, err
		}
	}
	srv, err = s.keep(srv)
	return srv, nil, err
}

func (s *Service) byCatalogAny(id, owner string) (Server, bool) {
	list, err := s.List()
	if err != nil {
		return Server{}, false
	}
	for _, srv := range list {
		if srv.CatalogID == id && srv.Owner == owner {
			return srv, true
		}
	}
	return Server{}, false
}

// apiTools is what an API offers agents: one request tool, described by its guide.
func apiTools(srv Server) []Tool {
	d := "An HTTP request to " + srv.URL + " with the keys put in by the runtime."
	if srv.Guide != "" {
		d += " " + oneLine(srv.Guide)
	}
	return []Tool{{Name: "api_request", Description: d}}
}

// SetEnv sets the keys of a keyed server; a local one is started with them and an API asked first, and keys an API
// refuses change nothing.
func (s *Service) SetEnv(ctx context.Context, id string, in map[string]string) (Server, error) {
	srv, err := s.Get(id)
	if err != nil {
		return Server{}, err
	}
	if srv.Transport != Stdio && srv.Transport != API {
		return Server{}, fmt.Errorf("%w: not a keyed server", ErrBadRequest)
	}
	old, _ := s.localEnvOf(srv)
	merged := map[string]string{}
	for _, f := range srv.Env {
		if v := strings.TrimSpace(in[f.Name]); v != "" {
			merged[f.Name] = v
		} else if v := old[f.Name]; v != "" {
			merged[f.Name] = v // a field left empty keeps its key
		}
	}
	env, err := envOf(srv.Env, merged)
	if err != nil {
		return Server{}, err
	}
	var tools []Tool
	var cerr error
	if srv.Transport == API {
		if cerr = s.checkAPI(ctx, srv, env); errors.Is(cerr, ErrDenied) {
			return Server{}, cerr
		}
		tools = apiTools(srv)
	} else {
		runner.drop(id)
		tools, cerr = s.inspectLocal(ctx, srv, env)
	}
	if err := s.writeCred(id, cred{Env: env}); err != nil {
		return Server{}, err
	}
	return s.update(id, func(x *Server) error {
		s.applyCheck(x, tools, cerr)
		x.Problem = problemOf(cerr)
		return nil
	})
}

// checkKeyed is Check of a local server or an API.
func (s *Service) checkKeyed(ctx context.Context, srv Server) (Server, error) {
	env, err := s.localEnvOf(srv)
	if err != nil {
		return s.update(srv.ID, func(x *Server) error {
			x.State, x.CheckedAt = StateSignIn, s.now().UTC()
			return nil
		})
	}
	var tools []Tool
	var cerr error
	if srv.Transport == API {
		cerr = s.checkAPI(ctx, srv, env)
		tools = apiTools(srv)
	} else {
		var p *localProc
		if p, cerr = runner.get(ctx, srv.ID, s.localSpecOf(srv, env)); cerr == nil {
			tools, cerr = p.tools(ctx)
		}
	}
	return s.update(srv.ID, func(x *Server) error {
		s.applyCheck(x, tools, cerr)
		x.Problem = problemOf(cerr)
		return nil
	})
}

var (
	placeholder = regexp.MustCompile(`\{([A-Z0-9_]+)\}`)
	// basicAuth is {BASIC:LOGIN:PASSWORD}: base64 of "login:password", for an API with HTTP basic auth.
	basicAuth = regexp.MustCompile(`\{BASIC:([A-Z0-9_]+):([A-Z0-9_]+)\}`)
)

// filled is fill, ok false when a key the template names is empty: the header or parameter is then left out.
func filled(tpl string, env map[string]string) (string, bool) {
	for _, m := range placeholder.FindAllStringSubmatch(tpl, -1) {
		if env[m[1]] == "" {
			return "", false
		}
	}
	for _, m := range basicAuth.FindAllStringSubmatch(tpl, -1) {
		if env[m[1]] == "" {
			return "", false
		}
	}
	return fill(tpl, env), true
}

func fill(tpl string, env map[string]string) string {
	tpl = basicAuth.ReplaceAllStringFunc(tpl, func(m string) string {
		p := basicAuth.FindStringSubmatch(m)
		return base64.StdEncoding.EncodeToString([]byte(env[p[1]] + ":" + env[p[2]]))
	})
	return placeholder.ReplaceAllStringFunc(tpl, func(m string) string { return env[m[1:len(m)-1]] })
}

// APIRequest is one request of an agent to an API: a path under its base, a query and a JSON body.
type APIRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
}

// APIAnswer is the API's answer as the agent gets it.
type APIAnswer struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Body        []byte `json:"-"`
}

// apiAnswerMax caps an API's answer.
const apiAnswerMax = 8 << 20

// apiURL is the request's address: the path must stay under the base (no other host, no climbing out of it).
func apiURL(base, path string, query map[string]string) (*url.URL, error) {
	b, err := url.Parse(base)
	if err != nil || b.Scheme != "https" && b.Scheme != "http" || b.Host == "" {
		return nil, fmt.Errorf("%w: bad base", ErrBadRequest)
	}
	path = strings.TrimSpace(path)
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") {
		return nil, fmt.Errorf("%w: the path must be under %s", ErrBadRequest, base)
	}
	rel, err := url.Parse(strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("%w: bad path", ErrBadRequest)
	}
	if !strings.HasSuffix(b.Path, "/") {
		b.Path += "/"
	}
	u := b.ResolveReference(rel)
	if u.Host != b.Host || !strings.HasPrefix(u.Path, b.Path) {
		return nil, fmt.Errorf("%w: the path must be under %s", ErrBadRequest, base)
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u, nil
}

func (s *Service) doAPI(ctx context.Context, srv Server, env map[string]string, in APIRequest) (APIAnswer, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}, method) {
		return APIAnswer{}, fmt.Errorf("%w: method %s", ErrBadRequest, method)
	}
	q := map[string]string{}
	for k, v := range in.Query {
		q[k] = v
	}
	for k, v := range srv.APIQuery {
		if fv, ok := filled(v, env); ok {
			q[k] = fv
		}
	}
	u, err := apiURL(strings.TrimRight(fill(srv.URL, env), "/"), in.Path, q)
	if err != nil {
		return APIAnswer{}, err
	}
	var body io.Reader
	if len(in.Body) > 0 && string(in.Body) != "null" {
		body = bytes.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	req.Header.Set("Accept", "application/json, */*;q=0.5")
	req.Header.Set("User-Agent", "harnessly/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range srv.APIHeaders {
		if fv, ok := filled(v, env); ok {
			req.Header.Set(k, fv)
		}
	}
	res, err := s.http.Client.Do(req)
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%w: %v", ErrUnreachable, redactErr(err))
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, apiAnswerMax))
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return APIAnswer{Status: res.StatusCode, ContentType: res.Header.Get("Content-Type"), Body: raw}, nil
}

func (s *Service) checkAPI(ctx context.Context, srv Server, env map[string]string) error {
	if srv.CheckPath == "" {
		return nil
	}
	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	a, err := s.doAPI(ctx, srv, env, APIRequest{Method: http.MethodGet, Path: srv.CheckPath})
	switch {
	case err != nil:
		return err
	case a.Status == http.StatusUnauthorized || a.Status == http.StatusForbidden:
		return fmt.Errorf("%w: %d", ErrDenied, a.Status)
	case a.Status >= 400:
		return fmt.Errorf("%w: answered HTTP %d", ErrUnreachable, a.Status)
	}
	return nil
}

// CallAPI makes an agent's request to the API id with the person's keys.
func (s *Service) CallAPI(ctx context.Context, id string, in APIRequest) (APIAnswer, error) {
	srv, err := s.Get(id)
	if err != nil {
		return APIAnswer{}, err
	}
	if srv.Transport != API {
		return APIAnswer{}, fmt.Errorf("%w: not an API", ErrBadRequest)
	}
	env, err := s.localEnvOf(srv)
	if err != nil {
		return APIAnswer{}, err
	}
	a, err := s.doAPI(ctx, srv, env, in)
	if err != nil {
		return a, err
	}
	// A key never comes back in an answer, even when an API echoes the request.
	for _, v := range env {
		if len(v) >= 8 {
			a.Body = bytes.ReplaceAll(a.Body, []byte(v), []byte("[key]"))
		}
	}
	return a, nil
}

// slugBase is what a new server's slug is made of: its catalog id (a Russian name would lose its letters: «Московская
// биржа (ISS)» is not «iss»), else its name.
func slugBase(srv Server) string {
	if srv.CatalogID != "" {
		return slugOf(srv.CatalogID)
	}
	return slugOf(srv.Name)
}
