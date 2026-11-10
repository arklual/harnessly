package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	// SnapshotTurns is the turns of a chat's first page, as the machine's own snapshot has them.
	SnapshotTurns = 33
	// FullTurns of the newest turns of a page come with all their steps; older ones with the entry that opened them and
	// their answer.
	FullTurns = 3
	// PageTurnsMax is the most turns one page may ask for.
	PageTurnsMax = 100
)

// Chat is one stored chat of the person: its machine and its record as the machine last described it.
type Chat struct {
	MachineID string
	Record    json.RawMessage
}

// List is the person's chats on the given machines that are not deleted, newest prompt first.
func List(ctx context.Context, db *store.DB, userID string, machines []string) ([]Chat, error) {
	if len(machines) == 0 {
		return nil, nil
	}
	in := map[string]bool{}
	for _, m := range machines {
		in[m] = true
	}
	rows, err := db.Query(ctx, `SELECT machine_id, record FROM history_conversations
		WHERE owner_id = ? AND record IS NOT NULL AND deleted_at IS NULL
		ORDER BY last_prompt_ms DESC, conv_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var record string
		if err := rows.Scan(&c.MachineID, &record); err != nil {
			return nil, err
		}
		if !in[c.MachineID] {
			continue
		}
		c.Record = json.RawMessage(record)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Page is a chat's feed in the shape the machine gives (`conv:<id>` snapshot turns and entries): turns oldest first,
// the steps of the newest FullTurns of them, of every other the entry that opened it and its answer.
type Page struct {
	MachineID    string            `json:"machineId"`
	Conversation json.RawMessage   `json:"conversation"`
	Turns        []json.RawMessage `json:"turns"`
	Entries      []json.RawMessage `json:"entries"`
	BeforeTurnID *string           `json:"beforeTurnId"`
	// Complete: the copy holds the chat back to its first turn (history_done, HRN-879). Until then the oldest turn kept
	// still offers more, and a page past what is kept comes back empty and incomplete: the page asks the machine for
	// the rest when it is there, instead of taking a cut feed for the whole one.
	Complete bool `json:"complete"`
}

// ErrNotFound: the person has no such chat on a machine they may open.
var ErrNotFound = errors.New("history: no such chat")

// head is the newest stored copy of a chat of the person on one of the machines.
func head(ctx context.Context, db *store.DB, userID, conv string, machines []string) (string, json.RawMessage, bool, error) {
	if !convID.MatchString(conv) || len(machines) == 0 {
		return "", nil, false, ErrNotFound
	}
	in := map[string]bool{}
	for _, m := range machines {
		in[m] = true
	}
	rows, err := db.Query(ctx, `SELECT machine_id, record, history_done FROM history_conversations
		WHERE conv_id = ? AND owner_id = ? AND record IS NOT NULL AND deleted_at IS NULL ORDER BY updated_at DESC`, conv, userID)
	if err != nil {
		return "", nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var machine, record string
		var done int
		if err := rows.Scan(&machine, &record, &done); err != nil {
			return "", nil, false, err
		}
		if in[machine] {
			return machine, json.RawMessage(record), done != 0, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, false, err
	}
	return "", nil, false, ErrNotFound
}

// Read is a page of a chat: up to limit turns ending right before the turn `before` ("" for the newest).
func Read(ctx context.Context, db *store.DB, userID, conv string, machines []string, before string, limit int) (Page, error) {
	machine, record, complete, err := head(ctx, db, userID, conv, machines)
	if err != nil {
		return Page{}, err
	}
	limit = min(max(limit, 1), PageTurnsMax)
	q := `SELECT body, started_ms, turn_id FROM history_turns WHERE machine_id = ? AND conv_id = ? AND left_after IS NULL`
	args := []any{machine, conv}
	if before != "" {
		var started int64
		err := db.QueryRow(ctx, `SELECT started_ms FROM history_turns WHERE machine_id = ? AND conv_id = ? AND turn_id = ?`,
			machine, conv, before).Scan(&started)
		if errors.Is(err, sql.ErrNoRows) {
			return Page{MachineID: machine, Conversation: record, Turns: []json.RawMessage{}, Entries: []json.RawMessage{}, Complete: complete}, nil
		}
		if err != nil {
			return Page{}, err
		}
		q += ` AND (started_ms < ? OR (started_ms = ? AND turn_id < ?))`
		args = append(args, started, started, before)
	}
	q += ` ORDER BY started_ms DESC, turn_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	type turn struct {
		body    json.RawMessage
		started int64
		id      string
	}
	var found []turn
	for rows.Next() {
		var t turn
		var body string
		if err := rows.Scan(&body, &t.started, &t.id); err != nil {
			rows.Close()
			return Page{}, err
		}
		t.body = json.RawMessage(body)
		found = append(found, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	more := len(found) > limit
	if more {
		found = found[:limit]
	}
	page := Page{MachineID: machine, Conversation: record, Turns: make([]json.RawMessage, 0, len(found)), Entries: []json.RawMessage{}, Complete: complete}
	for i := len(found) - 1; i >= 0; i-- {
		page.Turns = append(page.Turns, found[i].body)
	}
	if (more || !complete) && len(page.Turns) > 0 {
		id := found[len(found)-1].id
		page.BeforeTurnID = &id
	}
	if len(found) == 0 {
		return page, nil
	}
	// found is newest first: the newest FullTurns come whole, the rest by their face.
	var full, faces []string
	first := found[len(found)-1].started
	for i, t := range found {
		if i < FullTurns {
			full = append(full, t.id)
			continue
		}
		faces = append(faces, t.id)
		if b, ok := object(t.body); ok {
			if answer, ok := str(b["answerEntryId"]); ok && answer != "" {
				faces = append(faces, answer)
			}
		}
	}
	page.Entries, err = entries(ctx, db, machine, conv, full, faces, first)
	return page, err
}

func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func entries(ctx context.Context, db *store.DB, machine, conv string, full, faces []string, first int64) ([]json.RawMessage, error) {
	args := []any{machine, conv}
	for _, id := range full {
		args = append(args, id)
	}
	for _, id := range faces {
		args = append(args, id)
	}
	args = append(args, first)
	rows, err := db.Query(ctx, `SELECT body FROM history_entries WHERE machine_id = ? AND conv_id = ?
		AND (turn_id IN (`+placeholders(len(full))+`) OR entry_id IN (`+placeholders(len(faces))+`) OR (turn_id IS NULL AND at_ms >= ?))
		ORDER BY at_ms, ord`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, pageEntry(json.RawMessage(body)))
	}
	return out, rows.Err()
}

// TurnEntries are the steps of one folded turn, as `GET …/turns/<id>/entries` gives them.
func TurnEntries(ctx context.Context, db *store.DB, userID, conv, turn string, machines []string) (string, []json.RawMessage, error) {
	machine, _, _, err := head(ctx, db, userID, conv, machines)
	if err != nil {
		return "", nil, err
	}
	rows, err := db.Query(ctx, `SELECT body FROM history_entries WHERE machine_id = ? AND conv_id = ? AND turn_id = ? ORDER BY at_ms, ord`,
		machine, conv, turn)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return "", nil, err
		}
		out = append(out, pageEntry(json.RawMessage(body)))
	}
	return machine, out, rows.Err()
}

