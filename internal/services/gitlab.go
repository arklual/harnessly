package services

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"harnessly/internal/files"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

const (
	gitlabTimeout = 6 * time.Second
	// apiFresh is how long a GitLab answer is reused: the page polls every 15 s at most, so two tabs or two devices
	// asking at once share one request and nothing is older than a poll.
	apiFresh     = 5 * time.Second
	viewFiles    = 100
	viewDiffMax  = 600_000
	descMax      = 20_000
	shortSHA     = 8
	maxMentioned = 20
	maxBranches  = 8
)

var knownPipeline = map[string]bool{"running": true, "pending": true, "success": true, "failed": true, "canceled": true,
	"skipped": true, "manual": true, "created": true}

type apiEntry struct {
	done chan struct{}
	v    any
	err  error
	at   time.Time
}

type gitlabState struct {
	mu        sync.Mutex
	canonical map[string]any
	answers   map[string]*apiEntry
}

func newGitlabState() *gitlabState {
	return &gitlabState{canonical: map[string]any{}, answers: map[string]*apiEntry{}}
}

type access struct {
	host  string
	token string
}

func (s *Service) gitlabBase(host string) string { return "https://" + host + "/api/v4" }

// api is one GitLab read: null for any failure, the error only where the previous runtime threw (a body that is not
// JSON). A renamed project answers its old path with a redirect to /projects/<id>, followed within the API.
func (s *Service) api(ctx context.Context, a *access, pathname string) (any, error) {
	target := s.gitlabBase(a.host) + pathname
	key := a.token + "\x00" + target
	g := s.gitlab
	g.mu.Lock()
	for k, e := range g.answers {
		if e.err == nil && !e.at.IsZero() && time.Since(e.at) > apiFresh {
			delete(g.answers, k)
		}
	}
	if e, ok := g.answers[key]; ok {
		g.mu.Unlock()
		<-e.done
		return e.v, e.err
	}
	e := &apiEntry{done: make(chan struct{})}
	g.answers[key] = e
	g.mu.Unlock()
	v, err := s.apiRead(ctx, a, target)
	g.mu.Lock()
	e.v, e.err = v, err
	if err != nil {
		delete(g.answers, key)
	} else {
		e.at = time.Now()
	}
	g.mu.Unlock()
	close(e.done)
	return v, err
}

