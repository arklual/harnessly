package connections

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// MediaWiki is found by its api.php (siteinfo names the generator "MediaWiki 1.xx") and read through the action API.
// Most wikis read anonymously (method "none"); a private wiki answers readapidenied and takes an OAuth 2.0 owner-only
// access token as a Bearer ("pat"). A page's id is its title.

func init() { Register(&mediaWikiProvider{}) }

type mediaWikiProvider struct{}

func (*mediaWikiProvider) Kind() Kind { return MediaWiki }

var mediaWikiStops = []string{"wiki", "index.php", "api.php", "rest.php", "load.php", "w", "mediawiki"}

var mediaWikiEdit = regexp.MustCompile(`(?is)<span class="mw-editsection[^"]*">.*?</span></span>|<span class="mw-editsection[^"]*">.*?</span>`)

type mwEnvelope struct {
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

func mediaWikiVersion(generator string) (string, bool) {
	v, ok := strings.CutPrefix(generator, "MediaWiki ")
	return strings.TrimSpace(v), ok
}

func (*mediaWikiProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	origin := Origin(u)
	seen := map[string]bool{}
	for _, prefix := range PathPrefixes(u, mediaWikiStops...) {
		for _, tail := range []string{"/api.php", "/w/api.php", "/wiki/api.php", "/mediawiki/api.php"} {
			api := origin + prefix + tail
			if seen[api] {
				continue
			}
			seen[api] = true
			if d := mediaWikiProbe(ctx, p, u, api); d != nil {
				return d, nil
			}
		}
	}
	return nil, nil
}

func mediaWikiProbe(ctx context.Context, p *Prober, u *url.URL, api string) *Detection {
	var site struct {
		mwEnvelope
		Query struct {
			General struct {
				Generator   string `json:"generator"`
				SiteName    string `json:"sitename"`
				ArticlePath string `json:"articlepath"`
				ScriptPath  string `json:"scriptpath"`
			} `json:"general"`
		} `json:"query"`
	}
	if res, ok := p.GetJSON(ctx, api+"?"+Q("action", "query", "meta", "siteinfo", "siprop", "general", "format", "json", "formatversion", "2"), &site); !ok || !answeredAt(res, api) {
		return nil
	}
	private := site.Error != nil && site.Error.Code == "readapidenied"
	version, isMW := mediaWikiVersion(site.Query.General.Generator)
	if !private && !isMW {
		return nil
	}
	g := site.Query.General
	if g.ArticlePath == "" {
		g.ArticlePath = "/wiki/$1"
	}
	if g.ScriptPath == "" {
		g.ScriptPath = strings.TrimSuffix(strings.TrimPrefix(api, Origin(u)), "/api.php")
	}
	deployment := SelfHosted
	tokenURL := Origin(u) + g.ScriptPath + "/index.php?title=Special:OAuthConsumerRegistration/propose/oauth2"
	for _, d := range []string{"wikipedia.org", "wikimedia.org", "wiktionary.org", "wikibooks.org", "wikisource.org", "wikivoyage.org", "wikiquote.org", "wikinews.org", "wikiversity.org", "mediawiki.org"} {
		if HostIs(u.Hostname(), d) {
			deployment = Cloud
			tokenURL = "https://meta.wikimedia.org/wiki/Special:OAuthConsumerRegistration/propose/oauth2"
		}
	}
	pat := Method{ID: "pat", TokenURL: tokenURL, Fields: []Field{{ID: FieldToken, Secret: true, Placeholder: "OAuth 2.0 access token"}}}
	methods := []Method{pat}
	if !private && mediaWikiReadsAnonymously(ctx, p, api) {
		methods = []Method{{ID: "none", Recommended: true}, pat}
	} else {
		methods[0].Recommended = true
	}
	// The context path is what precedes the article path.
	ctxPath := strings.TrimSuffix(g.ArticlePath, "$1")
	ctxPath = strings.TrimSuffix(strings.TrimRight(ctxPath, "/"), "/index.php")
	ctxPath = strings.TrimSuffix(ctxPath, "/wiki")
	title := g.SiteName
	return &Detection{
		Kind: MediaWiki, BaseURL: Origin(u) + ctxPath, APIURL: api, Deployment: deployment, Version: version, Title: title,
		Methods: methods, Capabilities: []Capability{Pages},
		Extra: map[string]string{"articlepath": g.ArticlePath, "scriptpath": g.ScriptPath},
	}
}

func mediaWikiReadsAnonymously(ctx context.Context, p *Prober, api string) bool {
	var out struct {
		mwEnvelope
		Query struct {
			UserInfo struct {
				Rights []string `json:"rights"`
			} `json:"userinfo"`
		} `json:"query"`
	}
	if res, ok := p.GetJSON(ctx, api+"?"+Q("action", "query", "meta", "userinfo", "uiprop", "rights", "format", "json", "formatversion", "2"), &out); !ok || !answeredAt(res, api) || out.Error != nil {
		return false
	}
	for _, r := range out.Query.UserInfo.Rights {
		if r == "read" {
			return true
		}
	}
	return false
}

func (*mediaWikiProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &mediaWikiClient{conn: conn, token: cred[FieldToken], h: h}
}

func (*mediaWikiProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	b, err := url.Parse(conn.BaseURL)
	if err != nil || !strings.EqualFold(b.Host, u.Host) {
		return LinkTarget{}, false
	}
	title := ""
	if t := u.Query().Get("title"); t != "" && strings.HasSuffix(u.Path, "/index.php") {
		title = t
	} else {
		prefix := strings.TrimSuffix(orDefault(conn.Extra["articlepath"], "/wiki/$1"), "$1")
		path := u.EscapedPath()
		if !strings.HasPrefix(path, prefix) || len(path) == len(prefix) {
			return LinkTarget{}, false
		}
		rest := path[len(prefix):]
		if strings.Contains(rest, "index.php") || strings.Contains(rest, "api.php") || strings.HasPrefix(rest, "w/") {
			return LinkTarget{}, false
		}
		if d, err := url.PathUnescape(rest); err == nil {
			title = d
		}
	}
	title = strings.TrimSpace(strings.ReplaceAll(title, "_", " "))
	if title == "" || strings.HasPrefix(title, "Special:") {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "page", Key: title}, true
}

type mediaWikiClient struct {
	conn  Conn
	token string
	h     *HTTP
}

func (c *mediaWikiClient) api(args ...string) Request {
	api := c.conn.APIURL
	if api == "" {
		api = Join(c.conn.BaseURL, "/api.php")
	}
	args = append(args, "format", "json", "formatversion", "2")
	r := Request{URL: api + "?" + Q(args...)}
	if c.conn.Method == "pat" {
		r.Header = Header("Authorization", Bearer(c.token))
	}
	return r
}

// call runs an API call and maps the API's own errors (HTTP 200 with an error object) to ours.
func (c *mediaWikiClient) call(ctx context.Context, out any, args ...string) error {
	var raw struct {
		mwEnvelope
	}
	res, err := c.h.JSON(ctx, c.api(args...), &raw)
	if err != nil {
		return err
	}
	if raw.Error != nil {
		switch raw.Error.Code {
		case "readapidenied", "permissiondenied", "mwoauth-invalid-authorization", "badtoken":
			return fmt.Errorf("%w: %s", ErrDenied, raw.Error.Code)
		case "missingtitle", "invalidtitle", "nosuchpageid":
			return fmt.Errorf("%w: %s", ErrNotFound, raw.Error.Code)
		}
		return fmt.Errorf("%w: %s", ErrUnreachable, raw.Error.Code)
	}
	return res.Decode(out)
}

func (c *mediaWikiClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		Query struct {
			UserInfo struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Anon bool   `json:"anon"`
			} `json:"userinfo"`
		} `json:"query"`
	}
	if err := c.call(ctx, &out, "action", "query", "meta", "userinfo"); err != nil {
		return nil, err
	}
	ui := out.Query.UserInfo
	if ui.Anon {
		if c.conn.Method == "pat" {
			return nil, fmt.Errorf("%w: the token signs in as nobody", ErrDenied)
		}
		return &Person{Name: "Anonymous"}, nil
	}
	if ui.Name == "" {
		return nil, fmt.Errorf("%w: no user in the answer", ErrUnreachable)
	}
	return &Person{ID: fmt.Sprint(ui.ID), Login: ui.Name, Name: ui.Name}, nil
}

