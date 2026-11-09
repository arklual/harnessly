// Package migration keeps the journal of the move of the machines' own data into the hub's store (docs/migrations,
// HRN-883): what each machine reported of every object, what a person asked of the failed ones, and how far each
// machine is. The machines move their data themselves; the hub only writes down and answers.
package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// DocPath is where the page of a move lies in the repository and in the binary's docs (docs/migrations/<id>.md).
func DocPath(id string) string { return "docs/migrations/" + id + ".md" }

// Limits of what a machine reports.
const (
	kindMax   = 32
	objectMax = 1024
	codeMax   = 64
	errorMax  = 2000
	idMax     = 64
	askedMax  = 1000
)

// Asks a person can make of objects.
const (
	AskRetry = "retry"
	AskSkip  = "skip"
)

// ErrBadReport is a report the journal does not take.
var ErrBadReport = errors.New("migration: bad report")

// Journal is the journal over the hub's database.
type Journal struct {
	db  *store.DB
	now func() time.Time
}

// New opens the journal; now nil is time.Now.
func New(db *store.DB, now func() time.Time) *Journal {
	if now == nil {
		now = time.Now
	}
	return &Journal{db: db, now: now}
}

func validID(s string) bool {
	if s == "" || len(s) > idMax {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func validState(s string) bool {
	switch s {
	case hubproto.MigrationPending, hubproto.MigrationVerified, hubproto.MigrationFailed, hubproto.MigrationSkipped:
		return true
	}
	return false
}

// printable drops the control characters of what a machine wrote (an escape sequence must not reach the terminal of
// `hub doctor`, nor the page).
func printable(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r >= 0x7f && r <= 0x9f }) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return -1
		}
		return r
	}, s)
}

// validCode is an error code as the pages of the moves write them: MIG-READ.
func validCode(s string) bool {
	if len(s) > codeMax {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func validKind(s string) bool {
	if s == "" || len(s) > kindMax {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r == '-') {
			return false
		}
	}
	return true
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func check(rep hubproto.MigrationReport) error {
	if !validID(rep.Migration) {
		return fmt.Errorf("%w: migration id", ErrBadReport)
	}
	if rep.Total < 0 || len(rep.Objects) > hubproto.MigrationReportMax {
		return fmt.Errorf("%w: total or too many objects", ErrBadReport)
	}
	for _, o := range rep.Objects {
		if !validKind(o.Kind) || o.Object == "" || len(o.Object) > objectMax || printable(o.Object) != o.Object ||
			!validState(o.State) || !validCode(o.Code) {
			return fmt.Errorf("%w: object %q of %q", ErrBadReport, clip(o.Object, 80), clip(o.Kind, 40))
		}
	}
	return nil
}

// Report writes what a machine said and answers what was asked of it since. ownerID is the machine's person: a verified
// upload is theirs to read from the hub (upload_refs, upload_owners), as one the page put there; "" binds none.
func (j *Journal) Report(ctx context.Context, machineID, ownerID string, rep hubproto.MigrationReport) (hubproto.MigrationAnswer, error) {
	if err := check(rep); err != nil {
		return hubproto.MigrationAnswer{}, err
	}
	now := j.now().UnixMilli()
	var ans hubproto.MigrationAnswer
	err := j.db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		if _, err := q.Exec(ctx, `INSERT INTO migration_runs (machine_id, migration, version, total, started_at, changed_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (machine_id, migration) DO UPDATE SET version = excluded.version, total = excluded.total,
				changed_at = excluded.changed_at`,
			machineID, rep.Migration, clip(rep.Version, 64), rep.Total, now, now); err != nil {
			return err
		}
		for _, o := range rep.Objects {
			if err := put(ctx, q, machineID, rep.Migration, o, now); err != nil {
				return err
			}
			if err := bindUpload(ctx, q, machineID, ownerID, o, now); err != nil {
				return err
			}
		}
		if err := finish(ctx, q, machineID, rep.Migration, now); err != nil {
			return err
		}
		var err error
		ans.Retry, err = keys(ctx, q, `asked = 'retry'`, machineID, rep.Migration)
		if err != nil {
			return err
		}
		ans.Skip, err = keys(ctx, q, `state = 'skipped'`, machineID, rep.Migration)
		return err
	})
	return ans, err
}

