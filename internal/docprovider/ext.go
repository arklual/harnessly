package docprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// Protocol is what a documents adapter speaks. The adapter is an HTTP service of the extension frame
// (internal/hub/ext): GET /v1/describe, then POST calls with a JSON body, as the connection and memory adapters.
//
//	capability read      POST /v1/list {limit} -> {docs: [Summary]}        POST /v1/get {slug} -> Summary
//	                     POST /v1/read {slug, version} -> Content         POST /v1/versions {slug} -> {versions}
//	                     POST /v1/diff {slug, from, to} -> Diff            POST /v1/embed {slug} -> Embed
//	capability publish   POST /v1/publish PublishRequest -> Published
//	capability access    POST /v1/access {slug} -> Access                 POST /v1/access/set {slug, public?, grants?} -> Access
//	capability comments  POST /v1/threads {slug} -> {threads}             POST /v1/threads/create {slug, anchor, body, author} -> Thread
//	                     POST /v1/threads/reply {slug, threadId, body, author} -> Thread
//	                     POST /v1/threads/status {slug, threadId, status} -> Thread
//	                     POST /v1/threads/read {person, slug} -> {}
//	capability boards    POST /v1/boards {} -> {boards: [Board]}
//
// `read` is required; a call of a capability that is not listed is not sent.
const Protocol = "harnessly.docs/v1"

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// ExtProvider is a Provider backed by an adapter.
type ExtProvider struct {
	cl   *ext.Client
	desc ext.Description
}

// NewExtProvider is the provider of an adapter that has described itself.
func NewExtProvider(c *ext.Client, d ext.Description) (*ExtProvider, error) {
	if !nameRE.MatchString(d.Name) {
		return nil, fmt.Errorf("docprovider: the adapter's name %q is not a name (lowercase letters, digits, dashes)", d.Name)
	}
	if !d.Has("read") {
		return nil, errors.New("docprovider: the adapter does not list the read capability")
	}
	return &ExtProvider{cl: c, desc: d}, nil
}

// Register asks the adapter to describe itself and, when it speaks the protocol, makes a provider of it.
func Register(ctx context.Context, c *ext.Client) (*ExtProvider, error) {
	d, err := c.Describe(ctx, Protocol)
	if err != nil {
		return nil, err
	}
	return NewExtProvider(c, d)
}

// Name is the adapter's name.
func (p *ExtProvider) Name() string { return p.desc.Name }

// reasonOf maps an adapter's refusal to the errors the reader already answers.
func reasonOf(err error) error {
	var e *ext.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ext.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, ext.ErrNotFound):
		return &integrations.UpstreamError{Status: http.StatusNotFound, Text: "not found"}
	case errors.As(err, &e) && (e.Status == 401 || e.Status == 403 || e.Code == "denied"):
		return &integrations.UpstreamError{Status: http.StatusForbidden, Text: "denied"}
	case errors.As(err, &e) && e.Message != "":
		return &integrations.UpstreamError{Status: http.StatusBadGateway, Text: e.Message}
	}
	return fmt.Errorf("docprovider: the adapter is unreachable: %w", err)
}

func (p *ExtProvider) call(ctx context.Context, capability, path string, in map[string]any, mutating bool, out any) error {
	if !p.desc.Has(capability) {
		return ErrUnsupported
	}
	if in == nil {
		in = map[string]any{}
	}
	return reasonOf(p.cl.Do(ctx, "POST", path, in, mutating, out))
}

func (p *ExtProvider) List(ctx context.Context, limit int) ([]Summary, error) {
	var out struct {
		Docs []Summary `json:"docs"`
	}
	if err := p.call(ctx, "read", "/v1/list", map[string]any{"limit": limit}, false, &out); err != nil {
		return nil, err
	}
	if out.Docs == nil {
		out.Docs = []Summary{}
	}
	return out.Docs, nil
}

func (p *ExtProvider) Get(ctx context.Context, slug string) (Summary, error) {
	var s Summary
	err := p.call(ctx, "read", "/v1/get", map[string]any{"slug": slug}, false, &s)
	return s, err
}

func (p *ExtProvider) Read(ctx context.Context, slug, version string) (Content, error) {
	var c Content
	err := p.call(ctx, "read", "/v1/read", map[string]any{"slug": slug, "version": version}, false, &c)
	return c, err
}

func (p *ExtProvider) Versions(ctx context.Context, slug string) ([]Version, error) {
	var out struct {
		Versions []Version `json:"versions"`
	}
	if err := p.call(ctx, "read", "/v1/versions", map[string]any{"slug": slug}, false, &out); err != nil {
		return nil, err
	}
	if out.Versions == nil {
		out.Versions = []Version{}
	}
	return out.Versions, nil
}