// Delivered tells whether a file with this sha256 was handed over in one of the person's chats on the machine, as the
// hub's copy has it: the space's `files` collection is the machines', and a person reads from it only what their own
// chats show them.
func Delivered(ctx context.Context, db *store.DB, userID, machineID, sha string) (bool, error) {
	if len(sha) != 64 || strings.Trim(sha, "0123456789abcdef") != "" {
		return false, nil
	}
	var one int
	err := db.QueryRow(ctx, `SELECT 1 FROM history_turns t JOIN history_conversations c ON c.machine_id = t.machine_id AND c.conv_id = t.conv_id
		WHERE t.machine_id = ? AND c.owner_id = ? AND c.deleted_at IS NULL AND t.body LIKE ? LIMIT 1`,
		machineID, userID, `%"sha256":"`+sha+`"%`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// DeliveredFile is a file handed over in a chat as its turn names it.
type DeliveredFile struct {
	Sha256 string
	Mime   string
	Name   string
}

// deliveredScan is how many of the person's newest turns with a handed-over file a lookup by path reads.
const deliveredScan = 500

// DeliveredAt is the newest file handed over at this path in one of the person's chats on the machine; ok false when
// none of them names it. The pane opens a handed-over file by its path, and reads it from the hub with the machine off.
func DeliveredAt(ctx context.Context, db *store.DB, userID, machineID, path string) (DeliveredFile, bool, error) {
	rows, err := db.Query(ctx, `SELECT t.body FROM history_turns t JOIN history_conversations c ON c.machine_id = t.machine_id AND c.conv_id = t.conv_id
		WHERE t.machine_id = ? AND c.owner_id = ? AND c.deleted_at IS NULL AND t.body LIKE ? ORDER BY t.started_ms DESC LIMIT ?`,
		machineID, userID, `%"delivery"%`, deliveredScan)
	if err != nil {
		return DeliveredFile{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return DeliveredFile{}, false, err
		}
		var turn struct {
			Outputs []struct {
				Kind   string `json:"kind"`
				Ref    string `json:"ref"`
				Name   string `json:"name"`
				Mime   string `json:"mime"`
				Sha256 string `json:"sha256"`
			} `json:"outputs"`
		}
		if json.Unmarshal([]byte(body), &turn) != nil {
			continue
		}
		for i := len(turn.Outputs) - 1; i >= 0; i-- {
			o := turn.Outputs[i]
			if o.Kind == "delivery" && o.Ref == path && len(o.Sha256) == 64 {
				return DeliveredFile{Sha256: o.Sha256, Mime: o.Mime, Name: o.Name}, true, nil
			}
		}
	}
	return DeliveredFile{}, false, rows.Err()
}
