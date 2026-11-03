package connections

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Helpers Jira and Confluence share: Atlassian Cloud detection, auth, probes of self-hosted instances, and the two
// markups Jira speaks (ADF on Cloud, wiki markup on Server/Data Center).

// atlCloudTokenURL is where a person creates an API token for Atlassian Cloud.
const atlCloudTokenURL = "https://id.atlassian.com/manage-profile/security/api-tokens"

// atlCloudHost says whether host is an Atlassian Cloud site (acme.atlassian.net).
func atlCloudHost(host string) bool { return HostIs(host, "atlassian.net") }

// atlWikiPath says whether a path of an Atlassian Cloud site belongs to Confluence: on one site Jira lives at the
// root and Confluence under /wiki. Both providers decide with this, so exactly one claims a link.
func atlWikiPath(path string) bool { return path == "/wiki" || strings.HasPrefix(path, "/wiki/") }

// atlCloudID reads the site's cloudId from the anonymous /_edge/tenant_info; "" when the site does not say.
func atlCloudID(ctx context.Context, p *Prober, origin string) string {
	var t struct {
		CloudID string `json:"cloudId"`
	}
	if res, ok := p.GetJSON(ctx, origin+"/_edge/tenant_info", &t); !ok || !answeredAt(res, origin+"/_edge/tenant_info") {
		return ""
	}
	return t.CloudID
}

// atlAuth is the Authorization header for typed credentials: email and API token (Cloud) or login and password
// (self-hosted) make Basic, a lone token (personal access token) makes Bearer.
func atlAuth(cred Credentials) string {
	switch {
	case cred[FieldEmail] != "" && cred[FieldToken] != "":
		return Basic(cred[FieldEmail], cred[FieldToken])
	case cred[FieldUsername] != "" && cred[FieldPassword] != "":
		return Basic(cred[FieldUsername], cred[FieldPassword])
	case cred[FieldToken] != "":
		return Bearer(cred[FieldToken])
	}
	return ""
}

// atlVersion reads the major and minor of "9.12.4" or "8.14.0-m01".
func atlVersion(v string) (major, minor int, ok bool) {
	m := atlVersionRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

var atlVersionRE = regexp.MustCompile(`^(\d+)\.(\d+)`)

// atlAtLeast compares a version with major.minor; known is false when the version is not readable.
func atlAtLeast(v string, major, minor int) (known, atLeast bool) {
	ma, mi, ok := atlVersion(v)
	if !ok {
		return false, false
	}
	return true, ma > major || (ma == major && mi >= minor)
}

// atlEdition: Data Center answers with X-ANODEID and is the only edition from dcFrom (Jira 10, Confluence 9) on; the
// rest is Server.
func atlEdition(version string, h http.Header, dcFrom int) string {
	if h.Get("X-ANODEID") != "" {
		return "data-center"
	}
	if ma, _, ok := atlVersion(version); ok && ma >= dcFrom {
		return "data-center"
	}
	return "server"
}

// atlFirst runs probe for every context path at once and returns the answer of the first path (in the order given)
// that found something.
func atlFirst[T any](prefixes []string, probe func(prefix string) *T) *T {
	out := make([]*T, len(prefixes))
	var wg sync.WaitGroup
	for i, pre := range prefixes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { _ = recover() }()
			out[i] = probe(pre)
		}()
	}
	wg.Wait()
	for _, x := range out {
		if x != nil {
			return x
		}
	}
	return nil
}

// atlManifest is what /rest/applinks/1.0/manifest says about a self-hosted Atlassian product.
type atlManifest struct {
	TypeID  string `xml:"typeId"`
	Name    string `xml:"name"`
	Version string `xml:"version"`
}

// atlProbeManifest reads the anonymous application-links manifest; ok is false when it is not that XML.
func atlProbeManifest(ctx context.Context, p *Prober, base string) (m atlManifest, ok bool) {
	res, err := p.Get(ctx, base+"/rest/applinks/1.0/manifest", "application/xml")
	if err != nil || res.Status != http.StatusOK || !answeredAt(res, base+"/rest/applinks/1.0/manifest") || !strings.Contains(string(res.Body), "typeId") {
		return m, false
	}
	if xml.Unmarshal(res.Body, &m) != nil || m.TypeID == "" {
		return atlManifest{}, false
	}
	m.TypeID = strings.ToLower(strings.TrimSpace(m.TypeID))
	m.Version = strings.TrimSpace(m.Version)
	return m, true
}

