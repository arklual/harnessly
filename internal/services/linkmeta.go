package services

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const (
	metaFresh      = 5 * time.Minute
	metaMissFresh  = time.Minute
	metaEntries    = 500
	maxHTML        = 400_000
	maxAvatar      = 200_000
	avatarFresh    = 24 * time.Hour
	serviceTimeout = 8 * time.Second
	projectsFresh  = time.Hour
	issueFields    = "idReadable,summary,resolved,customFields(name,value(name,login,fullName,avatarUrl)),parent(issues(idReadable,summary,customFields(name,value(name))))"
	avatarPrefix   = "/hub/api/rest/avatar/"
)

// LoginRe is a tracker login the avatar route serves.
var LoginRe = regexp.MustCompile(`^[\w.@-]{1,100}$`)

type metaEntry struct {
	meta any // nil: nothing found
	at   time.Time
}

type metaCall struct {
	done chan struct{}
	meta any
}

type avatar struct {
	source *string
	image  *Image
	at     time.Time
}

// Image is a picture fetched for the page.
type Image struct {
	Type string
	Body []byte
}

type linkMetaState struct {
	mu       sync.Mutex
	order    []string
	entries  map[string]metaEntry
	inflight map[string]*metaCall
	avatars  map[string]*avatar
	projects []any
	projAt   time.Time
}

func newLinkMetaState() *linkMetaState {
	return &linkMetaState{entries: map[string]metaEntry{}, inflight: map[string]*metaCall{}, avatars: map[string]*avatar{}}
}

// linkTarget is a link the server may read metadata for.
type linkTarget struct {
	kind    string // issue, mr, artifact
	key     string
	host    string
	project string
	iid     float64
	url     string
}

func (t linkTarget) cacheKey() string {
	switch t.kind {
	case "issue":
		return "issue:" + t.key
	case "mr":
		return "mr:" + t.host + "/" + t.project + "!" + jscompat.Number(t.iid)
	}
	return "artifact:" + t.url
}

var (
	issueURLRe   = regexp.MustCompile(`^/(?:youtrack/)?issue/([A-Z][A-Z0-9]{0,15}-\d{1,7})(?:/[^?#]*)?$`)
	mrURLRe      = regexp.MustCompile(`^/((?:[\w.-]{1,100}/){1,6}[\w.-]{1,100})/-/merge_requests/(\d{1,9})(?:/[\w-]*)?$`)
	artifactRe   = regexp.MustCompile(`^/(?:a|b)/([\w-]{1,160})/?$`)
	portalPathRe = regexp.MustCompile(`^/(?:a/([\w-]+)|b/([\w-]+))/?$`)
	boardKeyRe   = regexp.MustCompile(`^[\w-]{1,64}$`)
	bareKeyRe    = regexp.MustCompile(`^([A-Z][A-Z0-9]{0,9})-(\d{1,7})$`)
	projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)
)

func (s *Service) isTrackerHost(host string) bool {
	for _, h := range s.cfg.TrackerHosts {
		if h == host {
			return true
		}
	}
	return false
}

// docSlugOf is the slug of a published document or board, from its own address or from the platform's reader.
func (s *Service) docSlugOf(raw string) (string, bool) {
	u, ok := parseWebURL(raw, nil)
	if !ok || u.Scheme != "https" {
		return "", false
	}
	switch {
	case s.cfg.DocsHost != "" && u.Host == s.cfg.DocsHost:
		m := portalPathRe.FindStringSubmatch(u.Pathname())
		if m == nil {
			return "", false
		}
		if m[1] != "" {
			return m[1], true
		}
		if m[2] == "api" {
			return "", false
		}
		return m[2], true
	case s.cfg.ReaderHost != "" && u.Host == s.cfg.ReaderHost:
		rest, ok := strings.CutPrefix(u.Pathname(), s.cfg.ReaderPath)
		rest = strings.TrimSuffix(rest, "/")
		if ok && rest != "" && !strings.ContainsFunc(rest, func(r rune) bool { return !isWordRune(r) && r != '-' }) {
			return rest, true
		}
	}
	return "", false
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
}

