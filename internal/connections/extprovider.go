package connections

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// ConnectionProtocol is what an adapter of a product speaks. An adapter lets a product that is not built in (a
// corporate GitLab with its own sign-in, an in-house tracker) connect without a rebuild of the runtime.
//
// The adapter is an HTTP service of the extension frame (internal/hub/ext): GET /v1/describe, then POST calls with a
// JSON body. The core keeps the credentials in its safe and sends them in the body of each call; the adapter keeps none
// of them, and the file that declares the adapter has no room for one.
//
//	POST /v1/detect       {url}                          -> Detection, or 404 when the link is not this product
//	POST /v1/link         {conn, url}                    -> {found, kind, key}
//	POST /v1/me           {conn, credentials}            -> Person
//	POST /v1/projects     {conn, credentials}            -> {projects: [Project]}
//	POST /v1/issues/search {conn, credentials, query}    -> {issues: [Issue]}
//	POST /v1/issues/get   {conn, credentials, key}       -> Issue
//	POST /v1/pages/search {conn, credentials, query}     -> {pages: [Page]}
//	POST /v1/pages/get    {conn, credentials, id}        -> Page
//
// The capabilities the adapter lists in describe are `detect`, `link`, `issues`, `pages`; a call of a capability that is
// not listed is not sent. The code capability (merge requests) has no source interface in the core yet: an adapter may
// list `code` in the detection's capabilities, and the connection keeps it, but the core asks nothing of it.
const ConnectionProtocol = "harnessly.connection/v1"

// linkWait bounds the call of Link: the contract says no network, so an adapter that is slow is treated as "not a
// link" and the answer of a fast one is kept.
const linkWait = 2 * time.Second

var kindRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// ErrKindTaken is the refusal to register an adapter whose kind a built-in product (or another adapter) already has.
var ErrKindTaken = errors.New("connections: the kind is taken")

// ExtProvider is a Provider backed by an adapter.
type ExtProvider struct {
	kind Kind
	cl   *ext.Client
	desc ext.Description

	mu    sync.Mutex
	links map[string]linkAnswer
}

type linkAnswer struct {
	t  LinkTarget
	ok bool
	at time.Time
}

// RegisterAdapter asks the adapter to describe itself and, when it speaks the connection protocol, registers it as the
// provider of the kind it names. Built-in providers come first in the registry, so an adapter never takes a link from
// one; a kind a built-in product already has is refused.
func RegisterAdapter(ctx context.Context, c *ext.Client) (*ExtProvider, error) {
	d, err := c.Describe(ctx, ConnectionProtocol)
	if err != nil {
		return nil, err
	}
	p, err := NewExtProvider(c, d)
	if err != nil {
		return nil, err
	}
	if ProviderOf(p.kind) != nil {
		return nil, fmt.Errorf("%w: %s", ErrKindTaken, p.kind)
	}
	Register(p)
	return p, nil
}

// NewExtProvider is the provider of an adapter that has described itself.
func NewExtProvider(c *ext.Client, d ext.Description) (*ExtProvider, error) {
	if !kindRE.MatchString(d.Name) {
		return nil, fmt.Errorf("connections: the adapter's name %q is not a kind (lowercase letters, digits, dashes)", d.Name)
	}
	if !d.Has("detect") {
		return nil, errors.New("connections: the adapter does not list the detect capability")
	}
	return &ExtProvider{kind: Kind(d.Name), cl: c, desc: d, links: map[string]linkAnswer{}}, nil
}

func (p *ExtProvider) Kind() Kind { return p.kind }

// connWire is the connection as an adapter sees it: what the page lists, never a credential.
type connWire struct {
	ID         string            `json:"id"`
	BaseURL    string            `json:"baseUrl"`
	APIURL     string            `json:"apiUrl,omitempty"`
	Deployment Deployment        `json:"deployment"`
	Method     string            `json:"method"`
	Extra      map[string]string `json:"extra,omitempty"`
}

func wireOf(c Conn) connWire {
	return connWire{ID: c.ID, BaseURL: c.BaseURL, APIURL: c.APIURL, Deployment: c.Deployment, Method: c.Method, Extra: c.Extra}
}

// reasonOf maps an adapter's refusal to the core's errors.
func reasonOf(err error) error {
	var e *ext.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ext.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, ext.ErrNotFound):
		return ErrNotFound
	case errors.As(err, &e) && (e.Status == 401 || e.Status == 403 || e.Code == "denied"):
		return ErrDenied
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

func (p *ExtProvider) Detect(ctx context.Context, _ *Prober, u *url.URL) (*Detection, error) {
	var d Detection
	err := p.cl.Do(ctx, "POST", "/v1/detect", map[string]string{"url": u.String()}, false, &d)
	switch {
	case errors.Is(err, ext.ErrNotFound), errors.Is(err, ext.ErrUnsupported):
		return nil, nil
	case err != nil:
		return nil, reasonOf(err)
	}
	if d.BaseURL == "" {
		return nil, nil
	}
	d.Kind = p.kind
	if d.Methods == nil {
		d.Methods = []Method{}
	}
	if d.Capabilities == nil {
		d.Capabilities = []Capability{}
	}
	return &d, nil
}

