// Package connections links the runtime to the person's own trackers, wikis and code hosts: Jira, Confluence,
// YouTrack, Linear, GitLab, GitHub and others, in the cloud or self-hosted on any domain and under any path. A person
// pastes a link; Detect finds the product, its deployment and version, and which credentials it takes; Connect checks
// them and keeps the connection. Every product is read through one format (Issue, Page, Project, Person), so the
// tracker page, link chips, task cards and agents work the same whichever product is behind them.
//
// What is kept: the connection itself (product, address, version, the non-secret login) in connections.json in the
// runtime's data folder, and its credentials in the safe (internal/secrets) under the name conn-<id>. No answer and
// no log line carries a credential.
package connections

import (
	"errors"
	"time"
)

// Kind names a product.
type Kind string

const (
	Jira          Kind = "jira"
	Confluence    Kind = "confluence"
	YouTrack      Kind = "youtrack"
	Linear        Kind = "linear"
	GitLab        Kind = "gitlab"
	GitHub        Kind = "github"
	Gitea         Kind = "gitea"
	Bitbucket     Kind = "bitbucket"
	Redmine       Kind = "redmine"
	YandexTracker Kind = "yandex-tracker"
	AzureDevOps   Kind = "azure-devops"
	Notion        Kind = "notion"
	MediaWiki     Kind = "mediawiki"
	Outline       Kind = "outline"
	BookStack     Kind = "bookstack"
	WikiJS        Kind = "wikijs"
)

// Capability is what a connection can be asked for.
type Capability string

const (
	// Issues: projects, search, one issue by key (trackers and code hosts' issues).
	Issues Capability = "issues"
	// Pages: search and read wiki pages.
	Pages Capability = "pages"
	// Code: repositories, merge requests (code hosts).
	Code Capability = "code"
)

// Deployment says where the product runs.
type Deployment string

const (
	Cloud      Deployment = "cloud"
	SelfHosted Deployment = "self-hosted"
)

// Field is one input the page shows for a sign-in method. ID is one of the Field* constants; the page has a label
// and a hint for each.
type Field struct {
	ID     string `json:"id"`
	Secret bool   `json:"secret"`
	// Optional fields may stay empty (an organization id the product can infer).
	Optional bool `json:"optional,omitempty"`
	// Placeholder is an example value ("you@company.com", "perm:…").
	Placeholder string `json:"placeholder,omitempty"`
}

const (
	FieldToken    = "token"
	FieldEmail    = "email"
	FieldUsername = "username"
	FieldPassword = "password"
	FieldOrg      = "org"
	FieldTokenID  = "tokenId"
)

// Method is one way to sign in: which fields to ask and where the person gets the token.
type Method struct {
	// ID: "api-token" (Atlassian Cloud email + token), "pat" (personal access token), "basic" (login and password),
	// "api-key", "oauth-token", "none" (public read).
	ID     string  `json:"id"`
	Fields []Field `json:"fields"`
	// TokenURL is the page where the person creates the token, on this very instance when it has one.
	TokenURL string `json:"tokenUrl,omitempty"`
	// Recommended marks the method the page preselects.
	Recommended bool `json:"recommended,omitempty"`
}

// Detection is what a pasted link turned out to be.
type Detection struct {
	Kind Kind `json:"kind"`
	// BaseURL is the instance's root with its context path, no trailing slash (https://jira.example.com/jira,
	// https://acme.atlassian.net/wiki).
	BaseURL string `json:"baseUrl"`
	// APIURL is where the API lives when it is not under BaseURL (api.github.com, GitHub Enterprise /api/v3).
	APIURL     string     `json:"apiUrl,omitempty"`
	Deployment Deployment `json:"deployment"`
	// Version as the product reports it ("9.12.4", "17.5.1-ee"); "" when it does not say before sign-in.
	Version string `json:"version,omitempty"`
	// Edition: "server", "data-center", "ce", "ee", "enterprise", … when known.
	Edition string `json:"edition,omitempty"`
	// Title is the instance's own name when it gives one ("Acme Jira").
	Title        string       `json:"title,omitempty"`
	Methods      []Method     `json:"methods"`
	Capabilities []Capability `json:"capabilities"`
	// Extra holds product facts later calls need (Jira Cloud's cloudId, a YouTrack context path); never secrets.
	Extra map[string]string `json:"extra,omitempty"`
}

