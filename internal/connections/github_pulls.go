package connections

import (
	"context"
	"strings"
	"time"
)

// The open pull requests the start of the «Ревью кода» app lists beside GitLab's merge requests: those waiting for the
// person's review, the person's own, and the rest the person takes part in — three searches of one GraphQL call.

// PullSummary is one open pull request of the lists.
type PullSummary struct {
	URL       string
	Number    int
	Title     string
	Project   string
	Source    string
	Target    string
	Author    *Person
	Draft     bool
	Conflicts bool
	Created   time.Time
	Updated   time.Time
	Assignees []string
	Reviewers []string
	Labels    []string
	Comments  int
	// Checks is the state of the head commit's checks: success, failed, pending, or "" without any.
	Checks string
	// Mine: the person opened it; Assigned: it is assigned to them; Asked: their review is requested.
	Mine     bool
	Assigned bool
	Asked    bool
}

// PullLister lists the open pull requests that concern the connection's person.
type PullLister interface {
	OpenPulls(ctx context.Context) (*Person, []PullSummary, error)
}

const ghPullFields = `fragment P on PullRequest{number title url isDraft createdAt updatedAt baseRefName headRefName mergeable
totalCommentsCount author{login avatarUrl ...on User{name}} repository{nameWithOwner}
assignees(first:10){nodes{login name}}
reviewRequests(first:10){nodes{requestedReviewer{...on User{login name}}}}
labels(first:5){nodes{name}}
commits(last:1){nodes{commit{statusCheckRollup{state}}}}}`

const ghPullsQuery = `query($w:String!,$m:String!,$a:String!){viewer{login name avatarUrl}
waiting:search(query:$w,type:ISSUE,first:50){nodes{...P}}
mine:search(query:$m,type:ISSUE,first:50){nodes{...P}}
all:search(query:$a,type:ISSUE,first:50){nodes{...P}}}
` + ghPullFields

type ghLogin struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

func (l ghLogin) shown() string {
	if l.Name != "" {
		return l.Name
	}
	return l.Login
}

type ghPullNode struct {
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	URL                string    `json:"url"`
	IsDraft            bool      `json:"isDraft"`
	CreatedAt          string    `json:"createdAt"`
	UpdatedAt          string    `json:"updatedAt"`
	BaseRefName        string    `json:"baseRefName"`
	HeadRefName        string    `json:"headRefName"`
	Mergeable          string    `json:"mergeable"`
	TotalCommentsCount int       `json:"totalCommentsCount"`
	Author             *gqlActor `json:"author"`
	Repository         struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []ghLogin `json:"nodes"`
	} `json:"assignees"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *ghLogin `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// checksOf is GitHub's state of a commit's checks in the lists' words.
func checksOf(state string) string {
	switch state {
	case "SUCCESS":
		return "success"
	case "FAILURE", "ERROR":
		return "failed"
	case "PENDING", "EXPECTED":
		return "pending"
	}
	return ""
}

func (n ghPullNode) summary(me string) PullSummary {
	p := PullSummary{URL: n.URL, Number: n.Number, Title: n.Title, Project: n.Repository.NameWithOwner,
		Source: n.HeadRefName, Target: n.BaseRefName, Author: n.Author.person(), Draft: n.IsDraft,
		Conflicts: n.Mergeable == "CONFLICTING", Created: ParseTime(n.CreatedAt), Updated: ParseTime(n.UpdatedAt),
		Assignees: []string{}, Reviewers: []string{}, Labels: []string{}, Comments: n.TotalCommentsCount}
	p.Mine = n.Author != nil && strings.EqualFold(n.Author.Login, me)
	for _, a := range n.Assignees.Nodes {
		p.Assignees = append(p.Assignees, a.shown())
		p.Assigned = p.Assigned || strings.EqualFold(a.Login, me)
	}
	for _, r := range n.ReviewRequests.Nodes {
		if r.RequestedReviewer == nil || r.RequestedReviewer.Login == "" {
			continue // a team asked: it names no person
		}
		p.Reviewers = append(p.Reviewers, r.RequestedReviewer.shown())
		p.Asked = p.Asked || strings.EqualFold(r.RequestedReviewer.Login, me)
	}
	for _, l := range n.Labels.Nodes {
		p.Labels = append(p.Labels, l.Name)
	}
	if c := n.Commits.Nodes; len(c) > 0 && c[0].Commit.StatusCheckRollup != nil {
		p.Checks = checksOf(c[0].Commit.StatusCheckRollup.State)
	}
	return p
}

// OpenPulls implements PullLister: each open pull request once, the waiting ones first.
func (c *ghClient) OpenPulls(ctx context.Context) (*Person, []PullSummary, error) {
	const open = "is:pr is:open archived:false "
	var out struct {
		Viewer  gqlActor `json:"viewer"`
		Waiting struct {
			Nodes []ghPullNode `json:"nodes"`
		} `json:"waiting"`
		Mine struct {
			Nodes []ghPullNode `json:"nodes"`
		} `json:"mine"`
		All struct {
			Nodes []ghPullNode `json:"nodes"`
		} `json:"all"`
	}
	vars := map[string]any{"w": open + "review-requested:@me", "m": open + "author:@me", "a": open + "involves:@me"}
	if err := c.gql(ctx, ghPullsQuery, vars, &out); err != nil {
		return nil, nil, err
	}
	me := out.Viewer.Login
	seen := map[string]bool{}
	list := []PullSummary{}
	for i, nodes := range [][]ghPullNode{out.Waiting.Nodes, out.Mine.Nodes, out.All.Nodes} {
		for _, n := range nodes {
			// A search of issues answers {} for a node that is no pull request.
			if n.URL == "" || n.Number < 1 || seen[n.URL] {
				continue
			}
			seen[n.URL] = true
			p := n.summary(me)
			// The search asked for the person's review: a request to a team they are in counts too.
			p.Asked = p.Asked || i == 0
			list = append(list, p)
		}
	}
	return out.Viewer.person(), list, nil
}
