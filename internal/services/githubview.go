package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// GitHub through the same view as GitLab: a pull request, a commit or an issue with its checks, discussion and diff.
// The token is the person's: `gh auth token` on the machine when gh is logged in, else GITHUB_TOKEN. It is used for
// the requests and goes nowhere else: not into an answer, not into a log, not into a cache key.

const (
	githubTimeout = 8 * time.Second
	// githubFresh is how long a GitHub answer is reused. The API allows 5000 requests an hour with a token and one view
	// reads about ten, so a pane reopened within half a minute costs nothing.
	githubFresh    = 30 * time.Second
	githubBodyMax  = 8 << 20
	githubTokenTTL = time.Minute
	// githubBlockMax caps how long a rate limit keeps the runtime from asking: a reset further away is asked again.
	githubBlockMax = 15 * time.Minute
	githubPage     = 100
)

type githubAnswer struct {
	done   chan struct{}
	status int
	v      any
	at     time.Time
}

type githubState struct {
	mu           sync.Mutex
	answers      map[string]*githubAnswer
	blockedUntil time.Time
	token        string
	tokenAt      time.Time
}

func newGithubState() *githubState { return &githubState{answers: map[string]*githubAnswer{}} }

func (s *Service) githubHost() string {
	if s.cfg.GitHubHost != "" {
		return s.cfg.GitHubHost
	}
	return "github.com"
}

func (s *Service) githubAPI() string {
	switch {
	case s.cfg.GitHubAPI != "":
		return s.cfg.GitHubAPI
	case s.githubHost() == "github.com":
		return "https://api.github.com"
	}
	return "https://" + s.githubHost() + "/api/v3"
}

// githubToken is the person's GitHub token, "" without one. gh is asked before the environment, as the person's own
// login is the one that is current; the answer is kept for a minute so a view does not start a process per request.
func (s *Service) githubToken(ctx context.Context) string {
	g := s.github
	g.mu.Lock()
	if !g.tokenAt.IsZero() && time.Since(g.tokenAt) < githubTokenTTL {
		t := g.token
		g.mu.Unlock()
		return t
	}
	g.mu.Unlock()
	token := s.githubTokenFromGh(ctx)
	if token == "" {
		token = s.secret("GITHUB_TOKEN", "github-token")
	}
	if token == "" {
		token = jscompat.Trim(os.Getenv("RUNTIME_GITHUB_TOKEN"))
	}
	g.mu.Lock()
	g.token, g.tokenAt = token, time.Now()
	g.mu.Unlock()
	return token
}

