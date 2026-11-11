package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The review of a GitHub pull request behind the same ReviewSource as GitLab's: the review threads at their lines
// (resolved or not), the conversation of the pull request as one thread, each submitted review's text as a thread of
// its own, the person's pending review as the draft, approvals, and the acts. Threads, the pending review and the
// approvals go through GraphQL (REST has no resolving and no pending threads); the conversation and new pull requests
// through REST. Nothing is kept: every answer is read again from GitHub.

// GitHubConversation is the thread of a pull request's conversation (its issue comments).
const GitHubConversation = "conversation"

// ghReviewPrefix starts the thread of a submitted review's text: review:<database id>.
const ghReviewPrefix = "review:"

// ghPullLinkRe reads a pull request link: /owner/repo/pull/N.
var ghPullLinkRe = regexp.MustCompile(`^/([^/]+/[^/]+)/pull/(\d+)(?:/.*)?$`)

// sameGitHubHost: a link's host is the connection's (github.com is also www.github.com).
func sameGitHubHost(link, base string) bool {
	norm := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }
	return norm(link) == norm(base)
}

// pullRequestOf reads a pull request link of a GitHub connection: its owner/repo and number.
func pullRequestOf(c Conn, u *url.URL) (string, int, bool) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || !sameGitHubHost(u.Hostname(), base.Hostname()) {
		return "", 0, false
	}
	m := ghPullLinkRe.FindStringSubmatch(u.Path)
	if m == nil || !ValidRepo(m[1]) {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return m[1], n, true
}

// graphqlURL is the GraphQL endpoint beside the REST one: api.github.com/graphql, <host>/api/graphql on a server.
func (c *ghClient) graphqlURL() string {
	if rest, ok := strings.CutSuffix(c.api, "/api/v3"); ok {
		return rest + "/api/graphql"
	}
	return c.api + "/graphql"
}

func (c *ghClient) header() http.Header {
	h := Header("Authorization", Bearer(c.token), "Accept", "application/vnd.github+json")
	if c.conn.Deployment == Cloud {
		h.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	return h
}

// send is one REST call that writes.
func (c *ghClient) send(ctx context.Context, method, path string, body, out any) error {
	if c.token == "" {
		return ErrDenied
	}
	res, err := c.h.Do(ctx, Request{Method: method, URL: c.api + "/" + strings.TrimLeft(path, "/"), Header: c.header(), Body: body})
	if err != nil {
		return err
	}
	if err := ghStatusErr(res); err != nil {
		return err
	}
	if out != nil && len(res.Body) > 0 {
		return res.Decode(out)
	}
	return nil
}

// ghStatusErr is StatusErr with GitHub's way of saying the token lacks a scope.
func ghStatusErr(res *Response) error {
	// A classic token names its scopes; a fine-grained one names none and is refused like any other.
	if scopes := res.Header.Get("X-OAuth-Scopes"); res.Status == http.StatusForbidden && scopes != "" && !strings.Contains(scopes, "repo") {
		return fmt.Errorf("%w: the token lacks the repo scope", ErrDenied)
	}
	return StatusErr(res)
}

const ghErrMax = 200

// gql is one GraphQL call; the first error GitHub names is the answer's error.
func (c *ghClient) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.token == "" {
		return ErrDenied
	}
	res, err := c.h.Do(ctx, Request{Method: http.MethodPost, URL: c.graphqlURL(), Header: c.header(),
		Body: map[string]any{"query": query, "variables": vars}})
	if err != nil {
		return err
	}
	if err := ghStatusErr(res); err != nil {
		return err
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := res.Decode(&env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		e := env.Errors[0]
		switch e.Type {
		case "NOT_FOUND":
			return fmt.Errorf("%w: %s", ErrNotFound, clipText(e.Message, ghErrMax))
		case "FORBIDDEN":
			return fmt.Errorf("%w: %s", ErrDenied, clipText(e.Message, ghErrMax))
		case "INSUFFICIENT_SCOPES":
			return fmt.Errorf("%w: the token lacks the repo scope", ErrDenied)
		}
		return fmt.Errorf("%w: %s", ErrUnreachable, clipText(e.Message, ghErrMax))
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("%w: not the answer asked of GitHub", ErrUnreachable)
		}
	}
	return nil
}

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

