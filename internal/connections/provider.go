package connections

import (
	"context"
	"net/url"
	"time"
)

// Conn is a kept connection: what the page lists and what a provider opens. Never a credential.
type Conn struct {
	ID         string     `json:"id"`
	Kind       Kind       `json:"kind"`
	Name       string     `json:"name"`
	BaseURL    string     `json:"baseUrl"`
	APIURL     string     `json:"apiUrl,omitempty"`
	Deployment Deployment `json:"deployment"`
	Version    string     `json:"version,omitempty"`
	Edition    string     `json:"edition,omitempty"`
	// Method is the sign-in method's id; Login the non-secret part of it (an email or a user name), shown in the list.
	Method       string            `json:"method"`
	Login        string            `json:"login,omitempty"`
	Capabilities []Capability      `json:"capabilities"`
	Extra        map[string]string `json:"extra,omitempty"`
	// Me is who the credentials sign in as, from the last check.
	Me *Person `json:"me,omitempty"`
	// State of the last check: "ok", "denied", "unreachable".
	State     string    `json:"state"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	CreatedAt time.Time `json:"createdAt"`
	// Primary marks the connection the tracker page and bare task keys use for its capability.
	Primary bool `json:"primary,omitempty"`
	// Env marks a connection the deployment configured (RUNTIME_TRACKER_URL …): listed, not editable.
	Env bool `json:"env,omitempty"`
}

// Has says whether the connection can do c.
func (c Conn) Has(cap Capability) bool {
	for _, x := range c.Capabilities {
		if x == cap {
			return true
		}
	}
	return false
}

// Client is an opened connection. Every client answers Me; the others it implements by its capabilities.
type Client interface {
	// Me checks the credentials and says who they sign in as.
	Me(ctx context.Context) (*Person, error)
}

// IssueSource is a client of a product with issues.
type IssueSource interface {
	Projects(ctx context.Context) ([]Project, error)
	Search(ctx context.Context, q Query) ([]Issue, error)
	// Issue is one issue by its key, with its description and its last comments.
	Issue(ctx context.Context, key string) (*Issue, error)
}

// PageSource is a client of a wiki.
type PageSource interface {
	SearchPages(ctx context.Context, q Query) ([]Page, error)
	// Page is one page by its id, with its body.
	Page(ctx context.Context, id string) (*Page, error)
}

// LinkTarget is what a link on a connection's host points at.
type LinkTarget struct {
	// Kind is "issue" or "page".
	Kind string `json:"kind"`
	// Key is an issue key or a page id, as IssueSource.Issue and PageSource.Page take it.
	Key string `json:"key"`
}

// Provider knows one product.
type Provider interface {
	Kind() Kind
	// Detect says whether u is an instance of this product: (nil, nil) when it is not. It may probe the instance through
	// p; u is the pasted link with its path, and the provider finds the context path itself.
	Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error)
	// Open is a client for a kept connection and its credentials.
	Open(conn Conn, cred Credentials, h *HTTP) Client
	// Link reads a link on the connection's instance: an issue or a page, else ok is false. No network.
	Link(conn Conn, u *url.URL) (LinkTarget, bool)
}

// registry holds every provider, in the order Detect tries them.
var registry []Provider

// Register adds a provider; providers call it from init.
func Register(p Provider) { registry = append(registry, p) }

// Providers are the registered providers.
func Providers() []Provider { return append([]Provider(nil), registry...) }

// ProviderOf is the provider of a kind, nil when there is none.
func ProviderOf(k Kind) Provider {
	for _, p := range registry {
		if p.Kind() == k {
			return p
		}
	}
	return nil
}
