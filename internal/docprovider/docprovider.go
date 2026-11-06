// Package docprovider is the contract of the documents and boards service behind the runtime's document reader: list,
// read, publish a new version, rights (public link, grants), comments (list, reply, close) and the address to embed.
// The built-in artifacts catalog and boards server (internal/integrations, RUNTIME_ARTIFACTS_URL, RUNTIME_BOARDS_URL)
// are the default implementation and nothing changes without an adapter. A corporate service connects as an adapter of
// the extension frame (internal/hub/ext), protocol harnessly.docs/v1 (docs/doc-adapters.md).
package docprovider

import (
	"context"
	"errors"
	"time"

	"github.com/ly-solutions/harnessly/internal/integrations"
)

// The shapes are the ones the page's reader already reads.
type (
	Summary = integrations.DocSummary
	Version = integrations.DocVersion
	Diff    = integrations.DocDiff
	Thread  = integrations.DocThread
	Anchor  = integrations.DocAnchor
	Author  = integrations.DocAuthor
	Board   = integrations.Board
)

// ErrUnsupported: the provider does not do this (a built-in catalog asked to publish, an adapter without comments).
var ErrUnsupported = errors.New("docprovider: not supported")

// Content is a document's text at one version.
type Content struct {
	// Format is "md" or "html".
	Format  string `json:"format"`
	Content string `json:"content"`
	Version string `json:"version"`
}

// PublishRequest is a new document or a new version of one (Slug set).
type PublishRequest struct {
	Title   string `json:"title"`
	Format  string `json:"format"`
	Content string `json:"content"`
	Slug    string `json:"slug,omitempty"`
	// Visibility is "private" (default) or "public".
	Visibility string `json:"visibility,omitempty"`
	Note       string `json:"note,omitempty"`
	// Conversation is the chat that published it.
	Conversation string `json:"conversation,omitempty"`
}

// Published is what a publish made.
type Published struct {
	Slug    string `json:"slug"`
	Version int    `json:"version"`
	URL     string `json:"url,omitempty"`
}

// Embed is an address for a frame or a widget; it may expire.
type Embed struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// Grant gives a person or a group a role on a document.
type Grant struct {
	Subject string `json:"subject"`
	// Role is "viewer", "commenter" or "editor".
	Role string `json:"role"`
}

// Access is who can reach a document.
type Access struct {
	Public    bool    `json:"public"`
	PublicURL *string `json:"publicUrl"`
	Grants    []Grant `json:"grants,omitempty"`
}

// AccessChange changes it: a field left nil stays as it was.
type AccessChange struct {
	Public *bool    `json:"public,omitempty"`
	Grants *[]Grant `json:"grants,omitempty"`
}

// Counts are the open threads, the unread ones and the last activity of each document, by slug, for one person.
type Counts struct {
	Open   map[string]int
	Unread map[string]int
	Last   map[string]string
}

// Provider is the documents and boards contract.
type Provider interface {
	List(ctx context.Context, limit int) ([]Summary, error)
	Get(ctx context.Context, slug string) (Summary, error)
	// Read is the text of a version ("" is the current one).
	Read(ctx context.Context, slug, version string) (Content, error)
	Versions(ctx context.Context, slug string) ([]Version, error)
	Diff(ctx context.Context, slug, from, to string) (Diff, error)
	Publish(ctx context.Context, req PublishRequest) (Published, error)
	Embed(ctx context.Context, slug string) (Embed, error)
	Access(ctx context.Context, slug string) (Access, error)
	SetAccess(ctx context.Context, slug string, change AccessChange) (Access, error)
	Threads(ctx context.Context, slug string) ([]Thread, error)
	CreateThread(ctx context.Context, slug string, anchor Anchor, body string, by Author) (Thread, error)
	Reply(ctx context.Context, slug, threadID, body string, by Author) (Thread, error)
	// SetStatus opens or closes ("open", "resolved") a thread.
	SetStatus(ctx context.Context, slug, threadID, status string) (Thread, error)
	Boards(ctx context.Context) ([]Board, error)
	Counts(ctx context.Context, person string) (Counts, error)
	MarkRead(ctx context.Context, person, slug string) error
}
