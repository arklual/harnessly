package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The page's document reader and documents list, backed by the artifacts catalog. The shapes are the ones the page's
// reader reads (DocSummary, DocVersion, DocDiff, DocAccess); the catalog's own answers are mapped onto them here.

// DocSummary is one document as the page lists it.
type DocSummary struct {
	Slug  string `json:"slug"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// URL is where people read it: the catalog's viewUrl (which may point back to this page's reader).
	URL         string     `json:"url"`
	Bound       bool       `json:"bound"`
	Owner       *DocAuthor `json:"owner"`
	MachineID   *string    `json:"machineId"`
	SourcePath  *string    `json:"sourcePath"`
	OpenThreads int        `json:"openThreads"`
	Unread      int        `json:"unread"`
	// LastActivityAt is the latest of the document's update and its threads' activity (ISO time).
	LastActivityAt string `json:"lastActivityAt"`
	// Session is the conversation that published it, when the catalog knows one.
	Session string `json:"-"`
}

// DocAuthor is who wrote a comment or owns a document.
type DocAuthor struct {
	Kind  string `json:"kind"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// DocVersion is one published version, newest first in lists.
type DocVersion struct {
	N         int     `json:"n"`
	SHA       string  `json:"sha"`
	CreatedAt string  `json:"createdAt"`
	Author    *string `json:"author"`
	Note      *string `json:"note"`
	Current   bool    `json:"current"`
}

// DocDiffBlock is a run of lines that stayed, appeared or went.
type DocDiffBlock struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// DocDiff compares two versions block by block.
type DocDiff struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Blocks []DocDiffBlock `json:"blocks"`
	Stats  struct {
		Added   int `json:"added"`
		Removed int `json:"removed"`
	} `json:"stats"`
}

// DocAccess is a document's public link state.
type DocAccess struct {
	Public    bool    `json:"public"`
	PublicURL *string `json:"publicUrl"`
}

type catalogItem struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Session      string `json:"session"`
	UpdatedAt    string `json:"updatedAt"`
	CreatedAt    string `json:"createdAt"`
	ViewURL      string `json:"viewUrl"`
	SourcePath   string `json:"sourcePath"`
	VersionCount int    `json:"versionCount"`
}

func (it catalogItem) summary() DocSummary {
	s := DocSummary{Slug: it.Slug, Kind: "artifact", Title: it.Title, URL: it.ViewURL, Bound: it.Session != "",
		LastActivityAt: it.UpdatedAt, Session: it.Session}
	if s.LastActivityAt == "" {
		s.LastActivityAt = it.CreatedAt
	}
	if it.SourcePath != "" {
		p := it.SourcePath
		s.SourcePath = &p
	}
	return s
}

func (c Config) readToken() string {
	if c.ArtifactsReadToken != "" {
		return c.ArtifactsReadToken
	}
	return c.ArtifactsToken
}

func (c Config) shareToken() string {
	if c.ArtifactsShareToken != "" {
		return c.ArtifactsShareToken
	}
	return c.ArtifactsToken
}

func docPath(slug string, sub string) string {
	return "/api/v1/artifacts/" + url.PathEscape(slug) + sub
}

// ListDocs is the catalog's newest documents, up to limit.
func (c Config) ListDocs(ctx context.Context, limit int) ([]DocSummary, error) {
	if c.ArtifactsURL == "" {
		return nil, ErrNotConfigured
	}
	var out struct {
		Items []catalogItem `json:"items"`
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+"/api/v1/artifacts?"+q.Encode(), c.readToken(), nil, &out); err != nil {
		return nil, err
	}
	docs := make([]DocSummary, 0, len(out.Items))
	for _, it := range out.Items {
		docs = append(docs, it.summary())
	}
	return docs, nil
}

// Doc is one document's summary.
func (c Config) Doc(ctx context.Context, slug string) (DocSummary, error) {
	if c.ArtifactsURL == "" {
		return DocSummary{}, ErrNotConfigured
	}
	var out struct {
		Artifact catalogItem `json:"artifact"`
	}
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+docPath(slug, ""), c.readToken(), nil, &out); err != nil {
		return DocSummary{}, err
	}
	return out.Artifact.summary(), nil
}

