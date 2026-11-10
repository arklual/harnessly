// Package history keeps the person's chats in the hub (docs/hub/history.md): the chat list and every chat's
// feed, written from the frames a machine's agent sends of its runtime's conversation channels (`convs`, `conv:<id>`)
// and read by the chat's owner while the machine is on or off. The model is a closed cluster's copy of the chats: a
// channel's events carry a seq of the runtime's epoch and a cursor says how far each channel is applied; a backfill
// (the chat list, pages of turns) carries no seq and is applied by the stable ids; every chat has a cursor of its own
// that says how far its pages were walked, so a chat ends up whole even when walks are cut short.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Owner is the machine a frame came from and the person its chats belong to: the machine's own, never anything a
// frame says.
type Owner struct {
	MachineID string
	UserID    string
}

var convID = regexp.MustCompile(`^tab-[A-Za-z0-9_-]{1,200}$`)

// convOf is the chat of a `conv:<id>` channel; "" for any other.
func convOf(ch string) string {
	id, ok := strings.CutPrefix(ch, "conv:")
	if !ok || !convID.MatchString(id) {
		return ""
	}
	return id
}

// replicated: the channels the history is made of.
func replicated(ch string) bool { return ch == "convs" || convOf(ch) != "" }

// ErrBadFrame is a frame that names no chat channel or carries too many events.
var ErrBadFrame = errors.New("history: bad frame")

// FrameResult is what an applied frame of live events tells the agent (hubproto.HistoryAck).
type FrameResult struct {
	AckSeq  int64
	Epoch   string
	Applied int
	Gap     bool
}

// ord is the order entries first arrived in, to tell apart entries of one turn that share a millisecond.
var ordClock atomic.Int64

func nextOrd() int64 {
	now := time.Now().UnixNano()
	for {
		last := ordClock.Load()
		next := max(now, last+1)
		if ordClock.CompareAndSwap(last, next) {
			return next
		}
	}
}

type obj = map[string]json.RawMessage

func str(raw json.RawMessage) (string, bool) {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func num(raw json.RawMessage) (int64, bool) {
	var f float64
	if raw == nil || json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return int64(f), true
}

func nullNum(raw json.RawMessage) any {
	if n, ok := num(raw); ok {
		return n
	}
	return nil
}

func object(raw json.RawMessage) (obj, bool) {
	var o obj
	if raw == nil || json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, false
	}
	return o, true
}

// ApplyFrame applies a frame of live events of one channel. The cursor says how far the channel is applied: a frame is
// cut to what comes after it, and a frame that starts later than the cursor is not applied at all (Gap) — the agent
// resends from AckSeq+1. A new epoch (the runtime restarted) starts a new count; Reset is a new baseline.
func ApplyFrame(ctx context.Context, db *store.DB, o Owner, f hubproto.HistoryEvents, now time.Time) (FrameResult, error) {
	if !replicated(f.Ch) || f.Epoch == "" || f.FromSeq < 1 || len(f.Events) > hubproto.HistoryFrameMax {
		return FrameResult{}, ErrBadFrame
	}
	// Nothing is stored with a secret in it: the gateway's rules, before the transaction (redact.go).
	f.Events = redactEvents(f.Events)
	var res FrameResult
	err := db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		var epoch string
		var seq int64
		err := q.QueryRow(ctx, `SELECT epoch, seq FROM history_cursors WHERE machine_id = ? AND ch = ?`, o.MachineID, f.Ch).Scan(&epoch, &seq)
		stored := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		last := f.FromSeq + int64(len(f.Events)) - 1
		continues := stored && epoch == f.Epoch
		from := f.FromSeq
		if continues && !f.Reset {
			if f.FromSeq > seq+1 {
				res = FrameResult{AckSeq: seq, Epoch: epoch, Gap: true}
				return nil
			}
			if last <= seq {
				res = FrameResult{AckSeq: seq, Epoch: epoch}
				return nil
			}
			from = seq + 1
		}
		for n := from; n <= last; n++ {
			if err := applyEvent(ctx, q, o, f.Ch, f.Events[n-f.FromSeq], n, now); err != nil {
				return err
			}
			res.Applied++
		}
		if last >= f.FromSeq {
			if _, err := q.Exec(ctx, `INSERT INTO history_cursors (machine_id, ch, epoch, seq, updated_at) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (machine_id, ch) DO UPDATE SET epoch = excluded.epoch, seq = excluded.seq, updated_at = excluded.updated_at`,
				o.MachineID, f.Ch, f.Epoch, last, store.Ms(now)); err != nil {
				return err
			}
		}
		res.AckSeq, res.Epoch = last, f.Epoch
		if continues && !f.Reset {
			res.AckSeq = max(last, seq)
		}
		return nil
	})
	return res, err
}

