package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Notion is a SaaS: notion.so / *.notion.site in the browser, https://api.notion.com/v1 for the API with an internal
// integration secret as a Bearer and the Notion-Version header. The integration sees only the pages shared with it.

func init() { Register(&notionProvider{}) }

type notionProvider struct{}

func (*notionProvider) Kind() Kind { return Notion }

const (
	notionAPI     = "https://api.notion.com"
	notionVersion = "2022-06-28"
	// notionMaxCalls bounds the requests spent on one page's block tree.
	notionMaxCalls = 25
)

var notionID = regexp.MustCompile(`(?i)([0-9a-f]{32})$`)

func notionHost(host string) bool {
	return HostIs(host, "notion.so") || HostIs(host, "notion.site") || HostIs(host, "notion.com")
}

func (*notionProvider) Detect(_ context.Context, _ *Prober, u *url.URL) (*Detection, error) {
	if !notionHost(u.Hostname()) {
		return nil, nil
	}
	return &Detection{
		Kind: Notion, BaseURL: "https://www.notion.so", APIURL: notionAPI, Deployment: Cloud, Title: "Notion",
		Methods: []Method{{ID: "api-key", Recommended: true, TokenURL: "https://www.notion.so/my-integrations",
			Fields: []Field{{ID: FieldToken, Secret: true, Placeholder: "ntn_…"}}}},
		Capabilities: []Capability{Pages},
	}, nil
}

func (*notionProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &notionClient{conn: conn, token: cred[FieldToken], h: h}
}