// DocVersions lists a document's versions, newest first, with version ids as `sha`.
func (c Config) DocVersions(ctx context.Context, slug string) ([]DocVersion, error) {
	if c.ArtifactsURL == "" {
		return nil, ErrNotConfigured
	}
	var out struct {
		Versions []struct {
			N         int     `json:"n"`
			ID        string  `json:"id"`
			CreatedAt string  `json:"createdAt"`
			Author    *string `json:"author"`
			Note      *string `json:"note"`
			Current   bool    `json:"current"`
		} `json:"versions"`
	}
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+docPath(slug, "/versions"), c.readToken(), nil, &out); err != nil {
		return nil, err
	}
	versions := make([]DocVersion, 0, len(out.Versions))
	for _, v := range out.Versions {
		versions = append(versions, DocVersion{N: v.N, SHA: v.ID, CreatedAt: v.CreatedAt, Author: v.Author, Note: v.Note, Current: v.Current})
	}
	return versions, nil
}

// DocDiff compares two versions (numbers or ids) of a document.
func (c Config) DocDiff(ctx context.Context, slug, from, to string) (DocDiff, error) {
	var d DocDiff
	if c.ArtifactsURL == "" {
		return d, ErrNotConfigured
	}
	var out struct {
		From   struct{ ID string } `json:"from"`
		To     struct{ ID string } `json:"to"`
		Blocks []DocDiffBlock      `json:"blocks"`
	}
	q := url.Values{"from": {from}, "to": {to}}
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+docPath(slug, "/diff")+"?"+q.Encode(), c.readToken(), nil, &out); err != nil {
		return d, err
	}
	d.From, d.To, d.Blocks = out.From.ID, out.To.ID, out.Blocks
	if d.Blocks == nil {
		d.Blocks = []DocDiffBlock{}
	}
	for _, b := range d.Blocks {
		switch b.Kind {
		case "added":
			d.Stats.Added++
		case "removed":
			d.Stats.Removed++
		}
	}
	return d, nil
}

type accessAnswer struct {
	Public    bool   `json:"public"`
	PublicURL string `json:"publicUrl"`
	ReadLink  *struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	} `json:"readLink"`
}

// DocReadLink is a short-lived read-only address of the document, for the reader's frame.
func (c Config) DocReadLink(ctx context.Context, slug string) (string, time.Time, error) {
	if c.ArtifactsURL == "" {
		return "", time.Time{}, ErrNotConfigured
	}
	var out accessAnswer
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+docPath(slug, "/access"), c.readToken(), nil, &out); err != nil {
		return "", time.Time{}, err
	}
	if out.ReadLink == nil || out.ReadLink.URL == "" {
		return "", time.Time{}, &UpstreamError{Status: http.StatusForbidden, Text: "the read token cannot read this document"}
	}
	at, _ := time.Parse(time.RFC3339, out.ReadLink.ExpiresAt)
	return out.ReadLink.URL, at, nil
}

// DocAccessOf is the document's public link state; open (when not nil) opens or closes the link first.
func (c Config) DocAccessOf(ctx context.Context, slug string, open *bool) (DocAccess, error) {
	if c.ArtifactsURL == "" {
		return DocAccess{}, ErrNotConfigured
	}
	var out accessAnswer
	var err error
	if open == nil {
		err = c.call(ctx, http.MethodGet, c.ArtifactsURL+docPath(slug, "/access"), c.shareToken(), nil, &out)
	} else {
		body, _ := json.Marshal(map[string]bool{"public": *open})
		err = c.call(ctx, http.MethodPut, c.ArtifactsURL+docPath(slug, "/access"), c.shareToken(), body, &out)
	}
	if err != nil {
		return DocAccess{}, err
	}
	a := DocAccess{Public: out.Public}
	if out.Public && out.PublicURL != "" {
		u := out.PublicURL
		a.PublicURL = &u
	}
	return a, nil
}
