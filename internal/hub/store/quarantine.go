package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// QuarantinedUsage is a record of a machine's spend the hub set aside instead of storing.
type QuarantinedUsage struct {
	MachineID string
	Epoch     string
	Seq       int64
	Kind      string
	Reason    string
	// Record is the record as the machine sent it.
	Record    string
	CreatedAt int64
}

// quarantineRecordMax bounds what is kept of one record.
const quarantineRecordMax = 64 << 10

// QuarantineUsage keeps a record aside. The same record of the same machine kept twice is one row; added says whether
// this call made it.
func (s Store) QuarantineUsage(ctx context.Context, r QuarantinedUsage) (added bool, err error) {
	// What a machine sent may be anything; what is kept must be text the database takes, or the record that could not
	// be read would hold the batch after all.
	clean := func(v string, limit int) string {
		if len(v) > limit {
			v = v[:limit]
		}
		return strings.ReplaceAll(strings.ToValidUTF8(v, "\uFFFD"), "\x00", "")
	}
	r.Record, r.Reason = clean(r.Record, quarantineRecordMax), clean(r.Reason, 500)
	r.Kind, r.Epoch = clean(r.Kind, 200), clean(r.Epoch, 200)
	sum := sha256.Sum256([]byte(r.MachineID + "\x00" + r.Epoch + "\x00" + strconv.FormatInt(r.Seq, 10) + "\x00" + r.Record))
	res, err := s.q.Exec(ctx, `INSERT INTO usage_quarantine (id, machine_id, epoch, seq, kind, reason, record, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`, hex.EncodeToString(sum[:]), r.MachineID, r.Epoch, r.Seq, r.Kind, r.Reason, r.Record, r.CreatedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UsageQuarantine lists the newest records set aside, for whoever looks into them.
func (s Store) UsageQuarantine(ctx context.Context, limit int) ([]QuarantinedUsage, error) {
	rows, err := s.q.Query(ctx, `SELECT machine_id, epoch, seq, kind, reason, record, created_at FROM usage_quarantine ORDER BY created_at DESC, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuarantinedUsage
	for rows.Next() {
		var r QuarantinedUsage
		if err := rows.Scan(&r.MachineID, &r.Epoch, &r.Seq, &r.Kind, &r.Reason, &r.Record, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// QueueStat is how many rows wait somewhere and when the oldest of them was made (Unix milliseconds, 0 when none).
type QueueStat struct {
	Rows     int64
	OldestMs int64
}

func (s Store) queueStat(ctx context.Context, query string) (QueueStat, error) {
	var st QueueStat
	var oldest *int64
	if err := s.q.QueryRow(ctx, query).Scan(&st.Rows, &oldest); err != nil {
		return st, err
	}
	if oldest != nil {
		st.OldestMs = *oldest
	}
	return st, nil
}

// UsageQuarantineStat counts the records set aside.
func (s Store) UsageQuarantineStat(ctx context.Context) (QueueStat, error) {
	return s.queueStat(ctx, `SELECT COUNT(*), MIN(created_at) FROM usage_quarantine`)
}

// OfflinePromptsStat counts the messages that wait for their machine.
func (s Store) OfflinePromptsStat(ctx context.Context) (QueueStat, error) {
	return s.queueStat(ctx, `SELECT COUNT(*), MIN(created_at) FROM offline_prompts WHERE state IN ('queued', 'sending')`)
}

// AutomationRunsStat counts the automation runs that wait to be started.
func (s Store) AutomationRunsStat(ctx context.Context) (QueueStat, error) {
	return s.queueStat(ctx, `SELECT COUNT(*), MIN(created_at) FROM automation_runs WHERE status = 'queued'`)
}