func (s *Service) githubTokenFromGh(ctx context.Context) string {
	argv := s.cfg.GitHubTokenCmd
	if len(argv) == 0 {
		argv = []string{"gh", "auth", "token"}
		if s.githubHost() != "github.com" {
			argv = append(argv, "--hostname", s.githubHost())
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return jscompat.Trim(out.String())
}

// ghRef is a pull request, a commit or an issue of GitHub.
type ghRef struct {
	owner, repo string
	kind        string // "pr", "commit", "issue"
	number      int
	sha         string
}

const ghName = `[A-Za-z0-9._-]{1,100}`

var (
	ghPullRe   = regexp.MustCompile(`^/(` + ghName + `)/(` + ghName + `)/pull/([1-9][0-9]{0,8})(?:/.*)?$`)
	ghIssueRe  = regexp.MustCompile(`^/(` + ghName + `)/(` + ghName + `)/issues/([1-9][0-9]{0,8})/?$`)
	ghCommitRe = regexp.MustCompile(`^/(` + ghName + `)/(` + ghName + `)/commits?/([0-9a-fA-F]{7,40})/?$`)
)

func (r *ghRef) repoPath() string {
	return "/repos/" + url.PathEscape(r.owner) + "/" + url.PathEscape(r.repo)
}

func (r *ghRef) label() string {
	if r.kind == "commit" {
		return strings.ToLower(r.sha[:min(len(r.sha), shortSHA)])
	}
	return "#" + strconv.Itoa(r.number)
}

func (s *Service) githubURL(r *ghRef) string {
	base := "https://" + s.githubHost() + "/" + r.owner + "/" + r.repo
	switch r.kind {
	case "pr":
		return base + "/pull/" + strconv.Itoa(r.number)
	case "issue":
		return base + "/issues/" + strconv.Itoa(r.number)
	}
	return base + "/commit/" + r.sha
}

// githubRefOf is a pull request, a commit or an issue of the GitHub the deployment reads; nil for any other link.
func (s *Service) githubRefOf(raw string) *ghRef {
	u, err := url.Parse(jscompat.Trim(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || strings.ToLower(u.Hostname()) != s.githubHost() {
		return nil
	}
	if m := ghPullRe.FindStringSubmatch(u.Path); m != nil {
		n, _ := strconv.Atoi(m[3])
		return &ghRef{owner: m[1], repo: m[2], kind: "pr", number: n}
	}
	if m := ghIssueRe.FindStringSubmatch(u.Path); m != nil {
		n, _ := strconv.Atoi(m[3])
		return &ghRef{owner: m[1], repo: m[2], kind: "issue", number: n}
	}
	if m := ghCommitRe.FindStringSubmatch(u.Path); m != nil {
		return &ghRef{owner: m[1], repo: m[2], kind: "commit", sha: strings.ToLower(m[3])}
	}
	return nil
}

// ghReply is one read of the API: the status (0: no answer) and the JSON of a 2xx.
type ghReply struct {
	status int
	v      any
}

func (a ghReply) ok() bool { return a.status >= 200 && a.status <= 299 }

// limited says why a reply is no view: the token is refused, the object is not there or not visible to the token, the
// hourly allowance is spent, or GitHub is out of reach.
func (a ghReply) limited() string {
	switch {
	case a.status == 401:
		return "bad-token"
	case a.status == 404 || a.status == 410 || a.status == 403:
		return "not-found"
	case a.status == 429:
		return "rate-limit"
	}
	return "unavailable"
}

// githubCall is one read: answers are shared for githubFresh (a second tab, a reopened pane), and an allowance that
// is spent keeps the runtime from asking until GitHub says it is renewed.
func (s *Service) githubCall(ctx context.Context, token, method, path string, body []byte) ghReply {
	g := s.github
	sum := sha256.Sum256([]byte(token))
	bodySum := sha256.Sum256(body)
	key := method + " " + hex.EncodeToString(sum[:4]) + " " + path + " " + hex.EncodeToString(bodySum[:4])
	g.mu.Lock()
	if time.Now().Before(g.blockedUntil) {
		g.mu.Unlock()
		return ghReply{status: 429}
	}
	for k, e := range g.answers {
		if !e.at.IsZero() && time.Since(e.at) > githubFresh {
			delete(g.answers, k)
		}
	}
	if e, ok := g.answers[key]; ok {
		g.mu.Unlock()
		<-e.done
		return ghReply{e.status, e.v}
	}
	e := &githubAnswer{done: make(chan struct{})}
	g.answers[key] = e
	g.mu.Unlock()

	status, v, header := s.githubRead(ctx, token, method, path, body)

	g.mu.Lock()
	e.status, e.v = status, v
	if status == 0 {
		delete(g.answers, key)
	} else {
		e.at = time.Now()
	}
	if until := rateLimitedUntil(status, header); !until.IsZero() {
		g.blockedUntil = until
	}
	g.mu.Unlock()
	close(e.done)
	return ghReply{status, v}
}

func (s *Service) githubRead(ctx context.Context, token, method, path string, body []byte) (int, any, http.Header) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), githubTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.githubAPI()+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "harnessly")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.follow.Do(req)
	if err != nil {
		return 0, nil, nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, githubBodyMax))
	if err != nil {
		return 0, nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, nil, resp.Header
	}
	v, perr := jsvalue.Parse(raw)
	if perr != nil {
		return http.StatusBadGateway, nil, resp.Header
	}
	return resp.StatusCode, v, resp.Header
}