// parseLinkTarget is the strict list of links the server reads metadata for; anything else is refused.
func (s *Service) parseLinkTarget(raw string) *linkTarget {
	u, ok := parseWebURL(raw, nil)
	if !ok || u.Scheme != "https" || u.Username != "" || u.Password != "" || u.Port != "" {
		return nil
	}
	path := u.Pathname()
	if s.isTrackerHost(u.Host) {
		if m := issueURLRe.FindStringSubmatch(path); m != nil {
			return &linkTarget{kind: "issue", key: m[1]}
		}
		return nil
	}
	if s.isGitlabHost(u.Host) {
		m := mrURLRe.FindStringSubmatch(path)
		if m == nil {
			return nil
		}
		for _, part := range strings.Split(m[1], "/") {
			if part == ".." || part == "." {
				return nil
			}
		}
		iid, _ := strconv.ParseFloat(m[2], 64)
		return &linkTarget{kind: "mr", host: u.Host, project: m[1], iid: iid}
	}
	href := u.Href()
	if s.cfg.ReaderHost != "" && u.Host == s.cfg.ReaderHost {
		if slug, ok := s.docSlugOf(href); ok {
			return &linkTarget{kind: "artifact", url: "https://" + s.cfg.DocsHost + "/a/" + slug}
		}
	}
	if s.cfg.DocsHost != "" && u.Host == s.cfg.DocsHost {
		m := artifactRe.FindStringSubmatch(path)
		if m == nil || m[1] == "api" {
			return nil
		}
		k := u.Param("k")
		if k != nil && !boardKeyRe.MatchString(*k) {
			return nil
		}
		target := "https://" + s.cfg.DocsHost + path
		if k != nil && *k != "" {
			target += "?k=" + *k
		}
		return &linkTarget{kind: "artifact", url: target}
	}
	return nil
}

// issueTargetFromKey: a bare task key, only for a project the tracker has.
func (s *Service) issueTargetFromKey(ctx context.Context, raw string) (*linkTarget, error) {
	m := bareKeyRe.FindStringSubmatch(jscompat.Trim(raw))
	if m == nil {
		return nil, nil
	}
	keys, err := s.ProjectKeys(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if ks, ok := k.(string); ok && ks == m[1] {
			return &linkTarget{kind: "issue", key: m[1] + "-" + m[2]}, nil
		}
	}
	return nil, nil
}

// fetchJSON is a tracker or GitLab read: redirects refused, null unless 2xx; a network failure or a body that is not
// JSON is an error.
func (s *Service) fetchJSON(ctx context.Context, target string, header http.Header) (any, error) {
	h := http.Header{"Accept": {"application/json"}}
	for k, v := range header {
		h[k] = v
	}
	resp, err := s.fetch(ctx, target, h, refuse, serviceTimeout, 0)
	if resp == nil {
		return nil, err
	}
	if !resp.ok() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, perr := jsvalue.Parse([]byte(decodeText(resp.body)))
	if perr != nil {
		return nil, errType
	}
	return v, nil
}

func (s *Service) trackerOrigin() string { return strings.TrimRight(s.cfg.TrackerURL, "/") }

func (s *Service) avatarPath(login any) string {
	return s.cfg.AvatarPath + "?login=" + files.EncodeURIComponent(str(login))
}

func (s *Service) rememberAvatar(login any, source any) {
	l, ok := login.(string)
	src, isString := source.(string)
	if !ok || !isString || src == "" {
		return
	}
	st := s.meta
	st.mu.Lock()
	defer st.mu.Unlock()
	if cur := st.avatars[l]; cur == nil || cur.source == nil || *cur.source != src {
		st.avatars[l] = &avatar{source: &src}
	}
}

// epicOf: an idea above an epic is strategy, not context for a task row, so only an epic is shown as the parent.
var ideaRe = regexp.MustCompile(`(?i)^(idea|идея)$`)