func (p *ExtProvider) Diff(ctx context.Context, slug, from, to string) (Diff, error) {
	var d Diff
	err := p.call(ctx, "read", "/v1/diff", map[string]any{"slug": slug, "from": from, "to": to}, false, &d)
	if d.Blocks == nil {
		d.Blocks = []integrations.DocDiffBlock{}
	}
	return d, err
}

func (p *ExtProvider) Publish(ctx context.Context, req PublishRequest) (Published, error) {
	var out Published
	in := map[string]any{"title": req.Title, "format": req.Format, "content": req.Content, "slug": req.Slug,
		"visibility": req.Visibility, "note": req.Note, "conversation": req.Conversation}
	err := p.call(ctx, "publish", "/v1/publish", in, true, &out)
	return out, err
}

func (p *ExtProvider) Embed(ctx context.Context, slug string) (Embed, error) {
	var e Embed
	err := p.call(ctx, "read", "/v1/embed", map[string]any{"slug": slug}, false, &e)
	if err == nil && e.URL == "" {
		err = &integrations.UpstreamError{Status: http.StatusForbidden, Text: "the adapter gave no address"}
	}
	return e, err
}

func (p *ExtProvider) Access(ctx context.Context, slug string) (Access, error) {
	var a Access
	err := p.call(ctx, "access", "/v1/access", map[string]any{"slug": slug}, false, &a)
	return a, err
}

func (p *ExtProvider) SetAccess(ctx context.Context, slug string, ch AccessChange) (Access, error) {
	in := map[string]any{"slug": slug}
	if ch.Public != nil {
		in["public"] = *ch.Public
	}
	if ch.Grants != nil {
		in["grants"] = *ch.Grants
	}
	var a Access
	err := p.call(ctx, "access", "/v1/access/set", in, true, &a)
	return a, err
}

func (p *ExtProvider) Threads(ctx context.Context, slug string) ([]Thread, error) {
	if !p.desc.Has("comments") {
		return []Thread{}, nil
	}
	var out struct {
		Threads []Thread `json:"threads"`
	}
	if err := p.call(ctx, "comments", "/v1/threads", map[string]any{"slug": slug}, false, &out); err != nil {
		return nil, err
	}
	if out.Threads == nil {
		out.Threads = []Thread{}
	}
	return out.Threads, nil
}

func (p *ExtProvider) CreateThread(ctx context.Context, slug string, a Anchor, body string, by Author) (Thread, error) {
	var t Thread
	err := p.call(ctx, "comments", "/v1/threads/create", map[string]any{"slug": slug, "anchor": a, "body": body, "author": by}, true, &t)
	return t, err
}

func (p *ExtProvider) Reply(ctx context.Context, slug, id, body string, by Author) (Thread, error) {
	var t Thread
	err := p.call(ctx, "comments", "/v1/threads/reply", map[string]any{"slug": slug, "threadId": id, "body": body, "author": by}, true, &t)
	return t, err
}

func (p *ExtProvider) SetStatus(ctx context.Context, slug, id, status string) (Thread, error) {
	var t Thread
	err := p.call(ctx, "comments", "/v1/threads/status", map[string]any{"slug": slug, "threadId": id, "status": status}, true, &t)
	return t, err
}

func (p *ExtProvider) Boards(ctx context.Context) ([]Board, error) {
	var out struct {
		Boards []Board `json:"boards"`
	}
	if err := p.call(ctx, "boards", "/v1/boards", nil, false, &out); err != nil {
		return nil, err
	}
	if out.Boards == nil {
		out.Boards = []Board{}
	}
	return out.Boards, nil
}

// Counts are in the list the adapter gives (openThreads, unread), so there is nothing to add.
func (p *ExtProvider) Counts(context.Context, string) (Counts, error) { return Counts{}, nil }

func (p *ExtProvider) MarkRead(ctx context.Context, person, slug string) error {
	if !p.desc.Has("comments") {
		return nil
	}
	return p.call(ctx, "comments", "/v1/threads/read", map[string]any{"person": person, "slug": slug}, true, &struct{}{})
}

// LoadAdapters makes a provider of every documents adapter the file declares (ext.Spec, kind "docs"). Entries of other
// protocols are skipped; one that does not answer is an error and the rest go on.
func LoadAdapters(ctx context.Context, path string) ([]*ExtProvider, []error) {
	if path == "" {
		return nil, nil
	}
	specs, err := ext.LoadSpecs(path)
	if err != nil {
		return nil, []error{err}
	}
	var out []*ExtProvider
	var errs []error
	for _, sp := range specs {
		if sp.Protocol != Protocol {
			continue
		}
		c, err := ext.NewClient(ext.Config{BaseURL: sp.URL, TokenFile: sp.TokenFile, Timeout: time.Duration(sp.TimeoutSeconds) * time.Second})
		if err == nil {
			var p *ExtProvider
			if p, err = Register(ctx, c); err == nil {
				out = append(out, p)
				continue
			}
		}
		errs = append(errs, fmt.Errorf("docs adapter %s: %w", sp.Name, err))
	}
	return out, errs
}
