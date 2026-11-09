package vaultsvc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/vault"
)

const (
	GrantHours   = 8
	maxLogin     = 256
	maxPassword  = 1024
	maxLabel     = 120
	maxJSON      = 8 * 1024
	maxEntries   = 200
	auditLimit   = 50
	tokenTTL     = 30 * time.Second
	maxTokens    = 1000
	headerPage   = "X-Hl-Vault"
	routeStatus  = "status"
	routeEntries = "entries"
	routeAudit   = "audit"
)

var (
	agentKeyRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,96}/[A-Za-z0-9._:-]{1,96}$`)
	tokenRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	nonceRe    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	slotRe     = regexp.MustCompile(`^https://[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	idRe       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	controlRe  = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// Person is who asks: the hub's account, never read from a request body.
type Person struct{ ID, Email string }

// Config is the safe's settings. A nil Keyring is a safe that is off.
type Config struct {
	Keyring *Keyring
	// KeyError says why there is no keyring (for the status answer); never carries a value.
	KeyError string
	// SharedMembers see the shared entries (emails, lowercase); SharedEditors change them, and must be members too.
	SharedMembers map[string]bool
	SharedEditors map[string]bool
}

// Service is the safe.
type Service struct {
	db  DB
	cfg Config
	log *slog.Logger
	now func() time.Time

	reveals, redemptions, writes, reads *limiter

	mu        sync.Mutex
	fills     map[string]*fillToken
	approvals map[string]*approval
}

type fillToken struct {
	person, entry, origin, slot, nonce, actor string
	agent                                     *string
	expires                                   time.Time
}

type approval struct {
	person, entry, origin, agent string
	expires                      time.Time
}

// New is the safe over db. now may be nil.
func New(db DB, cfg Config, log *slog.Logger, now func() time.Time) *Service {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	s := &Service{db: db, cfg: cfg, log: log, now: now, fills: map[string]*fillToken{}, approvals: map[string]*approval{}}
	s.reveals = newLimiter([]rule{{6, time.Minute}, {40, time.Hour}})
	s.redemptions = newLimiter([]rule{{30, time.Minute}})
	s.writes = newLimiter([]rule{{30, time.Hour}})
	s.reads = newLimiter([]rule{{120, time.Minute}})
	return s
}

// Emails reads a comma-separated list into a set of lowercase emails.
func Emails(v string) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Split(v, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out[e] = true
		}
	}
	return out
}

