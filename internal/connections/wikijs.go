package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Wiki.js 2.x speaks GraphQL at {base}/graphql with an API key as a Bearer. It is recognized by its schema (the types
// PageQuery and SiteQuery answer introspection). A page's id is its number; a link /<locale>/<path> becomes the key
// "<locale>/<path>", which Page resolves by path.

func init() { Register(&wikiJSProvider{}) }

type wikiJSProvider struct{}

func (*wikiJSProvider) Kind() Kind { return WikiJS }

var (
	wikiJSStops  = []string{"graphql", "login", "logout", "e", "h", "s", "t", "a", "p", "n", "tags", "_assets", "_graphql", "register"}
	wikiJSLocale = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
)

func (*wikiJSProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	for _, prefix := range PathPrefixes(u, wikiJSStops...) {
		base := Origin(u) + prefix
		var types struct {
			Data struct {
				Page *struct{ Name string } `json:"a"`
				Site *struct{ Name string } `json:"b"`
			} `json:"data"`
		}
		res, err := p.h.Do(ctx, Request{Method: http.MethodPost, URL: Join(base, "/graphql"),
			Body: map[string]string{"query": `{ a: __type(name: "PageQuery") { name } b: __type(name: "SiteQuery") { name } }`}})
		if err != nil || res.Status != 200 || !res.JSON() || !answeredAt(res, Join(base, "/graphql")) || res.Decode(&types) != nil || types.Data.Page == nil || types.Data.Site == nil {
			continue
		}
		title := "Wiki.js"
		var site struct {
			Data struct {
				Site struct {
					Config struct {
						Title string `json:"title"`
					} `json:"config"`
				} `json:"site"`
			} `json:"data"`
		}
		if r, err := p.h.Do(ctx, Request{Method: http.MethodPost, URL: Join(base, "/graphql"), Body: map[string]string{"query": `{ site { config { title } } }`}}); err == nil && r.Status == 200 {
			if r.Decode(&site) == nil && site.Data.Site.Config.Title != "" {
				title = site.Data.Site.Config.Title
			}
		}
		return &Detection{
			Kind: WikiJS, BaseURL: base, Deployment: SelfHosted, Edition: "2", Title: title,
			Methods: []Method{{ID: "api-key", Recommended: true, TokenURL: Join(base, "/a/api"),
				Fields: []Field{{ID: FieldToken, Secret: true, Placeholder: "API key"}}}},
			Capabilities: []Capability{Pages},
		}, nil
	}
	return nil, nil
}

func (*wikiJSProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &wikiJSClient{conn: conn, token: cred[FieldToken], h: h}
}

// Link: /<locale>/<path>.
func (*wikiJSProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	segs, ok := secondarySegments(conn.BaseURL, u)
	if !ok || len(segs) < 2 || !wikiJSLocale.MatchString(segs[0]) {
		return LinkTarget{}, false
	}
	for _, s := range wikiJSStops {
		if segs[0] == s {
			return LinkTarget{}, false
		}
	}
	return LinkTarget{Kind: "page", Key: segs[0] + "/" + strings.Join(segs[1:], "/")}, true
}

type wikiJSClient struct {
	conn  Conn
	token string
	h     *HTTP
}

type wikiJSError struct {
	Message string `json:"message"`
}

// query runs one GraphQL query; errors in the answer become ErrDenied or ErrUnreachable (never with the credentials).
func (c *wikiJSClient) query(ctx context.Context, q string, vars map[string]any, out any) error {
	var res struct {
		Data   map[string]any `json:"data"`
		Errors []wikiJSError  `json:"errors"`
	}
	r, err := c.h.JSON(ctx, Request{Method: http.MethodPost, URL: Join(c.conn.BaseURL, "/graphql"), Header: Header("Authorization", Bearer(c.token)),
		Body: map[string]any{"query": q, "variables": vars}}, &res)
	if err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		m := strings.ToLower(res.Errors[0].Message)
		if strings.Contains(m, "forbidden") || strings.Contains(m, "unauthor") || strings.Contains(m, "authenticat") || strings.Contains(m, "permission") {
			return fmt.Errorf("%w: %s", ErrDenied, res.Errors[0].Message)
		}
		return fmt.Errorf("%w: %s", ErrUnreachable, res.Errors[0].Message)
	}
	return r.Decode(out)
}

