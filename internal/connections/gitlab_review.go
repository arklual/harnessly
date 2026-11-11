package connections

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The review of a merge request in the «Ревью кода» app: its discussions at their lines, the person's draft notes
// (GitLab's own pending review), the approvals, and what the person does there — reply, edit or delete their note,
// resolve, add a draft, send the review, approve, take the draft mark off, open a merge request. The truth stays in
// GitLab: nothing here is kept, every answer is read again from it.

// ReviewSource is a code host's review of a merge request.
type ReviewSource interface {
	Review(ctx context.Context, project string, iid int) (*Review, error)
	// ReviewAct does one thing in the review and answers the review as it is after it.
	ReviewAct(ctx context.Context, project string, iid int, act ReviewAct) (*Review, error)
	// OpenMergeRequest opens a merge request of a pushed branch and answers its link.
	OpenMergeRequest(ctx context.Context, project string, in NewMergeRequest) (string, error)
}

// Review is a merge request's review as the connection's person sees it.
type Review struct {
	Me *Person `json:"me"`
	// Mine: the person opened the merge request.
	Mine  bool   `json:"mine"`
	Title string `json:"title"`
	State string `json:"state"`
	Draft bool   `json:"draft"`
	// Approved: the person approved it; CanApprove: they may.
	Approved   bool           `json:"approved"`
	CanApprove bool           `json:"canApprove"`
	ApprovedBy []string       `json:"approvedBy"`
	Refs       ReviewRefs     `json:"refs"`
	Threads    []ReviewThread `json:"threads"`
	Drafts     []ReviewDraft  `json:"drafts"`
}

// ReviewRefs are the commits the merge request's diff is taken between; a new note is placed on them.
type ReviewRefs struct {
	Base  string `json:"base"`
	Start string `json:"start"`
	Head  string `json:"head"`
}

// ReviewThread is one discussion, at a line of a file or on the merge request as a whole (Path empty).
type ReviewThread struct {
	ID         string `json:"id"`
	Resolvable bool   `json:"resolvable"`
	Resolved   bool   `json:"resolved"`
	Path       string `json:"path,omitempty"`
	Line       int    `json:"line,omitempty"`
	// Side is "after" (a line of the new text) or "before".
	Side string `json:"side,omitempty"`
	// Outdated: placed on an older version of the diff.
	Outdated bool         `json:"outdated,omitempty"`
	Notes    []ReviewNote `json:"notes"`
}

// ReviewNote is one note of a thread.
type ReviewNote struct {
	ID     int     `json:"id"`
	Author *Person `json:"author,omitempty"`
	// Mine: written by the person, who may edit or delete it.
	Mine    bool      `json:"mine"`
	Body    string    `json:"body"`
	Created time.Time `json:"created,omitzero"`
	Edited  bool      `json:"edited,omitempty"`
}

