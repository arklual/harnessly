// Package panel is what an agent may ask of the right pane of its own tab and what the pane answers: the command
// parser, the cleaning of a page's answer, and the bridge that carries a command to the person's page over the sync
// socket and the answer back to the agent that waits for it.
package panel

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

const (
	MaxArg     = 512
	MaxMessage = 400
)

// TabKinds are the kinds of tab the right pane can hold.
var TabKinds = []string{"overview", "tools", "browser", "terminal", "files", "disk", "changes", "artifacts", "gitlab",
	"subagents", "economics", "plan", "peers", "design", "side-chat", "gallery", "task"}

// IsTabKind says whether v names a kind of TabKinds.
func IsTabKind(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, k := range TabKinds {
		if k == s {
			return true
		}
	}
	return false
}

// Targets are the deployment's addresses a command may point a tab at.
type Targets struct {
	// StandDomain is the domain of the feature stands (".preview.example.org"); "" opens none.
	StandDomain string
	// PortalHost publishes documents (/a/<slug>) and boards (/b/<slug>); ReaderHost is the platform, whose reader
	// shows a document at ReaderPath<slug>.
	PortalHost, ReaderHost string
	ReaderPath             string
	// GitLabHosts are the GitLab servers whose merge requests and commits a tab shows; the first is named in hints.
	GitLabHosts []string
	// TaskKey matches a key of the tracker; TaskExamples are named in the hint.
	TaskKey      *regexp.Regexp
	TaskExamples string
}

func (t Targets) readerPath() string {
	if t.ReaderPath == "" {
		return "/agents/docs/"
	}
	return t.ReaderPath
}

// Command is a command the pane knows.
type Command struct {
	Kind   string // open, show, hide, status
	Tab    string
	Target string // "" for none
}

// JSON is the command as the sync socket carries it.
func (c Command) JSON() *jsvalue.Object {
	o := jsvalue.NewObject("kind", c.Kind)
	if c.Kind == "open" {
		o.Set("tab", c.Tab)
		if c.Target != "" {
			o.Set("target", c.Target)
		}
	}
	return o
}

type targetKind int

const (
	none targetKind = iota
	port
	path
	doc
	gitlab
	task
)

var targetOf = map[string]targetKind{"browser": port, "files": path, "changes": path, "artifacts": doc, "gitlab": gitlab, "task": task}

func clean(s string) bool { return tmux.StripControlChars(s) == s }

var portInput = regexp.MustCompile(`(?i)^(?:(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)?:)?(\d{1,5})(/.*)?$`)

func (t Targets) standURL(text string) string {
	if t.StandDomain == "" {
		return ""
	}
	u, err := url.Parse(jsvalue.TrimJS(text))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !t.isStandHost(host) {
		return ""
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	if p == "/" {
		p = ""
	}
	return "https://" + host + p
}

var standLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// isStandHost: one label under the stand domain; the panel's own slots (ai-…) and the sign-in host (auth) are not stands.
func (t Targets) isStandHost(host string) bool {
	if !strings.HasSuffix(host, t.StandDomain) {
		return false
	}
	label := strings.TrimSuffix(host, t.StandDomain)
	return standLabel.MatchString(label) && !strings.HasPrefix(label, "ai-") && label != "auth"
}

func (t Targets) portTarget(text string) string {
	if !clean(text) {
		return ""
	}
	if s := t.standURL(text); s != "" {
		return s
	}
	m := portInput.FindStringSubmatch(jsvalue.TrimJS(text))
	if m == nil {
		return ""
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 || n > 65535 {
		return ""
	}
	p := m[2]
	if p == "" {
		p = "/"
	}
	decoded, ok := decodeURI(p)
	if !ok {
		return ""
	}
	p = encodeURI(decoded)
	if p == "/" {
		p = ""
	}
	return "localhost:" + strconv.Itoa(n) + p
}

func pathTarget(text string) string {
	c := jsvalue.TrimJS(text)
	if c == "" || !clean(c) {
		return ""
	}
	for _, part := range strings.Split(c, "/") {
		if part == ".." {
			return ""
		}
	}
	return c
}

func urlTarget(text string) string {
	c := jsvalue.TrimJS(text)
	if c == "" || !clean(c) {
		return ""
	}
	return c
}

var (
	slugRe     = regexp.MustCompile(`^[\w-]+$`)
	boardKeyRe = regexp.MustCompile(`^[\w-]{1,64}$`)
	portalPath = regexp.MustCompile(`^/(a|b)/([\w-]+)/?$`)
)

func httpsURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil
	}
	return u
}

