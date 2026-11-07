package designaccess

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The guest links: /s/<token> on the share host. The hub judges the link by its row, asks the machine for a short
// read-only token over the link's project (the machine signs it), and reads the project through the machine's tunnel.

const (
	tokenTimeout  = 10 * time.Second
	fetchTimeout  = 30 * time.Second
	tokenMargin   = 30 * time.Second
	maxShell      = 256 << 10
	maxIndex      = 2 << 20
	maxDeck       = 256 << 10
	maxFile       = 32 << 20
	maxApp        = 16 << 20
	maxGuestBody  = 16 << 10
	ipPerMinute   = 1200
	pagePerMinute = 600
	commentPerMin = 10
	seenPerMinute = 60
	revHeader     = "X-Design-Rev"
	pagesHeader   = "X-Design-Pages"
	// refusedHeader marks the runtime's refusal to serve guests at all: its filesystem failed the share probe
	// (design.ShareRefusedHeader, GitHub issue 30). It is not a machine that is down, and it is said so.
	refusedHeader = "X-Design-Share-Refused"
)

var (
	guestRoute = regexp.MustCompile(`^/s/([^/]+)(?:/(index\.json|comments|seen|m/(video\.mp4|video-9x16\.mp4|deck\.pdf)|f/(.+)|app/(.+))|/)?$`)
	rangeRe    = regexp.MustCompile(`^bytes=\d*-\d*(?:\s*,\s*\d*-\d*){0,7}$`)
	scriptRe   = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	srcAttrRe  = regexp.MustCompile(`(?i)(?:^|\s)src\s*=`)
	typeAttrRe = regexp.MustCompile(`(?i)(?:^|\s)type\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	headOpenRe = regexp.MustCompile(`(?i)<head\b[^>]*>`)
	headEndRe  = regexp.MustCompile(`(?i)</head\s*>`)
)

// ---- budgets ----------------------------------------------------------------------------------------------------

type window struct {
	start time.Time
	n     int
}

type budget struct {
	mu sync.Mutex
	w  map[string]*window
}

func newBudget() *budget { return &budget{w: map[string]*window{}} }

func (b *budget) take(key string, limit int, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.w[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &window{start: now}
		b.w[key] = w
	}
	w.n++
	return w.n <= limit
}

func (b *budget) sweep(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, w := range b.w {
		if now.Sub(w.start) >= time.Minute {
			delete(b.w, k)
		}
	}
}

// ---- the machine's token ------------------------------------------------------------------------------------------

type cachedToken struct {
	tok   hubproto.DesignPublicToken
	until time.Time
}

func (s *Service) dropTokens() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.tokens {
		if !now.Before(t.until) {
			delete(s.tokens, k)
		}
	}
}

func (s *Service) guestToken(ctx context.Context, machine string, req hubproto.DesignPublicTokenRequest) (hubproto.DesignPublicToken, error) {
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%v\x00%q", machine, req.Project, req.Rev, req.Pin, req.Pages)
	now := s.now()
	s.mu.Lock()
	c, ok := s.tokens[key]
	s.mu.Unlock()
	if ok && now.Before(c.until) {
		return c.tok, nil
	}
	if s.reg.Get(machine) == nil {
		return hubproto.DesignPublicToken{}, errNoMachine
	}
	var tok hubproto.DesignPublicToken
	if err := s.reg.DoInto(ctx, machine, hubproto.OpDesignPublicToken, req, &tok, tokenTimeout); err != nil {
		return tok, err
	}
	if tok.Token == "" || tok.Header == "" {
		return tok, errors.New("machine gave no token")
	}
	until := time.UnixMilli(tok.ExpiresAt).Add(-tokenMargin)
	if until.After(now) {
		s.mu.Lock()
		s.tokens[key] = cachedToken{tok: tok, until: until}
		s.mu.Unlock()
	}
	return tok, nil
}

// errRuntime is the machine answering what the hub did not ask for: a guest gets «the owner's machine is unavailable».
var errRuntime = errors.New("machine answer out of scope")

// fetch reads design-public/<project>/<sub> of the machine with a token of the given scope.
func (s *Service) fetch(ctx context.Context, machine string, req hubproto.DesignPublicTokenRequest, segs []string, r *http.Request, pass ...string) (*http.Response, error) {
	tok, err := s.guestToken(ctx, machine, req)
	if err != nil {
		return nil, err
	}
	p := s.prefixOf(machine) + "/design-public/" + url.PathEscape(req.Project)
	for _, seg := range segs {
		p += "/" + url.PathEscape(seg)
	}
	method := http.MethodGet
	if r != nil && r.Method == http.MethodHead {
		method = http.MethodHead
	}
	out, err := http.NewRequestWithContext(ctx, method, "http://runtime/m/"+machine+p, nil)
	if err != nil {
		return nil, err
	}
	out.Header.Set(tok.Header, tok.Token)
	out.Header.Set("Accept-Encoding", "gzip, br")
	if r != nil {
		for _, h := range pass {
			if v := r.Header.Get(h); v != "" {
				out.Header.Set(h, v)
			}
		}
	}
	if s.transport == nil {
		return nil, errNoMachine
	}
	res, err := (&http.Client{Transport: s.transport(machine), Timeout: fetchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(out)
	if err != nil {
		return nil, err
	}
	if res.Header.Get(refusedHeader) != "" {
		res.Body.Close()
		return nil, gUnsafeRuntime
	}
	if res.StatusCode < 400 && ((req.Rev != "" && res.Header.Get(revHeader) != req.Rev) || (req.Pages != nil && res.Header.Get(pagesHeader) != "limited")) {
		res.Body.Close()
		return nil, errRuntime
	}
	return res, nil
}

func readAll(res *http.Response, max int64) ([]byte, error) {
	defer res.Body.Close()
	if res.ContentLength > max {
		return nil, errRuntime
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errRuntime
	}
	return b, nil
}

// pin takes the project's current version for a link pinned to it.
func (s *Service) pin(ctx context.Context, m store.Machine, project string) (string, error) {
	failed := fail("unavailable", "Машина владельца не ответила, версию не удалось закрепить. Попробуйте ещё раз")
	res, err := s.fetch(ctx, m.ID, hubproto.DesignPublicTokenRequest{Project: project, Pin: true}, []string{"__rev"}, nil)
	if errors.Is(err, gUnsafeRuntime) {
		return "", unsafeRuntime()
	}
	if err != nil {
		return "", failed
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return "", failed
	}
	b, err := readAll(res, 4096)
	if err != nil {
		return "", failed
	}
	var v struct {
		Rev string `json:"rev"`
	}
	if json.Unmarshal(b, &v) != nil {
		return "", failed
	}
	rev, err := checkRev(v.Rev)
	if err != nil {
		return "", failed
	}
	return rev, nil
}

// unsafeRuntime is the owner's answer when the machine refuses guest links: what is wrong and what to run instead.
func unsafeRuntime() *Error {
	return fail("share_unsafe_runtime", "Гостевые ссылки с этой машины выключены: среда рантайма не даёт гарантий файловой системы, "+
		"которые нужны гостевому доступу (так бывает в песочницах вроде gVisor). Запустите рантайм прямо на Linux, в виртуальной машине "+
		"или в контейнере с runc или crun и проверьте командой «harnessly doctor share»")
}

// shareGate asks the machine whether a link may be made before the hub makes one that is not pinned (a pinned one asks
// through pin). Only the refusal of an unsafe runtime stops it here: a machine that does not answer gets its link as
// before, and serves nothing to a guest until its probe passes, since every guest read is refused there too.
func (s *Service) shareGate(ctx context.Context, m store.Machine, project string) error {
	res, err := s.fetch(ctx, m.ID, hubproto.DesignPublicTokenRequest{Project: project, Pin: true}, []string{"__share"}, nil)
	if errors.Is(err, gUnsafeRuntime) {
		return unsafeRuntime()
	}
	if err == nil {
		res.Body.Close()
	}
	return nil
}

// ---- the guest handler --------------------------------------------------------------------------------------------

type guestError struct {
	status int
	text   string
}

func (e guestError) Error() string { return e.text }

var (
	gNotFound     = guestError{404, "Ссылка не найдена"}
	gNoProject    = guestError{404, "Проект не найден"}
	gNoFile       = guestError{404, "Файл не найден"}
	gNoPage       = guestError{404, "Страница не найдена"}
	gExpired      = guestError{410, "Срок ссылки истёк"}
	gRevoked      = guestError{410, "Ссылка отозвана"}
	gTooMany      = guestError{429, "Слишком много запросов, попробуйте через минуту"}
	gInternal     = guestError{500, "Что-то пошло не так, попробуйте позже"}
	gMachine      = guestError{502, "Машина владельца недоступна, попробуйте позже"}
	gViewOnly     = guestError{403, "Эту ссылку можно только смотреть"}
	gNoMedia      = guestError{403, "Файл недоступен"}
	gBadRange     = guestError{416, "Неверный диапазон"}
	gBadGuestBody = guestError{400, "Некорректное тело запроса"}
	// gUnsafeRuntime: the owner's machine refuses guests, its filesystem failed the share probe (see unsafeRuntime).
	gUnsafeRuntime = guestError{503, "Владелец пока не может открыть этот дизайн по ссылке: его машина не даёт нужных гарантий безопасности. Попробуйте позже"}
)

var statusTitles = map[int]string{
	400: "Некорректный запрос", 403: "Нет доступа", 404: "Ссылка не найдена", 405: "Так ссылку не открыть", 410: "Ссылка отозвана",
	416: "Неверный диапазон", 429: "Слишком много запросов, попробуйте через минуту", 500: "Что-то пошло не так, попробуйте позже",
	502: "Машина владельца недоступна, попробуйте позже", 503: "Сервис пока недоступен",
}

func baseHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cache-Control", "no-store")
}

func statusPage(w http.ResponseWriter, status int, text string, frameable bool) {
	h := w.Header()
	baseHeaders(h)
	if text == "" {
		text = statusTitles[status]
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"+map[bool]string{true: "", false: "; frame-ancestors 'none'"}[frameable])
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>Sinopia Design</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,sans-serif;background:#f6f6f4;color:#1d1d1b}@media(prefers-color-scheme:dark){body{background:#161615;color:#ededeb}}main{padding:24px;text-align:center}h1{font-size:20px;margin:0 0 8px}p{margin:0;opacity:.7}</style></head><body><main><h1>%s</h1><p>Sinopia Design</p></main></body></html>`, html.EscapeString(text))
}