// ReviewDraft is a note of the person's review not sent yet: a new thread at a line, or a reply to a thread.
type ReviewDraft struct {
	ID      int    `json:"id"`
	Body    string `json:"body"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Side    string `json:"side,omitempty"`
	ReplyTo string `json:"replyTo,omitempty"`
}

// Review acts.
const (
	ActReply       = "reply"
	ActEdit        = "edit"
	ActDelete      = "delete"
	ActResolve     = "resolve"
	ActDraft       = "draft"
	ActDraftEdit   = "draft-edit"
	ActDraftDelete = "draft-delete"
	ActPublish     = "publish"
	ActApprove     = "approve"
	ActUnapprove   = "unapprove"
	ActReady       = "ready"
)

// ReviewAct is one thing the person does in a review.
type ReviewAct struct {
	Op     string `json:"op"`
	Thread string `json:"thread,omitempty"`
	Note   int    `json:"note,omitempty"`
	Draft  int    `json:"draft,omitempty"`
	Body   string `json:"body,omitempty"`
	// Where a new draft thread goes: a file's line on a side, at the refs the person saw.
	Path    string `json:"path,omitempty"`
	OldPath string `json:"oldPath,omitempty"`
	Line    int    `json:"line,omitempty"`
	Side    string `json:"side,omitempty"`
	// Paired is the same line's number on the other side, for a line the change did not touch (GitLab wants both).
	Paired   int        `json:"paired,omitempty"`
	Refs     ReviewRefs `json:"refs,omitzero"`
	Resolved bool       `json:"resolved,omitempty"`
}

// NewMergeRequest is a merge request to open from a pushed branch.
type NewMergeRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Title  string `json:"title"`
	Draft  bool   `json:"draft"`
}

// ErrBadAct is an act that misses what it needs.
var ErrBadAct = fmt.Errorf("%w: review act", ErrUnsupported)

// ReviewOf opens a connection's review source.
func (s *Service) ReviewOf(c Conn) (ReviewSource, error) {
	cl, err := s.Open(c)
	if err != nil {
		return nil, err
	}
	src, ok := cl.(ReviewSource)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no reviews", ErrUnsupported, c.Kind)
	}
	return src, nil
}

// glMRLinkRe reads a merge request link below the instance root: group/sub/project/-/merge_requests/N.
var glMRLinkRe = regexp.MustCompile(`^/(.+?)/-/merge_requests/(\d+)(?:/.*)?$`)

// MergeRequestOf reads a merge request link of a connection's instance (a pull request's on GitHub): its project path
// and number.
func MergeRequestOf(c Conn, raw string) (string, int, bool) {
	u, err := url.Parse(raw)
	if err == nil && c.Kind == GitHub {
		return pullRequestOf(c, u)
	}
	base, berr := url.Parse(c.BaseURL)
	if err != nil || berr != nil || !strings.EqualFold(u.Host, base.Host) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(u.Path, strings.TrimRight(base.Path, "/"))
	m := glMRLinkRe.FindStringSubmatch(rest)
	if m == nil {
		return "", 0, false
	}
	iid, err := strconv.Atoi(m[2])
	if err != nil || iid < 1 {
		return "", 0, false
	}
	return m[1], iid, true
}

const reviewBodyMax = 64 << 10

func (c *glClient) send(ctx context.Context, method, path, query string, body, out any) error {
	if c.token == "" {
		return ErrDenied
	}
	u := c.base + "/api/v4/" + strings.TrimLeft(path, "/")
	if query != "" {
		u += "?" + query
	}
	res, err := c.h.Do(ctx, Request{Method: method, URL: u, Header: Header("PRIVATE-TOKEN", c.token), Body: body})
	if err != nil {
		return err
	}
	if res.Status == http.StatusForbidden && strings.Contains(string(res.Body), "insufficient_scope") {
		return fmt.Errorf("%w: the token lacks the api scope", ErrDenied)
	}
	if err := StatusErr(res); err != nil {
		return err
	}
	if out != nil && len(res.Body) > 0 {
		return res.Decode(out)
	}
	return nil
}

func mrPath(project string, iid int) string {
	return "projects/" + url.PathEscape(project) + "/merge_requests/" + strconv.Itoa(iid)
}

type glPosition struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
	OldPath  string `json:"old_path"`
	NewPath  string `json:"new_path"`
	OldLine  *int   `json:"old_line"`
	NewLine  *int   `json:"new_line"`
}

type glNote struct {
	ID         int         `json:"id"`
	Body       string      `json:"body"`
	Author     *glUser     `json:"author"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
	System     bool        `json:"system"`
	Resolvable bool        `json:"resolvable"`
	Resolved   bool        `json:"resolved"`
	Position   *glPosition `json:"position"`
}

type glDiscussion struct {
	ID    string   `json:"id"`
	Notes []glNote `json:"notes"`
}

type glDraft struct {
	ID           int         `json:"id"`
	Note         string      `json:"note"`
	DiscussionID string      `json:"discussion_id"`
	Position     *glPosition `json:"position"`
}

// placeOf is a position's file, line and side.
func placeOf(p *glPosition) (string, int, string) {
	if p == nil {
		return "", 0, ""
	}
	if p.NewLine != nil {
		return p.NewPath, *p.NewLine, "after"
	}
	if p.OldLine != nil {
		return p.OldPath, *p.OldLine, "before"
	}
	return p.NewPath, 0, ""
}

const reviewPagesMax = 10