var (
	atlMetaTag = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	atlAttr    = regexp.MustCompile(`([\w:-]+)\s*=\s*"([^"]*)"`)
)

// atlMetas are the attributes of every <meta> tag of an HTML page, names lower-cased.
func atlMetas(body string) []map[string]string {
	var out []map[string]string
	for _, tag := range atlMetaTag.FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, a := range atlAttr.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(a[1])] = a[2]
		}
		out = append(out, attrs)
	}
	return out
}

// atlLoginPage fetches a login page anonymously and returns its meta tags, nil when it is not a 200 page.
func atlLoginPage(ctx context.Context, p *Prober, url string) (metas []map[string]string, body string) {
	res, err := p.Get(ctx, url, "text/html")
	if err != nil || res.Status != http.StatusOK || !answeredAt(res, url) {
		return nil, ""
	}
	body = string(res.Body)
	return atlMetas(body), body
}

// ---- ADF (Atlassian Document Format) to Markdown ----

type atlNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []atlMark      `json:"marks"`
	Content []atlNode      `json:"content"`
}

type atlMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

var atlBlank = regexp.MustCompile(`\n{3,}`)

// atlADF is the Markdown of an ADF document (Jira Cloud descriptions and comments); "" for anything unreadable.
func atlADF(raw json.RawMessage) string {
	var doc atlNode
	if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(atlBlank.ReplaceAllString(atlBlocks(doc.Content), "\n\n"))
}