func (c *wikiJSClient) Me(ctx context.Context) (*Person, error) {
	var prof struct {
		Data struct {
			Users struct {
				Profile *struct {
					ID    int    `json:"id"`
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"profile"`
			} `json:"users"`
		} `json:"data"`
	}
	if err := c.query(ctx, `{ users { profile { id name email } } }`, nil, &prof); err == nil && prof.Data.Users.Profile != nil && prof.Data.Users.Profile.Name != "" {
		pr := prof.Data.Users.Profile
		return &Person{ID: strconv.Itoa(pr.ID), Name: pr.Name, Email: pr.Email}, nil
	}
	// An API key may have no profile; being able to list pages proves it signs in.
	var list struct {
		Data struct {
			Pages struct {
				List []struct{ ID int } `json:"list"`
			} `json:"pages"`
		} `json:"data"`
	}
	if err := c.query(ctx, `{ pages { list(limit: 1) { id } } }`, nil, &list); err != nil {
		return nil, err
	}
	return &Person{Name: "API key"}, nil
}

type wikiJSPage struct {
	ID          any    `json:"id"`
	Path        string `json:"path"`
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updatedAt"`
	Content     string `json:"content"`
	ContentType string `json:"contentType"`
	AuthorName  string `json:"authorName"`
}

func (c *wikiJSClient) page(p wikiJSPage) Page {
	return Page{ID: fmt.Sprint(p.ID), Title: p.Title, URL: Join(c.conn.BaseURL, "/"+p.Locale+"/"+p.Path), Space: p.Locale, Excerpt: p.Description, Updated: ParseTime(p.UpdatedAt)}
}

func (c *wikiJSClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	if text == "" {
		var out struct {
			Data struct {
				Pages struct {
					List []wikiJSPage `json:"list"`
				} `json:"pages"`
			} `json:"data"`
		}
		err := c.query(ctx, `query($n: Int!) { pages { list(limit: $n, orderBy: UPDATED, orderByDirection: DESC) { id path locale title description updatedAt } } }`,
			map[string]any{"n": q.LimitOr(20)}, &out)
		if err != nil {
			return nil, err
		}
		pages := make([]Page, 0, len(out.Data.Pages.List))
		for _, p := range out.Data.Pages.List {
			pages = append(pages, c.page(p))
		}
		return pages, nil
	}
	var out struct {
		Data struct {
			Pages struct {
				Search struct {
					Results []wikiJSPage `json:"results"`
				} `json:"search"`
			} `json:"pages"`
		} `json:"data"`
	}
	if err := c.query(ctx, `query($q: String!) { pages { search(query: $q) { results { id title path locale description } totalHits } } }`, map[string]any{"q": text}, &out); err != nil {
		return nil, err
	}
	res := out.Data.Pages.Search.Results
	res = res[:min(len(res), q.LimitOr(20))]
	pages := make([]Page, 0, len(res))
	for _, p := range res {
		pages = append(pages, c.page(p))
	}
	return pages, nil
}

func (c *wikiJSClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	const fields = `id path locale title description content contentType updatedAt authorName`
	var out struct {
		Data struct {
			Pages struct {
				Single       *wikiJSPage `json:"single"`
				SingleByPath *wikiJSPage `json:"singleByPath"`
			} `json:"pages"`
		} `json:"data"`
	}
	var found *wikiJSPage
	if n, err := strconv.Atoi(id); err == nil && n > 0 {
		if err := c.query(ctx, `query($id: Int!) { pages { single(id: $id) { `+fields+` } } }`, map[string]any{"id": n}, &out); err != nil {
			return nil, err
		}
		found = out.Data.Pages.Single
	} else if locale, path, ok := strings.Cut(id, "/"); ok && path != "" {
		if err := c.query(ctx, `query($path: String!, $locale: String!) { pages { singleByPath(path: $path, locale: $locale) { `+fields+` } } }`,
			map[string]any{"path": path, "locale": locale}, &out); err != nil {
			return nil, err
		}
		found = out.Data.Pages.SingleByPath
	}
	if found == nil {
		return nil, fmt.Errorf("%w: page %s", ErrNotFound, id)
	}
	pg := c.page(*found)
	pg.Body = found.Content
	if strings.EqualFold(found.ContentType, "html") {
		pg.Body = HTMLText(found.Content)
	}
	if found.AuthorName != "" {
		pg.Author = &Person{Name: found.AuthorName}
	}
	return &pg, nil
}