// rateLimitedUntil is when to ask again after an answer that says the allowance is spent; zero when it is not.
func rateLimitedUntil(status int, h http.Header) time.Time {
	now := time.Now()
	var wait time.Duration
	if after, err := strconv.Atoi(h.Get("Retry-After")); err == nil && after > 0 {
		wait = time.Duration(after) * time.Second
	} else if h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			wait = time.Until(time.Unix(reset, 0))
		}
	}
	if wait <= 0 && (status == 429 || status == 403 && h.Get("X-RateLimit-Remaining") == "0") {
		wait = time.Minute
	}
	if wait <= 0 {
		return time.Time{}
	}
	return now.Add(min(wait, githubBlockMax))
}

func (s *Service) ghGet(ctx context.Context, token, path string) ghReply {
	return s.githubCall(ctx, token, http.MethodGet, path, nil)
}

// ghList is a list the API answered, nil for any other answer.
func (s *Service) ghList(ctx context.Context, token, path string) []any {
	a := s.ghGet(ctx, token, path)
	if !a.ok() {
		return nil
	}
	list, _ := a.v.([]any)
	return list
}

func login(v any) any { return orNull(opt(v, "login")) }

func nonEmptyStr(v any) string {
	s, _ := v.(string)
	return s
}

func ghLabels(v any) []any {
	out := []any{}
	list, _ := v.([]any)
	for _, l := range list {
		if name := nonEmptyStr(opt(l, "name")); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// viewParts are the fields of the one shape both services answer.
type viewParts struct {
	kind, url, project, ref, title, description string
	author, at, state                           any
	draft                                       bool
	source, target                              any
	pipeline                                    any
	diff                                        string
	truncated                                   bool
	labels                                      []any
	stages, comments                            any
	limited                                     any
}

func (s *Service) githubViewOf(r *ghRef, p viewParts) *jsvalue.Object {
	if p.labels == nil {
		p.labels = []any{}
	}
	return obj("kind", p.kind, "provider", "github", "url", p.url, "host", s.githubHost(), "project", r.owner+"/"+r.repo, "ref", p.ref,
		"title", p.title, "description", p.description, "author", p.author, "at", p.at, "state", p.state, "draft", p.draft,
		"sourceBranch", p.source, "targetBranch", p.target, "pipeline", p.pipeline, "diff", p.diff, "truncated", p.truncated,
		"labels", p.labels, "stages", p.stages, "comments", p.comments, "limited", p.limited)
}

func ghKind(r *ghRef) string {
	switch r.kind {
	case "pr":
		return "mr"
	case "commit":
		return "commit"
	}
	return "issue"
}

// githubCard is what is known from the link alone, and why there is no more: the tab shows it with a link out.
func (s *Service) githubCard(r *ghRef, limited string) *jsvalue.Object {
	return s.githubViewOf(r, viewParts{kind: ghKind(r), url: s.githubURL(r), ref: r.label(), limited: limited})
}

// githubView reads a pull request, a commit or an issue; a card with the reason when it cannot.
func (s *Service) githubView(ctx context.Context, r *ghRef) *jsvalue.Object {
	token := s.githubToken(ctx)
	if token == "" {
		return s.githubCard(r, limitedNoAuth)
	}
	switch r.kind {
	case "pr":
		return s.githubPull(ctx, token, r)
	case "commit":
		return s.githubCommit(ctx, token, r)
	}
	item := s.ghGet(ctx, token, r.repoPath()+"/issues/"+strconv.Itoa(r.number))
	if !item.ok() {
		return s.githubCard(r, item.limited())
	}
	if truthy(opt(item.v, "pull_request")) {
		pr := *r
		pr.kind = "pr"
		return s.githubPull(ctx, token, &pr)
	}
	var comments any
	if rows := s.ghList(ctx, token, r.repoPath()+"/issues/"+strconv.Itoa(r.number)+"/comments?per_page="+strconv.Itoa(githubPage)); rows != nil {
		comments = ghIssueThreads(rows)
	}
	state := "opened"
	if nonEmptyStr(opt(item.v, "state")) == "closed" {
		state = "closed"
	}
	body, _ := opt(item.v, "body").(string)
	return s.githubViewOf(r, viewParts{kind: "issue", url: nonEmptyStr(opt(item.v, "html_url")), ref: r.label(),
		title: nonEmptyStr(opt(item.v, "title")), description: jscompat.Slice(body, descMax), author: login(opt(item.v, "user")),
		at: orNull(opt(item.v, "updated_at")), state: state, labels: ghLabels(opt(item.v, "labels")),
		comments: comments, limited: nil})
}

func (s *Service) githubPull(ctx context.Context, token string, r *ghRef) *jsvalue.Object {
	n := strconv.Itoa(r.number)
	pr := s.ghGet(ctx, token, r.repoPath()+"/pulls/"+n)
	if !pr.ok() {
		return s.githubCard(r, pr.limited())
	}
	head := nonEmptyStr(opt(opt(pr.v, "head"), "sha"))
	var fileRows, issueComments, reviewComments, reviews []any
	var resolved map[string]bool
	var stages any
	overall := ""
	var w sync.WaitGroup
	w.Add(6)
	go func() {
		defer w.Done()
		fileRows = s.ghList(ctx, token, r.repoPath()+"/pulls/"+n+"/files?per_page="+strconv.Itoa(viewFiles))
	}()
	go func() {
		defer w.Done()
		issueComments = s.ghList(ctx, token, r.repoPath()+"/issues/"+n+"/comments?per_page="+strconv.Itoa(githubPage))
	}()
	go func() {
		defer w.Done()
		reviewComments = s.ghList(ctx, token, r.repoPath()+"/pulls/"+n+"/comments?per_page="+strconv.Itoa(githubPage))
	}()
	go func() {
		defer w.Done()
		reviews = s.ghList(ctx, token, r.repoPath()+"/pulls/"+n+"/reviews?per_page="+strconv.Itoa(githubPage))
	}()
	go func() { defer w.Done(); resolved = s.githubResolved(ctx, token, r) }()
	go func() {
		defer w.Done()
		if head != "" {
			stages, overall = s.githubChecks(ctx, token, r, head)
		}
	}()
	w.Wait()

	diff, truncated := "", true
	if fileRows != nil {
		unified, err := unifiedDiffOf(ghFilesAsDiffRows(fileRows))
		if err == nil {
			var cut bool
			diff, cut = cutDiff(unified)
			changed, _ := opt(pr.v, "changed_files").(float64)
			truncated = cut || changed > float64(len(fileRows))
		}
	}
	state := "opened"
	switch {
	case truthy(opt(pr.v, "merged")) || nonEmptyStr(opt(pr.v, "merged_at")) != "":
		state = "merged"
	case nonEmptyStr(opt(pr.v, "state")) == "closed":
		state = "closed"
	}
	body, _ := opt(pr.v, "body").(string)
	htmlURL := nonEmptyStr(opt(pr.v, "html_url"))
	var pipeline any
	if overall != "" {
		pipeline = obj("status", overall, "url", htmlURL+"/checks")
	}
	var comments any
	if issueComments != nil || reviewComments != nil || reviews != nil {
		comments = ghPullThreads(issueComments, reviewComments, reviews, resolved)
	}
	return s.githubViewOf(r, viewParts{kind: "mr", url: htmlURL, ref: r.label(), title: nonEmptyStr(opt(pr.v, "title")),
		description: jscompat.Slice(body, descMax), author: login(opt(pr.v, "user")), at: orNull(opt(pr.v, "updated_at")),
		state: state, draft: truthy(opt(pr.v, "draft")), source: orNull(opt(opt(pr.v, "head"), "ref")),
		target: orNull(opt(opt(pr.v, "base"), "ref")), pipeline: pipeline, diff: diff, truncated: truncated,
		labels: ghLabels(opt(pr.v, "labels")), stages: stages, comments: comments, limited: nil})
}

func (s *Service) githubCommit(ctx context.Context, token string, r *ghRef) *jsvalue.Object {
	item := s.ghGet(ctx, token, r.repoPath()+"/commits/"+r.sha)
	if !item.ok() {
		return s.githubCard(r, item.limited())
	}
	sha := nonEmptyStr(opt(item.v, "sha"))
	if sha == "" {
		sha = r.sha
	}
	var commentRows []any
	var stages any
	overall := ""
	var w sync.WaitGroup
	w.Add(2)
	go func() {
		defer w.Done()
		commentRows = s.ghList(ctx, token, r.repoPath()+"/commits/"+sha+"/comments?per_page="+strconv.Itoa(githubPage))
	}()
	go func() { defer w.Done(); stages, overall = s.githubChecks(ctx, token, r, sha) }()
	w.Wait()

	rows, _ := opt(item.v, "files").([]any)
	more := len(rows) > viewFiles
	if more {
		rows = rows[:viewFiles]
	}
	diff, truncated := "", false
	if unified, err := unifiedDiffOf(ghFilesAsDiffRows(rows)); err == nil {
		var cut bool
		diff, cut = cutDiff(unified)
		truncated = cut || more
	}
	title, rest := splitMessage(nonEmptyStr(opt(opt(item.v, "commit"), "message")))
	author := orNull(opt(opt(item.v, "author"), "login"))
	if author == nil {
		author = orNull(opt(opt(opt(item.v, "commit"), "author"), "name"))
	}
	htmlURL := nonEmptyStr(opt(item.v, "html_url"))
	var pipeline any
	if overall != "" {
		pipeline = obj("status", overall, "url", htmlURL)
	}
	r2 := *r
	r2.sha = sha
	var comments any
	if commentRows != nil {
		comments = ghCommitThreads(commentRows)
	}
	return s.githubViewOf(&r2, viewParts{kind: "commit", url: htmlURL, ref: r2.label(), title: title,
		description: jscompat.Slice(rest, descMax), author: author, at: orNull(opt(opt(opt(item.v, "commit"), "author"), "date")),
		pipeline: pipeline, diff: diff, truncated: truncated, stages: stages, comments: comments, limited: nil})
}

// ghFilesAsDiffRows puts GitHub's files in the form GitLab lists them, so one writer makes the unified diff of both.
func ghFilesAsDiffRows(rows []any) []any {
	out := make([]any, 0, len(rows))
	for _, f := range rows {
		name := nonEmptyStr(opt(f, "filename"))
		prev := nonEmptyStr(opt(f, "previous_filename"))
		if prev == "" {
			prev = name
		}
		status := nonEmptyStr(opt(f, "status"))
		patch := nonEmptyStr(opt(f, "patch"))
		changes, _ := opt(f, "changes").(float64)
		out = append(out, obj("old_path", prev, "new_path", name, "new_file", status == "added", "deleted_file", status == "removed",
			"renamed_file", status == "renamed", "diff", patch, "too_large", patch == "" && changes > 0))
	}
	return out
}

// checkStatusOf is the status of a check run in the words of a pipeline.
func checkStatusOf(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return "success"
		case "neutral", "skipped", "stale":
			return "skipped"
		case "failure", "timed_out", "startup_failure":
			return "failed"
		case "cancelled":
			return "canceled"
		case "action_required":
			return "manual"
		}
		return "other"
	case "in_progress":
		return "running"
	case "queued", "waiting", "requested", "pending":
		return "pending"
	}
	return "other"
}

