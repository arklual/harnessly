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

// Errors a provider answers besides the reader's own (not found, denied, unreachable).
var (
	// ErrUnsupported: the provider does not do this (a built-in catalog asked to publish, an adapter without comments).
	ErrUnsupported = errors.New("docprovider: not supported")
	// ErrTooLarge: the document (or the answer about it) is bigger than the runtime takes from the provider.
	ErrTooLarge = errors.New("docprovider: too large")
	// ErrSearchRequired: the provider finds boards only by a query and has no list of them.
	ErrSearchRequired = errors.New("docprovider: boards are found by a query only")
)

// Capabilities is what a provider does, for the routes and the page to ask before they call.
type Capabilities struct {
	// Provider is the adapter's name, "builtin" for the built-in catalog.
	Provider string `json:"provider"`
	Publish  bool   `json:"publish"`
	Access   bool   `json:"access"`
	Comments bool   `json:"comments"`
	// Boards: there is a list of boards. BoardsSearch: boards can be found by a query (without Boards, only so).
	Boards       bool `json:"boards"`
	BoardsSearch bool `json:"boardsSearch"`
	// ProviderEmbed: documents and boards are framed through Embed, never through their viewer address.
	ProviderEmbed bool `json:"providerEmbed"`
	// MaxAnswerBytes is the biggest answer taken from the provider, 0 when there is no such limit.
	MaxAnswerBytes int64 `json:"maxAnswerBytes,omitempty"`
}

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
	// Slug is the runtime's name of the document; ExternalID the provider's own (they differ for an adapter's ID that
	// is not a slug, see Alias).
	Slug       string `json:"slug"`
	ExternalID string `json:"externalId,omitempty"`
	Version    int    `json:"version"`
	URL        string `json:"url,omitempty"`
}

// Embed is an address for a frame or a widget; it may expire.
type Embed struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// Resolved is what an address of the provider's service names (the optional `resolve` capability): a document or a
// board, by the provider's own ID. ID keeps its case and its hyphens as the provider wrote them; nothing is derived
// from the address.
type Resolved struct {
	// Kind is "document" or "board".
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Embed is false when the provider's service cannot be shown in a frame; the address then opens only as a link.
	Embed *bool `json:"embed,omitempty"`
}

// Embeddable says whether the provider frames what the address names (an adapter that says nothing does).
func (r Resolved) Embeddable() bool { return r.Embed == nil || *r.Embed }

// Resolver is a Provider that tells which document or board an address of its service names. It is optional: a provider
// without it (the built-in catalog, an adapter that does not list `resolve`) keeps the rules of the pane as they are.
// ok is false for an address the provider does not claim; the error is a refusal (denied) or an unreachable provider.
type Resolver interface {
	Resolve(ctx context.Context, address string) (r Resolved, ok bool, err error)
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
	// Boards is the list of boards; ErrSearchRequired when the provider only finds them by a query.
	Boards(ctx context.Context) ([]Board, error)
	// SearchBoards finds boards by a query.
	SearchBoards(ctx context.Context, query string) ([]Board, error)
	Capabilities() Capabilities
	Counts(ctx context.Context, person string) (Counts, error)
	MarkRead(ctx context.Context, person, slug string) error
}