// notionUUID is the canonical form of a 32-hex page id.
func notionUUID(hex string) string {
	h := strings.ToLower(hex)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Link: any notion.so / notion.site address ending in a 32-hex id: Title-<id>, <id>.
func (*notionProvider) Link(_ Conn, u *url.URL) (LinkTarget, bool) {
	if !notionHost(u.Hostname()) {
		return LinkTarget{}, false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	last := segs[len(segs)-1]
	m := notionID.FindStringSubmatch(last)
	if m == nil || (len(last) > 32 && last[len(last)-33] != '-') {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "page", Key: notionUUID(m[1])}, true
}

type notionClient struct {
	conn  Conn
	token string
	h     *HTTP
}

func (c *notionClient) req(method, path string, body any) Request {
	return Request{Method: method, URL: Join(orDefault(c.conn.APIURL, notionAPI), "/v1/"+strings.TrimLeft(path, "/")),
		Header: Header("Authorization", Bearer(c.token), "Notion-Version", notionVersion), Body: body}
}

func (c *notionClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Bot       *struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if _, err := c.h.JSON(ctx, c.req(http.MethodGet, "users/me", nil), &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("%w: no user in the answer", ErrUnreachable)
	}
	name := out.Name
	if name == "" {
		name = "Notion integration"
	}
	p := &Person{ID: out.ID, Name: name, Avatar: out.AvatarURL}
	if out.Bot != nil {
		p.Login = out.Bot.WorkspaceName
	}
	return p, nil
}

type notionRich struct {
	PlainText string `json:"plain_text"`
}

func notionPlain(rs []notionRich) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

type notionPageObj struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	LastEditedTime string `json:"last_edited_time"`
	Properties     map[string]struct {
		Type  string       `json:"type"`
		Title []notionRich `json:"title"`
	} `json:"properties"`
	Parent struct {
		Type string `json:"type"`
	} `json:"parent"`
}

func (p notionPageObj) title() string {
	for _, pr := range p.Properties {
		if pr.Type == "title" {
			if t := notionPlain(pr.Title); t != "" {
				return t
			}
		}
	}
	return "Untitled"
}

func (c *notionClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	body := map[string]any{
		"filter":    map[string]string{"property": "object", "value": "page"},
		"sort":      map[string]string{"direction": "descending", "timestamp": "last_edited_time"},
		"page_size": min(q.LimitOr(25), 100),
	}
	if text != "" {
		body["query"] = text
	}
	var out struct {
		Results []notionPageObj `json:"results"`
	}
	if _, err := c.h.JSON(ctx, c.req(http.MethodPost, "search", body), &out); err != nil {
		return nil, err
	}
	pages := make([]Page, 0, len(out.Results))
	for _, r := range out.Results {
		pages = append(pages, Page{ID: r.ID, Title: r.title(), URL: r.URL, Updated: ParseTime(r.LastEditedTime)})
	}
	return pages, nil
}

func (c *notionClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	if m := notionID.FindStringSubmatch(strings.ReplaceAll(id, "-", "")); m != nil && len(strings.ReplaceAll(id, "-", "")) == 32 {
		id = notionUUID(m[1])
	} else {
		return nil, fmt.Errorf("%w: bad page id %q", ErrNotFound, id)
	}
	var obj notionPageObj
	if _, err := c.h.JSON(ctx, c.req(http.MethodGet, "pages/"+id, nil), &obj); err != nil {
		return nil, err
	}
	calls := 0
	var lines []string
	if err := c.blocks(ctx, id, 0, &calls, &lines); err != nil {
		return nil, err
	}
	return &Page{ID: obj.ID, Title: obj.title(), URL: obj.URL, Updated: ParseTime(obj.LastEditedTime), Body: strings.TrimSpace(strings.Join(lines, "\n"))}, nil
}

// blocks appends the text of a block's children, to a depth of 4 and a budget of notionMaxCalls requests.
func (c *notionClient) blocks(ctx context.Context, id string, depth int, calls *int, lines *[]string) error {
	cursor := ""
	for {
		if *calls >= notionMaxCalls {
			return nil
		}
		*calls++
		path := "blocks/" + id + "/children?page_size=100"
		if cursor != "" {
			path += "&" + Q("start_cursor", cursor)
		}
		var out struct {
			Results    []map[string]any `json:"results"`
			HasMore    bool             `json:"has_more"`
			NextCursor string           `json:"next_cursor"`
		}
		if _, err := c.h.JSON(ctx, c.req(http.MethodGet, path, nil), &out); err != nil {
			return err
		}
		numbered := 0
		for _, b := range out.Results {
			typ, _ := b["type"].(string)
			content, _ := b[typ].(map[string]any)
			text := notionBlockText(typ, content)
			indent := strings.Repeat("  ", depth)
			if typ == "numbered_list_item" {
				numbered++
				text = fmt.Sprintf("%d. %s", numbered, strings.TrimPrefix(text, "1. "))
			} else {
				numbered = 0
			}
			if text != "" {
				*lines = append(*lines, indent+text)
			}
			if has, _ := b["has_children"].(bool); has && depth < 3 && typ != "child_page" && typ != "child_database" {
				if cid, _ := b["id"].(string); cid != "" {
					if err := c.blocks(ctx, cid, depth+1, calls, lines); err != nil {
						return err
					}
				}
			}
		}
		if !out.HasMore || out.NextCursor == "" {
			return nil
		}
		cursor = out.NextCursor
	}
}

// notionBlockText is one block as a line of Markdown-ish text; "" for blocks without text (images, dividers).
func notionBlockText(typ string, content map[string]any) string {
	var rich []notionRich
	if arr, ok := content["rich_text"].([]any); ok {
		for _, r := range arr {
			if m, ok := r.(map[string]any); ok {
				s, _ := m["plain_text"].(string)
				rich = append(rich, notionRich{PlainText: s})
			}
		}
	}
	t := notionPlain(rich)
	switch typ {
	case "paragraph", "toggle", "callout", "template":
		return t
	case "heading_1":
		return "# " + t
	case "heading_2":
		return "## " + t
	case "heading_3":
		return "### " + t
	case "bulleted_list_item":
		return "- " + t
	case "numbered_list_item":
		return "1. " + t
	case "to_do":
		if checked, _ := content["checked"].(bool); checked {
			return "- [x] " + t
		}
		return "- [ ] " + t
	case "quote":
		return "> " + t
	case "code":
		lang, _ := content["language"].(string)
		return "```" + lang + "\n" + t + "\n```"
	case "child_page":
		title, _ := content["title"].(string)
		return "[page] " + title
	}
	return ""
}