func durationOf(start, end any) any {
	a, errA := time.Parse(time.RFC3339, nonEmptyStr(start))
	b, errB := time.Parse(time.RFC3339, nonEmptyStr(end))
	if errA != nil || errB != nil || b.Before(a) {
		return nil
	}
	return float64(b.Sub(a).Milliseconds())
}

// githubChecks are the check runs of a commit grouped by the workflow that ran them, with the older commit statuses as
// one group more, and the one status that stands for them all.
func (s *Service) githubChecks(ctx context.Context, token string, r *ghRef, sha string) (any, string) {
	var runs, workflows, status any
	var w sync.WaitGroup
	w.Add(3)
	go func() {
		defer w.Done()
		if a := s.ghGet(ctx, token, r.repoPath()+"/commits/"+sha+"/check-runs?per_page="+strconv.Itoa(githubPage)); a.ok() {
			runs = opt(a.v, "check_runs")
		}
	}()
	go func() {
		defer w.Done()
		if a := s.ghGet(ctx, token, r.repoPath()+"/actions/runs?head_sha="+sha+"&per_page="+strconv.Itoa(githubPage)); a.ok() {
			workflows = opt(a.v, "workflow_runs")
		}
	}()
	go func() {
		defer w.Done()
		if a := s.ghGet(ctx, token, r.repoPath()+"/commits/"+sha+"/status?per_page="+strconv.Itoa(githubPage)); a.ok() {
			status = a.v
		}
	}()
	w.Wait()

	type workflow struct{ name, url string }
	bySuite := map[string]workflow{}
	rows, _ := workflows.([]any)
	for _, wf := range rows {
		bySuite[str(opt(wf, "check_suite_id"))] = workflow{nonEmptyStr(opt(wf, "name")), nonEmptyStr(opt(wf, "html_url"))}
	}
	var order []string
	groups := map[string][]*jsvalue.Object{}
	urls := map[string]any{}
	var all []string
	add := func(group string, url any, job *jsvalue.Object) {
		if _, seen := groups[group]; !seen {
			order = append(order, group)
			urls[group] = url
		}
		groups[group] = append(groups[group], job)
		all = append(all, job.String("status"))
	}
	checks, _ := runs.([]any)
	for _, c := range checks {
		group := nonEmptyStr(opt(opt(c, "app"), "name"))
		var url any
		if wf, ok := bySuite[str(opt(opt(c, "check_suite"), "id"))]; ok && wf.name != "" {
			group = wf.name
			if wf.url != "" {
				url = wf.url
			}
		}
		if group == "" {
			group = locale.Pick("Checks", "Проверки")
		}
		page := nonEmptyStr(opt(c, "html_url"))
		if page == "" {
			page = nonEmptyStr(opt(c, "details_url"))
		}
		add(group, url, jobOf(nonEmptyStr(opt(c, "name")), checkStatusOf(nonEmptyStr(opt(c, "status")), nonEmptyStr(opt(c, "conclusion"))),
			page, durationOf(opt(c, "started_at"), opt(c, "completed_at"))))
	}
	statuses, _ := opt(status, "statuses").([]any)
	for _, st := range statuses {
		state := map[string]string{"success": "success", "pending": "pending", "failure": "failed", "error": "failed"}[nonEmptyStr(opt(st, "state"))]
		if state == "" {
			state = "other"
		}
		add(locale.Pick("Statuses", "Статусы"), nil, jobOf(nonEmptyStr(opt(st, "context")), state, nonEmptyStr(opt(st, "target_url")), nil))
	}
	if len(order) == 0 {
		return nil, ""
	}
	out := make([]any, len(order))
	for i, name := range order {
		out[i] = stageOf(name, urls[name], groups[name])
	}
	return out, worstStatus(all)
}