// Review implements ReviewSource.
func (c *glClient) Review(ctx context.Context, project string, iid int) (*Review, error) {
	var me glUser
	if _, err := c.get(ctx, "user", "", &me); err != nil {
		return nil, err
	}
	var mr struct {
		Title    string  `json:"title"`
		State    string  `json:"state"`
		Draft    bool    `json:"draft"`
		Author   *glUser `json:"author"`
		DiffRefs *struct {
			BaseSHA  string `json:"base_sha"`
			StartSHA string `json:"start_sha"`
			HeadSHA  string `json:"head_sha"`
		} `json:"diff_refs"`
	}
	if _, err := c.get(ctx, mrPath(project, iid), "", &mr); err != nil {
		return nil, err
	}
	out := &Review{Me: c.person(&me), Title: mr.Title, State: mr.State, Draft: mr.Draft,
		Mine: mr.Author != nil && mr.Author.ID == me.ID, ApprovedBy: []string{}, Threads: []ReviewThread{},
		Drafts: []ReviewDraft{}}
	if mr.DiffRefs != nil {
		out.Refs = ReviewRefs{Base: mr.DiffRefs.BaseSHA, Start: mr.DiffRefs.StartSHA, Head: mr.DiffRefs.HeadSHA}
	}
	var approvals struct {
		UserHasApproved bool `json:"user_has_approved"`
		UserCanApprove  bool `json:"user_can_approve"`
		ApprovedBy      []struct {
			User *glUser `json:"user"`
		} `json:"approved_by"`
	}
	if _, err := c.get(ctx, mrPath(project, iid)+"/approvals", "", &approvals); err == nil {
		out.Approved, out.CanApprove = approvals.UserHasApproved, approvals.UserCanApprove
		for _, a := range approvals.ApprovedBy {
			if a.User != nil {
				out.ApprovedBy = append(out.ApprovedBy, a.User.Name)
			}
		}
	}
	for page := 1; page <= reviewPagesMax; page++ {
		var list []glDiscussion
		res, err := c.get(ctx, mrPath(project, iid)+"/discussions", Q("per_page", "100", "page", strconv.Itoa(page)), &list)
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			if t, ok := c.thread(d, me.ID, out.Refs.Head); ok {
				out.Threads = append(out.Threads, t)
			}
		}
		if res == nil || res.Header.Get("X-Next-Page") == "" {
			break
		}
	}
	var drafts []glDraft
	if _, err := c.get(ctx, mrPath(project, iid)+"/draft_notes", "", &drafts); err == nil {
		for _, d := range drafts {
			path, line, side := placeOf(d.Position)
			out.Drafts = append(out.Drafts, ReviewDraft{ID: d.ID, Body: d.Note, Path: path, Line: line, Side: side,
				ReplyTo: d.DiscussionID})
		}
	}
	return out, nil
}