// put writes one object. A failure counts an attempt; an object a person skipped stays skipped until the machine moves
// it after all; a report clears what was asked of it.
func put(ctx context.Context, q store.Q, machineID, migration string, o hubproto.MigrationObject, now int64) error {
	var state string
	var attempts int
	err := q.QueryRow(ctx, `SELECT state, attempts FROM migration_objects
		WHERE machine_id = ? AND migration = ? AND kind = ? AND object = ?`, machineID, migration, o.Kind, o.Object).Scan(&state, &attempts)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	next := o.State
	if state == hubproto.MigrationSkipped && next != hubproto.MigrationVerified {
		next = hubproto.MigrationSkipped
	}
	if o.State == hubproto.MigrationFailed {
		attempts++
	}
	if o.Attempts > attempts {
		attempts = o.Attempts
	}
	code, msg := o.Code, clip(printable(o.Error), errorMax)
	if next != hubproto.MigrationFailed && next != hubproto.MigrationSkipped {
		code, msg = "", ""
	}
	_, err = q.Exec(ctx, `INSERT INTO migration_objects (machine_id, migration, kind, object, sha256, size, state, code, error,
			attempts, asked, changed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)
		ON CONFLICT (machine_id, migration, kind, object) DO UPDATE SET sha256 = excluded.sha256, size = excluded.size,
			state = excluded.state, code = excluded.code, error = excluded.error, attempts = excluded.attempts, asked = '',
			changed_at = excluded.changed_at`,
		machineID, migration, o.Kind, o.Object, clip(printable(o.Sha256), 64), o.Size, next, code, msg, attempts, now)
	return err
}

