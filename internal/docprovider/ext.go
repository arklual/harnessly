package docprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
//	capability resolve   POST /v1/resolve {url} -> {kind: "document"|"board", id, embed?} (404: not this service's address)
//	capability boards         POST /v1/boards {} -> {boards: [Board]}
//	capability boards-search  POST /v1/boards/search {query, limit} -> {boards: [Board]}
//
// `read` is required; a call of a capability that is not listed is not sent. A document and a board are named by
// the adapter's own ID; the runtime shows it as an alias (Alias) and sends the ID back. An answer bigger than the
// client takes (1 MiB unless the spec's maxAnswerBytes raises it), or a 413 / `too_large`, is ErrTooLarge.
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

// Capabilities are the ones the adapter described.
func (p *ExtProvider) Capabilities() Capabilities {
	return Capabilities{Provider: p.desc.Name, Publish: p.desc.Has("publish"), Access: p.desc.Has("access"),
		Comments: p.desc.Has("comments"), Boards: p.desc.Has("boards"),
		BoardsSearch: p.desc.Has("boards-search") || p.desc.Has("boards"), ProviderEmbed: true, MaxAnswerBytes: p.cl.MaxAnswer()}
}

var errNoAlias = &integrations.UpstreamError{Status: http.StatusNotFound, Text: "not found"}

// id is the adapter's ID of the runtime's slug.
func id(slug string) (string, error) {
	x, ok := ExternalID(slug)
	if !ok {
		return "", errNoAlias
	}
	return x, nil
}

// local turns the adapter's IDs of a summary into the runtime's names; the viewer address stays the adapter's.
func local(s Summary) Summary {
	if a := Alias(s.Slug); a != s.Slug {
		s.ExternalID, s.Slug = s.Slug, a
	}
	s.ProviderEmbed = true
	return s
}

func localBoards(in []Board) []Board {
	out := make([]Board, 0, len(in))
	for _, b := range in {
		a := Alias(b.Slug)
		if a == "" {
			continue
		}
		if a != b.Slug {
			b.ExternalID, b.Slug = b.Slug, a
		}
		out = append(out, b)
	}
	return out
}

// reasonOf maps an adapter's refusal to the errors the reader already answers.
func reasonOf(err error) error {
	var e *ext.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ext.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, ext.ErrTooLarge):
		return ErrTooLarge
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
	docs := make([]Summary, 0, len(out.Docs))
	for _, d := range out.Docs {
		if Alias(d.Slug) != "" {
			docs = append(docs, local(d))
		}
	}
	return docs, nil
}

func (p *ExtProvider) Get(ctx context.Context, slug string) (Summary, error) {
	x, err := id(slug)
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	if err := p.call(ctx, "read", "/v1/get", map[string]any{"slug": x}, false, &s); err != nil {
		return Summary{}, err
	}
	if s.Slug == "" {
		s.Slug = x
	}
	return local(s), nil
}

func (p *ExtProvider) Read(ctx context.Context, slug, version string) (Content, error) {
	x, err := id(slug)
	if err != nil {
		return Content{}, err
	}
	var c Content
	err = p.call(ctx, "read", "/v1/read", map[string]any{"slug": x, "version": version}, false, &c)
	return c, err
}

// Versions is the history; an adapter that keeps none (501 on /v1/versions) has an empty one, not an error.
func (p *ExtProvider) Versions(ctx context.Context, slug string) ([]Version, error) {
	x, err := id(slug)
	if err != nil {
		return nil, err
	}
	var out struct {
		Versions []Version `json:"versions"`
	}
	if err := p.call(ctx, "read", "/v1/versions", map[string]any{"slug": x}, false, &out); errors.Is(err, ErrUnsupported) {
		return []Version{}, nil
	} else if err != nil {
		return nil, err
	}
	if out.Versions == nil {
		out.Versions = []Version{}
	}
	return out.Versions, nil
}

func (p *ExtProvider) Diff(ctx context.Context, slug, from, to string) (Diff, error) {
	x, err := id(slug)
	if err != nil {
		return Diff{}, err
	}
	var d Diff
	err = p.call(ctx, "read", "/v1/diff", map[string]any{"slug": x, "from": from, "to": to}, false, &d)
	if d.Blocks == nil {
		d.Blocks = []integrations.DocDiffBlock{}
	}
	return d, err
}

func (p *ExtProvider) Publish(ctx context.Context, req PublishRequest) (Published, error) {
	target := ""
	if req.Slug != "" {
		x, err := id(req.Slug)
		if err != nil {
			return Published{}, err
		}
		target = x
	}
	var out Published
	in := map[string]any{"title": req.Title, "format": req.Format, "content": req.Content, "slug": target,
		"visibility": req.Visibility, "note": req.Note, "conversation": req.Conversation}
	if err := p.call(ctx, "publish", "/v1/publish", in, true, &out); err != nil {
		return Published{}, err
	}
	a := Alias(out.Slug)
	if a == "" {
		return Published{}, &integrations.UpstreamError{Status: http.StatusBadGateway, Text: "the adapter named the document with an id longer than " + strconv.Itoa(MaxExternalID)}
	}
	out.ExternalID, out.Slug = out.Slug, a
	return out, nil
}