func epicOf(parent any) (any, error) {
	if !truthy(parent) {
		return nil, nil
	}
	var typ any = ""
	if fields := opt(parent, "customFields"); !nullish(fields) {
		f, err := findByName(fields, "Type")
		if err != nil {
			return nil, err
		}
		if v := opt(opt(f, "value"), "name"); !nullish(v) {
			typ = v
		}
	}
	if ideaRe.MatchString(str(typ)) {
		return nil, nil
	}
	return obj("key", opt(parent, "idReadable"), "title", opt(parent, "summary")), nil
}

func (s *Service) fetchIssue(ctx context.Context, key string) (any, error) {
	token := s.trackerToken()
	if token == "" {
		return nil, nil
	}
	issue, err := s.fetchJSON(ctx, s.trackerOrigin()+"/api/issues/"+files.EncodeURIComponent(key)+"?fields="+issueFields,
		http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		return nil, err
	}
	if !truthy(issue) {
		return nil, nil
	}
	fields := opt(issue, "customFields")
	field := func(name string) (any, error) {
		f, err := findByName(fields, name)
		if err != nil {
			return nil, err
		}
		return orNull(opt(f, "value")), nil
	}
	assignee, err := field("Assignee")
	if err != nil {
		return nil, err
	}
	if login := opt(assignee, "login"); truthy(login) {
		s.rememberAvatar(login, opt(assignee, "avatarUrl"))
	}
	state, err := field("State")
	if err != nil {
		return nil, err
	}
	var person any
	if truthy(assignee) {
		name := opt(assignee, "fullName")
		for _, alt := range []any{opt(assignee, "name"), opt(assignee, "login"), ""} {
			if truthy(name) {
				break
			}
			name = alt
		}
		var av any
		if login := opt(assignee, "login"); truthy(login) {
			av = s.avatarPath(login)
		}
		person = obj("name", name, "avatar", av)
	}
	typ, err := field("Type")
	if err != nil {
		return nil, err
	}
	parent, err := epicOf(opt(opt(opt(issue, "parent"), "issues"), "0"))
	if err != nil {
		return nil, err
	}
	return obj("kind", "issue", "key", opt(issue, "idReadable"), "title", opt(issue, "summary"), "state", orNull(opt(state, "name")),
		"resolved", !nullish(opt(issue, "resolved")), "assignee", person, "type", orNull(opt(typ, "name")), "parent", parent), nil
}

var mrStates = map[string]bool{"opened": true, "merged": true, "closed": true, "locked": true}

func (s *Service) fetchMr(ctx context.Context, t *linkTarget) (any, error) {
	h := http.Header{}
	if token := s.gitlabToken(t.host); token != "" {
		h.Set("Private-Token", token)
	}
	mr, err := s.fetchJSON(ctx, s.gitlabBase(t.host)+"/projects/"+files.EncodeURIComponent(t.project)+"/merge_requests/"+jscompat.Number(t.iid), h)
	if err != nil {
		return nil, err
	}
	if !truthy(mr) {
		return nil, nil
	}
	state := "opened"
	if st, ok := opt(mr, "state").(string); ok && mrStates[st] {
		state = st
	}
	return obj("kind", "mr", "iid", opt(mr, "iid"), "title", opt(mr, "title"), "state", state, "project", t.project,
		"draft", truthy(opt(mr, "draft"))), nil
}

var htmlType = regexp.MustCompile(`text/html`)

func (s *Service) fetchArtifact(ctx context.Context, target string) (any, error) {
	resp, err := s.fetch(ctx, target, http.Header{"Accept": {"text/html"}}, refuse, serviceTimeout, 0)
	if resp == nil {
		return nil, err
	}
	if !resp.ok() || !htmlType.MatchString(resp.header.Get("Content-Type")) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	title, description := parseArtifactHTML(decodeText(resp.body))
	if title == nil {
		return nil, nil
	}
	return obj("kind", "artifact", "title", *title, "description", strPtr(description)), nil
}

func strPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// ci is a case-insensitive pattern for ASCII text the way a JavaScript /i regexp matches it (no Unicode folding).
func ci(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			b.WriteString("[" + string(r) + string(r-32) + "]")
		} else {
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}

