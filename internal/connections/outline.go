package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Outline's API is RPC over POST with JSON bodies: {base}/api/<method>. Cloud is *.getoutline.com; self-hosted runs on
// any domain. A page's id is the document's UUID or its urlId (the part of /doc/<slug>-<urlId> after the last dash).

func init() { Register(&outlineProvider{}) }

type outlineProvider struct{}

func (*outlineProvider) Kind() Kind { return Outline }

var outlineStops = []string{"doc", "collection", "s", "home", "search", "settings", "api", "drafts", "archive", "trash", "templates", "welcome", "auth"}

func outlineMethods(base string) []Method {
	return []Method{{ID: "api-key", Recommended: true, TokenURL: Join(base, "/settings/tokens"),
		Fields: []Field{{ID: FieldToken, Secret: true, Placeholder: "ol_api_…"}}}}
}

func (*outlineProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	if HostIs(u.Hostname(), "getoutline.com") {
		base := "https://" + strings.ToLower(u.Hostname())
		return &Detection{Kind: Outline, BaseURL: base, Deployment: Cloud, Title: "Outline", Methods: outlineMethods(base), Capabilities: []Capability{Pages}}, nil
	}
	for _, prefix := range PathPrefixes(u, outlineStops...) {
		base := Origin(u) + prefix
		res, err := p.h.Do(ctx, Request{Method: http.MethodPost, URL: Join(base, "/api/auth.config"), Body: map[string]any{}})
		if err != nil || res.Status != 200 || !res.JSON() || !answeredAt(res, Join(base, "/api/auth.config")) {
			continue
		}
		var out struct {
			OK   bool `json:"ok"`
			Data struct {
				Name      string `json:"name"`
				Providers *[]any `json:"providers"`
			} `json:"data"`
		}
		if res.Decode(&out) != nil || !out.OK || out.Data.Providers == nil {
			continue
		}
		title := out.Data.Name
		if title == "" {
			title = "Outline"
		}
		return &Detection{Kind: Outline, BaseURL: base, Deployment: SelfHosted, Title: title, Methods: outlineMethods(base), Capabilities: []Capability{Pages}}, nil
	}
	return nil, nil
}

func (*outlineProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &outlineClient{conn: conn, token: cred[FieldToken], h: h}
}

// Link: {base}/doc/<slug>-<urlId>.
func (*outlineProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	segs, ok := secondarySegments(conn.BaseURL, u)
	if !ok || len(segs) < 2 || segs[len(segs)-2] != "doc" || segs[len(segs)-1] == "" {
		return LinkTarget{}, false
	}
	slug := segs[len(segs)-1]
	if i := strings.LastIndex(slug, "-"); i >= 0 {
		slug = slug[i+1:]
	}
	if slug == "" {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "page", Key: slug}, true
}

type outlineClient struct {
	conn  Conn
	token string
	h     *HTTP
}

type outlineDoc struct {
	ID        string `json:"id"`
	URLID     string `json:"urlId"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updatedAt"`
	CreatedBy *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"createdBy"`
}

func (c *outlineClient) post(ctx context.Context, method string, body, out any) error {
	_, err := c.h.JSON(ctx, Request{Method: http.MethodPost, URL: Join(c.conn.BaseURL, "/api/"+method), Header: Header("Authorization", Bearer(c.token)), Body: body}, out)
	return err
}

func (c *outlineClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		Data struct {
			User struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				Email     string `json:"email"`
				AvatarURL string `json:"avatarUrl"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := c.post(ctx, "auth.info", map[string]any{}, &out); err != nil {
		return nil, err
	}
	u := out.Data.User
	if u.ID == "" {
		return nil, fmt.Errorf("%w: no user in the answer", ErrDenied)
	}
	return &Person{ID: u.ID, Login: u.Email, Name: u.Name, Email: u.Email, Avatar: u.AvatarURL}, nil
}

func (c *outlineClient) docURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	b, err := url.Parse(c.conn.BaseURL)
	if err != nil {
		return path
	}
	return Origin(b) + "/" + strings.TrimLeft(path, "/")
}

func (c *outlineClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	body := map[string]any{"limit": q.LimitOr(25)}
	if q.Project != "" {
		body["collectionId"] = q.Project
	}
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	var docs []outlineDoc
	var contexts []string
	if text == "" {
		body["sort"], body["direction"] = "updatedAt", "DESC"
		var out struct {
			Data []outlineDoc `json:"data"`
		}
		if err := c.post(ctx, "documents.list", body, &out); err != nil {
			return nil, err
		}
		docs = out.Data
	} else {
		body["query"] = text
		var out struct {
			Data []struct {
				Context  string     `json:"context"`
				Document outlineDoc `json:"document"`
			} `json:"data"`
		}
		if err := c.post(ctx, "documents.search", body, &out); err != nil {
			return nil, err
		}
		for _, r := range out.Data {
			docs = append(docs, r.Document)
			contexts = append(contexts, r.Context)
		}
	}
	pages := make([]Page, 0, len(docs))
	for i, d := range docs {
		pg := Page{ID: d.ID, Title: d.Title, URL: c.docURL(d.URL), Updated: ParseTime(d.UpdatedAt)}
		if i < len(contexts) {
			pg.Excerpt = Clip(HTMLText(contexts[i]), 300)
		}
		pages = append(pages, pg)
	}
	return pages, nil
}

func (c *outlineClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: empty id", ErrNotFound)
	}
	var out struct {
		Data outlineDoc `json:"data"`
	}
	if err := c.post(ctx, "documents.info", map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	d := out.Data
	if d.ID == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	pg := &Page{ID: d.ID, Title: d.Title, URL: c.docURL(d.URL), Updated: ParseTime(d.UpdatedAt), Body: d.Text}
	if d.CreatedBy != nil {
		pg.Author = &Person{ID: d.CreatedBy.ID, Name: d.CreatedBy.Name, Email: d.CreatedBy.Email}
	}
	return pg, nil
}