func guestJSON(w http.ResponseWriter, status int, v any) {
	baseHeaders(w.Header())
	writeJSON(w, status, v)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Public serves the guest links on the share host.
func (s *Service) Public() http.Handler {
	return http.HandlerFunc(s.serveGuest)
}

type guestReq struct {
	l       *shareTarget
	m       store.Machine
	section string
	media   string
	file    string
	app     string
	asJSON  bool
}

func (s *Service) serveGuest(w http.ResponseWriter, r *http.Request) {
	match := guestRoute.FindStringSubmatch(r.URL.EscapedPath())
	if match == nil {
		statusPage(w, 404, "", false)
		return
	}
	g := guestReq{section: match[2], media: match[3], file: match[4], app: match[5]}
	switch {
	case g.media != "":
		g.section = "m"
	case g.file != "":
		g.section = "f"
	case g.app != "":
		g.section = "app"
	}
	g.asJSON = g.section == "comments" || g.section == "seen" || g.section == "index.json"
	allow := "GET"
	switch g.section {
	case "comments":
		allow = "GET, POST"
	case "seen":
		allow = "POST"
	case "m":
		allow = "GET, HEAD"
	}
	if !strings.Contains(allow, r.Method) {
		w.Header().Set("Allow", allow)
		s.guestFail(w, g, guestError{405, statusTitles[405]})
		return
	}
	token, err := url.PathUnescape(match[1])
	if err != nil || !shareTokenRe.MatchString(token) {
		s.guestFail(w, g, gNotFound)
		return
	}
	now := s.now()
	ip := clientIP(r)
	if !s.budget.take("ip\x00"+ip, ipPerMinute, now) {
		w.Header().Set("Retry-After", "60")
		s.guestFail(w, g, gTooMany)
		return
	}
	ctx := r.Context()
	state, l, err := s.resolveShare(ctx, now, token)
	if err != nil {
		s.log.Error("guest link not read", "error", err)
		s.guestFail(w, g, gInternal)
		return
	}
	switch state {
	case "missing":
		s.guestFail(w, g, gNotFound)
		return
	case "expired":
		s.guestFail(w, g, gExpired)
		return
	case "revoked":
		s.guestFail(w, g, gRevoked)
		return
	}
	m, err := s.db.S().MachineByID(ctx, l.Machine)
	if errors.Is(err, store.ErrNotFound) {
		s.guestFail(w, g, gNotFound)
		return
	}
	if err != nil {
		s.guestFail(w, g, gInternal)
		return
	}
	g.l, g.m = l, m
	t, err := s.read(ctx, m)
	if err != nil {
		s.guestFail(w, g, gInternal)
		return
	}
	ok, err := t.makerMayShare(l.Project, l.CreatedBy)
	if err != nil {
		s.guestFail(w, g, gInternal)
		return
	}
	if !ok {
		s.guestFail(w, g, gRevoked)
		return
	}
	if l.Version == "pinned" && l.Rev == "" {
		s.guestFail(w, g, gInternal)
		return
	}
	kind, limit := "page", pagePerMinute
	switch {
	case g.section == "comments" && r.Method == http.MethodPost:
		kind, limit = "comment", commentPerMin
	case g.section == "seen":
		kind, limit = "seen", seenPerMinute
	}
	if !s.budget.take(kind+"\x00"+l.ID+"\x00"+ip, limit, now) {
		w.Header().Set("Retry-After", "60")
		s.guestFail(w, g, gTooMany)
		return
	}
	if g.section == "" {
		if err := t.recordOpen(l.ID); err != nil {
			s.log.Warn("guest open not counted", "error", err)
		}
	}
	switch g.section {
	case "":
		err = s.guestShell(w, r, g)
	case "index.json":
		var idx map[string]any
		if idx, _, err = s.guestIndex(ctx, g); err == nil {
			guestJSON(w, 200, idx)
		}
	case "f":
		err = s.guestFile(w, r, g)
	case "app":
		err = s.guestApp(w, r, g)
	case "m":
		err = s.guestMedia(w, r, g)
	case "comments":
		err = s.guestComments(w, r, g, t)
	case "seen":
		err = s.guestSeen(w, r, g, t)
	}
	if err != nil {
		s.guestFail(w, g, err)
	}
}

func (s *Service) guestFail(w http.ResponseWriter, g guestReq, err error) {
	var ge guestError
	if !errors.As(err, &ge) {
		if e, ok := asError(err); ok {
			ge = guestError{statusOf[e.Code], e.Message}
			if ge.status == 0 {
				ge.status = 400
			}
		} else if errors.Is(err, errNoMachine) || errors.Is(err, errRuntime) {
			ge = gMachine
		} else {
			var ne net.Error
			if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "offline") {
				ge = gMachine
			} else {
				s.log.Warn("guest request failed", "error", err)
				ge = gMachine
			}
		}
	}
	if g.asJSON {
		guestJSON(w, ge.status, map[string]string{"error": ge.text})
		return
	}
	statusPage(w, ge.status, ge.text, g.section == "f")
}

