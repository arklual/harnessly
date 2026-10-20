// Package integrations connects the runtime to the sibling services people and agents share with it: a boards server
// (whiteboards) and an artifacts catalog (documents). Agents get the services' addresses and tokens in their
// environment; the page embeds the services and asks the runtime for what needs a server-side token (a board sign-in
// hand-off, the documents of a conversation). Tokens never reach the browser.
package integrations

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

// Config is the deployment's view of the services; an empty URL leaves that service out.
type Config struct {
	// BoardsURL is the boards server's origin (boards lives under /b on it).
	BoardsURL string
	// BoardsToken is the runtime's API token: it mints sign-in hand-offs. BoardsAgentToken goes to agents; empty, they
	// get BoardsToken.
	BoardsToken, BoardsAgentToken string
	// ArtifactsURL is the catalog's origin.
	ArtifactsURL string
	// ArtifactsToken is the editor token agents publish with; ArtifactsReadToken the runtime's viewer token for listing a
	// conversation's documents (ArtifactsToken when empty).
	ArtifactsToken, ArtifactsReadToken string
	// ArtifactsShareToken opens and closes a document's public link from the reader (ArtifactsToken when empty).
	ArtifactsShareToken string
	// Author is who agents publish as (ARTIFACTS_AUTHOR): RUNTIME_ARTIFACTS_AUTHOR, else "<RUNTIME_OWNER_NAME> (agent)";
	// empty leaves it to the CLI.
	Author string
	// Client calls the services; nil is a client with a 10 s timeout.
	Client *http.Client
}

// FromEnv reads the RUNTIME_BOARDS_* and RUNTIME_ARTIFACTS_* variables.
func FromEnv(getenv func(string) string) Config {
	c := Config{
		BoardsURL:           origin(getenv("RUNTIME_BOARDS_URL")),
		BoardsToken:         strings.TrimSpace(getenv("RUNTIME_BOARDS_TOKEN")),
		BoardsAgentToken:    strings.TrimSpace(getenv("RUNTIME_BOARDS_AGENT_TOKEN")),
		ArtifactsURL:        origin(getenv("RUNTIME_ARTIFACTS_URL")),
		ArtifactsToken:      strings.TrimSpace(getenv("RUNTIME_ARTIFACTS_TOKEN")),
		ArtifactsReadToken:  strings.TrimSpace(getenv("RUNTIME_ARTIFACTS_READ_TOKEN")),
		ArtifactsShareToken: strings.TrimSpace(getenv("RUNTIME_ARTIFACTS_SHARE_TOKEN")),
		Author:              strings.TrimSpace(getenv("RUNTIME_ARTIFACTS_AUTHOR")),
	}
	if name := strings.TrimSpace(getenv("RUNTIME_OWNER_NAME")); c.Author == "" && name != "" {
		c.Author = name + " (agent)"
	}
	return c
}

// origin trims a configured address to scheme://host[:port]; anything that is not an http(s) URL is "".
func origin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// Enabled says whether any service is configured.
func (c Config) Enabled() bool { return c.BoardsURL != "" || c.ArtifactsURL != "" }

// AgentEnv is what an agent tab's shell gets, as KEY=VALUE: the services' addresses and tokens, and the conversation
// its documents belong to (the tab's id; "" leaves ARTIFACTS_SESSION out).
func (c Config) AgentEnv(conversation string) []string {
	var out []string
	add := func(k, v string) {
		if v != "" {
			out = append(out, k+"="+v)
		}
	}
	if c.BoardsURL != "" {
		add("BOARDS_URL", c.BoardsURL)
		if c.BoardsAgentToken != "" {
			add("BOARDS_TOKEN", c.BoardsAgentToken)
		} else {
			add("BOARDS_TOKEN", c.BoardsToken)
		}
	}
	if c.ArtifactsURL != "" {
		add("ARTIFACTS_URL", c.ArtifactsURL)
		add("ARTIFACTS_TOKEN", c.ArtifactsToken)
		add("ARTIFACTS_SESSION", conversation)
		add("ARTIFACTS_AUTHOR", c.Author)
	}
	return out
}

// Service is what the page learns about one service: where it is.
type Service struct {
	URL string `json:"url"`
}