type entryJSON struct {
	ID         string  `json:"id"`
	Origin     string  `json:"origin"`
	Login      string  `json:"login"`
	Label      *string `json:"label"`
	Shared     bool    `json:"shared"`
	Mine       bool    `json:"mine"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

func iso(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z") }

func (s *Service) seesShared(p Person) bool { return s.cfg.SharedMembers[strings.ToLower(p.Email)] }
func (s *Service) editsShared(p Person) bool {
	return s.seesShared(p) && s.cfg.SharedEditors[strings.ToLower(p.Email)]
}
func (s *Service) editable(r row, p Person) bool {
	if r.Shared {
		return s.editsShared(p)
	}
	return r.OwnerID == p.ID
}

func (s *Service) present(r row, p Person) entryJSON {
	e := entryJSON{ID: r.ID, Origin: r.Origin, Login: r.Login, Label: r.Label, Shared: r.Shared, Mine: s.editable(r, p),
		CreatedAt: iso(r.CreatedAt), UpdatedAt: iso(r.UpdatedAt)}
	if r.LastUsedAt != nil {
		v := iso(*r.LastUsedAt)
		e.LastUsedAt = &v
	}
	return e
}

func json2(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	httpx.WriteJSON(w, status, v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	json2(w, status, map[string]string{"error": msg})
}

func empty(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) record(ctx context.Context, p Person, r *row, action, actor, outcome string, agent *string) {
	var entry, origin *string
	if r != nil {
		entry, origin = &r.ID, &r.Origin
	}
	lvl := slog.LevelInfo
	if outcome != "ok" {
		lvl = slog.LevelWarn
	}
	s.log.Log(ctx, lvl, "vault event", "person", p.ID, "entry", entry, "action", action, "actor", actor, "outcome", outcome)
	if err := writeAudit(ctx, s.db, randID(), p.ID, entry, origin, action, actor, agent, outcome, s.now().UnixMilli()); err != nil {
		s.log.Error("vault audit not written", "action", action, "error", err)
	}
}

func randID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func newToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (s *Service) keepFill(t *fillToken) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	at := s.now()
	for k, v := range s.fills {
		if !v.expires.After(at) {
			delete(s.fills, k)
		}
	}
	if len(s.fills) >= maxTokens {
		for k := range s.fills {
			delete(s.fills, k)
			break
		}
	}
	tok := newToken()
	s.fills[tok] = t
	return tok
}

func (s *Service) keepApproval(a *approval) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	at := s.now()
	for k, v := range s.approvals {
		if !v.expires.After(at) {
			delete(s.approvals, k)
		}
	}
	if len(s.approvals) >= maxTokens {
		for k := range s.approvals {
			delete(s.approvals, k)
			break
		}
	}
	tok := newToken()
	s.approvals[tok] = a
	return tok
}

func (s *Service) takeFill(tok string) *fillToken {
	if !tokenRe.MatchString(tok) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.fills[tok]
	delete(s.fills, tok)
	return t
}

func (s *Service) takeApproval(tok string) *approval {
	if !tokenRe.MatchString(tok) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.approvals[tok]
	delete(s.approvals, tok)
	return a
}

var errBody = errors.New("bad body")

func readJSON(r *http.Request) (map[string]any, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxJSON+1))
	if err != nil || len(b) > maxJSON {
		return nil, errBody
	}
	out := map[string]any{}
	if len(b) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, errBody
	}
	return out, nil
}

func str(v any, max int) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" || utf8.RuneCountInString(s) > max || controlRe.MatchString(s) {
		return "", false
	}
	return s, true
}

func secretOf(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" || utf8.RuneCountInString(s) > maxPassword {
		return "", false
	}
	return s, true
}

// labelOf: (value, present, ok). null or "" clears it.
func labelOf(m map[string]any) (*string, bool, bool) {
	v, has := m["label"]
	if !has {
		return nil, false, true
	}
	if v == nil {
		return nil, true, true
	}
	s, ok := v.(string)
	if !ok {
		return nil, true, false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true, true
	}
	if utf8.RuneCountInString(s) > maxLabel || controlRe.MatchString(s) {
		return nil, true, false
	}
	return &s, true, true
}

func agentOf(v any) *string {
	if s, ok := v.(string); ok && agentKeyRe.MatchString(s) {
		return &s
	}
	return nil
}

func sameOrigin(a, b any) bool {
	x := vault.Origin(a)
	return x != "" && x == vault.Origin(b)
}

// Enabled says whether the safe has a master key.
func (s *Service) Enabled() bool { return s.cfg.Keyring != nil }

func (s *Service) status(p Person) map[string]any {
	if s.cfg.Keyring == nil {
		return map[string]any{"enabled": false, "reason": "Сейф не настроен: у хаба нет мастер-ключа (HUB_VAULT_KEYS)"}
	}
	return map[string]any{"enabled": true, "canShare": s.editsShared(p), "grantHours": GrantHours}
}

// forgeable: a cross-site page can send neither the custom header nor JSON without a preflight this hub never answers.
func forgeable(r *http.Request) bool {
	if r.Header.Get(headerPage) != "1" {
		return true
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site != "" && site != "same-origin"
}

// Serve answers one call of the page: route is the path after `/vault`, without slashes at the ends.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, p Person, route string) {
	parts := []string{}
	for _, x := range strings.Split(route, "/") {
		if x != "" {
			parts = append(parts, x)
		}
	}
	ctx := r.Context()
	if p.ID == "" {
		fail(w, http.StatusForbidden, "Нет личности")
		return
	}
	if forgeable(r) {
		fail(w, http.StatusForbidden, "Запрос не со страницы рабочего места")
		return
	}
	m := r.Method
	if m != http.MethodGet && m != http.MethodDelete && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, http.StatusUnsupportedMediaType, "Нужен application/json")
		return
	}
	if len(parts) == 1 && parts[0] == routeStatus && m == http.MethodGet {
		json2(w, http.StatusOK, s.status(p))
		return
	}
	if s.cfg.Keyring == nil {
		fail(w, http.StatusServiceUnavailable, "Сейф не настроен")
		return
	}
	if len(parts) == 0 {
		fail(w, http.StatusNotFound, "Не найдено")
		return
	}
	section := parts[0]
	var id, action string
	if len(parts) > 1 {
		id = parts[1]
	}
	if len(parts) > 2 {
		action = parts[2]
	}
	if m == http.MethodGet && (section == routeEntries || section == routeAudit) && len(parts) == 1 {
		if ok, wait := s.reads.take(p.ID, s.now()); !ok {
			limited(w, wait)
			return
		}
		if section == routeAudit {
			ev, err := listAudit(ctx, s.db, p.ID, auditLimit)
			if err != nil {
				s.internal(w, "audit", err)
				return
			}
			out := make([]map[string]any, 0, len(ev))
			for _, e := range ev {
				out = append(out, map[string]any{"at": iso(e.At), "entryId": e.EntryID, "origin": e.Origin, "action": e.Action, "actor": e.Actor, "agentKey": e.AgentKey, "outcome": e.Outcome})
			}
			json2(w, http.StatusOK, map[string]any{"events": out})
			return
		}
		rows, err := listVisible(ctx, s.db, p.ID, s.seesShared(p))
		if err != nil {
			s.internal(w, "list", err)
			return
		}
		out := make([]entryJSON, 0, len(rows))
		for _, x := range rows {
			out = append(out, s.present(x, p))
		}
		json2(w, http.StatusOK, map[string]any{"entries": out})
		return
	}
	if section != routeEntries {
		fail(w, http.StatusNotFound, "Не найдено")
		return
	}
	var err error
	switch {
	case m == http.MethodPost && action == "fill" && len(parts) == 3:
		err = s.mint(w, r, p, id)
	default:
		if ok, wait := s.writes.take(p.ID, s.now()); !ok {
			limited(w, wait)
			return
		}
		err = s.write(w, r, p, parts, id, action)
	}
	switch {
	case err == nil:
	case errors.Is(err, errBody):
		fail(w, http.StatusBadRequest, "Некорректное тело запроса")
	default:
		s.internal(w, "route", err)
	}
}

func limited(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", itoa(int((wait+time.Second-1)/time.Second)))
	fail(w, http.StatusTooManyRequests, "Слишком часто, попробуйте позже")
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (s *Service) internal(w http.ResponseWriter, what string, err error) {
	s.log.Error("vault route failed", "what", what, "error", err)
	fail(w, http.StatusInternalServerError, "Внутренняя ошибка")
}

func (s *Service) visibleRow(ctx context.Context, id string, p Person) (*row, error) {
	if !idRe.MatchString(id) {
		return nil, nil
	}
	return getVisible(ctx, s.db, id, p.ID, s.seesShared(p))
}

func (s *Service) write(w http.ResponseWriter, r *http.Request, p Person, parts []string, id, action string) error {
	ctx := r.Context()
	m, n := r.Method, len(parts)
	switch {
	case m == http.MethodPost && n == 1:
		return s.create(w, r, p)
	case m == http.MethodPost && action == "approve" && n == 3:
		return s.approve(w, r, p, id)
	case m == http.MethodPatch && n == 2:
		return s.change(w, r, p, id)
	}
	if n < 2 {
		fail(w, http.StatusNotFound, "Не найдено")
		return nil
	}
	x, err := s.visibleRow(ctx, id, p)
	if err != nil {
		return err
	}
	if x == nil {
		fail(w, http.StatusNotFound, "Записи нет")
		return nil
	}
	switch {
	case m == http.MethodPost && action == "deny" && n == 3:
		body, err := readJSON(r)
		if err != nil {
			return err
		}
		s.record(ctx, p, x, "deny", "agent", "denied", agentOf(body["agent"]))
		empty(w)
	case m == http.MethodDelete && action == "grants" && n == 3:
		if err := revokeGrants(ctx, s.db, x.ID, p.ID); err != nil {
			return err
		}
		s.record(ctx, p, x, "revoke", "person", "ok", nil)
		empty(w)
	case m == http.MethodDelete && n == 2:
		if !s.editable(*x, p) {
			fail(w, http.StatusForbidden, "Общую запись удаляют только редакторы сейфа")
			return nil
		}
		ok, err := remove(ctx, s.db, x.ID, p.ID, s.editsShared(p))
		if err != nil {
			return err
		}
		if !ok {
			fail(w, http.StatusNotFound, "Записи нет")
			return nil
		}
		s.record(ctx, p, x, "delete", "person", "ok", nil)
		empty(w)
	default:
		fail(w, http.StatusNotFound, "Не найдено")
	}
	return nil
}

func (s *Service) create(w http.ResponseWriter, r *http.Request, p Person) error {
	ctx := r.Context()
	body, err := readJSON(r)
	if err != nil {
		return err
	}
	origin := vault.Origin(body["origin"])
	login, okLogin := str(body["login"], maxLogin)
	secret, okSecret := secretOf(body["password"])
	label, hasLabel, okLabel := labelOf(body)
	shared := body["shared"] == true
	switch {
	case origin == "":
		fail(w, http.StatusBadRequest, "Адрес сайта: только https://хост[:порт], без пути и логина")
	case !okLogin || !okSecret:
		fail(w, http.StatusBadRequest, "Нужны логин и пароль")
	case hasLabel && !okLabel:
		fail(w, http.StatusBadRequest, "Слишком длинная подпись")
	case shared && !s.editsShared(p):
		fail(w, http.StatusForbidden, "Общие записи заводят только редакторы сейфа")
	default:
		n, err := countOwn(ctx, s.db, p.ID)
		if err != nil {
			return err
		}
		if n >= maxEntries {
			fail(w, http.StatusConflict, "Не больше 200 записей")
			return nil
		}
		at := s.now().UnixMilli()
		x := row{ID: newUUID(), OwnerID: p.ID, Shared: shared, Origin: origin, Login: login, Label: label, CreatedAt: at, UpdatedAt: at}
		sl, err := s.cfg.Keyring.seal(secret, x.ad())
		if err != nil {
			return err
		}
		if err := insert(ctx, s.db, x, sl); errors.Is(err, errDuplicate) {
			fail(w, http.StatusConflict, "Такой логин для этого сайта уже есть")
			return nil
		} else if err != nil {
			return err
		}
		s.record(ctx, p, &x, "create", "person", "ok", nil)
		json2(w, http.StatusCreated, map[string]any{"entry": s.present(x, p)})
	}
	return nil
}

func (s *Service) change(w http.ResponseWriter, r *http.Request, p Person, id string) error {
	ctx := r.Context()
	body, err := readJSON(r)
	if err != nil {
		return err
	}
	x, err := s.visibleRow(ctx, id, p)
	if err != nil {
		return err
	}
	if x == nil {
		fail(w, http.StatusNotFound, "Записи нет")
		return nil
	}
	if !s.editable(*x, p) {
		fail(w, http.StatusForbidden, "Общую запись меняют только редакторы сейфа")
		return nil
	}
	next := *x
	if v, has := body["login"]; has {
		l, ok := str(v, maxLogin)
		if !ok {
			fail(w, http.StatusBadRequest, "Некорректные поля")
			return nil
		}
		next.Login = l
	}
	newSecret, hasSecret := "", false
	if v, has := body["password"]; has {
		sv, ok := secretOf(v)
		if !ok {
			fail(w, http.StatusBadRequest, "Некорректные поля")
			return nil
		}
		newSecret, hasSecret = sv, true
	}
	label, hasLabel, okLabel := labelOf(body)
	if hasLabel && !okLabel {
		fail(w, http.StatusBadRequest, "Некорректные поля")
		return nil
	}
	if hasLabel {
		next.Label = label
	}
	sl := x.sealed
	if hasSecret || next.Login != x.Login {
		plain := newSecret
		if !hasSecret {
			if plain, err = s.cfg.Keyring.open(x.sealed, x.ad()); err != nil {
				fail(w, http.StatusInternalServerError, "Запись не открывается: ключ не тот или запись повреждена")
				return nil
			}
		}
		if sl, err = s.cfg.Keyring.seal(plain, next.ad()); err != nil {
			return err
		}
	}
	ok, err := update(ctx, s.db, next, sl, p.ID, s.editsShared(p), s.now().UnixMilli())
	if errors.Is(err, errDuplicate) {
		fail(w, http.StatusConflict, "Такой логин для этого сайта уже есть")
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		fail(w, http.StatusNotFound, "Записи нет")
		return nil
	}
	next.UpdatedAt = s.now().UnixMilli()
	action := "update"
	if hasSecret {
		action = "update-password"
	}
	s.record(ctx, p, &next, action, "person", "ok", nil)
	json2(w, http.StatusOK, map[string]any{"entry": s.present(next, p)})
	return nil
}

// approve: the person pressed «Разрешить» for an agent tab — a one-time approval for this entry, site and tab.
func (s *Service) approve(w http.ResponseWriter, r *http.Request, p Person, id string) error {
	body, err := readJSON(r)
	if err != nil {
		return err
	}
	x, err := s.visibleRow(r.Context(), id, p)
	if err != nil {
		return err
	}
	if x == nil {
		fail(w, http.StatusNotFound, "Записи нет")
		return nil
	}
	agent := agentOf(body["agent"])
	if agent == nil {
		fail(w, http.StatusBadRequest, "Нужна вкладка агента")
		return nil
	}
	if !sameOrigin(x.Origin, body["origin"]) {
		fail(w, http.StatusConflict, "Запись не для этого сайта")
		return nil
	}
	tok := s.keepApproval(&approval{person: p.ID, entry: x.ID, origin: x.Origin, agent: *agent, expires: s.now().Add(tokenTTL)})
	json2(w, http.StatusOK, map[string]any{"approval": tok})
	return nil
}

// mint: a one-time token for one fill of one document of one slot; the value itself never leaves here for the page.
func (s *Service) mint(w http.ResponseWriter, r *http.Request, p Person, id string) error {
	ctx := r.Context()
	body, err := readJSON(r)
	if err != nil {
		return err
	}
	x, err := s.visibleRow(ctx, id, p)
	if err != nil {
		return err
	}
	if x == nil {
		fail(w, http.StatusNotFound, "Записи нет")
		return nil
	}
	actor, _ := body["for"].(string)
	if actor != "agent" && actor != "person" {
		fail(w, http.StatusBadRequest, "for: person или agent")
		return nil
	}
	var agent *string
	if actor == "agent" {
		if agent = agentOf(body["agent"]); agent == nil {
			fail(w, http.StatusBadRequest, "Нужна вкладка агента")
			return nil
		}
	}
	slot, _ := body["slot"].(string)
	nonce, _ := body["nonce"].(string)
	if !slotRe.MatchString(slot) || !nonceRe.MatchString(nonce) {
		fail(w, http.StatusBadRequest, "Нужны окно превью и документ")
		return nil
	}
	if ok, wait := s.reveals.take(p.ID, s.now()); !ok {
		s.log.Warn("vault event", "person", p.ID, "entry", x.ID, "action", "fill", "actor", actor, "outcome", "rate-limited")
		limited(w, wait)
		return nil
	}
	if !sameOrigin(x.Origin, body["origin"]) {
		s.record(ctx, p, x, "fill", actor, "origin-mismatch", agent)
		fail(w, http.StatusConflict, "Запись не для этого сайта")
		return nil
	}
	if agent != nil {
		if tok, has := body["approval"]; has {
			ts, _ := tok.(string)
			a := s.takeApproval(ts)
			if a == nil || !a.expires.After(s.now()) || a.person != p.ID || a.entry != x.ID || a.origin != x.Origin || a.agent != *agent {
				s.record(ctx, p, x, "approve", "agent", "approval-invalid", agent)
				json2(w, http.StatusConflict, map[string]any{"needsApproval": true, "error": "Подтверждение недействительно: спросите человека ещё раз"})
				return nil
			}
			at := s.now()
			if err := grant(ctx, s.db, x.ID, p.ID, *agent, at.UnixMilli(), at.Add(GrantHours*time.Hour).UnixMilli()); err != nil {
				return err
			}
			s.record(ctx, p, x, "approve", "agent", "ok", agent)
		} else {
			ok, err := hasGrant(ctx, s.db, x.ID, p.ID, *agent, s.now().UnixMilli())
			if err != nil {
				return err
			}
			if !ok {
				s.record(ctx, p, x, "fill", "agent", "needs-approval", agent)
				json2(w, http.StatusConflict, map[string]any{"needsApproval": true, "error": "Нужно подтверждение человека"})
				return nil
			}
		}
	}
	tok := s.keepFill(&fillToken{person: p.ID, entry: x.ID, origin: x.Origin, slot: slot, nonce: nonce, actor: actor, agent: agent, expires: s.now().Add(tokenTTL)})
	json2(w, http.StatusOK, map[string]any{"token": tok, "expiresInMs": tokenTTL.Milliseconds()})
	return nil
}

// Redeem is what the preview side of a slot calls for `POST <slot>/__tk/vault-fill`, with everything from its own
// signed cookie. A token is spent by the first attempt whatever its outcome; it opens only for its person, its slot, its
// document, and only while the slot names the entry's site. TargetOrigin is the site the slot shows now ("" when it
// shows a port).
type RedeemRequest struct {
	Token, Nonce, SlotOrigin, TargetOrigin string
	Person                                 Person
}

// Redeemed is the value for the bridge, or the refusal with its HTTP status.
type Redeemed struct {
	OK       bool
	Login    string
	Password string
	Status   int
	Error    string
}

func (s *Service) Redeem(ctx context.Context, q RedeemRequest) Redeemed {
	if s.cfg.Keyring == nil {
		return Redeemed{Status: 503, Error: "Сейф не настроен"}
	}
	if ok, _ := s.redemptions.take(q.Person.ID, s.now()); !ok {
		return Redeemed{Status: 429, Error: "Слишком часто"}
	}
	held := s.takeFill(q.Token)
	if held == nil || !held.expires.After(s.now()) {
		return Redeemed{Status: 410, Error: "Токен истёк или уже использован"}
	}
	holder := Person{ID: held.person}
	refuse := func(x *row, outcome, msg string) Redeemed {
		if x == nil {
			x = &row{ID: held.entry, Origin: held.origin}
		}
		s.record(ctx, holder, x, "reveal", held.actor, outcome, held.agent)
		return Redeemed{Status: 409, Error: msg}
	}
	switch {
	case held.person != q.Person.ID:
		return refuse(nil, "wrong-person", "Токен выдан другому человеку")
	case held.slot != q.SlotOrigin:
		return refuse(nil, "wrong-slot", "Токен выдан другому окну превью")
	case held.nonce != q.Nonce:
		return refuse(nil, "wrong-document", "Страница сменилась после выдачи токена")
	case q.TargetOrigin == "" || !sameOrigin(held.origin, q.TargetOrigin):
		return refuse(nil, "wrong-origin", "Окно превью показывает другой сайт")
	}
	// The holder's email is not known here, so shared entries are looked up by id and scope in SQL below.
	x, err := getByID(ctx, s.db, held.entry)
	if err != nil || x == nil || !(x.OwnerID == held.person && !x.Shared || x.Shared && s.cfg.SharedMembers[strings.ToLower(q.Person.Email)]) {
		return refuse(nil, "gone", "Записи больше нет")
	}
	plain, err := s.cfg.Keyring.open(x.sealed, x.ad())
	if err != nil {
		s.log.Error("vault entry does not decrypt", "entry", x.ID, "keyId", x.sealed.KeyID)
		s.record(ctx, holder, x, "reveal", held.actor, "decrypt-failed", held.agent)
		return Redeemed{Status: 500, Error: "Запись не открывается: ключ не тот или запись повреждена"}
	}
	_ = markUsed(ctx, s.db, x.ID, s.now().UnixMilli())
	s.record(ctx, holder, x, "reveal", held.actor, "ok", held.agent)
	return Redeemed{OK: true, Login: x.Login, Password: plain}
}