type gqlActor struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
	Name      string `json:"name"`
}

func (a *gqlActor) person() *Person {
	if a == nil {
		return nil
	}
	return (&ghUser{Login: a.Login, Name: a.Name, AvatarURL: a.AvatarURL}).person()
}

type gqlComment struct {
	ID              string    `json:"id"`
	DatabaseID      int       `json:"databaseId"`
	Body            string    `json:"body"`
	CreatedAt       string    `json:"createdAt"`
	LastEditedAt    string    `json:"lastEditedAt"`
	State           string    `json:"state"`
	ViewerDidAuthor bool      `json:"viewerDidAuthor"`
	Author          *gqlActor `json:"author"`
}

func (c gqlComment) note() ReviewNote {
	return ReviewNote{ID: c.DatabaseID, Author: c.Author.person(), Mine: c.ViewerDidAuthor, Body: c.Body,
		Created: ParseTime(c.CreatedAt), Edited: c.LastEditedAt != ""}
}

type gqlThread struct {
	ID           string `json:"id"`
	IsResolved   bool   `json:"isResolved"`
	IsOutdated   bool   `json:"isOutdated"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"originalLine"`
	DiffSide     string `json:"diffSide"`
	Comments     struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"comments"`
}

type gqlPage struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type gqlThreads struct {
	PageInfo gqlPage     `json:"pageInfo"`
	Nodes    []gqlThread `json:"nodes"`
}

// ghPull is what the review reads of a pull request.
type ghPull struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	State              string    `json:"state"`
	IsDraft            bool      `json:"isDraft"`
	HeadRefOid         string    `json:"headRefOid"`
	BaseRefOid         string    `json:"baseRefOid"`
	Author             *gqlActor `json:"author"`
	ViewerLatestReview *struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"viewerLatestReview"`
	LatestOpinionatedReviews struct {
		Nodes []struct {
			State  string    `json:"state"`
			Author *gqlActor `json:"author"`
		} `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	Pending struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	} `json:"pending"`
	Comments struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"comments"`
	Reviews struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"reviews"`
	ReviewThreads gqlThreads `json:"reviewThreads"`
}

const gqlActorFields = `fragment A on Actor{login avatarUrl ...on User{name}}
fragment C on Comment{body createdAt lastEditedAt viewerDidAuthor author{...A}}`

const gqlThreadFields = `pageInfo{hasNextPage endCursor} nodes{id isResolved isOutdated path line originalLine diffSide
comments(first:100){nodes{id databaseId state ...C}}}`

var ghPullQuery = `query($owner:String!,$name:String!,$n:Int!){viewer{login}
repository(owner:$owner,name:$name){pullRequest(number:$n){
id title state isDraft headRefOid baseRefOid author{...A}
viewerLatestReview{id state}
latestOpinionatedReviews(first:50){nodes{state author{...A}}}
pending:reviews(states:PENDING,first:1){nodes{id}}
comments(last:100){nodes{id databaseId ...C}}
reviews(last:50,states:[APPROVED,CHANGES_REQUESTED,COMMENTED,DISMISSED]){nodes{id databaseId state ...C}}
reviewThreads(first:50){` + gqlThreadFields + `}}}}
` + gqlActorFields

var ghThreadsQuery = `query($owner:String!,$name:String!,$n:Int!,$after:String){
repository(owner:$owner,name:$name){pullRequest(number:$n){reviewThreads(first:50,after:$after){` + gqlThreadFields + `}}}}
` + gqlActorFields

const ghThreadPagesMax = 10

// pull reads a pull request with all its review threads.
func (c *ghClient) pull(ctx context.Context, project string, n int) (*ghPull, string, error) {
	owner, name, ok := strings.Cut(project, "/")
	if !ok || !ValidRepo(project) {
		return nil, "", ErrNotFound
	}
	var out struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
		Repository *struct {
			PullRequest *ghPull `json:"pullRequest"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": name, "n": n}
	if err := c.gql(ctx, ghPullQuery, vars, &out); err != nil {
		return nil, "", err
	}
	if out.Repository == nil || out.Repository.PullRequest == nil {
		return nil, "", ErrNotFound
	}
	pr := out.Repository.PullRequest
	page := pr.ReviewThreads.PageInfo
	for i := 1; page.HasNextPage && i < ghThreadPagesMax; i++ {
		var more struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads gqlThreads `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars["after"] = page.EndCursor
		if err := c.gql(ctx, ghThreadsQuery, vars, &more); err != nil {
			return nil, "", err
		}
		got := more.Repository.PullRequest.ReviewThreads
		pr.ReviewThreads.Nodes = append(pr.ReviewThreads.Nodes, got.Nodes...)
		page = got.PageInfo
	}
	return pr, out.Viewer.Login, nil
}

func sideOf(diffSide string) string {
	if diffSide == "LEFT" {
		return "before"
	}
	return "after"
}

func lineOf(t gqlThread) int {
	if t.Line != nil {
		return *t.Line
	}
	if t.OriginalLine != nil {
		return *t.OriginalLine
	}
	return 0
}

// ghState is GitHub's state of a pull request in GitLab's words, which the page knows.
func ghState(s string) string {
	switch s {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	return "opened"
}

// reviewOf is the review a pull request read stands for.
func reviewOf(pr *ghPull, viewer string) *Review {
	mine := pr.Author != nil && strings.EqualFold(pr.Author.Login, viewer)
	out := &Review{Me: (&gqlActor{Login: viewer}).person(), Mine: mine, Title: pr.Title, State: ghState(pr.State),
		Draft: pr.IsDraft, ApprovedBy: []string{}, Threads: []ReviewThread{}, Drafts: []ReviewDraft{},
		Refs: ReviewRefs{Base: pr.BaseRefOid, Start: pr.BaseRefOid, Head: pr.HeadRefOid}}
	out.Approved = pr.ViewerLatestReview != nil && pr.ViewerLatestReview.State == "APPROVED"
	out.CanApprove = !mine && pr.State == "OPEN"
	for _, r := range pr.LatestOpinionatedReviews.Nodes {
		if r.State == "APPROVED" {
			if p := r.Author.person(); p != nil {
				out.ApprovedBy = append(out.ApprovedBy, p.Name)
			}
		}
	}
	for _, t := range pr.ReviewThreads.Nodes {
		th := ReviewThread{ID: t.ID, Resolvable: true, Resolved: t.IsResolved, Path: t.Path, Line: lineOf(t),
			Side: sideOf(t.DiffSide), Outdated: t.IsOutdated, Notes: []ReviewNote{}}
		var pending []gqlComment
		for _, cm := range t.Comments.Nodes {
			if cm.State == "PENDING" {
				pending = append(pending, cm)
				continue
			}
			th.Notes = append(th.Notes, cm.note())
		}
		for _, cm := range pending {
			d := ReviewDraft{ID: cm.DatabaseID, Body: cm.Body}
			if len(th.Notes) > 0 {
				d.ReplyTo = t.ID
			} else {
				d.Path, d.Line, d.Side = t.Path, th.Line, th.Side
			}
			out.Drafts = append(out.Drafts, d)
		}
		if len(th.Notes) > 0 {
			out.Threads = append(out.Threads, th)
		}
	}
	// The conversation and the reviews' texts are threads on the pull request as a whole, oldest first.
	type whole struct {
		at string
		t  ReviewThread
	}
	var all []whole
	if len(pr.Comments.Nodes) > 0 {
		conv := ReviewThread{ID: GitHubConversation, Notes: []ReviewNote{}}
		for _, cm := range pr.Comments.Nodes {
			conv.Notes = append(conv.Notes, cm.note())
		}
		all = append(all, whole{at: pr.Comments.Nodes[0].CreatedAt, t: conv})
	}
	for _, r := range pr.Reviews.Nodes {
		if strings.TrimSpace(r.Body) == "" {
			continue
		}
		all = append(all, whole{at: r.CreatedAt, t: ReviewThread{ID: ghReviewPrefix + strconv.Itoa(r.DatabaseID),
			Notes: []ReviewNote{r.note()}}})
	}
	sort.SliceStable(all, func(i, j int) bool { return ParseTime(all[i].at).Before(ParseTime(all[j].at)) })
	for _, w := range all {
		out.Threads = append(out.Threads, w.t)
	}
	return out
}

// Review implements ReviewSource.
func (c *ghClient) Review(ctx context.Context, project string, n int) (*Review, error) {
	pr, viewer, err := c.pull(ctx, project, n)
	if err != nil {
		return nil, err
	}
	return reviewOf(pr, viewer), nil
}

// commentNode is the node of a review comment by its number: of a thread's notes, or of the pending ones.
func commentNode(pr *ghPull, thread string, id int, pending bool) (string, bool) {
	for _, t := range pr.ReviewThreads.Nodes {
		if thread != "" && t.ID != thread {
			continue
		}
		for _, cm := range t.Comments.Nodes {
			if cm.DatabaseID == id && (cm.State == "PENDING") == pending {
				return cm.ID, true
			}
		}
	}
	return "", false
}

func hasThread(pr *ghPull, id string) bool {
	for _, t := range pr.ReviewThreads.Nodes {
		if t.ID == id {
			return true
		}
	}
	return false
}

// ownReview is the database id of the person's submitted review whose text a thread shows; false for another's.
func ownReview(pr *ghPull, thread string) (int, bool) {
	raw, ok := strings.CutPrefix(thread, ghReviewPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	for _, r := range pr.Reviews.Nodes {
		if r.DatabaseID == id && r.ViewerDidAuthor {
			return id, true
		}
	}
	return 0, false
}

const (
	gqlReply       = `mutation($t:ID!,$b:String!,$r:ID){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$t,body:$b,pullRequestReviewId:$r}){comment{id}}}`
	gqlNewThread   = `mutation($r:ID!,$p:String!,$l:Int!,$s:DiffSide!,$b:String!){addPullRequestReviewThread(input:{pullRequestReviewId:$r,path:$p,line:$l,side:$s,body:$b}){thread{id}}}`
	gqlEdit        = `mutation($id:ID!,$b:String!){updatePullRequestReviewComment(input:{pullRequestReviewCommentId:$id,body:$b}){pullRequestReviewComment{id}}}`
	gqlDelete      = `mutation($id:ID!){deletePullRequestReviewComment(input:{id:$id}){clientMutationId}}`
	gqlResolve     = `mutation($t:ID!){resolveReviewThread(input:{threadId:$t}){thread{id}}}`
	gqlUnresolve   = `mutation($t:ID!){unresolveReviewThread(input:{threadId:$t}){thread{id}}}`
	gqlStart       = `mutation($pr:ID!,$c:GitObjectID){addPullRequestReview(input:{pullRequestId:$pr,commitOID:$c}){pullRequestReview{id}}}`
	gqlApprove     = `mutation($pr:ID!){addPullRequestReview(input:{pullRequestId:$pr,event:APPROVE}){pullRequestReview{id}}}`
	gqlSubmit      = `mutation($r:ID!,$e:PullRequestReviewEvent!){submitPullRequestReview(input:{pullRequestReviewId:$r,event:$e}){pullRequestReview{id}}}`
	gqlDismiss     = `mutation($r:ID!,$m:String!){dismissPullRequestReview(input:{pullRequestReviewId:$r,message:$m}){pullRequestReview{id}}}`
	gqlReady       = `mutation($pr:ID!){markPullRequestReadyForReview(input:{pullRequestId:$pr}){pullRequest{id}}}`
	ghUnapproveMsg = "Approval withdrawn"
)

// pendingReview is the person's pending review, started when there is none.
func (c *ghClient) pendingReview(ctx context.Context, pr *ghPull) (string, error) {
	if len(pr.Pending.Nodes) > 0 {
		return pr.Pending.Nodes[0].ID, nil
	}
	var out struct {
		AddPullRequestReview struct {
			PullRequestReview struct {
				ID string `json:"id"`
			} `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	if err := c.gql(ctx, gqlStart, map[string]any{"pr": pr.ID, "c": pr.HeadRefOid}, &out); err != nil {
		return "", err
	}
	if id := out.AddPullRequestReview.PullRequestReview.ID; id != "" {
		return id, nil
	}
	return "", fmt.Errorf("%w: GitHub started no review", ErrUnreachable)
}

