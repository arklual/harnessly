// Package efficiency is the hub's side of the «Эффективность» screen (HRN-972): collectors that take facts from outside
// sources through one provider interface, the facts stored in the hub's database, and the figures computed from them on
// read. Addresses and tokens of a source are configuration; nothing here names a particular installation.
package efficiency

import (
	"context"
	"time"
)

// The kinds of facts, stored as the source column.
const (
	KindMR      = "mr"
	KindComment = "comment"
)

// MR is a merged merge request as a code-host provider reports it.
type MR struct {
	Project   string    `json:"project"`
	IID       int       `json:"iid"`
	Title     string    `json:"title"`
	Branch    string    `json:"branch"`
	Author    string    `json:"author"`
	MergedBy  string    `json:"mergedBy"`
	CreatedAt time.Time `json:"createdAt"`
	MergedAt  time.Time `json:"mergedAt"`
	// Approved says somebody other than the author approved it; ForeignNotes that somebody else commented.
	Approved     bool `json:"approved"`
	ForeignNotes bool `json:"foreignNotes"`
}

// Key is the stable identity of the request across passes.
func (m MR) Key() string { return m.Project + "!" + itoa(m.IID) }

// Comment is a comment of a tracker issue; only the head of the text is kept.
type Comment struct {
	Issue  string    `json:"issue"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Head   string    `json:"head"`
}

// CodeHost is a source of merged requests: GitLab, GitHub or any other that can list what was merged since a time.
// It reports every merged request however it was created (a web form, a push option, the host's API); counting only the
// ones a session mentioned made the money per request lie (see Summary).
type CodeHost interface {
	MergedMRs(ctx context.Context, since time.Time) ([]MR, error)
}

// Tracker is a source of issue comments.
type Tracker interface {
	Comments(ctx context.Context, from, to time.Time) ([]Comment, error)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