func (c *mediaWikiClient) pageURL(title string) string {
	b, err := url.Parse(c.conn.BaseURL)
	if err != nil {
		return ""
	}
	prefix := strings.TrimSuffix(orDefault(c.conn.Extra["articlepath"], "/wiki/$1"), "$1")
	t := strings.ReplaceAll(url.PathEscape(strings.ReplaceAll(title, " ", "_")), "%2F", "/")
	return Origin(b) + prefix + t
}

func (c *mediaWikiClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	limit := fmt.Sprint(q.LimitOr(20))
	if text == "" {
		var out struct {
			Query struct {
				AllPages []struct {
					Title string `json:"title"`
				} `json:"allpages"`
			} `json:"query"`
		}
		if err := c.call(ctx, &out, "action", "query", "list", "allpages", "aplimit", limit); err != nil {
			return nil, err
		}
		pages := make([]Page, 0, len(out.Query.AllPages))
		for _, p := range out.Query.AllPages {
			pages = append(pages, Page{ID: p.Title, Title: p.Title, URL: c.pageURL(p.Title)})
		}
		return pages, nil
	}
	var out struct {
		Query struct {
			Search []struct {
				Title     string `json:"title"`
				Snippet   string `json:"snippet"`
				Timestamp string `json:"timestamp"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := c.call(ctx, &out, "action", "query", "list", "search", "srsearch", text, "srlimit", limit, "srprop", "snippet|timestamp"); err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(out.Query.Search))
	for _, s := range out.Query.Search {
		pages = append(pages, Page{ID: s.Title, Title: s.Title, URL: c.pageURL(s.Title), Excerpt: HTMLText(s.Snippet), Updated: ParseTime(s.Timestamp)})
	}
	return pages, nil
}

func (c *mediaWikiClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: empty title", ErrNotFound)
	}
	var out struct {
		Parse struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"parse"`
	}
	if err := c.call(ctx, &out, "action", "parse", "page", id, "prop", "text", "redirects", "1"); err != nil {
		return nil, err
	}
	title := out.Parse.Title
	if title == "" {
		title = id
	}
	return &Page{ID: title, Title: title, URL: c.pageURL(title), Body: HTMLText(mediaWikiEdit.ReplaceAllString(out.Parse.Text, ""))}, nil
}
