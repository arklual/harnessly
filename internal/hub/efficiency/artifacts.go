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
	// KindArtifact is the source of the documents agents published: one fact per document, at its first version.
	KindArtifact = "artifacts"
	// CollectorArtifacts is the name of the collector in eff_source_state.
	CollectorArtifacts = "artifacts"
	// ArtifactsWindow is how far back the first publications are read.
	ArtifactsWindow = 60 * 24 * time.Hour

	artifactsPage  = 500
	artifactsPages = 40
)

// Artifact is a document on its first publication: who published it and which task it belongs to.
type Artifact struct {
	Slug        string    `json:"slug"`
	At          time.Time `json:"at"`
	AuthorEmail string    `json:"author_email"`
	AuthorName  string    `json:"author_name"`
	Task        string    `json:"task"`
}

// ArtifactSource is a source of the documents published since a time.
type ArtifactSource interface {
	Published(ctx context.Context, since time.Time) ([]Artifact, error)
}

// Artifacts is an ArtifactSource over the read API of the documents service.
type Artifacts struct {
	URL    string
	Token  string
	Client *http.Client
}

// NewArtifacts returns the provider; nil when the source is not configured.
func NewArtifacts(rawURL, token string) *Artifacts {
	if rawURL == "" || token == "" {
		return nil
	}
	return &Artifacts{URL: strings.TrimRight(rawURL, "/"), Token: token, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (a *Artifacts) get(ctx context.Context, path string, q url.Values, out any) error {
	u := a.URL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), a.Token, "***"))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), a.Token, "***"))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("documents service answered %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("documents service answer is not the expected JSON")
	}
	return nil
}

// Published lists the documents created since a time with the author of their first version. A document whose versions
// cannot be read is skipped and the first error is returned with the rest.
func (a *Artifacts) Published(ctx context.Context, since time.Time) ([]Artifact, error) {
	type item struct {
		Slug      string  `json:"slug"`
		Task      *string `json:"task"`
		CreatedAt string  `json:"createdAt"`
	}
	var items []item
	offset := 0
	for n := 0; ; n++ {
		if n >= artifactsPages {
			return nil, fmt.Errorf("documents service: more than %d pages", artifactsPages)
		}
		var page struct {
			Items      []item `json:"items"`
			NextOffset *int   `json:"nextOffset"`
		}
		if err := a.get(ctx, "/api/v1/artifacts", url.Values{"limit": {strconv.Itoa(artifactsPage)}, "offset": {strconv.Itoa(offset)}}, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextOffset == nil {
			break
		}
		if *page.NextOffset <= offset {
			return nil, fmt.Errorf("documents service: nextOffset does not move")
		}
		offset = *page.NextOffset
	}
	var out []Artifact
	var first error
	for _, it := range items {
		created, err := time.Parse(time.RFC3339, it.CreatedAt)
		if err != nil || created.Before(since) {
			continue
		}
		var vs struct {
			Versions []artifactVersion `json:"versions"`
		}
		if err := a.get(ctx, "/api/v1/artifacts/"+url.PathEscape(it.Slug)+"/versions", nil, &vs); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		art := Artifact{Slug: it.Slug, At: created}
		if it.Task != nil {
			art.Task = *it.Task
		}
		if v, ok := firstVersion(vs.Versions); ok {
			if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil {
				art.At = t
			}
			if v.Author != nil {
				art.AuthorName = *v.Author
				if strings.Contains(*v.Author, "@") {
					art.AuthorEmail = *v.Author
				}
			}
		}
		out = append(out, art)
	}
	return out, first
}

type artifactVersion struct {
	N         int     `json:"n"`
	CreatedAt string  `json:"createdAt"`
	Author    *string `json:"author"`
}

// firstVersion is the version with the lowest number.
func firstVersion(vs []artifactVersion) (artifactVersion, bool) {
	var best artifactVersion
	found := false
	for _, v := range vs {
		if !found || v.N < best.N {
			best, found = v, true
		}
	}
	return best, found
}