// ApplyBackfill applies events made from what the machine holds now; they carry no seq and change no channel cursor.
func ApplyBackfill(ctx context.Context, db *store.DB, o Owner, f hubproto.HistoryBackfill, now time.Time) error {
	if !replicated(f.Ch) || len(f.Events) > hubproto.HistoryFrameMax {
		return ErrBadFrame
	}
	f.Events = redactEvents(f.Events)
	return db.Tx(ctx, func(s store.Store) error {
		for _, ev := range f.Events {
			if err := applyEvent(ctx, s.Q(), o, f.Ch, ev, 0, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func applyEvent(ctx context.Context, q store.Q, o Owner, ch string, raw json.RawMessage, seq int64, now time.Time) error {
	e, ok := object(raw)
	if !ok {
		return nil
	}
	kind, _ := str(e["type"])
	if ch == "convs" {
		switch kind {
		case "conversation_upsert":
			return upsertConversation(ctx, q, o, e["conv"], seq, now)
		case "conversation_state", "conversation_seen":
			return changeConversation(ctx, q, o, kind, e, seq, now)
		case "conversation_removed":
			id, _ := str(e["id"])
			_, err := q.Exec(ctx, `UPDATE history_conversations SET deleted_at = ?, seq = ?, updated_at = ?
				WHERE machine_id = ? AND conv_id = ? AND owner_id = ? AND deleted_at IS NULL`,
				store.Ms(now), seq, store.Ms(now), o.MachineID, id, o.UserID)
			return err
		}
		return nil // projects, favorites and drafts are not part of the history
	}
	if conv := convOf(ch); conv != "" {
		return applyConvEvent(ctx, q, o, conv, kind, e, now, seq > 0)
	}
	return nil
}

// ensure makes the row of a chat a frame names, a stub when only its turns have arrived; false: the chat is somebody
// else's (never written to).
func ensure(ctx context.Context, q store.Q, o Owner, conv string, now time.Time) (bool, error) {
	if _, err := q.Exec(ctx, `INSERT INTO history_conversations (machine_id, conv_id, owner_id, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id, conv_id) DO NOTHING`, o.MachineID, conv, o.UserID, store.Ms(now)); err != nil {
		return false, err
	}
	var owner string
	if err := q.QueryRow(ctx, `SELECT owner_id FROM history_conversations WHERE machine_id = ? AND conv_id = ?`, o.MachineID, conv).Scan(&owner); err != nil {
		return false, err
	}
	return owner == o.UserID, nil
}

func upsertConversation(ctx context.Context, q store.Q, o Owner, raw json.RawMessage, seq int64, now time.Time) error {
	c, ok := object(raw)
	if !ok {
		return nil
	}
	id, _ := str(c["id"])
	rev, hasRev := num(c["rev"])
	if !convID.MatchString(id) || !hasRev {
		return nil
	}
	if mine, err := ensure(ctx, q, o, id, now); err != nil || !mine {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE history_conversations SET record = ?, rev = ?, seq = ?, last_prompt_ms = ?, last_activity_ms = ?,
		deleted_at = NULL, updated_at = ?
		WHERE machine_id = ? AND conv_id = ? AND owner_id = ? AND (record IS NULL OR rev <= ?)`,
		string(raw), rev, seq, nullNum(c["lastPromptAt"]), nullNum(c["lastActivityAt"]), store.Ms(now),
		o.MachineID, id, o.UserID, rev)
	return err
}

// changeConversation follows a chat's state and seen mark into its stored record.
func changeConversation(ctx context.Context, q store.Q, o Owner, kind string, e obj, seq int64, now time.Time) error {
	id, _ := str(e["id"])
	rev, ok := num(e["rev"])
	if !ok || !convID.MatchString(id) {
		return nil
	}
	field, value := "state", e["state"]
	if kind == "conversation_seen" {
		field, value = "seenAt", e["seenAt"]
		if _, ok := num(value); !ok {
			return nil
		}
	} else if _, ok := object(value); !ok {
		return nil
	}
	var record *string
	var have int64
	err := q.QueryRow(ctx, `SELECT record, rev FROM history_conversations WHERE machine_id = ? AND conv_id = ? AND owner_id = ?`,
		o.MachineID, id, o.UserID).Scan(&record, &have)
	if err != nil || record == nil || have > rev {
		return nil
	}
	c, ok := object(json.RawMessage(*record))
	if !ok {
		return nil
	}
	c[field] = value
	c["rev"], _ = json.Marshal(rev)
	out, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE history_conversations SET record = ?, rev = ?, seq = ?, updated_at = ?
		WHERE machine_id = ? AND conv_id = ? AND owner_id = ?`, string(out), rev, seq, store.Ms(now), o.MachineID, id, o.UserID)
	return err
}

var convKinds = map[string]bool{
	"turn_started": true, "turn_finished": true, "turn_failed": true, "step": true, "entry_update": true,
	"entry_replace": true, "compaction": true, "feed_reset": true,
}

// applyConvEvent is one event of a `conv:<id>` channel. Only what the history is made of is stored; the live parts
// (queue, gates, the agent's `now`) stay on the machine.
func applyConvEvent(ctx context.Context, q store.Q, o Owner, conv, kind string, e obj, now time.Time, live bool) error {
	if !convKinds[kind] {
		return nil
	}
	if mine, err := ensure(ctx, q, o, conv, now); err != nil || !mine {
		return err
	}
	switch kind {
	case "turn_started", "turn_finished", "turn_failed":
		return storeTurn(ctx, q, o, conv, e["turn"])
	case "step", "entry_update", "compaction":
		if err := storeEntry(ctx, q, o, conv, e["entry"]); err != nil {
			return err
		}
		counts, _ := object(e["counts"])
		entry, _ := object(e["entry"])
		turn, _ := str(entry["turnId"])
		tools, okT := num(counts["toolCount"])
		steps, okS := num(counts["stepCount"])
		if turn == "" || !okT || !okS {
			return nil
		}
		return bumpCounts(ctx, q, o, conv, turn, tools, steps)
	case "entry_replace":
		entry, _ := object(e["entry"])
		newID, _ := str(entry["id"])
		if oldID, ok := str(e["oldId"]); ok && oldID != "" && oldID != newID {
			if _, err := q.Exec(ctx, `DELETE FROM history_entries WHERE machine_id = ? AND conv_id = ? AND entry_id = ?`, o.MachineID, conv, oldID); err != nil {
				return err
			}
		}
		return storeEntry(ctx, q, o, conv, e["entry"])
	case "feed_reset":
		var turns, entries []json.RawMessage
		_ = json.Unmarshal(e["turns"], &turns)
		_ = json.Unmarshal(e["entries"], &entries)
		for _, t := range turns {
			if err := storeTurn(ctx, q, o, conv, t); err != nil {
				return err
			}
		}
		for _, x := range entries {
			if err := storeEntry(ctx, q, o, conv, x); err != nil {
				return err
			}
		}
		if live {
			if err := leaveTurns(ctx, q, o, conv, turns); err != nil {
				return err
			}
		}
		return moveCursor(ctx, q, o, conv, e)
	}
	return nil
}

// leaveTurns: a live `feed_reset` is the machine putting the chat on another transcript (a rewind or its undo), the
// newest turns of the feed as it is now. A stored turn from the oldest of them on that is not among them fell out of the
// feed: it is marked as left after the newest turn kept (” when none was kept), as the rewind's mark names it.
func leaveTurns(ctx context.Context, q store.Q, o Owner, conv string, turns []json.RawMessage) error {
	type kept struct {
		id      string
		started int64
	}
	var in []kept
	for _, raw := range turns {
		t, ok := object(raw)
		if !ok {
			continue
		}
		id, _ := str(t["id"])
		started, okS := num(t["startedAt"])
		if id != "" && okS {
			in = append(in, kept{id, started})
		}
	}
	newest, newestAt, from := "", int64(math.MinInt64), int64(math.MinInt64)
	args := []any{"", o.MachineID, conv}
	for i, k := range in {
		if i == 0 || k.started < from {
			from = k.started
		}
		if newest == "" || k.started >= newestAt {
			newest, newestAt = k.id, k.started
		}
		args = append(args, k.id)
	}
	args[0] = newest
	args = append(args, from)
	notIn := ""
	if len(in) > 0 {
		notIn = ` AND turn_id NOT IN (` + placeholders(len(in)) + `)`
	}
	_, err := q.Exec(ctx, `UPDATE history_turns SET left_after = ? WHERE machine_id = ? AND conv_id = ? AND left_after IS NULL`+
		notIn+` AND started_ms >= ?`, args...)
	return err
}

// storeTurn upserts a turn; a turn that has ended is not made running again by a late copy of an earlier state of it.
func storeTurn(ctx context.Context, q store.Q, o Owner, conv string, raw json.RawMessage) error {
	t, ok := object(raw)
	if !ok {
		return nil
	}
	id, _ := str(t["id"])
	started, okS := num(t["startedAt"])
	status, _ := str(t["status"])
	if id == "" || !okS || status == "" {
		return nil
	}
	_, err := q.Exec(ctx, `INSERT INTO history_turns (machine_id, conv_id, turn_id, started_ms, ended_ms, status, body) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, conv_id, turn_id) DO UPDATE SET started_ms = excluded.started_ms, ended_ms = excluded.ended_ms,
			status = excluded.status, body = excluded.body, left_after = NULL
		WHERE history_turns.ended_ms IS NULL OR excluded.ended_ms IS NOT NULL`,
		o.MachineID, conv, id, started, nullNum(t["endedAt"]), status, string(raw))
	return err
}

// storeEntry upserts an entry by its stable id, keeping the higher revision.
func storeEntry(ctx context.Context, q store.Q, o Owner, conv string, raw json.RawMessage) error {
	x, ok := object(raw)
	if !ok {
		return nil
	}
	id, _ := str(x["id"])
	at, okA := num(x["at"])
	kind, _ := str(x["t"])
	if id == "" || !okA || kind == "" {
		return nil
	}
	var turn any
	if t, ok := str(x["turnId"]); ok {
		turn = t
	}
	rev, _ := num(x["rev"])
	raw = keepLonger(ctx, q, o, conv, id, rev, x, raw)
	_, err := q.Exec(ctx, `INSERT INTO history_entries (machine_id, conv_id, entry_id, turn_id, at_ms, rev, kind, ord, body) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, conv_id, entry_id) DO UPDATE SET turn_id = excluded.turn_id, at_ms = excluded.at_ms, rev = excluded.rev,
			kind = excluded.kind, body = excluded.body
		WHERE history_entries.rev <= excluded.rev`,
		o.MachineID, conv, id, turn, at, rev, kind, nextOrd(), string(raw))
	return err
}

// bumpCounts follows the tool and step counts of a running turn into its stored body.
func bumpCounts(ctx context.Context, q store.Q, o Owner, conv, turn string, tools, steps int64) error {
	var body string
	err := q.QueryRow(ctx, `SELECT body FROM history_turns WHERE machine_id = ? AND conv_id = ? AND turn_id = ? AND ended_ms IS NULL`,
		o.MachineID, conv, turn).Scan(&body)
	if err != nil {
		return nil // no running turn of that id: nothing to follow
	}
	t, ok := object(json.RawMessage(body))
	if !ok {
		return nil
	}
	t["toolCount"], _ = json.Marshal(tools)
	t["stepCount"], _ = json.Marshal(steps)
	out, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE history_turns SET body = ? WHERE machine_id = ? AND conv_id = ? AND turn_id = ? AND ended_ms IS NULL`,
		string(out), o.MachineID, conv, turn)
	return err
}

// moveCursor: a page of an agent's walk through a chat says where it was read from (`history.from`, "" for the newest
// page). When that is where the chat's walk stands, the walk moves to the page's `beforeTurnId`, and a page with none is
// the chat's first, so the chat is held whole. A page read from anywhere else moves nothing.
func moveCursor(ctx context.Context, q store.Q, o Owner, conv string, e obj) error {
	mark, ok := object(e["history"])
	if !ok {
		return nil
	}
	from, ok := str(mark["from"])
	if !ok {
		return nil
	}
	next, _ := str(e["beforeTurnId"])
	var before any
	done := 1
	if next != "" {
		before, done = next, 0
	}
	_, err := q.Exec(ctx, `UPDATE history_conversations SET history_before = ?, history_done = ?
		WHERE machine_id = ? AND conv_id = ? AND owner_id = ? AND history_done = 0 AND COALESCE(history_before, '') = ?`,
		before, done, o.MachineID, conv, o.UserID, from)
	return err
}

// Start is what the agent is told when its link comes up: where every channel stands, the revision of every chat with
// a record, and the chats not held whole with the turn their walk goes on before.
func Start(ctx context.Context, db *store.DB, machineID string) (hubproto.HistoryStart, error) {
	st := hubproto.HistoryStart{Enabled: true, Cursors: map[string]hubproto.HistoryCursor{}, Known: map[string]int64{}, Pending: map[string]string{}}
	rows, err := db.Query(ctx, `SELECT ch, epoch, seq FROM history_cursors WHERE machine_id = ?`, machineID)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var ch string
		var c hubproto.HistoryCursor
		if err := rows.Scan(&ch, &c.Epoch, &c.Seq); err != nil {
			rows.Close()
			return st, err
		}
		st.Cursors[ch] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	rows, err = db.Query(ctx, `SELECT conv_id, rev, history_before, history_done FROM history_conversations WHERE machine_id = ? AND record IS NOT NULL`, machineID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var rev int64
		var before *string
		var done int
		if err := rows.Scan(&id, &rev, &before, &done); err != nil {
			return st, err
		}
		st.Known[id] = rev
		if done == 0 {
			st.Pending[id] = ""
			if before != nil {
				st.Pending[id] = *before
			}
		}
	}
	return st, rows.Err()
}
