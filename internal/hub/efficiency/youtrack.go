package efficiency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ytPage     = 100
	ytMaxPages = 400
	// ytPause is the pause before every request after the first of a pass: the tracker is shared with the people, so a
	// pass reads at most a few pages a second. A 429 or any other failure ends the pass; the collector backs off.
	ytPause = 300 * time.Millisecond
	// KindProgress is the source of the issues in progress: one fact per issue with the time of its last activity.
	KindProgress = "tracker-progress"
	// CommentKeep is how long the facts of comments are kept.
	CommentKeep = 40 * 24 * time.Hour
)

// Progress is an issue in progress and the time of its last comment or change of State.
type Progress struct {
	Issue          string    `json:"issue"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

// ProgressTracker is a Tracker that can also list the issues in progress.
type ProgressTracker interface {
	InProgress(ctx context.Context) ([]Progress, error)
}

// YouTrack is a Tracker over the YouTrack REST API, restricted to one project.
type YouTrack struct {
	URL     string
	Token   string
	Project string
	Client  *http.Client
}

// NewYouTrack returns the provider; nil when the source is not configured.
func NewYouTrack(rawURL, token, project string) *YouTrack {
	if rawURL == "" || token == "" || project == "" {
		return nil
	}
	return &YouTrack{URL: strings.TrimRight(rawURL, "/"), Token: token, Project: project, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (y *YouTrack) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.URL+"/api"+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+y.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := y.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), y.Token, "***"))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), y.Token, "***"))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("YouTrack answered %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("YouTrack answer is not the expected list")
	}
	return nil
}

func (y *YouTrack) paged(ctx context.Context, path string, q url.Values, each func(json.RawMessage) error) error {
	for page := 0; page < ytMaxPages; page++ {
		if page > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(ytPause):
			}
		}
		q.Set("$top", strconv.Itoa(ytPage))
		q.Set("$skip", strconv.Itoa(page*ytPage))
		var rows []json.RawMessage
		if err := y.get(ctx, path, q, &rows); err != nil {
			return err
		}
		for _, r := range rows {
			if err := each(r); err != nil {
				return err
			}
		}
		if len(rows) < ytPage {
			return nil
		}
	}
	return fmt.Errorf("%s: more than %d pages", path, ytMaxPages)
}

// Comments lists the comments added to the project's issues within [from, to).
func (y *YouTrack) Comments(ctx context.Context, from, to time.Time) ([]Comment, error) {
	var out []Comment
	q := url.Values{"categories": {"CommentsCategory"}, "start": {strconv.FormatInt(from.UnixMilli(), 10)}, "end": {strconv.FormatInt(to.UnixMilli(), 10)},
		"fields": {"timestamp,author(login),added(id,text,issue(idReadable))"}}
	err := y.paged(ctx, "/activities", q, func(raw json.RawMessage) error {
		var a struct {
			Timestamp int64 `json:"timestamp"`
			Author    *struct {
				Login string `json:"login"`
			} `json:"author"`
			Added []struct {
				Text  *string `json:"text"`
				Issue *struct {
					ID string `json:"idReadable"`
				} `json:"issue"`
			} `json:"added"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return err
		}
		for _, c := range a.Added {
			if c.Issue == nil || !strings.HasPrefix(c.Issue.ID, y.Project+"-") {
				continue
			}
			m := Comment{Issue: c.Issue.ID, At: time.UnixMilli(a.Timestamp)}
			if a.Author != nil {
				m.Author = a.Author.Login
			}
			if c.Text != nil {
				m.Head = *c.Text
			}
			out = append(out, m)
		}
		return nil
	})
	return out, err
}

// InProgress lists the project's issues in the State «In Progress» with the time of their last comment or State change.
func (y *YouTrack) InProgress(ctx context.Context) ([]Progress, error) {
	type issue struct {
		ID      string `json:"idReadable"`
		Created int64  `json:"created"`
	}
	var issues []issue
	err := y.paged(ctx, "/issues", url.Values{"query": {"project: " + y.Project + " State: {In Progress}"}, "fields": {"idReadable,created"}}, func(raw json.RawMessage) error {
		var i issue
		if err := json.Unmarshal(raw, &i); err != nil {
			return err
		}
		issues = append(issues, i)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Progress, 0, len(issues))
	for _, i := range issues {
		var acts []struct {
			Timestamp int64 `json:"timestamp"`
			Category  struct {
				ID string `json:"id"`
			} `json:"category"`
			Field *struct {
				Name string `json:"name"`
			} `json:"field"`
		}
		q := url.Values{"categories": {"CommentsCategory,CustomFieldCategory"}, "fields": {"timestamp,category(id),field(name)"}, "reverse": {"true"}, "$top": {"100"}}
		if err := y.get(ctx, "/issues/"+url.PathEscape(i.ID)+"/activities", q, &acts); err != nil {
			return out, err
		}
		last := i.Created
		for _, a := range acts {
			if (a.Category.ID == "CommentsCategory" || (a.Field != nil && a.Field.Name == "State")) && a.Timestamp > last {
				last = a.Timestamp
			}
		}
		out = append(out, Progress{Issue: i.ID, LastActivityAt: time.UnixMilli(last)})
	}
	return out, nil
}