var (
	titleRe       = regexp.MustCompile(`<` + ci("title") + `[^>]*>([\s\S]*?)</` + ci("title") + `>`)
	descMetaRe    = regexp.MustCompile(`<` + ci("meta") + `[^>]+(?:` + ci("name") + `|` + ci("property") + `)=["'](?:` + ci("description") + `|` + ci("og:description") + `)["'][^>]*>`)
	contentAttrRe = regexp.MustCompile(ci("content") + `=["']([^"']*)["']`)
	entityRe      = regexp.MustCompile(`&(#[xX][0-9a-fA-F]+|#[0-9]+|[a-zA-Z]+);`)
	spaceRunRe    = regexp.MustCompile(jscompat.SpaceClass + `+`)
)

var entities = map[string]string{"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'", "nbsp": " ", "laquo": "«",
	"raquo": "»", "mdash": "—", "ndash": "–"}

func decodeEntities(v string) string {
	return entityRe.ReplaceAllStringFunc(v, func(match string) string {
		code := match[1 : len(match)-1]
		if code[0] == '#' {
			var point float64
			if code[1] == 'x' || code[1] == 'X' {
				point = jscompat.ParseInt(code[2:], 16)
			} else {
				point = jscompat.ParseInt(code[1:], 10)
			}
			if !math.IsNaN(point) && !math.IsInf(point, 0) && point > 0 && point < 0x110000 {
				return string(rune(int(point)))
			}
			return match
		}
		if e, ok := entities[strings.ToLower(code)]; ok {
			return e
		}
		return match
	})
}

func cleanText(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	t := jscompat.Trim(spaceRunRe.ReplaceAllString(decodeEntities(*v), " "))
	if t == "" {
		return nil
	}
	t = jscompat.Slice(t, 300)
	return &t
}

// parseArtifactHTML is the title and description of a page, read from its head.
func parseArtifactHTML(html string) (*string, *string) {
	head := jscompat.Slice(html, maxHTML)
	var title, description *string
	if m := titleRe.FindStringSubmatch(head); m != nil {
		title = &m[1]
	}
	if meta := descMetaRe.FindString(head); meta != "" {
		if m := contentAttrRe.FindStringSubmatch(meta); m != nil {
			description = &m[1]
		}
	}
	return cleanText(title), cleanText(description)
}

func (s *Service) loadMeta(ctx context.Context, t *linkTarget) (any, error) {
	switch t.kind {
	case "issue":
		return s.fetchIssue(ctx, t.key)
	case "mr":
		return s.fetchMr(ctx, t)
	}
	return s.fetchArtifact(ctx, t.url)
}

// linkMeta is a link's metadata: kept 5 minutes (a miss 1 minute), one request per link in flight, the last good
// answer kept through a failure.
func (s *Service) linkMeta(ctx context.Context, t *linkTarget) any {
	key := t.cacheKey()
	st := s.meta
	st.mu.Lock()
	cached, has := st.entries[key]
	if has {
		fresh := metaMissFresh
		if cached.meta != nil {
			fresh = metaFresh
		}
		if time.Since(cached.at) < fresh {
			st.mu.Unlock()
			return cached.meta
		}
	}
	if call, ok := st.inflight[key]; ok {
		st.mu.Unlock()
		<-call.done
		return call.meta
	}
	call := &metaCall{done: make(chan struct{})}
	st.inflight[key] = call
	st.mu.Unlock()

	meta, err := s.loadMeta(context.WithoutCancel(ctx), t)
	st.mu.Lock()
	if err != nil {
		call.meta = cached.meta
	} else {
		if meta == nil {
			meta = cached.meta
		}
		call.meta = meta
		st.remember(key, metaEntry{meta: meta, at: time.Now()})
	}
	delete(st.inflight, key)
	st.mu.Unlock()
	close(call.done)
	return call.meta
}

func (st *linkMetaState) remember(key string, e metaEntry) {
	if _, ok := st.entries[key]; ok {
		for i, k := range st.order {
			if k == key {
				st.order = append(st.order[:i:i], st.order[i+1:]...)
				break
			}
		}
	}
	st.entries[key] = e
	st.order = append(st.order, key)
	if len(st.order) > metaEntries {
		delete(st.entries, st.order[0])
		st.order = st.order[1:]
	}
}