// Resolve asks the adapter which document or board an address names. An adapter without the `resolve` capability claims
// nothing (ok is false and no call is sent), so the address keeps the pane's own rules.
func (p *ExtProvider) Resolve(ctx context.Context, address string) (Resolved, bool, error) {
	if !p.desc.Has("resolve") {
		return Resolved{}, false, nil
	}
	var out Resolved
	err := p.call(ctx, "resolve", "/v1/resolve", map[string]any{"url": address}, false, &out)
	var up *integrations.UpstreamError
	switch {
	case errors.As(err, &up) && up.Status == http.StatusNotFound, errors.Is(err, ErrUnsupported):
		return Resolved{}, false, nil
	case err != nil:
		return Resolved{}, false, err
	case (out.Kind != "document" && out.Kind != "board") || Alias(out.ID) == "":
		return Resolved{}, false, nil
	}
	return out, true, nil
}

func (p *ExtProvider) Embed(ctx context.Context, slug string) (Embed, error) {
	x, err := id(slug)
	if err != nil {
		return Embed{}, err
	}
	var e Embed
	err = p.call(ctx, "read", "/v1/embed", map[string]any{"slug": x}, false, &e)
	if err == nil && e.URL == "" {
		err = &integrations.UpstreamError{Status: http.StatusForbidden, Text: "the adapter gave no address"}
	}
	return e, err
}

func (p *ExtProvider) Access(ctx context.Context, slug string) (Access, error) {
	x, err := id(slug)
	if err != nil {
		return Access{}, err
	}
	var a Access
	err = p.call(ctx, "access", "/v1/access", map[string]any{"slug": x}, false, &a)
	return a, err
}

func (p *ExtProvider) SetAccess(ctx context.Context, slug string, ch AccessChange) (Access, error) {
	x, err := id(slug)
	if err != nil {
		return Access{}, err
	}
	in := map[string]any{"slug": x}
	if ch.Public != nil {
		in["public"] = *ch.Public
	}
	if ch.Grants != nil {
		in["grants"] = *ch.Grants
	}
	var a Access
	err = p.call(ctx, "access", "/v1/access/set", in, true, &a)
	return a, err
}

func (p *ExtProvider) Threads(ctx context.Context, slug string) ([]Thread, error) {
	if !p.desc.Has("comments") {
		return []Thread{}, nil
	}
	x, err := id(slug)
	if err != nil {
		return nil, err
	}
	var out struct {
		Threads []Thread `json:"threads"`
	}
	if err := p.call(ctx, "comments", "/v1/threads", map[string]any{"slug": x}, false, &out); err != nil {
		return nil, err
	}
	if out.Threads == nil {
		out.Threads = []Thread{}
	}
	return out.Threads, nil
}

func (p *ExtProvider) CreateThread(ctx context.Context, slug string, a Anchor, body string, by Author) (Thread, error) {
	x, err := id(slug)
	if err != nil {
		return Thread{}, err
	}
	var t Thread
	err = p.call(ctx, "comments", "/v1/threads/create", map[string]any{"slug": x, "anchor": a, "body": body, "author": by}, true, &t)
	return t, err
}

func (p *ExtProvider) Reply(ctx context.Context, slug, threadID, body string, by Author) (Thread, error) {
	x, err := id(slug)
	if err != nil {
		return Thread{}, err
	}
	var t Thread
	err = p.call(ctx, "comments", "/v1/threads/reply", map[string]any{"slug": x, "threadId": threadID, "body": body, "author": by}, true, &t)
	return t, err
}

func (p *ExtProvider) SetStatus(ctx context.Context, slug, threadID, status string) (Thread, error) {
	x, err := id(slug)
	if err != nil {
		return Thread{}, err
	}
	var t Thread
	err = p.call(ctx, "comments", "/v1/threads/status", map[string]any{"slug": x, "threadId": threadID, "status": status}, true, &t)
	return t, err
}

// Boards is the adapter's list; an adapter that only searches answers ErrSearchRequired.
func (p *ExtProvider) Boards(ctx context.Context) ([]Board, error) {
	if !p.desc.Has("boards") && p.desc.Has("boards-search") {
		return nil, ErrSearchRequired
	}
	var out struct {
		Boards []Board `json:"boards"`
	}
	if err := p.call(ctx, "boards", "/v1/boards", nil, false, &out); err != nil {
		return nil, err
	}
	return localBoards(out.Boards), nil
}

// SearchBoards asks the adapter's search, or filters its list when it has no search.
func (p *ExtProvider) SearchBoards(ctx context.Context, query string) ([]Board, error) {
	if !p.desc.Has("boards-search") {
		all, err := p.Boards(ctx)
		if err != nil {
			return nil, err
		}
		return filterBoards(all, query), nil
	}
	var out struct {
		Boards []Board `json:"boards"`
	}
	in := map[string]any{"query": strings.TrimSpace(query), "limit": integrations.BoardsMax}
	if err := p.call(ctx, "boards-search", "/v1/boards/search", in, false, &out); err != nil {
		return nil, err
	}
	return localBoards(out.Boards), nil
}

// Counts are in the list the adapter gives (openThreads, unread), so there is nothing to add.
func (p *ExtProvider) Counts(context.Context, string) (Counts, error) { return Counts{}, nil }

func (p *ExtProvider) MarkRead(ctx context.Context, person, slug string) error {
	if !p.desc.Has("comments") {
		return nil
	}
	x, err := id(slug)
	if err != nil {
		return err
	}
	return p.call(ctx, "comments", "/v1/threads/read", map[string]any{"person": person, "slug": x}, true, &struct{}{})
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
		if err := sp.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("docs adapter %s: %w", sp.Name, err))
			continue
		}
		c, err := ext.NewClient(ext.Config{BaseURL: sp.URL, TokenFile: sp.TokenFile, Timeout: time.Duration(sp.TimeoutSeconds) * time.Second,
			MaxAnswer: sp.MaxAnswerBytes})
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