func (t Targets) docSlug(raw string) string {
	u := httpsURL(raw)
	if u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case t.PortalHost != "" && host == t.PortalHost:
		m := portalPath.FindStringSubmatch(u.Path)
		if m == nil || (m[1] == "b" && m[2] == "api") {
			return ""
		}
		return m[2]
	case t.ReaderHost != "" && host == t.ReaderHost:
		rest, ok := strings.CutPrefix(u.Path, t.readerPath())
		rest = strings.TrimSuffix(rest, "/")
		if ok && slugRe.MatchString(rest) {
			return rest
		}
	}
	return ""
}

func (t Targets) boardFrameURL(raw string) string {
	u := httpsURL(raw)
	if u == nil || t.PortalHost == "" || strings.ToLower(u.Hostname()) != t.PortalHost {
		return ""
	}
	m := portalPath.FindStringSubmatch(u.Path)
	if m == nil || m[1] != "b" || m[2] == "api" {
		return ""
	}
	q := u.Query()
	key := q.Get("k")
	if !boardKeyRe.MatchString(key) {
		return ""
	}
	out := "https://" + t.PortalHost + "/b/" + m[2] + "?k=" + key
	if at := q.Get("at"); at != "" && boardKeyRe.MatchString(at) {
		out += "&at=" + at
	}
	return out
}

func (t Targets) isPaneDocTarget(s string) bool {
	rest, ok := strings.CutPrefix(s, t.readerPath())
	return (ok && slugRe.MatchString(rest)) || (t.boardFrameURL(s) != "" && t.boardFrameURL(s) == s)
}

func (t Targets) docTarget(text string) string {
	u := urlTarget(text)
	if u == "" {
		return ""
	}
	if t.isPaneDocTarget(u) {
		return u
	}
	if b := t.boardFrameURL(u); b != "" {
		return b
	}
	if slug := t.docSlug(u); slug != "" {
		return t.readerPath() + slug
	}
	return ""
}

var (
	mrPath     = regexp.MustCompile(`^/(.+?)/-/merge_requests/(\d+)`)
	commitPath = regexp.MustCompile(`^/(.+?)/-/commit/([0-9a-f]{7,40})(?:/|$)`)
)

func (t Targets) gitlabTarget(text string) string {
	raw := urlTarget(text)
	u := httpsURL(raw)
	if u == nil || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	known := false
	for _, h := range t.GitLabHosts {
		known = known || h == host
	}
	if !known || (!mrPath.MatchString(u.Path) && !commitPath.MatchString(u.Path)) {
		return ""
	}
	return raw
}

func (t Targets) taskTarget(text string) string {
	key := jsvalue.TrimJS(text)
	if t.TaskKey != nil && t.TaskKey.MatchString(key) {
		return key
	}
	return ""
}

func (t Targets) hint(tab string, k targetKind) string {
	switch k {
	case port:
		return "open browser: открывается порт этой машины (4400, localhost:4400/путь) или фича-стенд https://<slug>" + t.StandDomain + "; другие внешние адреса агент не открывает"
	case doc:
		return "open artifacts: нужна ссылка на документ или доску " + t.PortalHost + " (https://" + t.PortalHost + "/a/… или /b/…)"
	case gitlab:
		first := ""
		if len(t.GitLabHosts) > 0 {
			first = t.GitLabHosts[0]
		}
		return "open gitlab: нужна ссылка на merge request или коммит GitLab (https://" + first + "/…)"
	case task:
		return "open task: нужен ключ задачи трекера (" + t.TaskExamples + ")"
	}
	return "open " + tab + ": нужен путь без «..» и управляющих символов"
}