// ReviewAct implements ReviewSource.
func (c *ghClient) ReviewAct(ctx context.Context, project string, n int, a ReviewAct) (*Review, error) {
	pr, _, err := c.pull(ctx, project, n)
	if err != nil {
		return nil, err
	}
	repo := "repos/" + project
	issue := repo + "/issues/" + strconv.Itoa(n)
	switch a.Op {
	case ActReply:
		switch {
		case !validBody(a.Body):
			return nil, ErrBadAct
		case a.Thread == GitHubConversation || strings.HasPrefix(a.Thread, ghReviewPrefix):
			// The conversation has no threads of its own: an answer to it, or to a review's text, is a comment on it.
			err = c.send(ctx, http.MethodPost, issue+"/comments", map[string]string{"body": a.Body}, nil)
		case hasThread(pr, a.Thread):
			err = c.gql(ctx, gqlReply, map[string]any{"t": a.Thread, "b": a.Body, "r": nil}, nil)
		default:
			return nil, ErrBadAct
		}
	case ActEdit:
		if a.Note < 1 || !validBody(a.Body) {
			return nil, ErrBadAct
		}
		if a.Thread == GitHubConversation {
			err = c.send(ctx, http.MethodPatch, repo+"/issues/comments/"+strconv.Itoa(a.Note), map[string]string{"body": a.Body}, nil)
		} else if id, ok := ownReview(pr, a.Thread); ok {
			err = c.send(ctx, http.MethodPut, repo+"/pulls/"+strconv.Itoa(n)+"/reviews/"+strconv.Itoa(id), map[string]string{"body": a.Body}, nil)
		} else if node, ok := commentNode(pr, a.Thread, a.Note, false); ok {
			err = c.gql(ctx, gqlEdit, map[string]any{"id": node, "b": a.Body}, nil)
		} else {
			return nil, ErrBadAct
		}
	case ActDelete:
		if a.Note < 1 {
			return nil, ErrBadAct
		}
		if a.Thread == GitHubConversation {
			err = c.send(ctx, http.MethodDelete, repo+"/issues/comments/"+strconv.Itoa(a.Note), nil, nil)
		} else if node, ok := commentNode(pr, a.Thread, a.Note, false); ok && a.Thread != "" {
			err = c.gql(ctx, gqlDelete, map[string]any{"id": node}, nil)
		} else {
			// A submitted review's text cannot be deleted on GitHub, only edited.
			return nil, ErrBadAct
		}
	case ActResolve:
		if !hasThread(pr, a.Thread) {
			return nil, ErrBadAct
		}
		q := gqlUnresolve
		if a.Resolved {
			q = gqlResolve
		}
		err = c.gql(ctx, q, map[string]any{"t": a.Thread}, nil)
	case ActDraft:
		err = c.draft(ctx, pr, a)
		if errors.Is(err, ErrBadAct) {
			return nil, err
		}
	case ActDraftEdit:
		node, ok := commentNode(pr, "", a.Draft, true)
		if a.Draft < 1 || !ok || !validBody(a.Body) {
			return nil, ErrBadAct
		}
		err = c.gql(ctx, gqlEdit, map[string]any{"id": node, "b": a.Body}, nil)
	case ActDraftDelete:
		node, ok := commentNode(pr, "", a.Draft, true)
		if a.Draft < 1 || !ok {
			return nil, ErrBadAct
		}
		err = c.gql(ctx, gqlDelete, map[string]any{"id": node}, nil)
	case ActPublish:
		if len(pr.Pending.Nodes) > 0 {
			err = c.gql(ctx, gqlSubmit, map[string]any{"r": pr.Pending.Nodes[0].ID, "e": "COMMENT"}, nil)
		}
	case ActApprove:
		// GitHub keeps one pending review per person: approving submits it, the drafts with the approval.
		if len(pr.Pending.Nodes) > 0 {
			err = c.gql(ctx, gqlSubmit, map[string]any{"r": pr.Pending.Nodes[0].ID, "e": "APPROVE"}, nil)
		} else {
			err = c.gql(ctx, gqlApprove, map[string]any{"pr": pr.ID}, nil)
		}
	case ActUnapprove:
		// GitHub takes an approval back only by dismissing the review, which needs the right to push to the repository.
		if pr.ViewerLatestReview == nil || pr.ViewerLatestReview.State != "APPROVED" {
			return nil, ErrBadAct
		}
		err = c.gql(ctx, gqlDismiss, map[string]any{"r": pr.ViewerLatestReview.ID, "m": ghUnapproveMsg}, nil)
	case ActReady:
		if pr.IsDraft {
			err = c.gql(ctx, gqlReady, map[string]any{"pr": pr.ID}, nil)
		}
	default:
		return nil, ErrBadAct
	}
	if err != nil {
		return nil, err
	}
	return c.Review(ctx, project, n)
}