func (p *ExtProvider) Open(conn Conn, cred Credentials, _ *HTTP) Client {
	return &extClient{p: p, conn: wireOf(conn), cred: cred}
}

// Link asks the adapter, with a short wait, and keeps the answer for a minute.
func (p *ExtProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	if !p.desc.Has("link") {
		return LinkTarget{}, false
	}
	key := conn.ID + "\n" + u.String()
	p.mu.Lock()
	if a, ok := p.links[key]; ok && time.Since(a.at) < time.Minute {
		p.mu.Unlock()
		return a.t, a.ok
	}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), linkWait)
	defer cancel()
	var out struct {
		Found bool   `json:"found"`
		Kind  string `json:"kind"`
		Key   string `json:"key"`
	}
	if err := p.cl.Do(ctx, "POST", "/v1/link", map[string]any{"conn": wireOf(conn), "url": u.String()}, false, &out); err != nil {
		return LinkTarget{}, false
	}
	t := LinkTarget{Kind: out.Kind, Key: out.Key}
	ok := out.Found && out.Key != "" && (out.Kind == "issue" || out.Kind == "page")
	p.mu.Lock()
	if len(p.links) > 512 {
		p.links = map[string]linkAnswer{}
	}
	p.links[key] = linkAnswer{t: t, ok: ok, at: time.Now()}
	p.mu.Unlock()
	return t, ok
}

// extClient is an opened connection of an adapter. It implements IssueSource and PageSource; a call the adapter does
// not list in describe answers ErrUnsupported without leaving the process.
type extClient struct {
	p    *ExtProvider
	conn connWire
	cred Credentials
}

func (c *extClient) call(ctx context.Context, capability, path string, extra map[string]any, out any) error {
	if capability != "" && !c.p.desc.Has(capability) {
		return ErrUnsupported
	}
	body := map[string]any{"conn": c.conn, "credentials": c.cred}
	for k, v := range extra {
		body[k] = v
	}
	return reasonOf(c.p.cl.Do(ctx, "POST", path, body, false, out))
}

func (c *extClient) Me(ctx context.Context) (*Person, error) {
	var me Person
	if err := c.call(ctx, "", "/v1/me", nil, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

func (c *extClient) Projects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.call(ctx, "issues", "/v1/projects", nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

func (c *extClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	q.Limit = q.LimitOr(50)
	var out struct {
		Issues []Issue `json:"issues"`
	}
	if err := c.call(ctx, "issues", "/v1/issues/search", map[string]any{"query": q}, &out); err != nil {
		return nil, err
	}
	return out.Issues, nil
}

func (c *extClient) Issue(ctx context.Context, key string) (*Issue, error) {
	var is Issue
	if err := c.call(ctx, "issues", "/v1/issues/get", map[string]any{"key": key}, &is); err != nil {
		return nil, err
	}
	return &is, nil
}

func (c *extClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	q.Limit = q.LimitOr(50)
	var out struct {
		Pages []Page `json:"pages"`
	}
	if err := c.call(ctx, "pages", "/v1/pages/search", map[string]any{"query": q}, &out); err != nil {
		return nil, err
	}
	return out.Pages, nil
}

func (c *extClient) Page(ctx context.Context, id string) (*Page, error) {
	var pg Page
	if err := c.call(ctx, "pages", "/v1/pages/get", map[string]any{"id": id}, &pg); err != nil {
		return nil, err
	}
	return &pg, nil
}

// LoadAdapters registers every adapter the extensions file declares for the connection protocol (a JSON array of
// ext.Spec; entries of other protocols are left to their own loaders). It returns the kinds it registered and one
// error for each adapter that did not: a silent or wrong adapter adds nothing and never stops the built-in products.
// With no path nothing happens.
func LoadAdapters(ctx context.Context, path string) ([]Kind, []error) {
	if path == "" {
		return nil, nil
	}
	specs, err := ext.LoadSpecs(path)
	if err != nil {
		return nil, []error{err}
	}
	var kinds []Kind
	var errs []error
	for _, s := range specs {
		if s.Protocol != ConnectionProtocol {
			continue
		}
		c, err := ext.NewClient(ext.Config{BaseURL: s.URL, TokenFile: s.TokenFile, Timeout: time.Duration(s.TimeoutSeconds) * time.Second})
		if err == nil {
			var p *ExtProvider
			if p, err = RegisterAdapter(ctx, c); err == nil {
				kinds = append(kinds, p.Kind())
				continue
			}
		}
		errs = append(errs, fmt.Errorf("connection adapter %s: %w", s.Name, err))
	}
	return kinds, errs
}