func (g guestReq) scope() hubproto.DesignPublicTokenRequest {
	return hubproto.DesignPublicTokenRequest{Project: g.l.Project, Rev: g.l.Rev, Pages: g.l.Pages}
}

type indexBoard struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	W     *int   `json:"w"`
	H     *int   `json:"h"`
}

// guestIndex is what the guest page reads of the project: the link's terms and the boards it opens.
func (s *Service) guestIndex(ctx context.Context, g guestReq) (map[string]any, []indexBoard, error) {
	res, err := s.fetch(ctx, g.m.ID, g.scope(), nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode == 404 {
		res.Body.Close()
		return nil, nil, gNoProject
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, nil, gMachine
	}
	raw, err := readAll(res, maxIndex)
	if err != nil {
		return nil, nil, gMachine
	}
	var idx struct {
		Title  string          `json:"title"`
		Kind   string          `json:"kind"`
		Deck   json.RawMessage `json:"deck"`
		Boards []indexBoard    `json:"boards"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil || json.Unmarshal(raw, &idx) != nil || idx.Boards == nil && string(probe["boards"]) != "[]" {
		return nil, nil, gMachine
	}
	boards := []indexBoard{}
	for _, b := range idx.Boards {
		if g.l.allows(b.Path) {
			boards = append(boards, b)
		}
	}
	kind := any(nil)
	switch idx.Kind {
	case "site", "slides", "video", "ds":
		kind = idx.Kind
	}
	var deck any
	if g.l.Version != "pinned" && g.l.Pages == nil && len(idx.Deck) > 0 && len(idx.Deck) <= maxDeck && idx.Deck[0] == '{' {
		deck = idx.Deck
	}
	title := g.l.Title
	if title == "" {
		title = idx.Title
	}
	var view any
	if g.l.View != nil {
		view = g.l.View
	}
	return map[string]any{"title": title, "rights": g.l.Rights, "expiresAt": iso(g.l.Expires), "view": view, "kind": kind, "deck": deck, "boards": boards}, boards, nil
}

// inlineHashes are the CSP hashes of the page's inline scripts.
func inlineHashes(page string) []string {
	var out []string
	for _, m := range scriptRe.FindAllStringSubmatch(page, -1) {
		attrs, body := m[1], m[2]
		if srcAttrRe.MatchString(attrs) {
			continue
		}
		typ := ""
		if t := typeAttrRe.FindStringSubmatch(attrs); t != nil {
			typ = strings.ToLower(strings.TrimSpace(t[1] + t[2] + t[3]))
		}
		if typ != "" && typ != "module" && typ != "text/javascript" && typ != "application/javascript" {
			continue
		}
		sum := sha256.Sum256([]byte(body))
		out = append(out, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return out
}

// safeJSON: encoding/json escapes <, >, & and the line separators, so the text cannot close the script it sits in.
func safeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func (s *Service) guestShell(w http.ResponseWriter, r *http.Request, g guestReq) error {
	ctx := r.Context()
	idx, _, err := s.guestIndex(ctx, g)
	if err != nil {
		return err
	}
	res, err := s.fetch(ctx, g.m.ID, g.scope(), []string{"_app", "index.html"}, nil)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return gMachine
	}
	raw, err := readAll(res, maxShell)
	if err != nil {
		return gMachine
	}
	page := string(raw)
	base := "/s/" + g.l.Token + "/"
	cfg := map[string]any{"v": 1, "token": g.l.Token, "filesBase": base + "f/", "commentsUrl": base + "comments"}
	for k, v := range idx {
		cfg[k] = v
	}
	js, err := safeJSON(cfg)
	if err != nil {
		return err
	}
	hashes := inlineHashes(page)
	baseTag := `<base href="` + base + `app/">`
	if loc := headOpenRe.FindStringIndex(page); loc != nil {
		page = page[:loc[1]] + baseTag + page[loc[1]:]
	} else {
		page = baseTag + page
	}
	cfgTag := `<script id="guest-config" type="application/json">` + js + `</script>`
	if loc := headEndRe.FindStringIndex(page); loc != nil {
		page = page[:loc[0]] + cfgTag + page[loc[0]:]
	} else {
		page = cfgTag + page
	}
	app := s.shareURL + base + "app/"
	h := w.Header()
	baseHeaders(h)
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src "+strings.Join(append([]string{app}, hashes...), " ")+"; style-src "+app+
		" 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:; connect-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, page)
	return nil
}

// segments decodes a guest path into safe segments.
func segments(raw string) ([]string, bool) {
	var out []string
	for _, p := range strings.Split(raw, "/") {
		seg, err := url.PathUnescape(p)
		if err != nil || seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "\\\x00") || hasControl(seg) {
			return nil, false
		}
		out = append(out, seg)
	}
	return out, len(out) > 0
}

var safeTypes = []string{"text/", "image/", "font/", "audio/", "video/", "application/json", "application/javascript", "application/wasm",
	"application/pdf", "application/xml", "application/manifest+json", "application/font-", "application/vnd.ms-fontobject"}

func safeType(v string) string {
	low := strings.ToLower(v)
	for _, p := range safeTypes {
		if strings.HasPrefix(low, p) {
			return v
		}
	}
	return "application/octet-stream"
}

func copyBody(w http.ResponseWriter, res *http.Response, max int64, head bool) error {
	defer res.Body.Close()
	if res.ContentLength > max {
		return gMachine
	}
	if res.ContentLength >= 0 {
		w.Header().Set("Content-Length", fmt.Sprint(res.ContentLength))
	}
	w.WriteHeader(res.StatusCode)
	if !head {
		_, _ = io.Copy(w, io.LimitReader(res.Body, max))
	}
	return nil
}

func passEncoding(h http.Header, res *http.Response) {
	if enc := res.Header.Get("Content-Encoding"); enc == "gzip" || enc == "br" {
		h.Set("Content-Encoding", enc)
		h.Set("Vary", "Accept-Encoding")
	}
}

func (s *Service) guestFile(w http.ResponseWriter, r *http.Request, g guestReq) error {
	segs, ok := segments(g.file)
	if !ok {
		return gNoFile
	}
	if strings.HasPrefix(segs[0], "__") && segs[0] != "__guest" && segs[0] != "__vendor" {
		return gNoFile
	}
	ext := strings.ToLower(path.Ext(segs[len(segs)-1]))
	if g.l.Pages != nil && (ext == ".html" || ext == ".htm") {
		_, boards, err := s.guestIndex(r.Context(), g)
		if err != nil {
			return err
		}
		want := path.Clean(strings.Join(segs, "/"))
		found := false
		for _, b := range boards {
			file, _, _ := strings.Cut(b.Path, "?")
			if path.Clean(file) == want {
				found = true
			}
		}
		if !found {
			return gNoFile
		}
	}
	res, err := s.fetch(r.Context(), g.m.ID, g.scope(), segs, r)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		if res.StatusCode == 404 || res.StatusCode == 403 {
			return gNoFile
		}
		return gMachine
	}
	h := w.Header()
	baseHeaders(h)
	h.Set("Content-Type", safeType(res.Header.Get("Content-Type")))
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-popups allow-forms allow-modals; default-src 'self' https: data: blob: 'unsafe-inline' 'unsafe-eval'; frame-ancestors "+s.shareURL)
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cache-Control", "private, max-age=30")
	passEncoding(h, res)
	return copyBody(w, res, maxFile, false)
}

func (s *Service) guestApp(w http.ResponseWriter, r *http.Request, g guestReq) error {
	segs, ok := segments(g.app)
	if !ok {
		return gNoFile
	}
	res, err := s.fetch(r.Context(), g.m.ID, g.scope(), append([]string{"_app"}, segs...), r)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		if res.StatusCode == 404 {
			return gNoFile
		}
		return gMachine
	}
	h := w.Header()
	baseHeaders(h)
	h.Set("Content-Type", safeType(res.Header.Get("Content-Type")))
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox")
	if segs[0] == "assets" && strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	passEncoding(h, res)
	return copyBody(w, res, maxApp, false)
}

func (s *Service) guestMedia(w http.ResponseWriter, r *http.Request, g guestReq) error {
	if g.l.Version == "pinned" || g.l.Pages != nil {
		return gNoFile
	}
	flag := "video"
	if g.media == "deck.pdf" {
		flag = "pdf"
	}
	if on, _ := g.l.View[flag].(bool); !on {
		return gNoMedia
	}
	if rg := r.Header.Get("Range"); rg != "" && !rangeRe.MatchString(rg) {
		return gBadRange
	}
	res, err := s.fetch(r.Context(), g.m.ID, g.scope(), []string{"__media", g.media}, r, "Range", "If-Range")
	if err != nil {
		return err
	}
	switch res.StatusCode {
	case 200, 206, 416:
	case 404:
		res.Body.Close()
		return gNoFile
	default:
		res.Body.Close()
		return gMachine
	}
	h := w.Header()
	baseHeaders(h)
	h.Set("Content-Type", safeType(res.Header.Get("Content-Type")))
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Cache-Control", "private, max-age=60")
	if g.media == "deck.pdf" {
		h.Set("Content-Disposition", `inline; filename="deck.pdf"`)
	}
	for _, k := range []string{"ETag", "Last-Modified", "Content-Range"} {
		if v := res.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	defer res.Body.Close()
	if res.ContentLength >= 0 {
		h.Set("Content-Length", fmt.Sprint(res.ContentLength))
	}
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, res.Body)
	}
	return nil
}

func guestBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxGuestBody+1))
	if err != nil || len(raw) > maxGuestBody {
		return nil, gBadGuestBody
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, gBadGuestBody
	}
	return v, nil
}

func (s *Service) guestComments(w http.ResponseWriter, r *http.Request, g guestReq, t *txn) error {
	if g.l.Rights != roleComment {
		return gViewOnly
	}
	if r.Method == http.MethodGet {
		list, err := t.listPublicComments(g.l)
		if err != nil {
			return err
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		guestJSON(w, 200, list)
		return nil
	}
	body, err := guestBody(r)
	if err != nil {
		return err
	}
	c, err := checkGuestComment(body)
	if err != nil {
		return err
	}
	if !g.l.allows(c.Board) {
		return gNoPage
	}
	var made guestComment
	if err := s.write(r.Context(), g.m, func(t *txn) error {
		var err error
		made, err = t.addGuestComment(g.l, c)
		return err
	}); err != nil {
		return err
	}
	guestJSON(w, http.StatusCreated, made)
	return nil
}

func (s *Service) guestSeen(w http.ResponseWriter, r *http.Request, g guestReq, t *txn) error {
	body, err := guestBody(r)
	if err != nil {
		return err
	}
	if _, err := t.recordSeen(g.l, body); err != nil {
		return err
	}
	baseHeaders(w.Header())
	w.WriteHeader(http.StatusNoContent)
	return nil
}
