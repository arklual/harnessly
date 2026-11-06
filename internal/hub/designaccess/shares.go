package designaccess

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Guest links of a project (/s/<token>): who has the address opens the project without signing in, read-only or with
// comments, all pages or chosen ones, the latest state or a pinned revision. A link lives while it is not revoked, not
// expired, and its maker may still share the project.

const (
	maxPages          = 200
	maxGuestName      = 60
	maxGuestText      = 2000
	maxBoard          = 512
	maxCoord          = 1_000_000
	maxLabel          = 120
	maxAnchorBytes    = 4096
	maxStateKeys      = 20
	maxStateKey       = 40
	maxStateText      = 80
	maxCommentsByLink = 1000
	revokedShown      = 30 * 24 * time.Hour
	maxViewBytes      = 2048
	maxViewKeys       = 32
	commentLimit      = 500
	maxSeen           = 120_000
	maxViewRowsByLink = 50_000
	viewGuestsShown   = 100
	viewBoardsShown   = 200
)

var (
	shareTokenRe = regexpToken(22)
	guestIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	anchorTagRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

func regexpToken(n int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`^[A-Za-z0-9_-]{%d}$`, n))
}

// randomToken is n random bytes, base64url without padding.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// control is a control character other than the line break and the tab.
func control(v string) bool {
	for _, r := range v {
		if (r < 0x20 && r != '\n' && r != '\t' && r != '\r') || r == 0x7f {
			return true
		}
	}
	return false
}

type shareRow struct {
	ID, Project, Title, Rights, Version string
	Rev                                 string
	Pages                               []string // nil: every page
	View                                map[string]any
	Expires, Revoked                    *time.Time
	Opens                               int64
	CreatedBy                           string
	CreatedAt                           time.Time
	Comments                            int64
}

func (r shareRow) json(url any) map[string]any {
	var pages, view any
	if r.Pages != nil {
		pages = r.Pages
	}
	if r.View != nil {
		view = r.View
	}
	return map[string]any{"id": r.ID, "title": r.Title, "rights": r.Rights, "version": r.Version, "pages": pages,
		"expiresAt": iso(r.Expires), "revokedAt": iso(r.Revoked), "opens": r.Opens, "createdAt": isoOf(r.CreatedAt),
		"url": url, "comments": r.Comments, "view": view}
}

const shareCols = `l.id, l.project_id, l.title, l.rights, l.version, l.rev, l.pages, l.view, l.expires_at, l.revoked_at, l.opens, l.created_by, l.created_at,
	(SELECT COUNT(*) FROM design_guest_comments c WHERE c.link_id = l.id)`

func scanShare(row interface{ Scan(...any) error }) (shareRow, error) {
	var r shareRow
	var rev, pages, view sql.NullString
	var exp, revoked sql.NullInt64
	var created int64
	if err := row.Scan(&r.ID, &r.Project, &r.Title, &r.Rights, &r.Version, &rev, &pages, &view, &exp, &revoked, &r.Opens, &r.CreatedBy, &created, &r.Comments); err != nil {
		return shareRow{}, err
	}
	r.Rev, r.Expires, r.Revoked, r.CreatedAt = rev.String, fromNull(exp), fromNull(revoked), fromMs(created)
	if pages.Valid && pages.String != "" {
		_ = json.Unmarshal([]byte(pages.String), &r.Pages)
		if r.Pages == nil {
			r.Pages = []string{}
		}
	}
	if view.Valid && view.String != "" {
		_ = json.Unmarshal([]byte(view.String), &r.View)
	}
	return r, nil
}

func checkRights(v any) (string, error) {
	if s, ok := v.(string); ok && (s == roleView || s == roleComment) {
		return s, nil
	}
	return "", fail("bad_request", "Права ссылки: view или comment")
}

func checkVersion(v any) (string, error) {
	if s, ok := v.(string); ok && (s == "latest" || s == "pinned") {
		return s, nil
	}
	return "", fail("bad_request", "Версия ссылки: latest или pinned")
}