func (s *Service) apiRead(ctx context.Context, a *access, target string) (any, error) {
	resp, err := s.fetch(context.WithoutCancel(ctx), target, map[string][]string{"Private-Token": {a.token}, "Accept": {"application/json"}}, follow, gitlabTimeout, 0)
	if resp == nil || !resp.ok() || !strings.HasPrefix(resp.url, s.gitlabBase(a.host)+"/") {
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

// canonicalProject is the project's current path, kept for good once GitLab named it.
func (s *Service) canonicalProject(ctx context.Context, a *access, project any) (any, error) {
	key := a.host + "/" + str(project)
	s.gitlab.mu.Lock()
	v, ok := s.gitlab.canonical[key]
	s.gitlab.mu.Unlock()
	if !ok {
		found, err := s.api(ctx, a, "/projects/"+files.EncodeURIComponent(str(project)))
		if err != nil {
			return nil, err
		}
		if !truthy(found) {
			return nil, nil
		}
		v = opt(found, "path_with_namespace")
		s.gitlab.mu.Lock()
		if prev, ok := s.gitlab.canonical[key]; ok {
			v = prev
		} else {
			s.gitlab.canonical[key] = v
		}
		s.gitlab.mu.Unlock()
	}
	return orNull(v), nil
}

var remoteRe = regexp.MustCompile(`^(?:[a-z+]+://)?(?:[^@/]+@)?([\w.-]+)(?::\d+)?[:/](.+?)(?:\.git)?/?$`)

// gitlabProjectOf is the GitLab and the group/sub/project of a remote in any form, credentials dropped.
func (s *Service) gitlabProjectOf(remote string) (string, string, bool) {
	m := remoteRe.FindStringSubmatch(jscompat.Trim(remote))
	if m == nil || !s.isGitlabHost(m[1]) {
		return "", "", false
	}
	project := strings.TrimLeft(m[2], "/")
	return m[1], project, project != ""
}

func pipelineOf(raw any) any {
	if !truthy(opt(raw, "web_url")) {
		return nil
	}
	status := "other"
	if st, ok := opt(raw, "status").(string); ok && knownPipeline[st] {
		status = st
	}
	return obj("status", status, "url", opt(raw, "web_url"))
}

func toMr(host string, project any, mr any) *jsvalue.Object {
	return obj("host", host, "project", project, "iid", opt(mr, "iid"), "title", opt(mr, "title"), "state", opt(mr, "state"),
		"draft", truthy(opt(mr, "draft")), "url", opt(mr, "web_url"), "pipeline", pipelineOf(opt(mr, "head_pipeline")))
}

// mrRef is a merge request link of a known GitLab.
type mrRef struct {
	host    string
	project string
	iid     float64
	sha     string // a commit link instead
}

var (
	mrPathRe     = regexp.MustCompile(`^/(.+?)/-/merge_requests/(\d+)`)
	commitPathRe = regexp.MustCompile(`^/(.+?)/-/commit/([0-9a-f]{7,40})(?:/|$)`)
)

func (s *Service) gitlabURL(raw string) *webURL {
	u, ok := parseWebURL(raw, nil)
	if !ok || u.Scheme != "https" || u.Username != "" || u.Password != "" || u.Port != "" || !s.isGitlabHost(u.Host) {
		return nil
	}
	return u
}

func (s *Service) mrRefOf(raw string) *mrRef {
	u := s.gitlabURL(raw)
	if u == nil {
		return nil
	}
	m := mrPathRe.FindStringSubmatch(u.Pathname())
	if m == nil {
		return nil
	}
	iid, _ := strconv.ParseFloat(m[2], 64)
	return &mrRef{host: u.Host, project: m[1], iid: iid}
}

// gitlabRefOf is a merge request or a commit of a known GitLab.
func (s *Service) gitlabRefOf(raw string) *mrRef {
	if r := s.mrRefOf(raw); r != nil {
		return r
	}
	u := s.gitlabURL(raw)
	if u == nil {
		return nil
	}
	m := commitPathRe.FindStringSubmatch(u.Pathname())
	if m == nil {
		return nil
	}
	return &mrRef{host: u.Host, project: m[1], sha: m[2]}
}

// IsGitLabRef: the view route serves this link.
func (s *Service) IsGitLabRef(raw string) bool { return s.gitlabRefOf(raw) != nil }

// firstErr keeps the first error of parallel work.
type firstErr struct {
	mu  sync.Mutex
	err error
}

func (f *firstErr) set(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	if f.err == nil {
		f.err = err
	}
	f.mu.Unlock()
}

func (f *firstErr) get() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

type orderedPush struct {
	order []int
	v     *jsvalue.Object
	at    string
}

type orderedMr struct {
	order []int
	key   string
	v     *jsvalue.Object
}

func less(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// SessionGitLab is what a session sent to GitLab: its branches with their last pipeline, and the merge requests with
// theirs. since is the session's start (ms), nil without a transcript; a commit of the session is what makes a
// repository count, so without one no git or GitLab work is needed for the repositories.
func (s *Service) SessionGitLab(ctx context.Context, paths []string, since *float64, mentioned []string) (*jsvalue.Object, error) {
	var tokens sync.Map
	accessFor := func(host string) *access {
		once, _ := tokens.LoadOrStore(host, sync.OnceValue(func() *access {
			if t := s.gitlabToken(host); t != "" {
				return &access{host: host, token: t}
			}
			return nil
		}))
		return once.(func() *access)()
	}
	var errs firstErr
	var mu sync.Mutex
	var pushes []orderedPush
	var mrs []orderedMr
	var wg sync.WaitGroup

	if since != nil {
		roots := repoRoots(ctx, paths)
		if len(roots) > 0 && *since > maxDateMs {
			return nil, errType // new Date(since).toISOString() threw
		}
		for ri, root := range roots {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var branch, remote string
				var branchOK, made bool
				var inner sync.WaitGroup
				inner.Add(3)
				go func() {
					defer inner.Done()
					out, err := git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
					branch, branchOK = jscompat.Trim(out), err == nil
				}()
				go func() { defer inner.Done(); made = madeCommits(ctx, root, *since) }()
				go func() { defer inner.Done(); remote, _ = git(ctx, root, "remote", "get-url", "origin") }()
				inner.Wait()
				if !made {
					return
				}
				host, path, ok := s.gitlabProjectOf(remote)
				if !ok {
					return
				}
				a := accessFor(host)
				if a == nil {
					return
				}
				project, err := s.canonicalProject(ctx, a, path)
				if err != nil {
					errs.set(err)
					return
				}
				if !truthy(project) {
					return
				}
				var current *string
				if branchOK && branch != "" {
					current = &branch
				}
				id := files.EncodeURIComponent(str(project))
				var bw sync.WaitGroup
				for bi, name := range s.sessionBranches(ctx, root, current, *since) {
					bw.Add(1)
					go func() {
						defer bw.Done()
						p, list, err := s.branchPush(ctx, a, root, id, project, name, ri, bi)
						if err != nil {
							errs.set(err)
							return
						}
						mu.Lock()
						defer mu.Unlock()
						if p != nil {
							pushes = append(pushes, *p)
						}
						mrs = append(mrs, list...)
					}()
				}
				bw.Wait()
			}()
		}
	}

	type mentionedMr struct {
		key string
		v   *jsvalue.Object
	}
	var refs []*mrRef
	for _, m := range mentioned {
		if r := s.mrRefOf(m); r != nil {
			if refs = append(refs, r); len(refs) == maxMentioned {
				break
			}
		}
	}
	found := make([]*mentionedMr, len(refs))
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := accessFor(ref.host)
			if a == nil {
				return
			}
			project, err := s.canonicalProject(ctx, a, ref.project)
			if err != nil {
				errs.set(err)
				return
			}
			if nullish(project) {
				project = ref.project
			}
			iid := jscompat.Number(ref.iid)
			mr, err := s.api(ctx, a, "/projects/"+files.EncodeURIComponent(str(project))+"/merge_requests/"+iid)
			if err != nil {
				errs.set(err)
				return
			}
			if truthy(mr) {
				found[i] = &mentionedMr{key: ref.host + "/" + str(project) + "!" + iid, v: toMr(ref.host, project, mr)}
			}
		}()
	}
	wg.Wait()
	if err := errs.get(); err != nil {
		return nil, err
	}

	sort.Slice(pushes, func(i, j int) bool { return less(pushes[i].order, pushes[j].order) })
	sort.Slice(mrs, func(i, j int) bool { return less(mrs[i].order, mrs[j].order) })
	type entry struct {
		key string
		v   *jsvalue.Object
	}
	var list []entry
	index := map[string]int{}
	for _, m := range mrs {
		if i, ok := index[m.key]; ok {
			list[i].v = m.v
			continue
		}
		index[m.key] = len(list)
		list = append(list, entry{m.key, m.v})
	}
	fromRepos := len(list)
	for _, f := range found {
		if f == nil {
			continue
		}
		if i, ok := index[f.key]; ok {
			if i >= fromRepos {
				list[i].v = f.v
			}
			continue
		}
		index[f.key] = len(list)
		list = append(list, entry{f.key, f.v})
	}
	coll := collate.New(language.AmericanEnglish)
	sort.SliceStable(pushes, func(i, j int) bool { return coll.CompareString(pushes[j].at, pushes[i].at) < 0 })
	sort.SliceStable(list, func(i, j int) bool {
		a, b := iidNumber(list[i].v), iidNumber(list[j].v)
		return b-a < 0
	})
	pushOut := make([]any, len(pushes))
	for i, p := range pushes {
		pushOut[i] = p.v
	}
	mrOut := make([]any, len(list))
	for i, e := range list {
		mrOut[i] = e.v
	}
	return obj("pushes", pushOut, "mrs", mrOut), nil
}

