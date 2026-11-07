// Package designaccess is who may open which design project (Sinopia) of a hub's machine, and the guest links that open
// one without signing in. The hub keeps the records; the runtime of the machine enforces the snapshot it is pushed
// (hubproto.OpDesignAccess) and signs the short tokens a guest's pages are read with (hubproto.OpDesignPublicToken).
//
// The page speaks two APIs, both answered here before the request would reach the machine: the project's own
// <platform>/design/access/… and the shared <platform>/access/design/<id>/…. Both read and write the same tables
// (store migration 0015), keyed by machine and project. See docs/hub/design-access.md.
package designaccess

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	roleOwner   = "owner"
	roleManage  = "manage"
	roleEdit    = "edit"
	roleComment = "comment"
	roleView    = "view"
	levelNone   = "none"
)

var rankOf = map[string]int{roleView: 0, roleComment: 1, roleEdit: 2, roleManage: 3, roleOwner: 4}

func rank(r string) int {
	if v, ok := rankOf[r]; ok {
		return v
	}
	return -1
}

func stronger(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

var (
	projectIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	uuidRe      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	emailRe     = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	isoDateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:\d{2}))$`)
	teamIDRe    = regexp.MustCompile(`^(space:)?[a-z0-9][a-z0-9_-]{0,62}$`)
	hexRevRe    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

const maxEmail = 254

// Error is a refusal the page shows as it is: the code picks the status, the message is the text.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func fail(code, msg string) *Error { return &Error{Code: code, Message: msg} }

var statusOf = map[string]int{
	"bad_request": 400, "forbidden": 403, "not_found": 404, "conflict": 409, "gone": 410, "rate_limited": 429, "unavailable": 503,
	"share_unsafe_runtime": 409,
}

func asError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Ref is one project: the machine it lives on and its library id.
type Ref struct {
	Machine string
	Project string
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// rowID is a numeric id that sorts by time: milliseconds and three random digits, within JavaScript's safe integers.
// It is where an insert starts looking, not a promise of uniqueness: insertRow takes the next free one.
func rowID(now time.Time) int64 {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return now.UnixMilli()*1000 + int64(binary.BigEndian.Uint16(b[:])%1000)
}

// maxSafeID is JavaScript's Number.MAX_SAFE_INTEGER: the page reads these ids as numbers.
const maxSafeID = 1<<53 - 1

var errNoRowID = errors.New("designaccess: no free row id within JavaScript's safe integers")

// insertRow inserts one row whose first value is a rowID, starting at rowID(t.now): an id already taken (several
// writes in one millisecond, a clock that stands still) is skipped by «ON CONFLICT (id) DO NOTHING» and the next
// integer is tried, until one is free or the request's context ends. A conflict on the id never fails a statement, so
// a Postgres transaction is not aborted by it and the business write around it goes on; every other error (a foreign
// key, another unique column, a dropped connection) is returned as it is. query is «INSERT INTO t (id, …) VALUES (?, …)»
// and args are the values after the id. It returns the id the row got.
func (t *txn) insertRow(query string, args ...any) (int64, error) {
	query += " ON CONFLICT (id) DO NOTHING"
	vals := make([]any, 0, len(args)+1)
	for id := rowID(t.now); ; id++ {
		if id > maxSafeID {
			return 0, errNoRowID
		}
		if err := t.ctx.Err(); err != nil {
			return 0, err
		}
		vals = append(append(vals[:0], id), args...)
		res, err := t.q.Exec(t.ctx, query, vals...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n == 1 {
			return id, nil
		}
	}
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

func fromNull(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMs(v.Int64)
	return &t
}

func isoOf(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// iso is a time as the page reads it, or JSON null.
func iso(t *time.Time) any {
	if t == nil {
		return nil
	}
	return isoOf(*t)
}

func live(expires *time.Time, now time.Time) bool { return expires == nil || expires.After(now) }

func normEmail(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func localPart(email string) string {
	at := strings.IndexByte(email, '@')
	if at < 0 {
		return email
	}
	return email[:at]
}

// clip trims a text and cuts it to n characters.
func clip(v string, n int) string {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) <= n {
		return v
	}
	r := []rune(v)
	return strings.TrimSpace(string(r[:n]))
}

func hasControl(v string) bool {
	for _, r := range v {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ttl is a requested term: none (no end), a step from now, or a date.
type ttl struct {
	none bool
	step time.Duration
	at   time.Time
}

var ttlSteps = map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}

func ttlOf(v any, msg string) (ttl, error) {
	if v == nil {
		return ttl{none: true}, nil
	}
	s, ok := v.(string)
	if ok {
		if d, ok := ttlSteps[s]; ok {
			return ttl{step: d}, nil
		}
		if isoDateRe.MatchString(s) {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return ttl{at: t}, nil
			}
		}
	}
	return ttl{}, fail("bad_request", msg)
}

// expiry is when a term ends; nil for none. A date already past is refused.
func expiry(t ttl, now time.Time) (*time.Time, error) {
	switch {
	case t.none:
		return nil, nil
	case t.step > 0:
		e := now.Add(t.step)
		return &e, nil
	default:
		if !t.at.After(now) {
			return nil, fail("bad_request", "Срок должен быть в будущем")
		}
		e := t.at.UTC()
		return &e, nil
	}
}
