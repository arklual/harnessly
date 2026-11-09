package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/msgindex"
)

// HRN-879, spec v10: the rest of what a person reads of their chats, from the hub's copy — the turns a rewind left
// behind, the search over the messages, and the person's sidebar — as the cluster's gateway serves them.

// rewoundTurns is how many of the turns a rewind left behind its page shows, the newest of them (the machine's).
const rewoundTurns = 33

// Rewound is the turns the chat's rewind n (of the record's `rewinds`, oldest first) left behind, by their faces.
func Rewound(ctx context.Context, db *store.DB, userID, conv string, n int, machines []string) (Page, error) {
	machine, record, _, err := head(ctx, db, userID, conv, machines)
	if err != nil {
		return Page{}, err
	}
	var rec struct {
		Rewinds []struct {
			AfterTurnID *string `json:"afterTurnId"`
		} `json:"rewinds"`
	}
	if json.Unmarshal(record, &rec) != nil || n < 0 || n >= len(rec.Rewinds) {
		return Page{}, ErrNotFound
	}
	after := ""
	if a := rec.Rewinds[n].AfterTurnID; a != nil {
		after = *a
	}
	rows, err := db.Query(ctx, `SELECT body FROM history_turns WHERE machine_id = ? AND conv_id = ? AND left_after = ?
		ORDER BY started_ms DESC, turn_id DESC LIMIT ?`, machine, conv, after, rewoundTurns)
	if err != nil {
		return Page{}, err
	}
	var found []json.RawMessage
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return Page{}, err
		}
		found = append(found, json.RawMessage(body))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{MachineID: machine, Conversation: record, Turns: make([]json.RawMessage, 0, len(found)), Entries: []json.RawMessage{}}
	var faces []string
	for i := len(found) - 1; i >= 0; i-- {
		page.Turns = append(page.Turns, found[i])
		if b, ok := object(found[i]); ok {
			if id, ok := str(b["id"]); ok {
				faces = append(faces, id)
			}
			if answer, ok := str(b["answerEntryId"]); ok && answer != "" {
				faces = append(faces, answer)
			}
		}
	}
	if len(faces) == 0 {
		return page, nil
	}
	args := []any{machine, conv}
	for _, f := range faces {
		args = append(args, f)
	}
	erows, err := db.Query(ctx, `SELECT body FROM history_entries WHERE machine_id = ? AND conv_id = ? AND entry_id IN (`+
		placeholders(len(faces))+`) ORDER BY at_ms, ord`, args...)
	if err != nil {
		return Page{}, err
	}
	defer erows.Close()
	for erows.Next() {
		var body string
		if err := erows.Scan(&body); err != nil {
			return Page{}, err
		}
		page.Entries = append(page.Entries, pageEntry(json.RawMessage(body)))
	}
	return page, erows.Err()
}

// Hit is one message found, as GET /api/v2/search gives it (wire.MessageHit).
type Hit struct {
	ConvID  string  `json:"convId"`
	EntryID string  `json:"entryId"`
	TurnID  *string `json:"turnId"`
	At      int64   `json:"at"`
	Role    string  `json:"role"`
	Snippet string  `json:"snippet"`
}

// searchScan is how many of the newest messages of the person's chats a search reads at most.
const searchScan = 20000