func iidNumber(o *jsvalue.Object) float64 {
	if v, ok := o.Get("iid"); ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return math.NaN()
}

// sessionBranches are the local branches that got commits during the session, the checked-out one first.
func (s *Service) sessionBranches(ctx context.Context, root string, current *string, since float64) []string {
	refs, _ := git(ctx, root, "for-each-ref", "--format=%(committerdate:unix) %(refname:short)", "refs/heads")
	cutoff := since/1000 - 3600
	var names []string
	if current != nil {
		names = append(names, *current)
	}
	for _, line := range strings.Split(refs, "\n") {
		parts := strings.Split(jscompat.Trim(line), " ")
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		if at := jsNumber(parts[0]); at >= cutoff && !contains(names, parts[1]) {
			names = append(names, parts[1])
		}
	}
	if len(names) > maxBranches {
		names = names[:maxBranches]
	}
	return names
}

// jsNumber is Number(s) for the decimal text git prints; NaN for anything else.
func jsNumber(s string) float64 {
	s = jscompat.Trim(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || strings.ContainsAny(s, "_xXpP") {
		return math.NaN()
	}
	return f
}

func (s *Service) branchPush(ctx context.Context, a *access, root, id string, project any, name string, ri, bi int) (*orderedPush, []orderedMr, error) {
	ref := files.EncodeURIComponent(name)
	var branch, pipelines, branchMrs any
	var errs firstErr
	var w sync.WaitGroup
	w.Add(3)
	go func() {
		defer w.Done()
		v, err := s.api(ctx, a, "/projects/"+id+"/repository/branches/"+ref)
		branch = v
		errs.set(err)
	}()
	go func() {
		defer w.Done()
		v, err := s.api(ctx, a, "/projects/"+id+"/pipelines?ref="+ref+"&per_page=1")
		pipelines = v
		errs.set(err)
	}()
	go func() {
		defer w.Done()
		v, err := s.api(ctx, a, "/projects/"+id+"/merge_requests?source_branch="+ref+"&state=all&per_page=3&order_by=updated_at")
		branchMrs = v
		errs.set(err)
	}()
	w.Wait()
	if err := errs.get(); err != nil {
		return nil, nil, err
	}
	var commit any = undefined
	remoteSha := undefined
	if !nullish(branch) {
		commit = opt(branch, "commit")
		var err error
		if remoteSha, err = prop(commit, "id"); err != nil {
			return nil, nil, err
		}
	}
	var out string
	if truthy(remoteSha) {
		out, _ = git(ctx, root, "rev-list", "--count", str(remoteSha)+".."+name)
	} else {
		out, _ = git(ctx, root, "rev-list", "--count", name, "--not", "--remotes")
	}
	unpushed := jsNumber(out)
	if math.IsNaN(unpushed) || unpushed == 0 {
		unpushed = 0
	}
	if !truthy(remoteSha) && unpushed == 0 {
		return nil, nil, nil
	}
	host := a.host
	push := obj("host", host, "project", project, "branch", name, "commitTitle", orNull(opt(commit, "title")),
		"pushedAt", orNull(opt(commit, "committed_date")), "unpushed", unpushed, "pipeline", pipelineOf(opt(pipelines, "0")))
	at := ""
	if v, ok := push.Get("pushedAt"); ok && v != nil {
		at = str(v)
	}
	p := &orderedPush{order: []int{ri, bi}, v: push, at: at}
	if nullish(branchMrs) {
		return p, nil, nil
	}
	list, isList := branchMrs.([]any)
	if !isList {
		return nil, nil, errType
	}
	found := make([]*orderedMr, len(list))
	w = sync.WaitGroup{}
	for mi, mr := range list {
		iid, err := prop(mr, "iid")
		if err != nil {
			return nil, nil, err
		}
		w.Add(1)
		go func() {
			defer w.Done()
			full, err := s.api(ctx, a, "/projects/"+id+"/merge_requests/"+str(iid))
			if err != nil {
				errs.set(err)
				return
			}
			src := mr
			if !nullish(full) {
				src = full
			}
			found[mi] = &orderedMr{order: []int{ri, bi, mi}, key: host + "/" + str(project) + "!" + str(iid), v: toMr(host, project, src)}
		}()
	}
	w.Wait()
	if err := errs.get(); err != nil {
		return nil, nil, err
	}
	var mrs []orderedMr
	for _, f := range found {
		mrs = append(mrs, *f)
	}
	return p, mrs, nil
}

// unifiedDiffOf puts back the file headers the API leaves out of its per-file hunks, so the view reads git's form.
func unifiedDiffOf(list any) (string, error) {
	files, ok := list.([]any)
	if !ok {
		return "", errType
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if nullish(f) {
			return "", errType
		}
		oldPath, newPath := str(opt(f, "old_path")), str(opt(f, "new_path"))
		newFile, deleted := truthy(opt(f, "new_file")), truthy(opt(f, "deleted_file"))
		head := []string{"diff --git a/" + oldPath + " b/" + newPath}
		if newFile {
			head = append(head, "new file mode 100644")
		}
		if deleted {
			head = append(head, "deleted file mode 100644")
		}
		if truthy(opt(f, "renamed_file")) {
			head = append(head, "rename from "+oldPath, "rename to "+newPath)
		}
		diff := opt(f, "diff")
		if !truthy(diff) {
			if truthy(opt(f, "too_large")) || truthy(opt(f, "collapsed")) {
				head = append(head, "Binary files a/"+oldPath+" and b/"+newPath+" differ")
			}
			out = append(out, strings.Join(head, "\n"))
			continue
		}
		d, isString := diff.(string)
		if !isString {
			return "", errType
		}
		body := d
		if !strings.HasPrefix(d, "--- ") {
			from, to := "a/"+oldPath, "b/"+newPath
			if newFile {
				from = "/dev/null"
			}
			if deleted {
				to = "/dev/null"
			}
			body = "--- " + from + "\n+++ " + to + "\n" + d
		}
		out = append(out, strings.Join(head, "\n")+"\n"+strings.TrimSuffix(body, "\n"))
	}
	return strings.Join(out, "\n"), nil
}

// cutDiff keeps a diff within what a tab shows, cut at a file boundary when there is one (lengths in UTF-16 units).
func cutDiff(diff string) (string, bool) {
	if jscompat.Len(diff) <= viewDiffMax {
		return diff, false
	}
	units := utf16.Encode([]rune(diff))
	marker := utf16.Encode([]rune("\ndiff --git"))
	at := -1
	for i := min(viewDiffMax, len(units)-len(marker)); i >= 0; i-- {
		match := true
		for j := range marker {
			if units[i+j] != marker[j] {
				match = false
				break
			}
		}
		if match {
			at = i
			break
		}
	}
	end := at + 1
	if end == 0 {
		end = viewDiffMax
	}
	return string(utf16.Decode(units[:end])), true
}

func jsSlice(v any, n int) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", errType
	}
	return jscompat.Slice(s, n), nil
}