// Credentials are the values the person typed, by Field id.
type Credentials map[string]string

// Person is a user of a connected product.
type Person struct {
	ID     string `json:"id,omitempty"`
	Login  string `json:"login,omitempty"`
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// Category is a status reduced to what every product can say.
type Category string

const (
	Todo    Category = "todo"
	Doing   Category = "doing"
	Done    Category = "done"
	Unknown Category = "unknown"
)

// Status is an issue's state in the product's words and as a category.
type Status struct {
	Name     string   `json:"name"`
	Category Category `json:"category"`
}

// Ref points at another issue.
type Ref struct {
	Key   string `json:"key"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Comment on an issue.
type Comment struct {
	ID      string    `json:"id,omitempty"`
	Author  *Person   `json:"author,omitempty"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

// Issue is a task, bug, ticket, issue or work item in one format.
type Issue struct {
	// Key is how people name it: "ABC-12" (Jira, YouTrack, Linear), "group/project#12" (GitLab, GitHub).
	Key      string    `json:"key"`
	ID       string    `json:"id,omitempty"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Status   Status    `json:"status"`
	Type     string    `json:"type,omitempty"`
	Priority string    `json:"priority,omitempty"`
	Project  string    `json:"project,omitempty"`
	Assignee *Person   `json:"assignee,omitempty"`
	Reporter *Person   `json:"reporter,omitempty"`
	Parent   *Ref      `json:"parent,omitempty"`
	Labels   []string  `json:"labels,omitempty"`
	Created  time.Time `json:"created,omitzero"`
	Updated  time.Time `json:"updated,omitzero"`
	Resolved time.Time `json:"resolved,omitzero"`
	// Description is plain text or Markdown, converted from the product's own markup (ADF, wiki markup, HTML).
	Description string    `json:"description,omitempty"`
	Comments    []Comment `json:"comments,omitempty"`
}

// Project is a Jira project, a YouTrack project, a Linear team, a GitLab project, a GitHub repository.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Page is a wiki page.
type Page struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	URL     string    `json:"url"`
	Space   string    `json:"space,omitempty"`
	Excerpt string    `json:"excerpt,omitempty"`
	Author  *Person   `json:"author,omitempty"`
	Updated time.Time `json:"updated,omitzero"`
	// Body is Markdown or plain text; only Page() fills it, search leaves it empty.
	Body string `json:"body,omitempty"`
}

// Query asks for issues or pages.
type Query struct {
	// Text is free text; empty lists by the other filters.
	Text string `json:"text,omitempty"`
	// Mine: assigned to the signed-in person.
	Mine bool `json:"mine,omitempty"`
	// Open: not done.
	Open bool `json:"open,omitempty"`
	// Project narrows to one project key, team or space.
	Project string `json:"project,omitempty"`
	// Native is a query in the product's own language (JQL, CQL, YouTrack query); wins over the fields above.
	Native string `json:"native,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// LimitOr is the query's limit, or def when unset, capped at 200.
func (q Query) LimitOr(def int) int {
	n := q.Limit
	if n <= 0 {
		n = def
	}
	return min(n, 200)
}

// Errors a provider answers with; the routes map them to reasons the page shows.
var (
	// ErrDenied: the credentials are wrong, expired or lack a permission.
	ErrDenied = errors.New("denied")
	// ErrUnreachable: the instance did not answer or answered something unexpected.
	ErrUnreachable = errors.New("unreachable")
	// ErrNotFound: no such issue or page.
	ErrNotFound = errors.New("not-found")
	// ErrUnsupported: this product or connection cannot do that.
	ErrUnsupported = errors.New("unsupported")
	// ErrUnknown: the link is not a product we know.
	ErrUnknown = errors.New("unknown-product")
)

// Reason is the short word for an error the page and the CLI show.
func Reason(err error) string {
	for _, e := range []error{ErrDenied, ErrNotFound, ErrUnsupported, ErrUnknown, ErrUnreachable} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return ErrUnreachable.Error()
}