// githubResolved is which review threads are resolved, by the id of the comment that opened each (the REST API does not
// say; GraphQL does). Nil when it cannot be read: the threads then show no resolved state.
func (s *Service) githubResolved(ctx context.Context, token string, r *ghRef) map[string]bool {
	query := `query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){reviewThreads(first:100){nodes{isResolved comments(first:1){nodes{databaseId}}}}}}}`
	body := jsvalue.Stringify(obj("query", query, "variables", obj("o", r.owner, "r", r.repo, "n", float64(r.number))), "")
	a := s.githubCall(ctx, token, http.MethodPost, "/graphql", body)
	if !a.ok() {
		return nil
	}
	nodes, ok := opt(opt(opt(opt(opt(a.v, "data"), "repository"), "pullRequest"), "reviewThreads"), "nodes").([]any)
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for _, n := range nodes {
		first := opt(opt(opt(n, "comments"), "nodes"), "0")
		if id, ok := opt(first, "databaseId").(float64); ok {
			out[jscompat.Number(id)] = truthy(opt(n, "isResolved"))
		}
	}
	return out
}

type stamped struct {
	at string
	v  any
}

func sortedThreads(list []stamped) []any {
	sort.SliceStable(list, func(i, j int) bool { return list[i].at < list[j].at })
	out := make([]any, 0, len(list))
	for i, t := range list {
		if i == threadsMax {
			break
		}
		out = append(out, t.v)
	}
	return out
}