func (t Targets) parseOpen(tab, target any) (Command, string) {
	if !IsTabKind(tab) {
		return Command{}, "open: неизвестная вкладка, доступны " + strings.Join(TabKinds, ", ")
	}
	name := tab.(string)
	if s, isStr := target.(string); target == nil || (isStr && jsvalue.TrimJS(s) == "") {
		return Command{Kind: "open", Tab: name}, ""
	}
	k := targetOf[name]
	if k == none {
		return Command{}, "open " + name + ": этой вкладке цель не нужна"
	}
	text, isStr := target.(string)
	if !isStr || jscompat.Len(text) > MaxArg {
		return Command{}, "open " + name + ": цель слишком длинная или не строка"
	}
	var c string
	switch k {
	case port:
		c = t.portTarget(text)
	case path:
		c = pathTarget(text)
	case doc:
		c = t.docTarget(text)
	case gitlab:
		c = t.gitlabTarget(text)
	case task:
		c = t.taskTarget(text)
	}
	if c == "" {
		return Command{}, t.hint(name, k)
	}
	return Command{Kind: "open", Tab: name, Target: c}, ""
}

// Parse reduces a request body to a command the pane knows; the error is the text the agent reads. Unknown keys,
// kinds and values are refused, never passed on.
func (t Targets) Parse(input any) (Command, string) {
	o, ok := input.(*jsvalue.Object)
	if !ok {
		return Command{}, "Нужен объект команды"
	}
	kind, _ := o.Get("kind")
	switch kind {
	case "open":
		tab, _ := o.Get("tab")
		target, _ := o.Get("target")
		return t.parseOpen(tab, target)
	case "show", "hide", "status":
		return Command{Kind: kind.(string)}, ""
	}
	return Command{}, "Неизвестная команда: open, show, hide, status"
}

// Result is a page's answer to a command, shape-checked, cleaned and clipped: the browser is not trusted.
type Result struct {
	OK      bool
	Message string
	State   *jsvalue.Object // {visible, open, active}; nil when the page sent none
}

// CleanState keeps only a well-formed pane state.
func CleanState(v any) *jsvalue.Object {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	var visible any
	if b, isBool := func() (bool, bool) { x, _ := o.Get("visible"); b, ok := x.(bool); return b, ok }(); isBool {
		visible = b
	}
	open := []any{}
	for _, k := range o.Array("open") {
		if IsTabKind(k) {
			open = append(open, k)
		}
	}
	var active any
	if a, _ := o.Get("active"); IsTabKind(a) {
		active = a
	}
	return jsvalue.NewObject("visible", visible, "open", open, "active", active)
}

// ParseResult reads a page's answer; nil when the body is not one.
func ParseResult(input any) *Result {
	o, ok := input.(*jsvalue.Object)
	if !ok {
		return nil
	}
	okv, _ := o.Get("ok")
	b, isBool := okv.(bool)
	mv, _ := o.Get("message")
	m, isStr := mv.(string)
	if !isBool || !isStr {
		return nil
	}
	st, _ := o.Get("state")
	return &Result{OK: b, Message: cleanMessage(m), State: CleanState(st)}
}

var spaces = regexp.MustCompile(jscompat.SpaceClass + `+`)

func cleanMessage(s string) string {
	return jscompat.Slice(jsvalue.TrimJS(spaces.ReplaceAllString(tmux.StripControlChars(s), " ")), MaxMessage)
}

// RateDecision is a sliding window over the times of the commands let through; a refused one is not counted.
func RateDecision(stamps []int64, now int64, limit int, windowMs int64) (bool, []int64) {
	var recent []int64
	for _, s := range stamps {
		if now-s < windowMs {
			recent = append(recent, s)
		}
	}
	if len(recent) >= limit {
		return false, recent
	}
	return true, append(recent, now)
}
