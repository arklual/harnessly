package mcpconn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/connections"
)

// canonical checks a pasted server URL and writes it one way: http(s), lowercase scheme and host, no credentials, no
// fragment. The path and query stay as they are.
func canonical(raw string) (string, *url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", nil, fmt.Errorf("%w: not an http(s) URL", ErrBadRequest)
	}
	if u.User != nil {
		return "", nil, fmt.Errorf("%w: the URL must not carry credentials", ErrBadRequest)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), u, nil
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }

// probe finds a server's transport and how it signs in. hint SSE (or a path ending in /sse) tries the legacy
// transport first.
func (s *Service) probe(ctx context.Context, raw string, hint Transport) (*ProbeResult, error) {
	canon, u, err := canonical(raw)
	if err != nil {
		return nil, err
	}
	order := []Transport{HTTP, SSE}
	if hint == SSE || strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/sse") {
		order = []Transport{SSE, HTTP}
	}
	var last error
	for _, t := range order {
		info, err := s.initialize(ctx, t, canon, nil)
		if err == nil {
			return &ProbeResult{URL: canon, Transport: t, Auth: AuthNone, Name: nameOf(info, u)}, nil
		}
		var se *statusError
		if errors.As(err, &se) && errors.Is(err, ErrDenied) {
			return s.discoverAuth(ctx, canon, u, t, se.Header)
		}
		if errors.Is(err, ErrBadRequest) {
			return nil, err
		}
		// A server that did not answer at all will not answer the other transport either.
		if se == nil && !errors.Is(err, errNotMCP) {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// initialize opens a session, runs the handshake and closes it.
func (s *Service) initialize(ctx context.Context, t Transport, rawURL string, header http.Header) (serverInfo, error) {
	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	sess, err := open(ctx, s.http.Client, t, rawURL, header)
	if err != nil {
		return serverInfo{}, err
	}
	defer sess.close()
	return handshake(ctx, sess)
}

// nameOf is the server's own title or name, else a name made from its host.
func nameOf(info serverInfo, u *url.URL) string {
	for _, n := range []string{info.Title, info.Name} {
		if n = strings.TrimSpace(n); n != "" {
			return oneLine(n)
		}
	}
	return nameFromHost(u.Hostname())
}

// nameFromHost: mcp.linear.app → Linear.
func nameFromHost(host string) string {
	labels := strings.Split(host, ".")
	for len(labels) > 2 && slices.Contains([]string{"mcp", "www", "api", "app"}, labels[0]) {
		labels = labels[1:]
	}
	n := labels[0]
	if n == "" {
		return host
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

var (
	resourceMetadataRe = regexp.MustCompile(`(?i)\bresource_metadata\s*=\s*(?:"([^"]*)"|([^\s,]+))`)
	scopeRe            = regexp.MustCompile(`(?i)\bscope\s*=\s*(?:"([^"]*)"|([^\s,]+))`)
)

func challengeParam(re *regexp.Regexp, values []string) string {
	for _, v := range values {
		if m := re.FindStringSubmatch(v); m != nil {
			return m[1] + m[2]
		}
	}
	return ""
}

type protectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authServerMeta struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// discoverAuth reads a server's OAuth metadata after it refused an unsigned initialize: the protected resource
// metadata (RFC 9728) named by WWW-Authenticate or at its well-known addresses, then the authorization server's
// metadata (RFC 8414 / OpenID). Without protected resource metadata the MCP 2025-03-26 rule applies: the server's
// origin is the authorization server. No metadata at all means a typed header.
func (s *Service) discoverAuth(ctx context.Context, canon string, u *url.URL, t Transport, challenge http.Header) (*ProbeResult, error) {
	res := &ProbeResult{URL: canon, Transport: t, Auth: AuthHeader, HeaderName: "Authorization", Name: nameFromHost(u.Hostname())}
	values := challenge.Values("WWW-Authenticate")

	var candidates []string
	if m := challengeParam(resourceMetadataRe, values); m != "" {
		if mu, err := url.Parse(m); err == nil && (mu.Scheme == "https" || mu.Scheme == "http") && mu.Host != "" {
			candidates = append(candidates, mu.String())
		}
	}
	if p := strings.TrimRight(u.EscapedPath(), "/"); p != "" {
		candidates = append(candidates, origin(u)+"/.well-known/oauth-protected-resource"+p)
	}
	candidates = append(candidates, origin(u)+"/.well-known/oauth-protected-resource")

	var prm *protectedResource
	for _, c := range candidates {
		var doc protectedResource
		if s.getJSON(ctx, c, &doc) && len(doc.AuthorizationServers) > 0 {
			prm = &doc
			break
		}
	}

	d := &discovery{Resource: canon}
	var meta *authServerMeta
	if prm != nil {
		d.AuthServer = strings.TrimSpace(prm.AuthorizationServers[0])
		if r := strings.TrimSpace(prm.Resource); r != "" && covers(r, canon) {
			d.Resource = r
		}
		d.Scope = strings.Join(prm.ScopesSupported, " ")
		meta = s.authServerMeta(ctx, d.AuthServer)
		if meta == nil {
			res.OAuthUnavailable = true
			return res, nil
		}
	} else {
		d.AuthServer = origin(u)
		var doc authServerMeta
		if !s.getJSON(ctx, origin(u)+"/.well-known/oauth-authorization-server", &doc) {
			return res, nil
		}
		meta = &doc
		if meta.AuthorizationEndpoint == "" {
			meta.AuthorizationEndpoint = origin(u) + "/authorize"
		}
		if meta.TokenEndpoint == "" {
			meta.TokenEndpoint = origin(u) + "/token"
		}
	}
	if d.Scope == "" {
		d.Scope = challengeParam(scopeRe, values)
	}
	d.Authorize = meta.AuthorizationEndpoint
	d.Token = meta.TokenEndpoint
	d.Register = meta.RegistrationEndpoint
	d.AuthMethods = meta.TokenEndpointAuthMethodsSupported
	pkce := len(meta.CodeChallengeMethodsSupported) == 0 || slices.Contains(meta.CodeChallengeMethodsSupported, "S256")
	if !httpURL(d.Authorize) || !httpURL(d.Token) || !httpURL(d.Register) || !pkce {
		res.OAuthUnavailable = true
		return res, nil
	}
	res.Auth = AuthOAuth
	res.HeaderName = ""
	res.oauth = d
	return res, nil
}

// authServerMeta fetches an authorization server's metadata: RFC 8414 with the issuer's path inserted after the
// well-known prefix, then OpenID Connect discovery both ways.
func (s *Service) authServerMeta(ctx context.Context, issuer string) *authServerMeta {
	iu, err := url.Parse(issuer)
	if err != nil || (iu.Scheme != "https" && iu.Scheme != "http") || iu.Host == "" {
		return nil
	}
	o := origin(iu)
	p := strings.TrimRight(iu.EscapedPath(), "/")
	var urls []string
	if p != "" {
		urls = []string{
			o + "/.well-known/oauth-authorization-server" + p,
			o + "/.well-known/openid-configuration" + p,
			o + p + "/.well-known/openid-configuration",
		}
	} else {
		urls = []string{o + "/.well-known/oauth-authorization-server", o + "/.well-known/openid-configuration"}
	}
	for _, mu := range urls {
		var doc authServerMeta
		if s.getJSON(ctx, mu, &doc) && doc.AuthorizationEndpoint != "" && doc.TokenEndpoint != "" {
			// RFC 8414 §3.3: metadata naming another issuer is not this server's (mix-up); a missing issuer is tolerated for older servers.
			if doc.Issuer != "" && strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(issuer, "/") {
				continue
			}
			return &doc
		}
	}
	return nil
}

// getJSON fetches a metadata document; false when it is missing or not JSON.
func (s *Service) getJSON(ctx context.Context, rawURL string, out any) bool {
	ctx, cancel := timeoutCtx(ctx)
	defer cancel()
	_, err := s.http.JSON(ctx, connections.Request{URL: rawURL, Header: http.Header{"User-Agent": {userAgent}}}, out)
	return err == nil
}

// covers says whether resource r names the server at canon: the same origin, and a path that is canon's or a parent.
func covers(r, canon string) bool {
	ru, err1 := url.Parse(r)
	cu, err2 := url.Parse(canon)
	if err1 != nil || err2 != nil || !strings.EqualFold(ru.Scheme, cu.Scheme) || !strings.EqualFold(ru.Host, cu.Host) {
		return false
	}
	rp := strings.TrimRight(ru.Path, "/")
	cp := strings.TrimRight(cu.Path, "/")
	return rp == "" || rp == cp || strings.HasPrefix(cp, rp+"/")
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}