func atlBlocks(nodes []atlNode) string {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if s := atlBlock(n); strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func atlStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func atlInt(m map[string]any, key string, def int) int {
	if f, ok := m[key].(float64); ok {
		return int(f)
	}
	return def
}

func atlBlock(n atlNode) string {
	switch n.Type {
	case "paragraph":
		return atlInline(n.Content)
	case "heading":
		return strings.Repeat("#", min(max(atlInt(n.Attrs, "level", 1), 1), 6)) + " " + atlInline(n.Content)
	case "bulletList", "orderedList":
		return atlList(n)
	case "taskList", "decisionList":
		var lines []string
		for _, it := range n.Content {
			box := "- [ ] "
			if strings.EqualFold(atlStr(it.Attrs, "state"), "DONE") {
				box = "- [x] "
			}
			lines = append(lines, box+atlInline(it.Content))
		}
		return strings.Join(lines, "\n")
	case "codeBlock":
		var sb strings.Builder
		for _, c := range n.Content {
			sb.WriteString(c.Text)
		}
		return "```" + atlStr(n.Attrs, "language") + "\n" + strings.TrimRight(sb.String(), "\n") + "\n```"
	case "blockquote":
		return atlQuote(atlBlocks(n.Content))
	case "rule":
		return "---"
	case "panel":
		label := atlStr(n.Attrs, "panelType")
		if label != "" {
			label = strings.ToUpper(label[:1]) + label[1:]
		}
		return atlQuote("**" + label + "**\n\n" + atlBlocks(n.Content))
	case "expand", "nestedExpand":
		body := atlBlocks(n.Content)
		if t := atlStr(n.Attrs, "title"); t != "" {
			return "**" + t + "**\n\n" + body
		}
		return body
	case "table":
		return atlTable(n)
	case "mediaSingle", "mediaGroup":
		return atlBlocks(n.Content)
	case "media":
		return atlMedia(n)
	case "blockCard", "embedCard":
		if u := atlStr(n.Attrs, "url"); u != "" {
			return "<" + u + ">"
		}
		return ""
	}
	if len(n.Content) > 0 {
		return atlBlocks(n.Content)
	}
	return atlInline([]atlNode{n})
}

func atlMedia(n atlNode) string {
	name := atlStr(n.Attrs, "alt")
	if name == "" {
		name = atlStr(n.Attrs, "id")
	}
	if name == "" {
		return "[attachment]"
	}
	return "[attachment: " + name + "]"
}

func atlQuote(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

func atlList(n atlNode) string {
	var lines []string
	order := atlInt(n.Attrs, "order", 1)
	for i, item := range n.Content {
		bullet := "- "
		if n.Type == "orderedList" {
			bullet = fmt.Sprintf("%d. ", order+i)
		}
		var kids []string
		for _, c := range item.Content {
			if s := atlBlock(c); s != "" {
				kids = append(kids, s)
			}
		}
		text := strings.Split(strings.Join(kids, "\n"), "\n")
		pad := strings.Repeat(" ", len(bullet))
		for j, l := range text {
			if j == 0 {
				lines = append(lines, bullet+l)
			} else if l != "" {
				lines = append(lines, pad+l)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func atlTable(n atlNode) string {
	var rows []string
	for i, row := range n.Content {
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			t := strings.ReplaceAll(atlBlocks(cell.Content), "\n\n", " ")
			t = strings.ReplaceAll(strings.ReplaceAll(t, "\n", " "), "|", `\|`)
			cells = append(cells, strings.TrimSpace(t))
		}
		rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			rows = append(rows, "|"+strings.Repeat(" --- |", len(cells)))
		}
	}
	return strings.Join(rows, "\n")
}

func atlInline(nodes []atlNode) string {
	var sb strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			sb.WriteString(atlMarks(n))
		case "hardBreak":
			sb.WriteString("\n")
		case "mention":
			t := atlStr(n.Attrs, "text")
			if t == "" {
				t = "@user"
			}
			sb.WriteString(t)
		case "emoji":
			t := atlStr(n.Attrs, "text")
			if t == "" {
				t = atlStr(n.Attrs, "shortName")
			}
			sb.WriteString(t)
		case "inlineCard":
			if u := atlStr(n.Attrs, "url"); u != "" {
				sb.WriteString("<" + u + ">")
			}
		case "status":
			sb.WriteString("[" + atlStr(n.Attrs, "text") + "]")
		case "date":
			if ms, err := strconv.ParseInt(atlStr(n.Attrs, "timestamp"), 10, 64); err == nil {
				sb.WriteString(time.UnixMilli(ms).UTC().Format("2006-01-02"))
			}
		case "media":
			sb.WriteString(atlMedia(n))
		default:
			sb.WriteString(atlInline(n.Content))
		}
	}
	return sb.String()
}

// atlMarks wraps a text node in its marks. Spaces stay outside the markers, or Markdown would not read them.
func atlMarks(n atlNode) string {
	text := n.Text
	if text == "" || strings.TrimSpace(text) == "" {
		return text
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	trail := text[len(strings.TrimRight(text, " \t")):]
	core := strings.TrimSpace(text)
	for _, m := range n.Marks {
		switch m.Type {
		case "code":
			core = "`" + core + "`"
		case "strong":
			core = "**" + core + "**"
		case "em":
			core = "*" + core + "*"
		case "strike":
			core = "~~" + core + "~~"
		case "link":
			if href := atlStr(m.Attrs, "href"); href != "" {
				core = "[" + core + "](" + href + ")"
			}
		}
	}
	return lead + core + trail
}

// ---- Jira wiki markup to Markdown ----

var (
	wikiBlockRE   = regexp.MustCompile(`(?s)\{(code|noformat)(?::([^}]*))?\}(.*?)\{(?:code|noformat)\}`)
	wikiMacroRE   = regexp.MustCompile(`(?i)^\{(panel|quote|toc|anchor|section|column|expand|info|note|warning|tip|color|excerpt|children|attachments)(?::([^}]*))?\}`)
	wikiColorRE   = regexp.MustCompile(`(?i)\{color(?::[^}]*)?\}`)
	wikiHeadingRE = regexp.MustCompile(`^h([1-6])\.\s*(.*)$`)
	wikiListRE    = regexp.MustCompile(`^\s*([*#-]+)\s+(.*)$`)
	wikiLinkRE    = regexp.MustCompile(`\[([^\[\]|]+)\|([^\[\]|]+)(?:\|[^\[\]]*)?\]`)
	wikiURLRE     = regexp.MustCompile(`\[(https?://[^\[\]|\s]+)\]`)
	wikiUserRE    = regexp.MustCompile(`\[~(?:accountid:)?([^\[\]]+)\]`)
	wikiAttachRE  = regexp.MustCompile(`\[\^([^\[\]]+)\]`)
	wikiImageRE   = regexp.MustCompile(`!([^!\s|]+\.[A-Za-z0-9]{2,5})(?:\|[^!]*)?!`)
	wikiMonoRE    = regexp.MustCompile(`\{\{(.+?)\}\}`)
	wikiBoldRE    = regexp.MustCompile(`(^|[^\w*])\*(\S(?:[^*\n]*?\S)?)\*($|[^\w*])`)
	wikiItalicRE  = regexp.MustCompile(`(^|[^\w])_(\S(?:[^_\n]*?\S)?)_($|[^\w])`)
)

// atlWiki turns Jira wiki markup (Server/Data Center descriptions and comments) into Markdown. It covers what people
// write — headings, lists, bold and italic, code, quotes, links, tables — and leaves the rest as it stands.
func atlWiki(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var blocks []string
	s = wikiBlockRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikiBlockRE.FindStringSubmatch(m)
		lang := strings.TrimSpace(sub[2])
		if strings.Contains(lang, "=") || sub[1] == "noformat" {
			lang = ""
		}
		blocks = append(blocks, "```"+lang+"\n"+strings.Trim(sub[3], "\n")+"\n```")
		return fmt.Sprintf("\n\x00%d\x00\n", len(blocks)-1)
	})
	var out []string
	quote := false
	tableHead := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "{quote}" {
			quote = !quote
			continue
		}
		if m := wikiMacroRE.FindStringSubmatch(t); m != nil && strings.EqualFold(m[1], "panel") && m[2] != "" {
			if title := atlWikiParam(m[2], "title"); title != "" {
				out = append(out, "**"+title+"**")
			}
			continue
		} else if m != nil && strings.TrimSpace(t[len(m[0]):]) == "" {
			continue // a macro tag alone on its line: {panel}, {toc}, {info}
		}
		line = wikiColorRE.ReplaceAllString(line, "")
		var text string
		switch {
		case strings.HasPrefix(t, "||"):
			cells := strings.Split(strings.Trim(t, "|"), "||")
			text = "| " + strings.Join(atlTrimAll(cells), " | ") + " |"
			text += "\n|" + strings.Repeat(" --- |", len(cells))
			tableHead = true
		case strings.HasPrefix(t, "|"):
			cells := strings.Split(strings.Trim(t, "|"), "|")
			text = "| " + atlWikiInline(strings.Join(atlTrimAll(cells), " | ")) + " |"
			if !tableHead && len(out) > 0 && !strings.HasPrefix(out[len(out)-1], "|") {
				text = "| " + strings.Repeat(" |", len(cells)) + "\n|" + strings.Repeat(" --- |", len(cells)) + "\n" + text
			}
		case wikiHeadingRE.MatchString(t):
			m := wikiHeadingRE.FindStringSubmatch(t)
			n, _ := strconv.Atoi(m[1])
			text = strings.Repeat("#", n) + " " + atlWikiInline(m[2])
		case strings.HasPrefix(t, "bq. "):
			text = "> " + atlWikiInline(t[4:])
		case t == "----":
			text = "---"
		case wikiListRE.MatchString(line) && !strings.HasPrefix(t, "--"):
			m := wikiListRE.FindStringSubmatch(line)
			marks := m[1]
			bullet := "- "
			if marks[len(marks)-1] == '#' {
				bullet = "1. "
			}
			text = strings.Repeat("  ", len(marks)-1) + bullet + atlWikiInline(m[2])
		default:
			text = atlWikiInline(strings.ReplaceAll(line, `\\`, "\n"))
		}
		if !strings.HasPrefix(t, "|") {
			tableHead = false
		}
		if quote {
			text = atlQuote(text)
		}
		out = append(out, text)
	}
	res := strings.Join(out, "\n")
	for i, b := range blocks {
		res = strings.Replace(res, fmt.Sprintf("\x00%d\x00", i), b, 1)
	}
	return strings.TrimSpace(atlBlank.ReplaceAllString(res, "\n\n"))
}

func atlTrimAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// atlWikiParam reads one parameter of a macro: "title=Foo|borderStyle=solid".
func atlWikiParam(params, name string) string {
	for _, kv := range strings.Split(params, "|") {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func atlWikiInline(s string) string {
	s = wikiLinkRE.ReplaceAllString(s, "[$1]($2)")
	s = wikiURLRE.ReplaceAllString(s, "<$1>")
	s = wikiUserRE.ReplaceAllString(s, "@$1")
	s = wikiAttachRE.ReplaceAllString(s, "[attachment: $1]")
	s = wikiImageRE.ReplaceAllString(s, "[image: $1]")
	s = wikiMonoRE.ReplaceAllString(s, "`$1`")
	// Adjacent matches share their boundary character, so run until nothing changes.
	for range 3 {
		next := wikiBoldRE.ReplaceAllString(s, "$1\x01$2\x02$3")
		if next == s {
			break
		}
		s = next
	}
	for range 3 {
		next := wikiItalicRE.ReplaceAllString(s, "$1\x03$2\x04$3")
		if next == s {
			break
		}
		s = next
	}
	return strings.NewReplacer("\x01", "**", "\x02", "**", "\x03", "*", "\x04", "*").Replace(s)
}
