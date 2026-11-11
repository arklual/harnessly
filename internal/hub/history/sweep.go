package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The sweep of deleted chats works in small steps so that the first pass over a database that was never swept is as
// gentle as any other: a statement removes at most sweepChunk rows, statements are sweepPause apart, and a pass stops
// at sweepPassRows rows or sweepPassChats chats, whichever comes first. What is left waits for the next pass.
const (
	sweepLease     = "history-deleted-chats"
	sweepEvery     = 10 * time.Minute
	sweepFirst     = 2 * time.Minute
	sweepChunk     = 500
	sweepPause     = 200 * time.Millisecond
	sweepPassRows  = 20_000
	sweepPassChats = 50
)

// SweepDeps are what the sweeper is built from.
type SweepDeps struct {
	DB  *store.DB
	Log *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Keep is how long the copy of a chat deleted on its machine stays in the hub; zero or less keeps it for good.
	Keep time.Duration
}

// Sweeper removes the hub's copy of the chats their machines deleted, once the copy has been marked for longer than
// Keep. It touches nothing of a chat that is not marked: the mark is set only by the machine's own conversation_removed
// (ApplyFrame) and is lifted when the machine sends the chat again. Of the replicas of a hub one does a pass at a time
// (store.TakeLease).
type Sweeper struct {
	db     *store.DB
	log    *slog.Logger
	now    func() time.Time
	keep   time.Duration
	holder string

	every, pause               time.Duration
	chunk, passRows, passChats int
}

// NewSweeper builds the sweeper.
func NewSweeper(d SweepDeps) *Sweeper {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	s := &Sweeper{db: d.DB, log: d.Log, now: d.Now, keep: d.Keep, holder: hex.EncodeToString(b),
		every: sweepEvery, pause: sweepPause, chunk: sweepChunk, passRows: sweepPassRows, passChats: sweepPassChats}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// SweepResult is what a pass did.
type SweepResult struct {
	// Held: another replica has the job, this pass did nothing.
	Held bool
	// Chats is the chats removed whole; Rows the turns and entries removed, of those chats and of the one the pass
	// stopped in.
	Chats, Rows int
}

// Run sweeps until ctx ends: the first pass a little after the start, then one every sweepEvery.
func (s *Sweeper) Run(ctx context.Context) {
	if s.keep <= 0 {
		return
	}
	wait := sweepFirst
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = s.every
		res, err := s.Pass(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			metrics.HistoryPurgeErrors.Inc()
			s.log.Warn("history: deleted chats not swept", "error", err, "chats", res.Chats, "rows", res.Rows)
		case res.Chats > 0 || res.Rows > 0:
			s.log.Info("history: deleted chats swept", "chats", res.Chats, "rows", res.Rows)
		}
	}
}

// Pass removes what one pass may: the chats marked deleted for longer than Keep, oldest mark first.
func (s *Sweeper) Pass(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	if s.keep <= 0 {
		return res, nil
	}
	// The lease outlives the time to the next pass, so the replica that sweeps keeps the job while it is alive.
	ttl := s.every + s.every/2
	ok, err := s.db.S().TakeLease(ctx, sweepLease, s.holder, s.now(), ttl)
	if err != nil {
		return res, err
	}
	if !ok {
		res.Held = true
		return res, nil
	}
	before := store.Ms(s.now().Add(-s.keep))
	rows, err := s.db.Query(ctx, `SELECT machine_id, conv_id, owner_id FROM history_conversations
		WHERE deleted_at IS NOT NULL AND deleted_at <= ? ORDER BY deleted_at LIMIT ?`, before, s.passChats)
	if err != nil {
		return res, err
	}
	type chat struct{ machine, conv, owner string }
	var chats []chat
	for rows.Next() {
		var c chat
		if err := rows.Scan(&c.machine, &c.conv, &c.owner); err != nil {
			rows.Close()
			return res, err
		}
		chats = append(chats, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, c := range chats {
		whole, err := s.chat(ctx, c.machine, c.conv, c.owner, before, &res)
		if err != nil {
			return res, err
		}
		if !whole {
			break // the pass's rows are spent
		}
	}
	return res, nil
}

// chat removes one chat's entries and turns chunk by chunk and then its row, its channel cursor and its owner's review
// drafts of it; false: the pass ran out of rows before the chat was gone. Every statement asks again that the chat is still marked and past its term, so
// a chat its machine sent again in the meantime is left alone.
func (s *Sweeper) chat(ctx context.Context, machine, conv, owner string, before int64, res *SweepResult) (bool, error) {
	for _, t := range []struct{ table, key string }{{"history_entries", "entry_id"}, {"history_turns", "turn_id"}} {
		for {
			if res.Rows >= s.passRows {
				return false, nil
			}
			limit := min(s.chunk, s.passRows-res.Rows)
			r, err := s.db.Exec(ctx, `DELETE FROM `+t.table+` WHERE machine_id = ? AND conv_id = ? AND `+t.key+` IN (
					SELECT `+t.key+` FROM `+t.table+` WHERE machine_id = ? AND conv_id = ? LIMIT ?)
				AND EXISTS (SELECT 1 FROM history_conversations c WHERE c.machine_id = ? AND c.conv_id = ?
					AND c.deleted_at IS NOT NULL AND c.deleted_at <= ?)`,
				machine, conv, machine, conv, limit, machine, conv, before)
			if err != nil {
				return false, err
			}
			n, err := r.RowsAffected()
			if err != nil {
				return false, err
			}
			res.Rows += int(n)
			metrics.HistoryRowsPurged.Add(n)
			if n < int64(limit) {
				break
			}
			if err := s.rest(ctx); err != nil {
				return false, err
			}
		}
	}
	var gone int64
	err := s.db.Tx(ctx, func(st store.Store) error {
		q := st.Q()
		// A turn or an entry that arrived since keeps the row: the next pass takes them all.
		r, err := q.Exec(ctx, `DELETE FROM history_conversations WHERE machine_id = ? AND conv_id = ?
				AND deleted_at IS NOT NULL AND deleted_at <= ?
				AND NOT EXISTS (SELECT 1 FROM history_entries e WHERE e.machine_id = ? AND e.conv_id = ?)
				AND NOT EXISTS (SELECT 1 FROM history_turns t WHERE t.machine_id = ? AND t.conv_id = ?)`,
			machine, conv, before, machine, conv, machine, conv)
		if err != nil {
			return err
		}
		if gone, err = r.RowsAffected(); err != nil || gone == 0 {
			return err
		}
		if _, err := q.Exec(ctx, `DELETE FROM history_cursors WHERE machine_id = ? AND ch = ?`, machine, "conv:"+conv); err != nil {
			return err
		}
		// The review drafts are the person's and keyed by the chat alone: they go with the last copy of the chat.
		_, err = q.Exec(ctx, `DELETE FROM review_notes WHERE owner_id = ? AND conv_id = ?
				AND NOT EXISTS (SELECT 1 FROM history_conversations c WHERE c.owner_id = ? AND c.conv_id = ?)`,
			owner, conv, owner, conv)
		return err
	})
	if err != nil {
		return false, err
	}
	res.Chats += int(gone)
	metrics.HistoryChatsPurged.Add(gone)
	return true, s.rest(ctx)
}

// rest is the pause between two statements of a pass.
func (s *Sweeper) rest(ctx context.Context) error {
	if s.pause <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.pause):
		return nil
	}
}