// ProjectKeys are the short names of the tracker's live projects, kept an hour; a failed read gives the last list.
func (s *Service) ProjectKeys(ctx context.Context) ([]any, error) {
	st := s.meta
	st.mu.Lock()
	if st.projects != nil && time.Since(st.projAt) < projectsFresh {
		keys := st.projects
		st.mu.Unlock()
		return keys, nil
	}
	st.mu.Unlock()
	var projects any
	if token := s.trackerToken(); token != "" {
		projects, _ = s.fetchJSON(ctx, s.trackerOrigin()+"/api/admin/projects?fields=shortName,archived&$top=200",
			http.Header{"Authorization": {"Bearer " + token}})
	}
	keys := []any{}
	if !nullish(projects) {
		list, ok := projects.([]any)
		if !ok {
			return nil, errType
		}
		for _, p := range list {
			if nullish(p) {
				return nil, errType
			}
			name := opt(p, "shortName")
			if !truthy(opt(p, "archived")) && projectKeyRe.MatchString(str(name)) {
				keys = append(keys, name)
			}
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(keys) > 0 {
		st.projects, st.projAt = keys, time.Now()
		return keys, nil
	}
	if st.projects != nil {
		return st.projects, nil
	}
	return keys, nil
}

func (s *Service) lookupAvatarSource(ctx context.Context, login, token string) (*string, error) {
	users, err := s.fetchJSON(ctx, s.trackerOrigin()+"/api/users?query="+files.EncodeURIComponent(login)+"&fields=login,avatarUrl&$top=10",
		http.Header{"Authorization": {"Bearer " + token}})
	if err != nil || nullish(users) {
		return nil, nil
	}
	list, ok := users.([]any)
	if !ok {
		return nil, errType
	}
	for _, u := range list {
		if nullish(u) {
			return nil, errType
		}
		if l, ok := opt(u, "login").(string); ok && l == login {
			src := opt(u, "avatarUrl")
			if nullish(src) {
				return nil, nil
			}
			v := str(src)
			return &v, nil
		}
	}
	return nil, nil
}

var avatarType = regexp.MustCompile(`^image/(png|jpeg|gif|webp|svg\+xml)`)

// TrackerAvatar is a tracker user's avatar, fetched with the server's token so the browser never needs it.
func (s *Service) TrackerAvatar(ctx context.Context, login string) (*Image, error) {
	st := s.meta
	st.mu.Lock()
	cached := st.avatars[login]
	var source *string
	if cached != nil {
		if cached.image != nil && time.Since(cached.at) < avatarFresh {
			st.mu.Unlock()
			return cached.image, nil
		}
		if cached.image == nil && !cached.at.IsZero() && time.Since(cached.at) < metaMissFresh {
			st.mu.Unlock()
			return nil, nil
		}
		source = cached.source
	}
	st.mu.Unlock()
	token := s.trackerToken()
	if token == "" {
		return nil, nil
	}
	if source == nil {
		var err error
		if source, err = s.lookupAvatarSource(ctx, login, token); err != nil {
			return nil, err
		}
	}
	var image *Image
	if source != nil && *source != "" {
		base, ok := parseWebURL(s.trackerOrigin(), nil)
		if !ok {
			return nil, errType
		}
		u, ok := parseWebURL(*source, base)
		if !ok {
			return nil, errType
		}
		if u.Origin() == base.Origin() && strings.HasPrefix(u.Pathname(), avatarPrefix) {
			resp, err := s.fetch(ctx, u.Href(), http.Header{"Authorization": {"Bearer " + token}}, refuse, serviceTimeout, maxAvatar)
			if resp != nil && resp.ok() && avatarType.MatchString(resp.header.Get("Content-Type")) {
				if err != nil {
					return nil, err
				}
				if len(resp.body) > 0 && !resp.tooBig {
					image = &Image{Type: resp.header.Get("Content-Type"), Body: resp.body}
				}
			}
		}
	}
	st.mu.Lock()
	st.avatars[login] = &avatar{source: source, image: image, at: time.Now()}
	st.mu.Unlock()
	return image, nil
}