// Search is the person's messages and the agents' answers that hold every word of the query (as the machine's index
// cuts words: a word of the message begins with each), newest first, in the chats of the machines that are not deleted.
func Search(ctx context.Context, db *store.DB, userID, query string, machines []string, limit int) ([]Hit, error) {
	words := msgindex.Tokens(strings.TrimSpace(query))
	out := []Hit{}
	if len(words) == 0 || len(machines) == 0 || len([]rune(strings.TrimSpace(query))) < 2 {
		return out, nil
	}
	args := []any{userID}
	for _, m := range machines {
		args = append(args, m)
	}
	args = append(args, searchScan)
	rows, err := db.Query(ctx, `SELECT e.conv_id, e.entry_id, e.turn_id, e.at_ms, e.kind, e.body FROM history_entries e
		JOIN history_conversations c ON c.machine_id = e.machine_id AND c.conv_id = e.conv_id
		LEFT JOIN history_turns t ON t.machine_id = e.machine_id AND t.conv_id = e.conv_id AND t.turn_id = e.turn_id
		WHERE t.left_after IS NULL AND c.owner_id = ? AND c.machine_id IN (`+placeholders(len(machines))+`) AND c.deleted_at IS NULL AND c.record IS NOT NULL
			AND e.kind IN ('user', 'say')
		ORDER BY e.at_ms DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() && len(out) < limit {
		var conv, entry, kind, body string
		var turn sql.NullString
		var at int64
		if err := rows.Scan(&conv, &entry, &turn, &at, &kind, &body); err != nil {
			return nil, err
		}
		var b struct {
			Text string `json:"text"`
			MD   string `json:"md"`
		}
		if json.Unmarshal([]byte(body), &b) != nil {
			continue
		}
		text, role := b.Text, "user"
		if kind == "say" {
			text, role = b.MD, "agent"
		}
		if !holdsAll(text, words) {
			continue
		}
		h := Hit{ConvID: conv, EntryID: entry, At: at, Role: role, Snippet: msgindex.Snippet(text, words)}
		if turn.Valid && turn.String != "" {
			t := turn.String
			h.TurnID = &t
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// holdsAll: a word of the text begins with each of the query's words.
func holdsAll(text string, words []string) bool {
	have := msgindex.Tokens(text)
	for _, w := range words {
		found := false
		for _, h := range have {
			if strings.HasPrefix(h, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// SidebarMaxBytes is the longest sidebar document kept (the gateway's SIDEBAR_MAX_BYTES).
const SidebarMaxBytes = 256 * 1024

// ErrSidebarChanged: the document was changed since the revision the write was made on.
var ErrSidebarChanged = errors.New("history: the sidebar changed")

// Sidebar is the person's sidebar document and its revision; rev 0 and a nil doc when there is none yet.
func Sidebar(ctx context.Context, db *store.DB, userID string) (int64, json.RawMessage, error) {
	var rev int64
	var doc string
	err := db.QueryRow(ctx, `SELECT rev, doc FROM history_sidebars WHERE owner_id = ?`, userID).Scan(&rev, &doc)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return rev, json.RawMessage(doc), nil
}

// WriteSidebar writes the document made on baseRev (0: the person has none yet) as baseRev+1; a document changed
// meanwhile is not overwritten (ErrSidebarChanged).
func WriteSidebar(ctx context.Context, db *store.DB, userID string, baseRev int64, doc json.RawMessage, nowMs int64) (int64, error) {
	var res sql.Result
	var err error
	if baseRev == 0 {
		res, err = db.Exec(ctx, `INSERT INTO history_sidebars (owner_id, rev, doc, updated_at) VALUES (?, 1, ?, ?)
			ON CONFLICT (owner_id) DO NOTHING`, userID, string(doc), nowMs)
	} else {
		res, err = db.Exec(ctx, `UPDATE history_sidebars SET rev = rev + 1, doc = ?, updated_at = ? WHERE owner_id = ? AND rev = ?`,
			string(doc), nowMs, userID, baseRev)
	}
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrSidebarChanged
	}
	return baseRev + 1, nil
}

// The shape of a sidebar document, as the gateway checks it (its sidebar data module): the projects of the
// profile with the machine projects each stands for, the favorites' layout by chat id, the machines already merged in.
type sidebarDoc struct {
	Projects []struct {
		ID        *string  `json:"id"`
		Name      *string  `json:"name"`
		CreatedAt *float64 `json:"createdAt"`
		Members   []struct {
			MachineID *string `json:"machineId"`
			ProjectID *string `json:"projectId"`
		} `json:"members"`
	} `json:"projects"`
	Favorites *struct {
		Groups []struct {
			ID    *string  `json:"id"`
			Name  *string  `json:"name"`
			Chats []string `json:"chats"`
		} `json:"groups"`
		Loose []string `json:"loose"`
	} `json:"favorites"`
	Merged []string `json:"merged"`
}

var sidebarID = regexp.MustCompile(`^[A-Za-z0-9_:.-]{1,120}$`)

func sidebarIDs(v []string, max int) bool {
	if v == nil || len(v) > max {
		return false
	}
	for _, id := range v {
		if !sidebarID.MatchString(id) {
			return false
		}
	}
	return true
}

func sidebarName(v *string) bool {
	return v != nil && strings.TrimSpace(*v) != "" && len([]rune(*v)) <= 200
}

// ParseSidebar is the document as the page sent it, or false when it is not one: the shape is checked whole, nothing
// is fixed up.
func ParseSidebar(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) > SidebarMaxBytes {
		return nil, false
	}
	var d sidebarDoc
	if json.Unmarshal(raw, &d) != nil || d.Projects == nil || d.Favorites == nil || !sidebarIDs(d.Merged, 200) {
		return nil, false
	}
	if len(d.Projects) > 500 || d.Favorites.Groups == nil || len(d.Favorites.Groups) > 100 || !sidebarIDs(d.Favorites.Loose, 2000) {
		return nil, false
	}
	for _, p := range d.Projects {
		if p.ID == nil || !sidebarID.MatchString(*p.ID) || !sidebarName(p.Name) || p.CreatedAt == nil || p.Members == nil || len(p.Members) > 50 {
			return nil, false
		}
		for _, m := range p.Members {
			if m.MachineID == nil || m.ProjectID == nil || !sidebarID.MatchString(*m.MachineID) || !sidebarID.MatchString(*m.ProjectID) {
				return nil, false
			}
		}
	}
	for _, g := range d.Favorites.Groups {
		if g.ID == nil || !sidebarID.MatchString(*g.ID) || !sidebarName(g.Name) || !sidebarIDs(g.Chats, 2000) {
			return nil, false
		}
	}
	return raw, true
}

// MoveOwner gives the chats a machine sent for one person to another, when the machine is assigned to them (#57): the
// copy follows the machine's owner, nothing of it is dropped.
func MoveOwner(ctx context.Context, db *store.DB, machineID, from, to string) (int64, error) {
	res, err := db.Exec(ctx, `UPDATE history_conversations SET owner_id = ? WHERE machine_id = ? AND owner_id = ?`, to, machineID, from)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
