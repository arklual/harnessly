package connections

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// BookStack is told by its API's own answer to an anonymous call: 401 with "No authorization token found on the
// request". The API signs in with "Authorization: Token <id>:<secret>". A page's id is its number; a link by slugs
// (/books/<book>/page/<page>) becomes the key "<book>/<page>" that Page resolves through the API.

func init() { Register(&bookStackProvider{}) }

type bookStackProvider struct{}

func (*bookStackProvider) Kind() Kind { return BookStack }

var bookStackStops = []string{"books", "shelves", "link", "login", "settings", "api", "search", "pages", "page", "chapters", "my-account", "user", "register", "tags", "favourites"}

func (*bookStackProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	for _, prefix := range PathPrefixes(u, bookStackStops...) {
		base := Origin(u) + prefix
		res, err := p.Get(ctx, Join(base, "/api/system"), "")
		if err != nil || res.Status != 401 || !res.JSON() || !answeredAt(res, Join(base, "/api/system")) {
			continue
		}
		var out struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		if res.Decode(&out) != nil || !strings.Contains(out.Error.Message, "No authorization token found") {
			continue
		}
		return &Detection{
			Kind: BookStack, BaseURL: base, Deployment: SelfHosted, Title: "BookStack",
			Methods: []Method{{ID: "api-key", Recommended: true, TokenURL: Join(base, "/my-account/api-tokens"),
				Fields: []Field{{ID: FieldTokenID, Placeholder: "token id"}, {ID: FieldToken, Secret: true, Placeholder: "token secret"}}}},
			Capabilities: []Capability{Pages},
		}, nil
	}
	return nil, nil
}

func (*bookStackProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &bookStackClient{conn: conn, id: strings.TrimSpace(cred[FieldTokenID]), secret: strings.TrimSpace(cred[FieldToken]), h: h}
}

// Link: /link/<id> (permalink) and /books/<book>/page/<page>.
func (*bookStackProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	segs, ok := secondarySegments(conn.BaseURL, u)
	if !ok {
		return LinkTarget{}, false
	}
	switch {
	case len(segs) == 2 && segs[0] == "link":
		if n, err := strconv.Atoi(segs[1]); err == nil && n > 0 {
			return LinkTarget{Kind: "page", Key: segs[1]}, true
		}
	case len(segs) >= 4 && segs[0] == "books" && segs[2] == "page" && segs[1] != "" && segs[3] != "":
		return LinkTarget{Kind: "page", Key: segs[1] + "/" + segs[3]}, true
	}
	return LinkTarget{}, false
}

type bookStackClient struct {
	conn   Conn
	id     string
	secret string
	h      *HTTP
}

func (c *bookStackClient) get(ctx context.Context, path string, out any) error {
	_, err := c.h.JSON(ctx, Request{URL: Join(c.conn.BaseURL, "/api/"+strings.TrimLeft(path, "/")), Header: Header("Authorization", "Token "+c.id+":"+c.secret)}, out)
	return err
}

func (c *bookStackClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		Version string `json:"version"`
		AppName string `json:"app_name"`
	}
	if err := c.get(ctx, "system", &out); err != nil {
		return nil, err
	}
	// BookStack's API has no "who am I"; the token id is what identifies the sign-in.
	return &Person{ID: c.id, Name: "API token " + c.id}, nil
}

func (c *bookStackClient) pageURL(id int, bookSlug, slug, given string) string {
	if given != "" {
		return given
	}
	if bookSlug != "" && slug != "" {
		return Join(c.conn.BaseURL, "/books/"+bookSlug+"/page/"+slug)
	}
	return Join(c.conn.BaseURL, "/link/"+strconv.Itoa(id))
}

