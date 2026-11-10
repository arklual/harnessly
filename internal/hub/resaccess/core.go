// Package resaccess is the one model of «who may open a resource» for the hub (docs/hub/resource-access.md):
// a resource is (kind, id) — a document, a design or an app — with an owner, a level for its home team, people and teams
// with a role until a date, requests for access, an access log and guest links that open it without signing in.
//
// The rules are those of the design access (internal/hub/designaccess) and of the platform this was ported from:
//
//	role = the strongest of the owner, the person's own grant (until it runs out), the home team's level when the person is
//	       in the home team, and a role given to another team the person is in (until it runs out); «edit and may share»
//	       given to a team is the role manage.
//	canManage = the owner, manage, an editor where the owner lets editors share, and — for a resource open to its home
//	       team — the lead of that team and the administrator of its organization (the steward).
//
// A resource with no row of its own is open as it was before the model: the package asks the kind's old records (a
// document's doc_meta, a design's design_* rows) and the first change by a manager copies them into the new tables.
package resaccess

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Kinds of resource.
const (
	KindDoc    = "doc"
	KindDesign = "design"
	KindApp    = "app"
)

// Roles, weakest first; a team level is one of none, view, comment, edit.
const (
	roleView    = "view"
	roleComment = "comment"
	roleEdit    = "edit"
	roleManage  = "manage"
	roleOwner   = "owner"
	levelNone   = "none"
)

var roleRank = map[string]int{"": 0, roleView: 1, roleComment: 2, roleEdit: 3, roleManage: 4, roleOwner: 5}

// Rank is how strong a role is: 0 for none, then view, comment, edit, manage, owner.
func Rank(role string) int { return roleRank[role] }

const (
	denyCooldown = 24 * time.Hour
	maxMessage   = 1000
	maxTitle     = 200
	logLimit     = 100
	maxLinks     = 200
)

var (
	teamLevels    = map[string]bool{levelNone: true, roleView: true, roleComment: true, roleEdit: true}
	grantRoles    = map[string]bool{roleManage: true, roleEdit: true, roleComment: true, roleView: true}
	requestLevels = map[string]bool{roleComment: true, roleEdit: true}
	kinds         = map[string]bool{KindDoc: true, KindDesign: true, KindApp: true}
	emailRe       = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	isoDateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
)

// Ref names a resource.
type Ref struct{ Kind, ID string }

// Error is a refusal with the code the page matches on.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

func fail(code, msg string) error { return &Error{Code: code, Message: msg} }

// Status is the HTTP status of an error code.
func Status(err error) int {
	var e *Error
	if !errors.As(err, &e) {
		return 500
	}
	switch e.Code {
	case "bad_request":
		return 400
	case "forbidden":
		return 403
	case "not_found":
		return 404
	case "conflict":
		return 409
	case "gone":
		return 410
	case "rate_limited":
		return 429
	case "unavailable":
		return 503
	}
	return 500
}

// resource is the stored (or reconstructed) row of a resource.
type resource struct {
	Ref
	Owner           string
	OwnerName       string
	HomeTeam        string
	TeamLevel       string
	EditorsCanShare bool
	Title           string
	Stored          bool
}

type member struct {
	Email     string
	Role      string
	GrantedBy string
	GrantedAt time.Time
	Expires   *time.Time
}

type teamGrant struct {
	Team      string
	Role      string
	CanShare  bool
	GrantedBy string
	GrantedAt time.Time
	Expires   *time.Time
}

type folded struct {
	Role      string // "" when the resource is closed to the person
	Until     *time.Time
	CanManage bool
	Steward   bool
}

func stronger(a, b string) string {
	if roleRank[b] > roleRank[a] {
		return b
	}
	return a
}

func live(expires *time.Time, now time.Time) bool { return expires == nil || expires.After(now) }

// fold is the one rule of access (see the package comment).
func fold(r resource, m *member, grants []teamGrant, person personTeams, homeOrg string, now time.Time) folded {
	if r.Owner != "" && r.Owner == person.email {
		return folded{Role: roleOwner, CanManage: true}
	}
	own := ""
	if m != nil && live(m.Expires, now) {
		own = m.Role
	}
	_, inHome := person.teams[r.HomeTeam]
	if r.HomeTeam == "" {
		inHome = true
	}
	teamOn := r.TeamLevel != levelNone && r.TeamLevel != ""
	shared := ""
	if teamOn && inHome {
		shared = r.TeamLevel
	}
	for _, g := range grants {
		if _, in := person.teams[g.Team]; !in || !live(g.Expires, now) {
			continue
		}
		role := g.Role
		if role == roleEdit && g.CanShare {
			role = roleManage
		}
		shared = stronger(shared, role)
	}
	role := stronger(own, shared)
	ownWins := own != "" && (shared == "" || roleRank[own] > roleRank[shared])
	byRole := role == roleManage || (role == roleEdit && r.EditorsCanShare)
	steward := false
	if teamOn && r.HomeTeam != "" {
		steward = person.teams[r.HomeTeam].lead || (homeOrg != "" && person.admin[homeOrg])
	}
	f := folded{Role: role, CanManage: byRole || steward, Steward: steward}
	if ownWins && m != nil {
		f.Until = m.Expires
	}
	return f
}

// ---- terms -----------------------------------------------------------------------------------------------------------

type ttl struct {
	none bool
	step time.Duration
	at   time.Time
}

var ttlSteps = map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}

const ttlMessage = "Срок: 1h, 1d, 7d, 30d, дата со смещением или null"

func ttlOf(v any) (ttl, error) {
	if v == nil {
		return ttl{none: true}, nil
	}
	if s, ok := v.(string); ok {
		if d, ok := ttlSteps[s]; ok {
			return ttl{step: d}, nil
		}
		if isoDateRe.MatchString(s) {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return ttl{at: t}, nil
			}
		}
	}
	return ttl{}, fail("bad_request", ttlMessage)
}

// expiry is when a term ends; nil for none. A date already past is refused.
func expiry(t ttl, now time.Time) (*time.Time, error) {
	switch {
	case t.none:
		return nil, nil
	case t.step > 0:
		e := now.Add(t.step)
		return &e, nil
	}
	if !t.at.After(now) {
		return nil, fail("bad_request", "Срок должен быть в будущем")
	}
	e := t.at.UTC()
	return &e, nil
}

// ---- small helpers ---------------------------------------------------------------------------------------------------

func normEmail(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func ms(t time.Time) int64     { return t.UnixMilli() }
func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }
func isoOf(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func iso(t *time.Time) any {
	if t == nil {
		return nil
	}
	return isoOf(*t)
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func clip(v string, n int) string {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) <= n {
		return v
	}
	return strings.TrimSpace(string([]rune(v)[:n]))
}