// checkPages: board paths, or nil for all. An empty list would show nothing, so it is refused; repeats go, order stays.
func checkPages(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fail("bad_request", "pages — список путей досок или null")
	}
	if len(list) == 0 {
		return nil, fail("bad_request", "pages: выберите хотя бы одну страницу или передайте null для всех")
	}
	if len(list) > maxPages {
		return nil, fail("bad_request", fmt.Sprintf("pages: не больше %d страниц", maxPages))
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" || len(s) > maxBoard || control(s) {
			return nil, fail("bad_request", "pages — пути досок: непустые строки без управляющих символов")
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, nil
}

func checkRev(rev string) (string, error) {
	if !hexRevRe.MatchString(rev) {
		return "", fail("unavailable", "Машина владельца вернула некорректную ревизию")
	}
	return rev, nil
}

// checkView: a flat object of scalars up to 2 KB, or nothing; the look is only a hint for the viewer.
func checkView(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	bad := fail("bad_request", "view — плоский объект из строк, чисел, true/false и null, до 2 КБ")
	obj, ok := v.(map[string]any)
	if !ok || len(obj) > maxViewKeys {
		return nil, bad
	}
	for k, item := range obj {
		if len(k) > 64 {
			return nil, bad
		}
		switch x := item.(type) {
		case nil, string, bool:
		case float64:
			if math.IsInf(x, 0) || math.IsNaN(x) {
				return nil, bad
			}
		default:
			return nil, bad
		}
	}
	if raw, _ := json.Marshal(obj); len(raw) > maxViewBytes {
		return nil, bad
	}
	return obj, nil
}

