// Package sources is what a whole conversation refers to, read from its transcript the way the previous runtime's
// conversation sources route did: the links of the agent's answers (documents, merge requests, tracker issues and the
// rest of the web) and the filesystem paths it worked with.
package sources

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/preview/weburl"
)

// Links names the services whose links are told apart.
type Links struct {
	// TrackerURL is the issue tracker's origin (https://tracker.example.com): a bare issue key becomes
	// <TrackerURL>/issue/<key>. TrackerHosts are the hosts its issue links use, IssueKeys its project keys.
	TrackerURL   string
	TrackerHosts []string
	IssueKeys    []string
	// GitLabHosts are the GitLabs whose merge requests are recognised.
	GitLabHosts []string
	// DocsHost serves published documents (/a/<slug>) and boards (/b/<slug>); ReaderHost and ReaderPath are where the
	// platform reads a document (<ReaderPath><slug>).
	DocsHost, ReaderHost, ReaderPath string
}

// Link is one link of the conversation, as the previous runtime listed it.
type Link struct {
	URL       string
	Label     *string
	Timestamp float64
	Count     int
	Kind      string
}

// Group is the link's place in the panel.
func (l Link) Group() string {
	switch l.Kind {
	case "artifact", "claude-artifact":
		return "artifact"
	case "mr", "issue":
		return l.Kind
	}
	return "other"
}

// Value is the link as the route writes it.
func (l Link) Value() *jsvalue.Object {
	var label any
	if l.Label != nil {
		label = *l.Label
	}
	var ts any = l.Timestamp
	if math.IsNaN(l.Timestamp) || math.IsInf(l.Timestamp, 0) {
		ts = nil
	}
	return jsvalue.NewObject("url", l.URL, "label", label, "timestamp", ts, "count", l.Count, "kind", l.Kind, "group", l.Group())
}

var (
	issuePath          = regexp.MustCompile(`^/(?:youtrack/)?issue/([A-Z][A-Z0-9]*-\d+)(?:/|$)`)
	mrPath             = regexp.MustCompile(`^/((?:[\w.-]+/)+[\w.-]+)/-/merge_requests/(\d+)(?:/|$)`)
	artifactPath       = regexp.MustCompile(`^/a/[\w-]+/?$`)
	boardPath          = regexp.MustCompile(`^/b/[\w-]+/?$`)
	portalPath         = regexp.MustCompile(`^/(?:a/([\w-]+)|b/([\w-]+))/?$`)
	claudeArtifactPath = regexp.MustCompile(`^/(?:public/artifacts|artifact|code/artifact)/[\w-]+/?$`)
)

// isBoardPath is /b/<slug> except /b/api, the boards' documentation.
func isBoardPath(p string) bool {
	return boardPath.MatchString(p) && !(p == "/b/api" || p == "/b/api/")
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type presented struct {
	kind    string
	key     string
	project string
	iid     string
}

// present is the kind of a web link (presentLink of the previous runtime, only what the collector reads of it).
func (c *Links) present(href string, u *weburl.URL) presented {
	host := strings.TrimPrefix(u.Hostname, "www.")
	path := u.Path
	switch {
	case has(c.TrackerHosts, host):
		if m := issuePath.FindStringSubmatch(path); m != nil {
			return presented{kind: "issue", key: m[1]}
		}
		return presented{kind: "web"}
	case has(c.GitLabHosts, host):
		if m := mrPath.FindStringSubmatch(path); m != nil {
			iid, _ := strconv.ParseFloat(m[2], 64)
			return presented{kind: "mr", project: m[1], iid: jscompat.Number(iid)}
		}
		return presented{kind: "gitlab"}
	case c.DocsHost != "" && host == c.DocsHost:
		if isBoardPath(path) || artifactPath.MatchString(path) {
			return presented{kind: "artifact"}
		}
		return presented{kind: "web"}
	case c.ReaderHost != "" && u.Hostname == c.ReaderHost && c.docSlug(href) != "":
		return presented{kind: "artifact"}
	case host == "claude.ai" && claudeArtifactPath.MatchString(path):
		return presented{kind: "claude-artifact"}
	case host == "github.com":
		return presented{kind: "github"}
	}
	return presented{kind: "web"}
}

// docSlug is the slug of a document or a board, from its portal address or the platform reader's; "" for none.
func (c *Links) docSlug(raw string) string {
	u, ok := weburl.Parse(raw)
	if !ok || u.Scheme != "https" {
		return ""
	}
	switch {
	case c.DocsHost != "" && u.Hostname == c.DocsHost:
		m := portalPath.FindStringSubmatch(u.Path)
		if m == nil {
			return ""
		}
		if m[1] != "" {
			return m[1]
		}
		if m[2] == "api" {
			return ""
		}
		return m[2]
	case c.ReaderHost != "" && u.Hostname == c.ReaderHost:
		rest, ok := strings.CutPrefix(u.Path, c.ReaderPath)
		rest = strings.TrimSuffix(rest, "/")
		if ok && rest != "" && !strings.ContainsFunc(rest, func(r rune) bool { return !isWordRune(r) && r != '-' }) {
			return rest
		}
	}
	return ""
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
}

type found struct {
	id, url, kind string
	label         *string
}

// trailing punctuation a sentence leaves after a link.
func cleanURL(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return strings.ContainsRune(".,;:!?»”’", r) })
}

