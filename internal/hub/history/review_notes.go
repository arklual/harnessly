package history

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The draft remarks of a code review (table review_notes): what a person noted on the lines of a chat's work in the
// «Ревью кода» app before sending them to the chat's agent. They are the person's own: kept per person and chat, read
// from any device, never shown to anybody else. Sending them is the page's job (one message into the chat's queue);
// the hub only keeps them.

const (
	// ReviewNotesMax is the most notes one person keeps on one chat.
	ReviewNotesMax  = 500
	reviewPathMax   = 1024
	reviewTextMax   = 8000
	reviewLinesMax  = 8000
	reviewLineLimit = 10_000_000
	reviewRefMax    = 1024
)

var (
	reviewNoteID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reviewSha    = regexp.MustCompile(`^[0-9a-f]{0,64}$`)

	// ErrBadNote is a note that cannot be kept as it is.
	ErrBadNote = errors.New("history: bad review note")
	// ErrTooManyNotes is one note more than a chat may keep.
	ErrTooManyNotes = errors.New("history: too many review notes")
)

// Review note states: a draft, sent to the agent, closed as done.
const (
	ReviewDraft = "draft"
	ReviewSent  = "sent"
	ReviewDone  = "done"
)

// ReviewNote is one remark on a range of lines of a file in a chat's work.
type ReviewNote struct {
	ID string `json:"id"`
	// Worktree is where the file is (a chat may have worked in several); Base the commit the lines were read at.
	Worktree string `json:"worktree,omitempty"`
	Base     string `json:"base,omitempty"`
	Path     string `json:"path"`
	// Side is the version the lines are of: "after" (the new text) or "before".
	Side string `json:"side"`
	From int    `json:"from"`
	To   int    `json:"to"`
	// Lines is the text of the lines when the note was made: the note follows it to a new diff.
	Lines string `json:"lines"`
	Text  string `json:"text"`
	State string `json:"state"`
	// Outdated: the lines were not found in a newer diff.
	Outdated  bool  `json:"outdated,omitempty"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
	SentAt    int64 `json:"sentAt,omitempty"`
}

func cleanText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// cleanNote is the note as it may be kept, false for one that may not.
func cleanNote(n ReviewNote) (ReviewNote, bool) {
	if !reviewNoteID.MatchString(n.ID) || n.Path == "" || !cleanText(n.Path, reviewPathMax) ||
		!cleanText(n.Worktree, reviewRefMax) || !reviewSha.MatchString(n.Base) {
		return n, false
	}
	if n.Side != "after" && n.Side != "before" {
		return n, false
	}
	if n.From < 1 || n.To < n.From || n.To > reviewLineLimit {
		return n, false
	}
	if !cleanText(n.Lines, reviewLinesMax) || !cleanText(n.Text, reviewTextMax) || strings.TrimSpace(n.Text) == "" {
		return n, false
	}
	switch n.State {
	case ReviewDraft, ReviewSent, ReviewDone:
	default:
		return n, false
	}
	if n.CreatedAt < 0 || n.UpdatedAt < 0 || n.SentAt < 0 {
		return n, false
	}
	return n, true
}

// ReviewNotes are the person's notes on a chat, by file and line.
func ReviewNotes(ctx context.Context, db *store.DB, userID, conv string) ([]ReviewNote, error) {
	out := []ReviewNote{}
	if !convID.MatchString(conv) {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT data FROM review_notes WHERE owner_id = ? AND conv_id = ?`, userID, conv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var n ReviewNote
		if json.Unmarshal([]byte(data), &n) == nil {
			out = append(out, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.CreatedAt < b.CreatedAt
	})
	return out, nil
}

// PutReviewNote keeps a note, new or changed. A chat keeps at most ReviewNotesMax of a person's notes.
func PutReviewNote(ctx context.Context, db *store.DB, userID, conv string, note ReviewNote, nowMs int64) (ReviewNote, error) {
	n, ok := cleanNote(note)
	if !ok || !convID.MatchString(conv) || userID == "" {
		return note, ErrBadNote
	}
	n.UpdatedAt = nowMs
	if n.CreatedAt == 0 {
		n.CreatedAt = nowMs
	}
	data, err := json.Marshal(n)
	if err != nil {
		return note, err
	}
	err = db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		var count int
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM review_notes WHERE owner_id = ? AND conv_id = ? AND note_id <> ?`,
			userID, conv, n.ID).Scan(&count); err != nil {
			return err
		}
		if count >= ReviewNotesMax {
			return ErrTooManyNotes
		}
		_, err := q.Exec(ctx, `INSERT INTO review_notes (owner_id, conv_id, note_id, data, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (owner_id, conv_id, note_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
			userID, conv, n.ID, string(data), nowMs)
		return err
	})
	if err != nil {
		return note, err
	}
	return n, nil
}

// DeleteReviewNote forgets a note; one that is not there is not an error.
func DeleteReviewNote(ctx context.Context, db *store.DB, userID, conv, id string) error {
	if !convID.MatchString(conv) || !reviewNoteID.MatchString(id) {
		return ErrBadNote
	}
	_, err := db.Exec(ctx, `DELETE FROM review_notes WHERE owner_id = ? AND conv_id = ? AND note_id = ?`, userID, conv, id)
	return err
}