func (c *glClient) thread(d glDiscussion, me int, head string) (ReviewThread, bool) {
	t := ReviewThread{ID: d.ID, Notes: []ReviewNote{}}
	for i, n := range d.Notes {
		if n.System {
			continue
		}
		if i == 0 {
			t.Resolvable, t.Resolved = n.Resolvable, n.Resolved
			t.Path, t.Line, t.Side = placeOf(n.Position)
			t.Outdated = n.Position != nil && head != "" && n.Position.HeadSHA != "" && n.Position.HeadSHA != head
		}
		t.Notes = append(t.Notes, ReviewNote{ID: n.ID, Author: c.person(n.Author), Mine: n.Author != nil && n.Author.ID == me,
			Body: n.Body, Created: ParseTime(n.CreatedAt), Edited: n.UpdatedAt != "" && n.UpdatedAt != n.CreatedAt})
	}
	return t, len(t.Notes) > 0
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

func validBody(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= reviewBodyMax
}

// ReviewAct implements ReviewSource.
func (c *glClient) ReviewAct(ctx context.Context, project string, iid int, a ReviewAct) (*Review, error) {
	mr := mrPath(project, iid)
	thread := func() string { return mr + "/discussions/" + url.PathEscape(a.Thread) }
	var err error
	switch a.Op {
	case ActReply:
		if a.Thread == "" || !validBody(a.Body) {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodPost, thread()+"/notes", "", map[string]string{"body": a.Body}, nil)
	case ActEdit:
		if a.Thread == "" || a.Note < 1 || !validBody(a.Body) {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodPut, thread()+"/notes/"+strconv.Itoa(a.Note), "", map[string]string{"body": a.Body}, nil)
	case ActDelete:
		if a.Thread == "" || a.Note < 1 {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodDelete, thread()+"/notes/"+strconv.Itoa(a.Note), "", nil, nil)
	case ActResolve:
		if a.Thread == "" {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodPut, thread(), Q("resolved", strconv.FormatBool(a.Resolved)), nil, nil)
	case ActDraft:
		body, ok := draftBody(a)
		if !ok {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodPost, mr+"/draft_notes", "", body, nil)
	case ActDraftEdit:
		if a.Draft < 1 || !validBody(a.Body) {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodPut, mr+"/draft_notes/"+strconv.Itoa(a.Draft), "", map[string]string{"note": a.Body}, nil)
	case ActDraftDelete:
		if a.Draft < 1 {
			return nil, ErrBadAct
		}
		err = c.send(ctx, http.MethodDelete, mr+"/draft_notes/"+strconv.Itoa(a.Draft), "", nil, nil)
	case ActPublish:
		err = c.send(ctx, http.MethodPost, mr+"/draft_notes/bulk_publish", "", nil, nil)
	case ActApprove:
		err = c.send(ctx, http.MethodPost, mr+"/approve", "", nil, nil)
	case ActUnapprove:
		err = c.send(ctx, http.MethodPost, mr+"/unapprove", "", nil, nil)
	case ActReady:
		var cur struct {
			Title string `json:"title"`
		}
		if _, err := c.get(ctx, mr, "", &cur); err != nil {
			return nil, err
		}
		err = c.send(ctx, http.MethodPut, mr, "", map[string]string{"title": withoutDraftMark(cur.Title)}, nil)
	default:
		return nil, ErrBadAct
	}
	if err != nil {
		return nil, err
	}
	return c.Review(ctx, project, iid)
}

// draftBody is the draft note GitLab is asked to keep: a reply to a thread, or a new thread at a line.
func draftBody(a ReviewAct) (map[string]any, bool) {
	if !validBody(a.Body) {
		return nil, false
	}
	if a.Thread != "" {
		return map[string]any{"note": a.Body, "in_reply_to_discussion_id": a.Thread}, true
	}
	if a.Path == "" || a.Line < 1 || !shaRe.MatchString(a.Refs.Base) || !shaRe.MatchString(a.Refs.Head) ||
		!shaRe.MatchString(a.Refs.Start) {
		return nil, false
	}
	old := a.OldPath
	if old == "" {
		old = a.Path
	}
	pos := map[string]any{"position_type": "text", "base_sha": a.Refs.Base, "start_sha": a.Refs.Start,
		"head_sha": a.Refs.Head, "new_path": a.Path, "old_path": old}
	switch a.Side {
	case "after":
		pos["new_line"] = a.Line
		if a.Paired > 0 {
			pos["old_line"] = a.Paired
		}
	case "before":
		pos["old_line"] = a.Line
		if a.Paired > 0 {
			pos["new_line"] = a.Paired
		}
	default:
		return nil, false
	}
	return map[string]any{"note": a.Body, "position": pos}, true
}

var draftMarkRe = regexp.MustCompile(`(?i)^\s*(\[draft\]|\(draft\)|draft:|draft\s+-|wip:|\[wip\])\s*`)

// withoutDraftMark is a title without GitLab's draft mark, which is what takes a merge request out of draft.
func withoutDraftMark(title string) string {
	for {
		next := draftMarkRe.ReplaceAllString(title, "")
		if next == title {
			return title
		}
		title = next
	}
}

// OpenMergeRequest implements ReviewSource.
func (c *glClient) OpenMergeRequest(ctx context.Context, project string, in NewMergeRequest) (string, error) {
	if strings.TrimSpace(in.Source) == "" || strings.TrimSpace(in.Target) == "" || strings.TrimSpace(in.Title) == "" ||
		in.Source == in.Target {
		return "", ErrBadAct
	}
	title := strings.TrimSpace(in.Title)
	if in.Draft {
		title = "Draft: " + withoutDraftMark(title)
	}
	var out struct {
		WebURL string `json:"web_url"`
	}
	err := c.send(ctx, http.MethodPost, "projects/"+url.PathEscape(project)+"/merge_requests", "",
		map[string]any{"source_branch": in.Source, "target_branch": in.Target, "title": title,
			"remove_source_branch": true}, &out)
	if errors.Is(err, ErrUnreachable) {
		// 409: a merge request of the branch is open already.
		return "", fmt.Errorf("%w: a merge request of %s may be open already", err, in.Source)
	}
	if err != nil {
		return "", err
	}
	return out.WebURL, nil
}