func (c *bookStackClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	count := strconv.Itoa(q.LimitOr(20))
	if text == "" {
		var out struct {
			Data []struct {
				ID        int    `json:"id"`
				Name      string `json:"name"`
				Slug      string `json:"slug"`
				BookSlug  string `json:"book_slug"`
				UpdatedAt string `json:"updated_at"`
			} `json:"data"`
		}
		if err := c.get(ctx, "pages?"+Q("count", count, "sort", "-updated_at"), &out); err != nil {
			return nil, err
		}
		pages := make([]Page, 0, len(out.Data))
		for _, d := range out.Data {
			pages = append(pages, Page{ID: strconv.Itoa(d.ID), Title: d.Name, URL: c.pageURL(d.ID, d.BookSlug, d.Slug, ""), Updated: ParseTime(d.UpdatedAt)})
		}
		return pages, nil
	}
	if q.Native == "" {
		text += " {type:page}"
	}
	var out struct {
		Data []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Slug     string `json:"slug"`
			BookSlug string `json:"book_slug"`
			Type     string `json:"type"`
			URL      string `json:"url"`
			Book     *struct {
				Name string `json:"name"`
			} `json:"book"`
			Preview struct {
				Content string `json:"content"`
			} `json:"preview_content"`
			UpdatedAt string `json:"updated_at"`
		} `json:"data"`
	}
	if err := c.get(ctx, "search?"+Q("query", text, "count", count), &out); err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(out.Data))
	for _, d := range out.Data {
		if d.Type != "" && d.Type != "page" {
			continue
		}
		pg := Page{ID: strconv.Itoa(d.ID), Title: d.Name, URL: c.pageURL(d.ID, d.BookSlug, d.Slug, d.URL), Excerpt: Clip(HTMLText(d.Preview.Content), 300), Updated: ParseTime(d.UpdatedAt)}
		if d.Book != nil {
			pg.Space = d.Book.Name
		}
		pages = append(pages, pg)
	}
	return pages, nil
}

// resolve turns "<book-slug>/<page-slug>" into the page's number.
func (c *bookStackClient) resolve(ctx context.Context, ref string) (string, error) {
	bookSlug, slug, _ := strings.Cut(ref, "/")
	var books struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := c.get(ctx, "books?"+Q("filter[slug]", bookSlug, "count", "1"), &books); err != nil {
		return "", err
	}
	if len(books.Data) == 0 {
		return "", fmt.Errorf("%w: book %s", ErrNotFound, bookSlug)
	}
	var pages struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := c.get(ctx, "pages?"+Q("filter[book_id]", strconv.Itoa(books.Data[0].ID), "filter[slug]", slug, "count", "1"), &pages); err != nil {
		return "", err
	}
	if len(pages.Data) == 0 {
		return "", fmt.Errorf("%w: page %s", ErrNotFound, ref)
	}
	return strconv.Itoa(pages.Data[0].ID), nil
}

func (c *bookStackClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	if strings.Contains(id, "/") {
		var err error
		if id, err = c.resolve(ctx, id); err != nil {
			return nil, err
		}
	}
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%w: bad page id %q", ErrNotFound, id)
	}
	var out struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Slug      string `json:"slug"`
		BookSlug  string `json:"book_slug"`
		URL       string `json:"url"`
		HTML      string `json:"html"`
		Markdown  string `json:"markdown"`
		UpdatedAt string `json:"updated_at"`
		CreatedBy *struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"created_by"`
	}
	if err := c.get(ctx, "pages/"+id, &out); err != nil {
		return nil, err
	}
	body := out.Markdown
	if strings.TrimSpace(body) == "" {
		body = HTMLText(out.HTML)
	}
	pg := &Page{ID: strconv.Itoa(out.ID), Title: out.Name, URL: c.pageURL(out.ID, out.BookSlug, out.Slug, out.URL), Updated: ParseTime(out.UpdatedAt), Body: body}
	if out.CreatedBy != nil {
		pg.Author = &Person{ID: strconv.Itoa(out.CreatedBy.ID), Name: out.CreatedBy.Name}
	}
	return pg, nil
}