func jsonOrNil(v any, isNil bool) any {
	if isNil {
		return nil
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// makerMayShare is the one rule behind a live link.
func (t *txn) makerMayShare(projectID, createdBy string) (bool, error) {
	p, err := t.getProject(projectID, false)
	if err != nil || p == nil {
		return false, err
	}
	f, err := t.fold(*p, createdBy)
	return f.CanManage, err
}

// listShares: live links and the ones revoked in the last 30 days; a link whose maker lost the right to share is listed
// as revoked now, the way a guest is told.
func (t *txn) listShares(id string) ([]map[string]any, error) {
	rows, err := t.q.Query(t.ctx, `SELECT `+shareCols+` FROM design_share_links l WHERE l.machine_id = ? AND l.project_id = ? AND (l.revoked_at IS NULL OR l.revoked_at > ?)
		ORDER BY l.created_at DESC, l.id`, t.m.ID, id, ms(t.now.Add(-revokedShown)))
	if err != nil {
		return nil, err
	}
	var all []shareRow
	for rows.Next() {
		r, err := scanShare(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	may := map[string]bool{}
	out := []map[string]any{}
	for _, r := range all {
		maker := normEmail(r.CreatedBy)
		if r.Revoked == nil {
			if _, ok := may[maker]; !ok {
				ok, err := t.makerMayShare(id, maker)
				if err != nil {
					return nil, err
				}
				may[maker] = ok
			}
			if !may[maker] {
				now := t.now
				r.Revoked = &now
			}
		}
		out = append(out, r.json(nil))
	}
	return out, nil
}

type shareInput struct {
	Title   string
	View    map[string]any
	Rights  string
	TTL     ttl
	Version string
	Pages   []string
	Rev     string
}

func (t *txn) createShare(id, actor string, in shareInput, urlOf func(string) string) (map[string]any, string, error) {
	expires, err := expiry(in.TTL, t.now)
	if err != nil {
		return nil, "", err
	}
	if _, err := t.lockProject(id); err != nil {
		return nil, "", err
	}
	token := randomToken(16)
	linkID := newUUID()
	r := shareRow{ID: linkID, Project: id, Title: clip(in.Title, maxTitle), Rights: in.Rights, Version: in.Version, Rev: in.Rev, Pages: in.Pages,
		View: in.View, Expires: expires, CreatedBy: normEmail(actor), CreatedAt: t.now}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO design_share_links (id, machine_id, project_id, token_hash, title, rights, expires_at, view, version, rev, pages, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, linkID, t.m.ID, id, store.HashToken(token), r.Title, r.Rights, nullTime(expires),
		jsonOrNil(in.View, in.View == nil), in.Version, nullStr(in.Rev), jsonOrNil(in.Pages, in.Pages == nil), r.CreatedBy, ms(t.now)); err != nil {
		return nil, "", err
	}
	details := map[string]any{"rights": in.Rights, "until": iso(expires), "version": in.Version}
	if in.Pages != nil {
		details["pages"] = len(in.Pages)
	}
	return r.json(urlOf(token)), linkID, t.writeLog(id, actor, "share.create", linkID, details)
}

// shareState is what the API decides on before it changes a live link.
func (t *txn) shareState(id, linkID string) (createdBy, version, rev string, err error) {
	var r sql.NullString
	err = t.q.QueryRow(t.ctx, `SELECT created_by, version, rev FROM design_share_links WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NULL`,
		linkID, t.m.ID, id).Scan(&createdBy, &version, &r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", fail("not_found", "Ссылка не найдена или уже отозвана")
	}
	return createdBy, version, r.String, err
}

type shareChange struct {
	Title   *string
	Rights  *string
	TTL     *ttl
	Version *string
	Pages   *[]string // set: a list, or nil inside for all
	View    *map[string]any
	Rev     *string // set: a revision, or "" for none
}

func (t *txn) updateShare(id, actor, linkID string, c shareChange) (map[string]any, error) {
	var expires *time.Time
	if c.TTL != nil {
		var err error
		if expires, err = expiry(*c.TTL, t.now); err != nil {
			return nil, err
		}
	}
	if _, err := t.lockProject(id); err != nil {
		return nil, err
	}
	sets := []string{}
	args := []any{}
	details := map[string]any{}
	if c.Title != nil {
		title := clip(*c.Title, maxTitle)
		sets, args = append(sets, "title = ?"), append(args, title)
		details["title"] = title
	}
	if c.Rights != nil {
		sets, args = append(sets, "rights = ?"), append(args, *c.Rights)
		details["rights"] = *c.Rights
	}
	if c.TTL != nil {
		sets, args = append(sets, "expires_at = ?"), append(args, nullTime(expires))
		details["until"] = iso(expires)
	}
	if c.Version != nil {
		sets, args = append(sets, "version = ?"), append(args, *c.Version)
		details["version"] = *c.Version
	}
	if c.Rev != nil {
		sets, args = append(sets, "rev = ?"), append(args, nullStr(*c.Rev))
		if *c.Rev != "" {
			details["rev"] = *c.Rev
		}
	}
	if c.Pages != nil {
		sets, args = append(sets, "pages = ?"), append(args, jsonOrNil(*c.Pages, *c.Pages == nil))
		if *c.Pages == nil {
			details["pages"] = "all"
		} else {
			details["pages"] = len(*c.Pages)
		}
	}
	if c.View != nil {
		sets, args = append(sets, "view = ?"), append(args, jsonOrNil(*c.View, *c.View == nil))
		if *c.View == nil {
			details["view"] = "cleared"
		} else {
			details["view"] = "changed"
		}
	}
	if len(sets) > 0 {
		args = append(args, linkID, t.m.ID, id)
		res, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET `+strings.Join(sets, ", ")+` WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NULL`, args...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, fail("not_found", "Ссылка не найдена или уже отозвана")
		}
	}
	r, err := scanShare(t.q.QueryRow(t.ctx, `SELECT `+shareCols+` FROM design_share_links l WHERE l.id = ? AND l.machine_id = ? AND l.project_id = ? AND l.revoked_at IS NULL`, linkID, t.m.ID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fail("not_found", "Ссылка не найдена или уже отозвана")
	}
	if err != nil {
		return nil, err
	}
	return r.json(nil), t.writeLog(id, actor, "share.update", linkID, details)
}

func (t *txn) revokeShare(id, actor, linkID string) error {
	if _, err := t.lockProject(id); err != nil {
		return err
	}
	res, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET revoked_at = ? WHERE id = ? AND machine_id = ? AND project_id = ? AND revoked_at IS NULL`, ms(t.now), linkID, t.m.ID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fail("not_found", "Ссылка не найдена или уже отозвана")
	}
	return t.writeLog(id, actor, "share.revoke", linkID, nil)
}

type guestComment struct {
	ID        int64          `json:"id"`
	LinkID    string         `json:"linkId,omitempty"`
	LinkTitle string         `json:"linkTitle,omitempty"`
	Board     string         `json:"board"`
	X         *float64       `json:"x"`
	Y         *float64       `json:"y"`
	Text      string         `json:"text"`
	Name      string         `json:"name"`
	Anchor    map[string]any `json:"anchor"`
	At        string         `json:"at"`
}

func scanComment(row interface{ Scan(...any) error }) (guestComment, error) {
	var c guestComment
	var x, y sql.NullFloat64
	var anchor sql.NullString
	var at int64
	if err := row.Scan(&c.ID, &c.LinkID, &c.LinkTitle, &c.Board, &x, &y, &c.Text, &c.Name, &anchor, &at); err != nil {
		return guestComment{}, err
	}
	if x.Valid {
		c.X = &x.Float64
	}
	if y.Valid {
		c.Y = &y.Float64
	}
	if anchor.Valid && anchor.String != "" {
		_ = json.Unmarshal([]byte(anchor.String), &c.Anchor)
	}
	c.At = isoOf(fromMs(at))
	return c, nil
}

// listGuestComments: everything guests left under any link of the project, newest first.
func (t *txn) listGuestComments(id string) ([]guestComment, error) {
	rows, err := t.q.Query(t.ctx, `SELECT c.id, c.link_id, l.title, c.board, c.x, c.y, c.text, c.guest_name, c.anchor, c.created_at
		FROM design_guest_comments c JOIN design_share_links l ON l.id = c.link_id
		WHERE l.machine_id = ? AND l.project_id = ? ORDER BY c.id DESC LIMIT ?`, t.m.ID, id, commentLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []guestComment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- the guest side -----------------------------------------------------------------------------------------------

// shareTarget is a live link as the guest gateway serves it.
type shareTarget struct {
	ID, Token, Machine, Project, Title, Rights, Version, Rev, CreatedBy string
	Pages                                                               []string
	View                                                                map[string]any
	Expires                                                             *time.Time
}

func (l shareTarget) allows(board string) bool {
	if l.Pages == nil {
		return true
	}
	for _, p := range l.Pages {
		if p == board {
			return true
		}
	}
	return false
}

// resolveShare judges a token by its row: missing, revoked, expired, or ok. Whether its maker may still share is the
// caller's to ask, on the machine's side.
func (s *Service) resolveShare(ctx context.Context, now time.Time, token string) (string, *shareTarget, error) {
	if !shareTokenRe.MatchString(token) {
		return "missing", nil, nil
	}
	var r shareRow
	var machineID string
	var rev, pages, view sql.NullString
	var exp, revoked sql.NullInt64
	err := s.db.QueryRow(ctx, `SELECT id, machine_id, project_id, title, rights, version, rev, pages, view, expires_at, revoked_at, created_by FROM design_share_links WHERE token_hash = ?`,
		store.HashToken(token)).Scan(&r.ID, &machineID, &r.Project, &r.Title, &r.Rights, &r.Version, &rev, &pages, &view, &exp, &revoked, &r.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return "missing", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if revoked.Valid {
		return "revoked", nil, nil
	}
	if e := fromNull(exp); e != nil && !e.After(now) {
		return "expired", nil, nil
	}
	l := &shareTarget{ID: r.ID, Token: token, Machine: machineID, Project: r.Project, Title: r.Title, Rights: r.Rights, Version: r.Version, CreatedBy: r.CreatedBy, Expires: fromNull(exp)}
	if r.Version == "pinned" {
		l.Rev = rev.String
	}
	if pages.Valid && pages.String != "" {
		_ = json.Unmarshal([]byte(pages.String), &l.Pages)
		if l.Pages == nil {
			l.Pages = []string{}
		}
	}
	if view.Valid && view.String != "" {
		_ = json.Unmarshal([]byte(view.String), &l.View)
	}
	return "ok", l, nil
}

func (t *txn) recordOpen(linkID string) error {
	_, err := t.q.Exec(t.ctx, `UPDATE design_share_links SET opens = opens + 1, last_opened_at = ? WHERE id = ?`, ms(t.now), linkID)
	return err
}

// listPublicComments: a guest sees the comments of their own link, on its pages only.
func (t *txn) listPublicComments(l *shareTarget) ([]guestComment, error) {
	rows, err := t.q.Query(t.ctx, `SELECT id, link_id, '', board, x, y, text, guest_name, anchor, created_at FROM design_guest_comments WHERE link_id = ? ORDER BY id LIMIT ?`, l.ID, commentLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []guestComment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		if !l.allows(c.Board) {
			continue
		}
		c.LinkID, c.LinkTitle = "", ""
		out = append(out, c)
	}
	return out, rows.Err()
}

func fraction(v any, field string) (*float64, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1 {
		return nil, fail("bad_request", field+" — число от 0 до 1")
	}
	return &f, nil
}

func finite(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// checkAnchor: the block of a board a comment is about, as the guest app's selection makes it; only the contract's keys stay.
func checkAnchor(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	bad := fail("bad_request", "Некорректный блок доски")
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, bad
	}
	selector, ok := obj["selector"].(string)
	if !ok || strings.TrimSpace(selector) == "" || len(selector) > 400 || control(selector) {
		return nil, bad
	}
	var dataID any
	switch d := obj["dataId"].(type) {
	case nil:
	case float64:
		if _, ok := finite(d); !ok {
			return nil, bad
		}
		dataID = d
	case string:
		if len(d) > 200 || control(d) {
			return nil, bad
		}
		dataID = d
	default:
		return nil, bad
	}
	tag, ok := obj["tag"].(string)
	if !ok || !anchorTagRe.MatchString(tag) {
		return nil, bad
	}
	label, hasLabel := obj["label"]
	if hasLabel && label != nil {
		if s, ok := label.(string); !ok || control(s) {
			return nil, bad
		}
	}
	box, ok := obj["rect"].(map[string]any)
	if !ok {
		return nil, bad
	}
	rect := map[string]any{}
	for _, k := range []string{"x", "y", "width", "height"} {
		f, ok := finite(box[k])
		if !ok || math.Abs(f) > maxCoord {
			return nil, bad
		}
		rect[k] = f
	}
	if rect["width"].(float64) < 0 || rect["height"].(float64) < 0 {
		return nil, bad
	}
	anchor := map[string]any{"selector": selector, "dataId": dataID, "tag": tag, "rect": rect}
	if s, ok := label.(string); ok && strings.TrimSpace(s) != "" {
		anchor["label"] = clip(s, maxLabel)
	}
	for _, k := range []string{"vw", "vh"} {
		if obj[k] == nil {
			continue
		}
		f, ok := finite(obj[k])
		if !ok || f < 0 || f > maxCoord {
			return nil, bad
		}
		anchor[k] = f
	}
	if st := obj["state"]; st != nil {
		m, ok := st.(map[string]any)
		if !ok || len(m) > maxStateKeys {
			return nil, bad
		}
		for k, item := range m {
			if k == "" || len(k) > maxStateKey || control(k) {
				return nil, bad
			}
			switch x := item.(type) {
			case bool:
			case float64:
				if _, ok := finite(x); !ok {
					return nil, bad
				}
			case string:
				if len([]rune(x)) > maxStateText || control(x) {
					return nil, bad
				}
			default:
				return nil, bad
			}
		}
		if len(m) > 0 {
			anchor["state"] = m
		}
	}
	if raw, _ := json.Marshal(anchor); len(raw) > maxAnchorBytes {
		return nil, bad
	}
	return anchor, nil
}

type commentInput struct {
	Name, Text, Board, GuestID string
	X, Y                       *float64
	Anchor                     map[string]any
}

// checkGuestComment checks what the page cannot be trusted to: lengths, plain text, a point inside the board, the trap field.
func checkGuestComment(in map[string]any) (commentInput, error) {
	if hp, ok := in["hp"]; ok && hp != nil && hp != "" {
		return commentInput{}, fail("bad_request", "Не удалось отправить")
	}
	str := func(k string) string { s, _ := in[k].(string); return strings.TrimSpace(s) }
	name := str("name")
	if name == "" || len([]rune(name)) > maxGuestName || control(name) {
		return commentInput{}, fail("bad_request", fmt.Sprintf("Имя — от 1 до %d символов", maxGuestName))
	}
	text := str("text")
	if text == "" || len([]rune(text)) > maxGuestText || control(text) {
		return commentInput{}, fail("bad_request", fmt.Sprintf("Комментарий — от 1 до %d символов, только текст", maxGuestText))
	}
	board := str("board")
	if board == "" || len(board) > maxBoard || control(board) {
		return commentInput{}, fail("bad_request", "Не указана доска")
	}
	guest, _ := in["guestId"].(string)
	if !guestIDRe.MatchString(guest) {
		return commentInput{}, fail("bad_request", "Некорректный guestId")
	}
	anchor, err := checkAnchor(in["anchor"])
	if err != nil {
		return commentInput{}, err
	}
	x, err := fraction(in["x"], "x")
	if err != nil {
		return commentInput{}, err
	}
	y, err := fraction(in["y"], "y")
	if err != nil {
		return commentInput{}, err
	}
	if (x == nil) != (y == nil) {
		return commentInput{}, fail("bad_request", "Точка задаётся парой x и y")
	}
	return commentInput{Name: name, Text: text, Board: board, GuestID: guest, X: x, Y: y, Anchor: anchor}, nil
}

func (t *txn) addGuestComment(l *shareTarget, c commentInput) (guestComment, error) {
	if !l.allows(c.Board) {
		return guestComment{}, fail("not_found", "Страница не найдена")
	}
	q := `SELECT id FROM design_share_links WHERE id = ?` + t.forUpdate()
	var id string
	if err := t.q.QueryRow(t.ctx, q, l.ID).Scan(&id); err != nil {
		return guestComment{}, err
	}
	var n int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM design_guest_comments WHERE link_id = ?`, l.ID).Scan(&n); err != nil {
		return guestComment{}, err
	}
	if n >= maxCommentsByLink {
		return guestComment{}, fail("rate_limited", "К ссылке набралось слишком много комментариев")
	}
	out := guestComment{ID: rowID(t.now), Board: c.Board, X: c.X, Y: c.Y, Text: c.Text, Name: c.Name, Anchor: c.Anchor, At: isoOf(t.now)}
	var x, y any
	if c.X != nil {
		x, y = *c.X, *c.Y
	}
	if _, err := t.q.Exec(t.ctx, `INSERT INTO design_guest_comments (id, link_id, board, x, y, text, guest_id, guest_name, anchor, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		out.ID, l.ID, c.Board, x, y, c.Text, c.GuestID, c.Name, jsonOrNil(c.Anchor, c.Anchor == nil), ms(t.now)); err != nil {
		return guestComment{}, err
	}
	return out, t.writeLog(l.Project, "guest:"+c.Name, "share.comment", l.ID, map[string]any{"board": c.Board})
}

// recordSeen stores one report of time on a board; a link without stats, or one that holds enough rows, drops it.
func (t *txn) recordSeen(l *shareTarget, in map[string]any) (bool, error) {
	guest, _ := in["guestId"].(string)
	if !guestIDRe.MatchString(guest) {
		return false, fail("bad_request", "Некорректный guestId")
	}
	board, _ := in["board"].(string)
	board = strings.TrimSpace(board)
	if board == "" || len(board) > maxBoard || control(board) {
		return false, fail("bad_request", "Не указана доска")
	}
	msv, ok := finite(in["ms"])
	if !ok || msv <= 0 {
		return false, fail("bad_request", "ms — положительное число")
	}
	if stats, _ := l.View["stats"].(bool); !stats {
		return false, nil
	}
	if !l.allows(board) {
		return false, fail("not_found", "Страница не найдена")
	}
	res, err := t.q.Exec(t.ctx, `INSERT INTO design_share_views (link_id, guest_id, board, ms, at)
		SELECT ?, ?, ?, ?, ? WHERE (SELECT COUNT(*) FROM design_share_views WHERE link_id = ?) < ?`,
		l.ID, guest, board, int64(math.Min(math.Round(msv), maxSeen)), ms(t.now), l.ID, maxViewRowsByLink)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// shareViews is the owner's «who looked and how long» of one link.
func (t *txn) shareViews(id, linkID string) (map[string]any, error) {
	var view sql.NullString
	err := t.q.QueryRow(t.ctx, `SELECT view FROM design_share_links WHERE id = ? AND machine_id = ? AND project_id = ?`, linkID, t.m.ID, id).Scan(&view)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fail("not_found", "Ссылка не найдена")
	}
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if view.Valid {
		_ = json.Unmarshal([]byte(view.String), &v)
	}
	if stats, _ := v["stats"].(bool); !stats {
		return map[string]any{"enabled": false, "viewers": 0, "ms": 0, "boards": []any{}, "guests": []any{}}, nil
	}
	var viewers, total int64
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(DISTINCT guest_id), COALESCE(SUM(ms), 0) FROM design_share_views WHERE link_id = ?`, linkID).Scan(&viewers, &total); err != nil {
		return nil, err
	}
	boards := []map[string]any{}
	rows, err := t.q.Query(t.ctx, `SELECT board, SUM(ms), COUNT(DISTINCT guest_id) FROM design_share_views WHERE link_id = ? GROUP BY board ORDER BY SUM(ms) DESC, board LIMIT ?`, linkID, viewBoardsShown)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b string
		var m, n int64
		if err := rows.Scan(&b, &m, &n); err != nil {
			rows.Close()
			return nil, err
		}
		boards = append(boards, map[string]any{"board": b, "ms": m, "viewers": n})
	}
	rows.Close()
	guests := []map[string]any{}
	rows, err = t.q.Query(t.ctx, `SELECT v.guest_id,
		(SELECT c.guest_name FROM design_guest_comments c WHERE c.link_id = v.link_id AND c.guest_id = v.guest_id ORDER BY c.id DESC LIMIT 1),
		SUM(v.ms), COUNT(DISTINCT v.board), MAX(v.at)
		FROM design_share_views v WHERE v.link_id = ? GROUP BY v.link_id, v.guest_id ORDER BY MAX(v.at) DESC LIMIT ?`, linkID, viewGuestsShown)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g string
		var name sql.NullString
		var m, n, last int64
		if err := rows.Scan(&g, &name, &m, &n, &last); err != nil {
			rows.Close()
			return nil, err
		}
		guests = append(guests, map[string]any{"guestId": g, "name": nullStr(name.String), "ms": m, "boards": n, "lastAt": isoOf(fromMs(last))})
	}
	rows.Close()
	return map[string]any{"enabled": true, "viewers": viewers, "ms": total, "boards": boards, "guests": guests}, nil
}