// bindUpload makes a verified upload the machine's person's in the hub, as the page does for one it puts there itself
// (internal/hub/proxy/uploads.go): the chat's picture then reads from the hub with the machine off.
func bindUpload(ctx context.Context, q store.Q, machineID, ownerID string, o hubproto.MigrationObject, now int64) error {
	if o.Kind != KindUploads || o.State != hubproto.MigrationVerified || ownerID == "" || !sha256Hex(o.Sha256) {
		return nil
	}
	if _, err := q.Exec(ctx, `INSERT INTO upload_owners (machine_id, sha256, owner_id, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id, sha256, owner_id) DO NOTHING`, machineID, o.Sha256, ownerID, now); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO upload_refs (machine_id, upload_id, owner_id, sha256, mime, name, size, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (machine_id, upload_id, owner_id) DO NOTHING`,
		machineID, o.Object, ownerID, o.Sha256, clip(printable(o.Mime), 128), clip(printable(o.Name), 255), o.Size, now)
	return err
}

// KindUploads is the kind of the chats' uploads (filestore.KindUploads).
const KindUploads = "uploads"

func sha256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// finish marks the run over when every object of the inventory is verified or skipped, and not over otherwise.
func finish(ctx context.Context, q store.Q, machineID, migration string, now int64) error {
	c, err := counts(ctx, q, machineID, migration)
	if err != nil {
		return err
	}
	var total int64
	if err := q.QueryRow(ctx, `SELECT total FROM migration_runs WHERE machine_id = ? AND migration = ?`, machineID, migration).Scan(&total); err != nil {
		return err
	}
	if total > 0 && c.verified+c.skipped >= total && c.failed == 0 && c.pending == 0 {
		_, err = q.Exec(ctx, `UPDATE migration_runs SET finished_at = COALESCE(finished_at, ?) WHERE machine_id = ? AND migration = ?`,
			now, machineID, migration)
		return err
	}
	_, err = q.Exec(ctx, `UPDATE migration_runs SET finished_at = NULL WHERE machine_id = ? AND migration = ?`, machineID, migration)
	return err
}

type tally struct{ verified, pending, failed, skipped, retrying int64 }

func counts(ctx context.Context, q store.Q, machineID, migration string) (tally, error) {
	var c tally
	rows, err := q.Query(ctx, `SELECT state, asked, COUNT(*) FROM migration_objects WHERE machine_id = ? AND migration = ?
		GROUP BY state, asked`, machineID, migration)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var state, asked string
		var n int64
		if err := rows.Scan(&state, &asked, &n); err != nil {
			return c, err
		}
		switch state {
		case hubproto.MigrationVerified:
			c.verified += n
		case hubproto.MigrationPending:
			c.pending += n
		case hubproto.MigrationFailed:
			c.failed += n
			if asked == AskRetry {
				c.retrying += n
			}
		case hubproto.MigrationSkipped:
			c.skipped += n
		}
	}
	return c, rows.Err()
}

func keys(ctx context.Context, q store.Q, where, machineID, migration string) ([]hubproto.MigrationKey, error) {
	rows, err := q.Query(ctx, `SELECT kind, object FROM migration_objects WHERE machine_id = ? AND migration = ? AND `+where+`
		ORDER BY kind, object LIMIT ?`, machineID, migration, askedMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hubproto.MigrationKey
	for rows.Next() {
		var k hubproto.MigrationKey
		if err := rows.Scan(&k.Kind, &k.Object); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ErrNoMove is a machine that never reported a move.
var ErrNoMove = errors.New("migration: the machine reported no move")

// Status is how far a machine's move is: the given migration, or the one it reported last when id is "".
func (j *Journal) Status(ctx context.Context, machineID, id string) (hubproto.MigrationStatus, error) {
	q := j.db.S().Q()
	st := hubproto.MigrationStatus{Machine: machineID}
	var started, changed int64
	var finished sql.NullInt64
	query := `SELECT migration, version, total, started_at, changed_at, finished_at FROM migration_runs WHERE machine_id = ?`
	args := []any{machineID}
	if id != "" {
		query += ` AND migration = ?`
		args = append(args, id)
	}
	err := q.QueryRow(ctx, query+` ORDER BY changed_at DESC LIMIT 1`, args...).Scan(&st.Migration, &st.Version, &st.Total,
		&started, &changed, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNoMove
	}
	if err != nil {
		return st, err
	}
	c, err := counts(ctx, q, machineID, st.Migration)
	if err != nil {
		return st, err
	}
	st.Verified, st.Failed, st.Skipped, st.Retrying = c.verified, c.failed, c.skipped, c.retrying
	// What the inventory holds and the machine has not reported yet waits too.
	st.Pending = c.pending
	if rest := st.Total - c.verified - c.failed - c.skipped - c.pending; rest > 0 {
		st.Pending += rest
	}
	st.StartedAt, st.ChangedAt = ms(started), ms(changed)
	if finished.Valid {
		st.FinishedAt = ms(finished.Int64)
	}
	st.Doc = DocPath(st.Migration)
	rows, err := q.Query(ctx, `SELECT kind, object, sha256, size, code, error, attempts, changed_at FROM migration_objects
		WHERE machine_id = ? AND migration = ? AND state = 'failed' ORDER BY changed_at, kind, object LIMIT ?`,
		machineID, st.Migration, hubproto.MigrationFailuresMax)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		o := hubproto.MigrationObject{State: hubproto.MigrationFailed}
		var at int64
		if err := rows.Scan(&o.Kind, &o.Object, &o.Sha256, &o.Size, &o.Code, &o.Error, &o.Attempts, &at); err != nil {
			return st, err
		}
		o.ChangedAt = ms(at)
		st.Failures = append(st.Failures, o)
	}
	return st, rows.Err()
}

func ms(v int64) string { return time.UnixMilli(v).UTC().Format(time.RFC3339) }

// Ask marks failed objects of a machine's last move: retry (the machine tries them again when it next reports) or
// skip (they stay on the machine only, and the move can be over without them). No keys is every failed object. It
// says how many objects it marked.
func (j *Journal) Ask(ctx context.Context, machineID, what string, objects []hubproto.MigrationKey) (int64, error) {
	if what != AskRetry && what != AskSkip {
		return 0, fmt.Errorf("%w: ask %q", ErrBadReport, what)
	}
	if len(objects) > askedMax {
		return 0, fmt.Errorf("%w: too many objects", ErrBadReport)
	}
	st, err := j.Status(ctx, machineID, "")
	if err != nil {
		return 0, err
	}
	now := j.now().UnixMilli()
	set := `asked = 'retry'`
	if what == AskSkip {
		set = `state = 'skipped', asked = ''`
	}
	var marked int64
	err = j.db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		base := `UPDATE migration_objects SET ` + set + `, changed_at = ? WHERE machine_id = ? AND migration = ? AND state = 'failed'`
		if len(objects) == 0 {
			res, err := q.Exec(ctx, base, now, machineID, st.Migration)
			if err != nil {
				return err
			}
			marked, _ = res.RowsAffected()
		}
		for _, k := range objects {
			res, err := q.Exec(ctx, base+` AND kind = ? AND object = ?`, now, machineID, st.Migration, k.Kind, k.Object)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			marked += n
		}
		return finish(ctx, q, machineID, st.Migration, now)
	})
	return marked, err
}

// Summary is every machine's last move, the unfinished first (`hub doctor`).
func (j *Journal) Summary(ctx context.Context) ([]hubproto.MigrationStatus, error) {
	rows, err := j.db.S().Q().Query(ctx, `SELECT machine_id FROM migration_runs GROUP BY machine_id ORDER BY machine_id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var open, done []hubproto.MigrationStatus
	for _, id := range ids {
		st, err := j.Status(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if st.FinishedAt == "" {
			open = append(open, st)
		} else {
			done = append(done, st)
		}
	}
	return append(open, done...), nil
}

// Line is a status in one line of words, as `hub doctor` and the logs say it.
func Line(st hubproto.MigrationStatus) string {
	var b strings.Builder
	moved := st.Verified + st.Skipped
	fmt.Fprintf(&b, "%s: %d of %d", st.Machine, moved, st.Total)
	if st.Skipped > 0 {
		fmt.Fprintf(&b, " (%d left on the machine)", st.Skipped)
	}
	switch {
	case st.Failed > 0:
		fmt.Fprintf(&b, ", %d failed", st.Failed)
		if codes := failedCodes(st.Failures); codes != "" {
			b.WriteString(" (" + codes + ")")
		}
	case st.FinishedAt != "":
		b.WriteString(", done")
	default:
		b.WriteString(", moving")
	}
	return b.String()
}

func failedCodes(fs []hubproto.MigrationObject) string {
	seen := map[string]int{}
	var order []string
	for _, f := range fs {
		if f.Code == "" {
			continue
		}
		if seen[f.Code] == 0 {
			order = append(order, f.Code)
		}
		seen[f.Code]++
	}
	parts := make([]string, 0, len(order))
	for _, c := range order {
		parts = append(parts, fmt.Sprintf("%s ×%d", c, seen[c]))
	}
	return strings.Join(parts, ", ")
}