// draft adds a note to the person's pending review: a reply to a review thread, or a new thread at a line.
func (c *ghClient) draft(ctx context.Context, pr *ghPull, a ReviewAct) error {
	if !validBody(a.Body) {
		return ErrBadAct
	}
	var vars map[string]any
	q := gqlReply
	switch {
	case a.Thread != "":
		// The conversation is not part of a review: only a review thread takes a draft reply.
		if !hasThread(pr, a.Thread) {
			return ErrBadAct
		}
		vars = map[string]any{"t": a.Thread, "b": a.Body}
	case a.Path != "" && a.Line > 0 && (a.Side == "after" || a.Side == "before"):
		side := "RIGHT"
		if a.Side == "before" {
			side = "LEFT"
		}
		q = gqlNewThread
		vars = map[string]any{"p": a.Path, "l": a.Line, "s": side, "b": a.Body}
	default:
		return ErrBadAct
	}
	review, err := c.pendingReview(ctx, pr)
	if err != nil {
		return err
	}
	vars["r"] = review
	return c.gql(ctx, q, vars, nil)
}

// OpenMergeRequest implements ReviewSource: a pull request of a pushed branch.
func (c *ghClient) OpenMergeRequest(ctx context.Context, project string, in NewMergeRequest) (string, error) {
	if !ValidRepo(project) || strings.TrimSpace(in.Source) == "" || strings.TrimSpace(in.Target) == "" ||
		strings.TrimSpace(in.Title) == "" || in.Source == in.Target {
		return "", ErrBadAct
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	err := c.send(ctx, http.MethodPost, "repos/"+project+"/pulls", map[string]any{"head": in.Source, "base": in.Target,
		"title": strings.TrimSpace(in.Title), "draft": in.Draft}, &out)
	if errors.Is(err, ErrUnreachable) {
		// 422: a pull request of the branch is open already, or the branch is not pushed.
		return "", fmt.Errorf("%w: a pull request of %s may be open already", err, in.Source)
	}
	if err != nil {
		return "", err
	}
	return out.HTMLURL, nil
}