func ghNote(c any) (*jsvalue.Object, string) {
	at := nonEmptyStr(opt(c, "created_at"))
	return noteOf(login(opt(c, "user")), orNull(opt(c, "created_at")), opt(c, "body")), at
}

func ghIssueThreads(rows []any) []any {
	var list []stamped
	for _, c := range rows {
		note, at := ghNote(c)
		list = append(list, stamped{at, threadOf("ic-"+str(opt(c, "id")), nil, nil, nil, false, []any{note})})
	}
	return sortedThreads(list)
}

func ghCommitThreads(rows []any) []any {
	var list []stamped
	for _, c := range rows {
		note, at := ghNote(c)
		list = append(list, stamped{at, threadOf("cc-"+str(opt(c, "id")), nil, opt(c, "path"), opt(c, "line"), false, []any{note})})
	}
	return sortedThreads(list)
}

// ghPullThreads are the discussion of a pull request: its comments, the text of its reviews and the threads of
// comments on lines, a reply under the comment it answers.
func ghPullThreads(issue, review, reviews []any, resolved map[string]bool) []any {
	var list []stamped
	for _, c := range issue {
		note, at := ghNote(c)
		list = append(list, stamped{at, threadOf("ic-"+str(opt(c, "id")), nil, nil, nil, false, []any{note})})
	}
	for _, rv := range reviews {
		if nonEmptyStr(opt(rv, "body")) == "" {
			continue
		}
		at := nonEmptyStr(opt(rv, "submitted_at"))
		note := noteOf(login(opt(rv, "user")), orNull(opt(rv, "submitted_at")), opt(rv, "body"))
		list = append(list, stamped{at, threadOf("rv-"+str(opt(rv, "id")), nil, nil, nil, false, []any{note})})
	}
	roots := map[string]*struct {
		at    string
		first any
		notes []any
	}{}
	var rootOrder []string
	sorted := append([]any(nil), review...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return nonEmptyStr(opt(sorted[i], "created_at")) < nonEmptyStr(opt(sorted[j], "created_at"))
	})
	for _, c := range sorted {
		root := str(opt(c, "id"))
		if parent := opt(c, "in_reply_to_id"); truthy(parent) {
			root = str(parent)
		}
		t, ok := roots[root]
		if !ok {
			t = &struct {
				at    string
				first any
				notes []any
			}{at: nonEmptyStr(opt(c, "created_at")), first: c}
			roots[root] = t
			rootOrder = append(rootOrder, root)
		}
		note, _ := ghNote(c)
		t.notes = append(t.notes, note)
	}
	for _, id := range rootOrder {
		t := roots[id]
		var state any
		if done, ok := resolved[id]; ok {
			state = done
		}
		line := opt(t.first, "line")
		outdated := nullish(line) || line == nil
		if outdated {
			line = opt(t.first, "original_line")
		}
		list = append(list, stamped{t.at, threadOf("rc-"+id, state, opt(t.first, "path"), line, outdated, t.notes)})
	}
	return sortedThreads(list)
}
