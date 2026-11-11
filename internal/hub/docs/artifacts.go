package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// artifactsClient talks to the documents service (github.com/ly-solutions/artifacts) with an editor token. The hub is
// the only client: people and agents never see the token.
type artifactsClient struct {
	base  string
	token string
	hc    *http.Client
}

func newArtifacts(base, token string, hc *http.Client) *artifactsClient {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &artifactsClient{base: strings.TrimRight(base, "/"), token: token, hc: hc}
}

// upstreamError is a refusal of the documents service, with its status and its words.
type upstreamError struct {
	Status int
	Code   string
	Text   string
	Body   json.RawMessage
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("documents service answered %d %s", e.Status, e.Code)
}

const maxAnswer = 8 << 20

func (c *artifactsClient) do(ctx context.Context, method, path string, in any, out any) error {
	res, err := c.raw(ctx, method, path, in, "application/json")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		e := &upstreamError{Status: res.StatusCode, Body: b}
		var said struct {
			Error    string   `json:"error"`
			Problems []string `json:"problems"`
		}
		if json.Unmarshal(b, &said) == nil {
			e.Code, e.Text = said.Error, strings.Join(said.Problems, "; ")
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// raw sends a request and returns the answer for the caller to read; the body of an error answer is the caller's too.
func (c *artifactsClient) raw(ctx context.Context, method, path string, in any, accept string) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.hc.Do(req)
}

type artifact struct {
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	Author         string   `json:"author"`
	Public         bool     `json:"public"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	VersionCount   int      `json:"versionCount"`
	CurrentVersion *version `json:"currentVersion"`
	ViewURL        string   `json:"viewUrl"`
}

type version struct {
	N         int      `json:"n"`
	ID        string   `json:"id"`
	CreatedAt string   `json:"createdAt"`
	Author    string   `json:"author"`
	Note      string   `json:"note"`
	Formats   []string `json:"formats"`
	Current   bool     `json:"current"`
}

func (c *artifactsClient) list(ctx context.Context) ([]artifact, error) {
	var out struct {
		Items      []artifact `json:"items"`
		NextOffset *int       `json:"nextOffset"`
	}
	var all []artifact
	for off := 0; len(all) < 2000; {
		if err := c.do(ctx, "GET", fmt.Sprintf("/api/v1/artifacts?limit=500&offset=%d", off), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		if out.NextOffset == nil {
			break
		}
		off = *out.NextOffset
	}
	return all, nil
}

func (c *artifactsClient) get(ctx context.Context, slug string) (*artifact, []version, error) {
	var out struct {
		Artifact struct {
			artifact
			Versions []version `json:"versions"`
		} `json:"artifact"`
	}
	if err := c.do(ctx, "GET", "/api/v1/artifacts/"+url.PathEscape(slug), nil, &out); err != nil {
		return nil, nil, err
	}
	a := out.Artifact.artifact
	return &a, out.Artifact.Versions, nil
}

type publishIn struct {
	Title       string  `json:"title"`
	HTML        string  `json:"html,omitempty"`
	Markdown    string  `json:"markdown,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Note        *string `json:"note,omitempty"`
	Author      string  `json:"author,omitempty"`
	BaseVersion *int    `json:"baseVersion,omitempty"`
	New         bool    `json:"new,omitempty"`
}

type publishOut struct {
	Created  bool     `json:"created"`
	Version  version  `json:"version"`
	Artifact artifact `json:"artifact"`
}

func (c *artifactsClient) publish(ctx context.Context, in publishIn) (*publishOut, error) {
	var out publishOut
	if err := c.do(ctx, "POST", "/api/v1/artifacts", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *artifactsClient) setPublic(ctx context.Context, slug string, open bool) (publicURL string, public bool, err error) {
	var out struct {
		Public    bool   `json:"public"`
		PublicURL string `json:"publicUrl"`
	}
	err = c.do(ctx, "PUT", "/api/v1/artifacts/"+url.PathEscape(slug)+"/access", map[string]bool{"public": open}, &out)
	return out.PublicURL, out.Public, err
}

func (c *artifactsClient) access(ctx context.Context, slug string) (publicURL string, public bool, err error) {
	var out struct {
		Public    bool   `json:"public"`
		PublicURL string `json:"publicUrl"`
	}
	err = c.do(ctx, "GET", "/api/v1/artifacts/"+url.PathEscape(slug)+"/access", nil, &out)
	return out.PublicURL, out.Public, err
}

// markdown returns the Markdown of a version; ok is false when the version has none (a page of its own).
func (c *artifactsClient) markdown(ctx context.Context, slug, v string) (md string, n int, ok bool, err error) {
	q := "?format=json"
	if v != "" {
		q += "&v=" + url.QueryEscape(v)
	}
	// The service names the version as an object ({"n": 2, "id": "…"}), not a bare number.
	var out struct {
		Version struct {
			N int `json:"n"`
		} `json:"version"`
		HTML     string `json:"html"`
		Markdown string `json:"markdown"`
	}
	if err = c.do(ctx, "GET", "/api/v1/artifacts/"+url.PathEscape(slug)+"/content"+q, nil, &out); err != nil {
		return
	}
	return out.Markdown, out.Version.N, out.Markdown != "" && out.HTML == "", nil
}

func (c *artifactsClient) diff(ctx context.Context, slug, from, to string) (json.RawMessage, error) {
	var out json.RawMessage
	q := url.Values{"from": {from}, "to": {to}, "source": {"text"}}
	err := c.do(ctx, "GET", "/api/v1/artifacts/"+url.PathEscape(slug)+"/diff?"+q.Encode(), nil, &out)
	return out, err
}

// page fetches the rendered document alone (the reader's /raw), for the hub to serve inside the viewer's frame.
func (c *artifactsClient) page(ctx context.Context, slug, v, theme string) (*http.Response, error) {
	q := url.Values{}
	if v != "" {
		q.Set("v", v)
	}
	if theme == "light" || theme == "dark" {
		q.Set("theme", theme)
	}
	p := "/a/" + url.PathEscape(slug) + "/raw"
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return c.raw(ctx, "GET", p, nil, "text/html")
}

func isStatus(err error, status int) bool {
	var e *upstreamError
	return errors.As(err, &e) && e.Status == status
}
