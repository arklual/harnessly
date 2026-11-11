package history

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The work of the chats (hubproto.ChatWork, table chat_work): the machine's agent sends the records its runtime keeps,
// the hub stores the newest of each chat's worktree, and a chat's «Changes» read the work of the chat and of the chats
// delegated from it at any depth, on any of the person's machines, whether the chats are open or not.

var (
	workSha    = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	workBranch = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,255}$`)
)

const (
	workPathMax   = 1024
	workURLMax    = 512
	workTreeDepth = 8   // how deep the delegated chats are followed
	workTreeMax   = 500 // the most chats one subtree reads
)

// cleanWork is the record as it may be stored, false for one that is not a chat's work.
func cleanWork(e hubproto.ChatWorkEntry) (hubproto.ChatWorkEntry, bool) {
	if !convID.MatchString(e.Conv) || (e.Parent != "" && !convID.MatchString(e.Parent)) {
		return e, false
	}
	for _, p := range []string{e.Worktree, e.Repo} {
		if len(p) > workPathMax || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
			return e, false
		}
	}
	if e.Branch != "" && !workBranch.MatchString(e.Branch) {
		e.Branch = ""
	}
	if e.Base != "" && !workSha.MatchString(e.Base) {
		e.Base = ""
	}
	if e.Head != "" && !workSha.MatchString(e.Head) {
		e.Head = ""
	}
	commits := e.Commits[:0:0]
	for _, c := range e.Commits {
		if workSha.MatchString(c) && len(commits) < hubproto.ChatWorkCommitsMax {
			commits = append(commits, c)
		}
	}
	e.Commits = commits
	if len(e.Remote) > workURLMax || strings.ContainsAny(e.Remote, "\x00\n") {
		e.Remote = ""
	}
	if len(e.MR) > workURLMax || !strings.HasPrefix(e.MR, "https://") && !strings.HasPrefix(e.MR, "http://") {
		e.MR = ""
	}
	if e.Source != hubproto.ChatWorkLive && e.Source != hubproto.ChatWorkScan {
		e.Source = hubproto.ChatWorkLive
	}
	return e, true
}

// ApplyWork stores the records a machine sent for its owner. A record older than the stored one of its chat and
// worktree changes nothing.
func ApplyWork(ctx context.Context, db *store.DB, o Owner, entries []hubproto.ChatWorkEntry, now time.Time) error {
	if len(entries) > hubproto.ChatWorkFrameMax {
		return ErrBadFrame
	}
	return db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		for _, raw := range entries {
			e, ok := cleanWork(raw)
			if !ok {
				continue
			}
			commits, _ := json.Marshal(e.Commits)
			if _, err := q.Exec(ctx, `INSERT INTO chat_work (machine_id, conv_id, worktree, owner_id, parent_id, repo, branch,
					base_sha, head_sha, commits, remote, mr, dirty, source, since_ms, at_ms, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (machine_id, conv_id, worktree) DO UPDATE SET parent_id = excluded.parent_id, repo = excluded.repo,
					branch = excluded.branch, base_sha = excluded.base_sha, head_sha = excluded.head_sha,
					commits = excluded.commits, remote = excluded.remote, mr = excluded.mr, dirty = excluded.dirty,
					source = excluded.source, since_ms = excluded.since_ms, at_ms = excluded.at_ms,
					updated_at = excluded.updated_at
				WHERE chat_work.owner_id = excluded.owner_id AND chat_work.at_ms <= excluded.at_ms`,
				o.MachineID, e.Conv, e.Worktree, o.UserID, e.Parent, e.Repo, e.Branch, e.Base, e.Head, string(commits),
				e.Remote, e.MR, boolInt(e.Dirty), e.Source, e.Since, e.At, store.Ms(now)); err != nil {
				return err
			}
		}
		return nil
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// WorkEntry is a stored record with the machine it is on.
type WorkEntry struct {
	hubproto.ChatWorkEntry
	MachineID string `json:"machineId"`
}

// Work is the person's stored work of a chat and of the chats delegated from it, at any depth, on the given machines,
// the newest look first.
func Work(ctx context.Context, db *store.DB, userID, conv string, machines []string) ([]WorkEntry, error) {
	if !convID.MatchString(conv) || len(machines) == 0 {
		return []WorkEntry{}, nil
	}
	if err := fillParents(ctx, db, userID); err != nil {
		return nil, err
	}
	ids, err := subtree(ctx, db, userID, conv)
	if err != nil {
		return nil, err
	}
	q := `SELECT machine_id, conv_id, worktree, parent_id, repo, branch, base_sha, head_sha, commits, remote, mr, dirty,
			source, since_ms, at_ms
		FROM chat_work WHERE owner_id = ? AND conv_id IN (` + marks(len(ids)) + `) AND machine_id IN (` + marks(len(machines)) + `)
		ORDER BY at_ms DESC, conv_id, worktree`
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
	}
	for _, m := range machines {
		args = append(args, m)
	}
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkEntry{}
	for rows.Next() {
		var w WorkEntry
		var commits string
		var dirty int
		if err := rows.Scan(&w.MachineID, &w.Conv, &w.Worktree, &w.Parent, &w.Repo, &w.Branch, &w.Base, &w.Head, &commits,
			&w.Remote, &w.MR, &dirty, &w.Source, &w.Since, &w.At); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(commits), &w.Commits)
		w.Dirty = dirty != 0
		out = append(out, w)
	}
	return out, rows.Err()
}

func marks(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// subtree is the chat and the chats delegated from it: by the chats' records and by the work's own parent.
func subtree(ctx context.Context, db *store.DB, userID, conv string) ([]string, error) {
	ids := []string{conv}
	seen := map[string]bool{conv: true}
	frontier := []string{conv}
	for depth := 0; depth < workTreeDepth && len(frontier) > 0 && len(ids) < workTreeMax; depth++ {
		args := []any{userID}
		for _, id := range frontier {
			args = append(args, id)
		}
		in := marks(len(frontier))
		all := append(append([]any{}, args...), args...)
		rows, err := db.Query(ctx, `SELECT conv_id FROM history_conversations WHERE owner_id = ? AND parent_id IN (`+in+`)
			UNION SELECT conv_id FROM chat_work WHERE owner_id = ? AND parent_id IN (`+in+`)`, all...)
		if err != nil {
			return nil, err
		}
		var next []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[id] && len(ids) < workTreeMax {
				seen[id] = true
				ids = append(ids, id)
				next = append(next, id)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		frontier = next
	}
	return ids, nil
}

// fillParents writes parent_id of the person's chats stored before the column was there: from the record's parentId,
// "" for a chat delegated from none. A chat written since has it from its upsert.
func fillParents(ctx context.Context, db *store.DB, userID string) error {
	rows, err := db.Query(ctx, `SELECT machine_id, conv_id, record FROM history_conversations
		WHERE owner_id = ? AND parent_id IS NULL AND record IS NOT NULL`, userID)
	if err != nil {
		return err
	}
	type fill struct{ machine, conv, parent string }
	var fills []fill
	for rows.Next() {
		var f fill
		var record string
		if err := rows.Scan(&f.machine, &f.conv, &record); err != nil {
			rows.Close()
			return err
		}
		f.parent = parentOf(json.RawMessage(record))
		fills = append(fills, f)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, f := range fills {
		if _, err := db.Exec(ctx, `UPDATE history_conversations SET parent_id = ? WHERE machine_id = ? AND conv_id = ? AND parent_id IS NULL`,
			f.parent, f.machine, f.conv); err != nil {
			return err
		}
	}
	return nil
}

// parentOf is a chat record's parentId, "" for none.
func parentOf(record json.RawMessage) string {
	var c struct {
		ParentID string `json:"parentId"`
	}
	if json.Unmarshal(record, &c) != nil || !convID.MatchString(c.ParentID) {
		return ""
	}
	return c.ParentID
}

// Work applies a chatwork event of a machine.
func (s *Service) Work(ctx context.Context, m store.Machine, raw json.RawMessage) {
	if m.CreatedBy == "" {
		return
	}
	var f hubproto.ChatWork
	if json.Unmarshal(raw, &f) != nil {
		return
	}
	if err := ApplyWork(ctx, s.db, owner(m), f.Entries, s.now()); err != nil {
		s.log.Warn("history: the chats' work not stored", "machine", m.ID, "records", len(f.Entries), "error", err)
	}
}