// Public is the page's view: the services' origins, nil for one that is not configured. No tokens.
type Public struct {
	Boards    *Service `json:"boards"`
	Artifacts *Service `json:"artifacts"`
}

// Public is what the page may know.
func (c Config) Public() Public {
	var p Public
	if c.BoardsURL != "" {
		p.Boards = &Service{URL: c.BoardsURL}
	}
	if c.ArtifactsURL != "" {
		p.Artifacts = &Service{URL: c.ArtifactsURL}
	}
	return p
}

// User is the person a board hand-off signs in.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// ErrNotConfigured: the service the call needs has no address.
var ErrNotConfigured = errors.New("not configured")

// ErrBadPath: a hand-off target that is not a boards path.
var ErrBadPath = errors.New("the target must be a boards path (/b/…)")

// UpstreamError is a service's refusal, its status and its error text.
type UpstreamError struct {
	Status int
	Text   string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("the service answered %d: %s", e.Status, e.Text)
}

func (c Config) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// BoardPath checks a hand-off target: a path under /b/ on the boards server, with its query.
func BoardPath(next string) (string, error) {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/b/") || strings.Contains(u.Path, "..") {
		return "", ErrBadPath
	}
	return u.RequestURI(), nil
}

// BoardSignIn is the address the page loads in the board pane for next: a one-time hand-off that signs the person in
// to boards and lands on next, or next itself when the runtime holds no boards token (boards then signs people in its
// own way: a shared identity provider or proxy, or no auth at all).
func (c Config) BoardSignIn(ctx context.Context, who User, next string) (string, error) {
	if c.BoardsURL == "" {
		return "", ErrNotConfigured
	}
	target, err := BoardPath(next)
	if err != nil {
		return "", err
	}
	if c.BoardsToken == "" {
		return c.BoardsURL + target, nil
	}
	body, _ := json.Marshal(map[string]any{"user": who, "next": target, "ttl": 60})
	var out struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, http.MethodPost, c.BoardsURL+"/b/api/handoff", c.BoardsToken, body, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, c.BoardsURL+"/b/") {
		return "", &UpstreamError{Status: http.StatusBadGateway, Text: "the hand-off link is not on the boards server"}
	}
	return out.URL, nil
}

// Document is one entry of a conversation's documents.
type Document struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
	Version   int    `json:"version"`
	ViewURL   string `json:"viewUrl"`
	EmbedURL  string `json:"embedUrl"`
}

// Documents lists what a conversation published to the catalog, newest first.
func (c Config) Documents(ctx context.Context, conversation string) ([]Document, error) {
	if c.ArtifactsURL == "" {
		return nil, ErrNotConfigured
	}
	token := c.ArtifactsReadToken
	if token == "" {
		token = c.ArtifactsToken
	}
	q := url.Values{"session": {conversation}, "limit": {"100"}}
	var out struct {
		Items []struct {
			Slug           string `json:"slug"`
			Title          string `json:"title"`
			UpdatedAt      string `json:"updatedAt"`
			VersionCount   int    `json:"versionCount"`
			ViewURL        string `json:"viewUrl"`
			EmbedURL       string `json:"embedUrl"`
			CurrentVersion *struct {
				N int `json:"n"`
			} `json:"currentVersion"`
		} `json:"items"`
	}
	if err := c.call(ctx, http.MethodGet, c.ArtifactsURL+"/api/v1/artifacts?"+q.Encode(), token, nil, &out); err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(out.Items))
	for _, it := range out.Items {
		v := it.VersionCount
		if it.CurrentVersion != nil && it.CurrentVersion.N > 0 {
			v = it.CurrentVersion.N
		}
		docs = append(docs, Document{Slug: it.Slug, Title: it.Title, UpdatedAt: it.UpdatedAt, Version: v,
			ViewURL: it.ViewURL, EmbedURL: it.EmbedURL})
	}
	return docs, nil
}

func (c Config) call(ctx context.Context, method, target, token string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		text := e.Error
		if text == "" {
			text = e.Message
		}
		if text == "" {
			text = http.StatusText(res.StatusCode)
		}
		return &UpstreamError{Status: res.StatusCode, Text: text}
	}
	return json.Unmarshal(raw, out)
}