func (c *Links) issueURL(key string) string { return c.TrackerURL + "/issue/" + key }

// find is one link found in an answer; false for a local address or what is not a URL.
func (c *Links) find(raw string, label *string) (found, bool) {
	cleaned := cleanURL(raw)
	u, ok := weburl.Parse(cleaned)
	if !ok {
		return found{}, false
	}
	switch u.Hostname {
	case "localhost", "127.0.0.1", "0.0.0.0":
		return found{}, false
	}
	p := c.present(cleaned, u)
	f := found{url: cleaned, kind: p.kind, label: label}
	switch {
	case p.kind == "issue" && p.key != "":
		f.id, f.url = "issue:"+p.key, c.issueURL(p.key)
	case p.kind == "mr" && p.project != "":
		f.id = "mr:" + p.project + "!" + p.iid
	default:
		slug := ""
		if p.kind == "artifact" {
			slug = c.docSlug(cleaned)
		}
		if slug != "" {
			f.id = "doc:" + slug
		} else {
			f.id, _, _ = strings.Cut(cleaned, "#")
		}
	}
	return f, true
}

// linksOf are the links of one answer in the order the previous runtime found them: labelled Markdown links, bare
// URLs, then bare issue keys.
func (c *Links) linksOf(text string) []found {
	var out []found
	labelled := map[string]bool{}
	var rest strings.Builder
	last := 0
	for _, m := range markdownLinks(text) {
		url := text[m.urlFrom:m.urlTo]
		labelled[cleanURL(url)] = true
		label := jscompat.Trim(text[m.labelFrom:m.labelTo])
		if f, ok := c.find(url, &label); ok {
			out = append(out, f)
		}
		rest.WriteString(text[last:m.from])
		rest.WriteByte(' ')
		last = m.to
	}
	rest.WriteString(text[last:])
	unlabelled := rest.String()
	var keyText strings.Builder
	last = 0
	for _, m := range bareURLs(unlabelled) {
		url := unlabelled[m[0]:m[1]]
		if !labelled[cleanURL(url)] {
			if f, ok := c.find(url, nil); ok {
				out = append(out, f)
			}
		}
		keyText.WriteString(unlabelled[last:m[0]])
		keyText.WriteByte(' ')
		last = m[1]
	}
	keyText.WriteString(unlabelled[last:])
	for _, key := range c.issueKeys(keyText.String()) {
		if f, ok := c.find(c.issueURL(key), nil); ok {
			out = append(out, f)
		}
	}
	return out
}

type mdLink struct{ from, to, labelFrom, labelTo, urlFrom, urlTo int }

// isJSSpace is \s of a JavaScript regular expression.
func isJSSpace(r rune) bool { return jscompat.IsSpace(r) }

// markdownLinks are the matches of /\[([^\]\n]{1,200})\]\((https?:\/\/[^)\s]+)\)/g; the label's limit counts UTF-16
// code units, as the previous runtime's expression did.
func markdownLinks(text string) []mdLink {
	var out []mdLink
	for i := 0; i < len(text); {
		j := strings.IndexByte(text[i:], '[')
		if j < 0 {
			break
		}
		start := i + j
		if m, ok := markdownLinkAt(text, start); ok {
			out = append(out, m)
			i = m.to
			continue
		}
		i = start + 1
	}
	return out
}

