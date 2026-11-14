// Package searchindex is the search of Sinopia's ⌘K from a database instead of from every machine's library
// Designs, the apps of a team and the documents of the apps are put into one table when they are written
// to the store, never when someone asks; a question reads that table only. The storage sits behind Index: the hub's
// Postgres (the table search_docs, migration 0068) and the runtime's own SQLite (OpenSQLite) are the same SQL, so a
// runtime with no hub answers the same questions. Who may see a hit is not the index's business: the caller says it
// (Query.Allow), as the library does with design access.
package searchindex

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
)

// Kinds of a document.
const (
	KindDesign = "design" // a design project of the library; ID and Parent are the project's id
	KindApp    = "app"    // an app installed for the team
	KindKind   = "kind"   // a document type an app adds
	KindDoc    = "doc"    // a document of an app's type
)

// Doc is one searchable thing. Space scopes the questions: the hub's space, "" for a runtime on its own.
type Doc struct {
	Space     string `json:"-"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Parent    string `json:"parent,omitempty"` // the project or the app the thing belongs to
	Title     string `json:"title"`
	Body      string `json:"-"` // more words to find it by: the boards' titles, a description
	UpdatedAt int64  `json:"updatedAt"`
}

// Hit is a found Doc.
type Hit = Doc

// Query is a question to the index.
type Query struct {
	Space string
	Text  string
	Kinds []string // none: every kind
	Limit int      // 0: 20
	// Allow says whether the person may see a hit; nil allows all. It is asked for the best matches first, so a space
	// with many closed projects still fills the page.
	Allow func(Hit) bool
}

// Index is where the documents are kept.
type Index interface {
	// Put adds the documents or replaces the ones with the same space, kind and id.
	Put(ctx context.Context, docs ...Doc) error
	// Remove forgets one document.
	Remove(ctx context.Context, space, kind, id string) error
	// Replace makes docs the whole set of a kind for a space (parent "" for all of it, else for that parent only).
	Replace(ctx context.Context, space, kind, parent string, docs []Doc) error
	// Search finds the documents that hold every word of the text, best first.
	Search(ctx context.Context, q Query) ([]Hit, error)
	// Count is how many documents a space has (a runtime asks it to know whether the first build is still to come).
	Count(ctx context.Context, space string) (int, error)
}

// DB is the part of a database the index uses: the hub's store.Q and a plain sql.DB both fit.
type DB interface {
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ErrEmpty is a question with no word in it.
var ErrEmpty = errors.New("searchindex: empty question")

const (
	maxTitle = 300
	maxBody  = 8 << 10
	maxWords = 8
)

// Normalize lowercases, turns ё into е and every run of anything but letters and digits into one space.
func Normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if r == 'ё' {
			r = 'е'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// Words are the words of a question, at most maxWords, none repeated.
func Words(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.Fields(Normalize(s)) {
		if !seen[w] && len(out) < maxWords {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !strings.HasSuffix(s, " ") && s[len(s)-1] >= 0x80 {
		s = s[:len(s)-1] // not in the middle of a rune
	}
	return s
}
