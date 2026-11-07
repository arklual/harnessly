package docprovider

import (
	"context"
	"strings"

	"github.com/ly-solutions/harnessly/internal/integrations"
)

// Builtin is the default provider: the artifacts catalog and the boards server of the deployment, and the comment
// threads the runtime keeps in its data directory. It is the code the reader used before the contract existed.
// Publishing goes through the platform agent (internal/docshare), not through this contract, and reading the text goes
// through the embedded frame, so both answer ErrUnsupported here.
type Builtin struct {
	Cfg   integrations.Config
	Store *integrations.Threads
}

func (b Builtin) List(ctx context.Context, limit int) ([]Summary, error) {
	if b.Cfg.ArtifactsURL == "" {
		return []Summary{}, nil
	}
	return b.Cfg.ListDocs(ctx, limit)
}

func (b Builtin) Get(ctx context.Context, slug string) (Summary, error) { return b.Cfg.Doc(ctx, slug) }

func (b Builtin) Read(context.Context, string, string) (Content, error) {
	return Content{}, ErrUnsupported
}

func (b Builtin) Versions(ctx context.Context, slug string) ([]Version, error) {
	return b.Cfg.DocVersions(ctx, slug)
}

func (b Builtin) Diff(ctx context.Context, slug, from, to string) (Diff, error) {
	return b.Cfg.DocDiff(ctx, slug, from, to)
}

func (b Builtin) Publish(context.Context, PublishRequest) (Published, error) {
	return Published{}, ErrUnsupported
}

func (b Builtin) Embed(ctx context.Context, slug string) (Embed, error) {
	u, at, err := b.Cfg.DocReadLink(ctx, slug)
	return Embed{URL: u, ExpiresAt: at}, err
}

func (b Builtin) Access(ctx context.Context, slug string) (Access, error) {
	a, err := b.Cfg.DocAccessOf(ctx, slug, nil)
	return Access{Public: a.Public, PublicURL: a.PublicURL}, err
}

func (b Builtin) SetAccess(ctx context.Context, slug string, ch AccessChange) (Access, error) {
	if ch.Grants != nil {
		return Access{}, ErrUnsupported
	}
	if ch.Public == nil {
		return b.Access(ctx, slug)
	}
	a, err := b.Cfg.DocAccessOf(ctx, slug, ch.Public)
	return Access{Public: a.Public, PublicURL: a.PublicURL}, err
}

func (b Builtin) Threads(_ context.Context, slug string) ([]Thread, error) { return b.Store.Of(slug) }

func (b Builtin) CreateThread(_ context.Context, slug string, a Anchor, body string, by Author) (Thread, error) {
	return b.Store.Create(slug, a, body, by)
}

func (b Builtin) Reply(_ context.Context, slug, id, body string, by Author) (Thread, error) {
	return b.Store.Reply(slug, id, body, by)
}

func (b Builtin) SetStatus(_ context.Context, slug, id, status string) (Thread, error) {
	return b.Store.SetStatus(slug, id, status)
}

func (b Builtin) Boards(ctx context.Context) ([]Board, error) { return b.Cfg.ListBoards(ctx) }

// SearchBoards filters the list by the title: the boards server has no search of its own.
func (b Builtin) SearchBoards(ctx context.Context, query string) ([]Board, error) {
	all, err := b.Cfg.ListBoards(ctx)
	if err != nil {
		return nil, err
	}
	return filterBoards(all, query), nil
}

func filterBoards(all []Board, query string) []Board {
	q := strings.ToLower(strings.TrimSpace(query))
	out := []Board{}
	for _, x := range all {
		if q == "" || strings.Contains(strings.ToLower(x.Title+" "+x.Slug), q) {
			out = append(out, x)
		}
	}
	return out
}

func (b Builtin) Capabilities() Capabilities {
	return Capabilities{Provider: "builtin", Access: true, Comments: true, Boards: b.Cfg.BoardsURL != "", BoardsSearch: b.Cfg.BoardsURL != ""}
}

// ConversationDocuments is the catalog's own list by session: the catalog binds what the platform agent published.
func (b Builtin) ConversationDocuments(ctx context.Context, conversation string) ([]integrations.Document, error) {
	return b.Cfg.Documents(ctx, conversation)
}

func (b Builtin) Counts(_ context.Context, person string) (Counts, error) {
	open, unread, last, err := b.Store.Counts(person)
	return Counts{Open: open, Unread: unread, Last: last}, err
}

func (b Builtin) MarkRead(_ context.Context, person, slug string) error {
	return b.Store.MarkRead(person, slug)
}