// GitLabView is what a tab shows of a merge request or a commit; nil when GitLab does not give it to this token.
func (s *Service) GitLabView(ctx context.Context, raw string) (*jsvalue.Object, error) {
	ref := s.gitlabRefOf(raw)
	if ref == nil {
		return nil, nil
	}
	token := s.gitlabToken(ref.host)
	if token == "" {
		return nil, nil
	}
	a := &access{host: ref.host, token: token}
	project, err := s.canonicalProject(ctx, a, ref.project)
	if err != nil {
		return nil, err
	}
	if nullish(project) {
		project = ref.project
	}
	id := files.EncodeURIComponent(str(project))
	var item, list any
	var errs firstErr
	var w sync.WaitGroup
	w.Add(2)
	itemPath, listPath := "/projects/"+id+"/merge_requests/"+jscompat.Number(ref.iid), "/projects/"+id+"/merge_requests/"+jscompat.Number(ref.iid)+"/diffs?per_page="+strconv.Itoa(viewFiles)
	if ref.sha != "" {
		itemPath, listPath = "/projects/"+id+"/repository/commits/"+ref.sha, "/projects/"+id+"/repository/commits/"+ref.sha+"/diff?per_page="+strconv.Itoa(viewFiles)
	}
	go func() { defer w.Done(); v, err := s.api(ctx, a, itemPath); item = v; errs.set(err) }()
	go func() { defer w.Done(); v, err := s.api(ctx, a, listPath); list = v; errs.set(err) }()
	w.Wait()
	if err := errs.get(); err != nil {
		return nil, err
	}
	if !truthy(item) {
		return nil, nil
	}
	fileList := list
	if nullish(fileList) {
		fileList = []any{}
	}
	unified, err := unifiedDiffOf(fileList)
	if err != nil {
		return nil, err
	}
	diff, cut := cutDiff(unified)
	count := 0
	if l, ok := list.([]any); ok {
		count = len(l)
	}
	if ref.sha == "" {
		changes := opt(item, "changes_count")
		changesText := ""
		if !nullish(changes) {
			changesText = str(changes)
		}
		total := jscompat.ParseInt(changesText, 10)
		if math.IsNaN(total) || math.IsInf(total, 0) {
			total = 0
		}
		desc := opt(item, "description")
		if nullish(desc) {
			desc = ""
		}
		description, err := jsSlice(desc, descMax)
		if err != nil {
			return nil, err
		}
		return obj("kind", "mr", "url", opt(item, "web_url"), "host", ref.host, "project", project, "ref", "!"+str(opt(item, "iid")),
			"title", opt(item, "title"), "description", description, "author", orNull(opt(opt(item, "author"), "name")),
			"at", orNull(opt(item, "updated_at")), "state", opt(item, "state"), "draft", truthy(opt(item, "draft")),
			"sourceBranch", orNull(opt(item, "source_branch")), "targetBranch", orNull(opt(item, "target_branch")),
			"pipeline", pipelineOf(opt(item, "head_pipeline")), "diff", diff,
			"truncated", cut || list == nil || total > float64(count) || strings.HasSuffix(changesText, "+")), nil
	}
	msg := opt(item, "message")
	if nullish(msg) {
		msg = ""
	}
	message, isString := msg.(string)
	if !isString {
		return nil, errType
	}
	title := opt(item, "title")
	description := ""
	if strings.HasPrefix(message, str(title)) {
		t, ok := title.(string)
		switch {
		case ok:
			description = jscompat.Slice(jscompat.Trim(sliceFrom(message, jscompat.Len(t))), descMax)
		case nullish(title):
			return nil, errType
		default:
			description = jscompat.Slice(jscompat.Trim(message), descMax)
		}
	}
	idValue, err := prop(item, "id")
	if err != nil {
		return nil, err
	}
	short, err := jsSlice(idValue, shortSHA)
	if err != nil {
		return nil, err
	}
	return obj("kind", "commit", "url", opt(item, "web_url"), "host", ref.host, "project", project, "ref", short,
		"title", title, "description", description, "author", orNull(opt(item, "author_name")), "at", orNull(opt(item, "created_at")),
		"state", nil, "draft", false, "sourceBranch", nil, "targetBranch", nil, "pipeline", pipelineOf(opt(item, "last_pipeline")),
		"diff", diff, "truncated", cut || list == nil || count >= viewFiles), nil
}

// sliceFrom is s.slice(n) in UTF-16 units.
func sliceFrom(s string, n int) string {
	if n <= 0 {
		return s
	}
	units := utf16.Encode([]rune(s))
	if n >= len(units) {
		return ""
	}
	return string(utf16.Decode(units[n:]))
}