func markdownLinkAt(text string, start int) (mdLink, bool) {
	units := 0
	k := start + 1
	for k < len(text) && text[k] != ']' && text[k] != '\n' {
		r, size := utf8.DecodeRuneInString(text[k:])
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
		k += size
	}
	if units < 1 || units > 200 || k+1 >= len(text) || text[k] != ']' || text[k+1] != '(' {
		return mdLink{}, false
	}
	m := mdLink{from: start, labelFrom: start + 1, labelTo: k, urlFrom: k + 2}
	p := k + 2
	switch {
	case strings.HasPrefix(text[p:], "https://"):
		p += 8
	case strings.HasPrefix(text[p:], "http://"):
		p += 7
	default:
		return mdLink{}, false
	}
	q := p
	for q < len(text) {
		r, size := utf8.DecodeRuneInString(text[q:])
		if r == ')' || isJSSpace(r) {
			break
		}
		q += size
	}
	if q == p || q >= len(text) || text[q] != ')' {
		return mdLink{}, false
	}
	m.urlTo, m.to = q, q+1
	return m, true
}

// bareURLs are the matches of /https?:\/\/[^\s<>()"'`\]]+/g.
func bareURLs(text string) [][2]int {
	var out [][2]int
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], "http")
		if j < 0 {
			break
		}
		start := i + j
		p := start + 4
		if p < len(text) && text[p] == 's' && strings.HasPrefix(text[p+1:], "://") {
			p += 4
		} else if strings.HasPrefix(text[p:], "://") {
			p += 3
		} else {
			i = start + 1
			continue
		}
		q := p
		for q < len(text) {
			r, size := utf8.DecodeRuneInString(text[q:])
			if isJSSpace(r) || strings.ContainsRune("<>()\"'`]", r) {
				break
			}
			q += size
		}
		if q == p {
			i = start + 1
			continue
		}
		out = append(out, [2]int{start, q})
		i = q
	}
	return out
}

func isKeyBoundary(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// issueKeys are the matches of (?<![\w/.-])((?:KEY|…)-\d{1,6})(?![\w-]) with the tracker's project keys.
func (c *Links) issueKeys(text string) []string {
	keys := make([]string, 0, len(c.IssueKeys))
	for _, k := range c.IssueKeys {
		k = strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, k)
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	var out []string
	for i := 0; i < len(text); i++ {
		if i > 0 {
			if b := text[i-1]; isKeyBoundary(b) || b == '/' || b == '.' || b == '-' {
				continue
			}
		}
		for _, k := range keys {
			p := i + len(k)
			if !strings.HasPrefix(text[i:], k) || p >= len(text) || text[p] != '-' {
				continue
			}
			q := p + 1
			for q < len(text) && text[q] >= '0' && text[q] <= '9' {
				q++
			}
			if q == p+1 || q-p-1 > 6 || q < len(text) && (isKeyBoundary(text[q]) || text[q] == '-') {
				continue
			}
			out = append(out, text[i:q])
			i = q - 1
			break
		}
	}
	return out
}

// Collector gathers the links of entries fed in order, so a transcript can be indexed a stretch at a time.
type Collector struct {
	Links *Links
	order []string
	byID  map[string]*Link
}

// toNumber is Number(v) of an entry's timestamp as Math.max reads it.
func toNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		t := jscompat.Trim(x)
		if t == "" {
			return 0
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	}
	return math.NaN()
}

// jsMax is Math.max of two numbers: NaN wins.
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Max(a, b)
}

// Add takes in the links of the assistant answers among entries.
func (c *Collector) Add(entries []*jsvalue.Object) {
	if c.byID == nil {
		c.byID = map[string]*Link{}
	}
	for _, e := range entries {
		if e.String("type") != "assistant-message" {
			continue
		}
		md, _ := e.Get("markdown")
		text, _ := md.(string)
		tsRaw, present := e.Get("timestamp")
		ts := toNumber(tsRaw)
		if !present {
			ts = math.NaN()
		}
		for _, f := range c.Links.linksOf(text) {
			l := c.byID[f.id]
			if l == nil {
				l = &Link{URL: f.url, Timestamp: 0}
				c.byID[f.id] = l
				c.order = append(c.order, f.id)
			}
			if f.label != nil {
				label := *f.label
				l.Label = &label
			}
			l.Timestamp = jsMax(ts, l.Timestamp)
			l.Count++
			l.Kind = f.kind
		}
	}
}

// List is the links, newest first.
func (c *Collector) List() []Link {
	out := make([]Link, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, *c.byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[j].Timestamp-out[i].Timestamp < 0 })
	return out
}
