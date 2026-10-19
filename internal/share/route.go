package share

import (
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"harnessly/internal/agent"
	"harnessly/internal/files"
	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/launch"
	"harnessly/internal/nodejson"
	"harnessly/internal/transcript"
)

// Route answers the snapshot and link routes under the platform API, as the previous runtime's dispatcher and handlers
// did: the dispatcher's refusal and body parsing first, then the handler's own checks.
type Route struct {
	Store *Store
	Page  Page
	Token TokenConfig
	// Secret is the pair's secret: share-read tokens are checked with a key derived from it, and it is a known secret.
	Secret []byte
	// Home and EnvHome are shortened to "~" (the user's home, and $HOME when it differs).
	Home, EnvHome string
	// WorkspaceDir is dropped from the front of paths ("" for none); ProfileHomes is the per-person profiles folder.
	WorkspaceDir, ProfileHomes string
	// Environ is the process environment whose secret-looking values are redacted (nil: none).
	Environ func() []string
	// ShareBaseHeader carries the link prefix the platform gives this runtime; ShareTokenHeader the share-read token.
	ShareBaseHeader, ShareTokenHeader string
	// FilePrefix starts a downloaded file's name.
	FilePrefix string
	// SessionOK is a page session; APIRefusal the old API's refusal of a caller with neither session nor cli-token;
	// Claims the session token's claims (nil without one).
	SessionOK  func(*http.Request) bool
	APIRefusal func(*http.Request) (int, any, http.Header, bool)
	Claims     func(*http.Request) map[string]any
	// FindTab is a tab of this runtime's layouts by id, as stored.
	FindTab func(tabID string) (*jsvalue.Object, bool)
	// Headlines reads a Claude transcript's AI title.
	Headlines *launch.Headlines
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time
}

// Kind is which of the routes a request is for.
type Kind int

// The routes.
const (
	NotShare Kind = iota
	Snapshots
	Download
	Preview
	Shares
	ShareOne
	ShareRead
)

// Match says which route a request is for and the id in its path, as the previous runtime's router split and decoded
// the path; platformAPI is the platform's prefix ("/api/<platform>").
func Match(r *http.Request, platformAPI string) (Kind, string) {
	parts, _, ok := files.APIParts(r)
	want := strings.Split(strings.Trim(platformAPI, "/"), "/")
	if !ok || len(parts) <= len(want) {
		return NotShare, ""
	}
	for i, p := range want {
		if parts[i] != p {
			return NotShare, ""
		}
	}
	rest := parts[len(want):]
	switch {
	case len(rest) == 1 && rest[0] == "snapshots":
		return Snapshots, ""
	case len(rest) == 3 && rest[0] == "snapshots" && rest[2] == "download":
		return Download, rest[1]
	case len(rest) == 3 && rest[0] == "snapshots" && rest[2] == "preview":
		return Preview, rest[1]
	case len(rest) == 1 && rest[0] == "shares":
		return Shares, ""
	case len(rest) == 2 && rest[0] == "shares":
		return ShareOne, rest[1]
	case len(rest) == 2 && rest[0] == "share-read":
		return ShareRead, rest[1]
	}
	return NotShare, ""
}

func (rt *Route) now() time.Time {
	if rt.Now != nil {
		return rt.Now()
	}
	return time.Now()
}

func errorBody(msg string) *jsvalue.Object { return jsvalue.NewObject("error", msg) }

func noStore() http.Header { return http.Header{"Cache-Control": {"no-store"}} }

// internalError is the dispatcher's answer to a handler that threw.
// Headers the handler had set stay on the answer.
func internalError(where string, err error, h http.Header) (int, any, http.Header) {
	agent.Log("error", "api handler failed", agent.F{K: "path", V: where}, agent.F{K: "err", V: err.Error()})
	return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, h
}

// body is the dispatcher's parse; a refusal is the answer to send.
func body(r *http.Request) (any, bool, int, any, http.Header) {
	v, _, status, refusal := nodejson.ParseBody(r, 1<<20, "1mb")
	if status != 0 {
		return nil, false, status, gate.Raw{ContentType: "text/plain", Body: []byte(refusal)}, nil
	}
	return v, true, 0, nil, nil
}

// field is body?.[k]: undefined for a body that is not an object.
func field(v any, k string) any {
	if o, ok := v.(*jsvalue.Object); ok {
		return get(o, k)
	}
	return jsvalue.Undefined
}

func jsonType(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

// textAnswer is a string sent the way the dispatcher sent one: its ETag, 304 when the caller holds it.
func textAnswer(r *http.Request, status int, contentType string, text string, h http.Header) (int, any, http.Header) {
	b := []byte(text)
	tag := nodejson.ETag(b)
	h.Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		return http.StatusNotModified, nil, h
	}
	h.Set("Content-Length", strconv.Itoa(len(b)))
	return status, gate.Raw{ContentType: contentType, Body: b}, h
}

func htmlHeaders() http.Header {
	h := http.Header{}
	for _, kv := range HTMLHeaders() {
		h.Set(kv[0], kv[1])
	}
	return h
}

// redactor knows this runtime's exact secrets and home; built fresh per capture and per render.
func (rt *Route) redactor() *Redactor {
	var env []string
	if rt.Environ != nil {
		// The gate's own configuration is not the environment the previous runtime redacted with.
		for _, kv := range rt.Environ() {
			if !strings.HasPrefix(kv, "RUNTIME_") {
				env = append(env, kv)
			}
		}
	}
	known := append(EnvSecrets(env, ""), string(rt.Secret))
	if raw, err := os.ReadFile(rt.Store.DataDir + "/cli-token"); err == nil {
		known = append(known, jscompat.Trim(jscompat.DecodeUTF8(raw)))
	}
	return NewRedactor(RedactorConfig{KnownSecrets: known, HomeDirs: []string{rt.Home, rt.EnvHome}, WorkspaceDir: rt.WorkspaceDir,
		ProfileHomes: rt.ProfileHomes})
}

var tabIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Snapshots is POST {tabId, options} → 201 {draft}: a redacted snapshot draft of the conversation.
func (rt *Route) Snapshots(r *http.Request) (int, any, http.Header) {
	if status, v, h, refused := rt.APIRefusal(r); refused {
		return status, v, h
	}
	b, ok, status, v, h := body(r)
	if !ok {
		return status, v, h
	}
	if !rt.SessionOK(r) {
		return nodejson.Answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	h = noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return http.StatusMethodNotAllowed, nil, h
	}
	if !jsonType(r) {
		return nodejson.Answer(r, http.StatusUnsupportedMediaType, errorBody("Нужен application/json"), h)
	}
	tabID, isStr := field(b, "tabId").(string)
	if !isStr || !tabIDRe.MatchString(tabID) {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверный диалог"), h)
	}
	opts, ok := ParseOptions(field(b, "options"))
	if !ok {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверные настройки снимка"), h)
	}
	rt.Store.SweepDrafts()
	var sub any
	if rt.Claims != nil {
		if s, ok := rt.Claims(r)["sub"].(string); ok && s != "user" {
			sub = s
		}
	}
	draft, status, msg, err := rt.capture(tabID, opts, sub)
	if err != nil {
		return internalError(r.URL.Path, err, h)
	}
	if draft == nil {
		return nodejson.Answer(r, status, errorBody(msg), h)
	}
	return nodejson.Answer(r, http.StatusCreated, jsvalue.NewObject("draft", DraftView(draft)), h)
}

var providerByPanel = map[string]string{"claude-code": "claude", "codex-cli": "codex"}

// firstTruthy is a || b || …: the first truthy value, else the last.
func firstTruthy(vs ...any) any {
	for _, v := range vs {
		if truthy(v) {
			return v
		}
	}
	return vs[len(vs)-1]
}

var (
	titleWrapper = regexp.MustCompile(`</?(pasted_content|system-reminder|cc-memory|cross-session-message|command-[a-z-]+|local-command-[a-z-]+)[^>]*>`)
)

// CleanTitle is cleanConversationTitle: transport wrappers stripped, whitespace collapsed.
func CleanTitle(title string) string {
	text := jscompat.Trim(spaceRun.ReplaceAllString(titleWrapper.ReplaceAllString(title, " "), " "))
	if text == "" {
		return "Без названия"
	}
	return text
}

func (rt *Route) capture(tabID string, opts Options, sub any) (*jsvalue.Object, int, string, error) {
	var tab *jsvalue.Object
	if rt.FindTab != nil {
		tab, _ = rt.FindTab(tabID)
	}
	if tab == nil {
		return nil, 404, "Диалог не найден на этой машине", nil
	}
	panel := ""
	if p, ok := tab.Get("panelType"); ok && p != nil {
		panel = jscompat.String(p)
	}
	provider := providerByPanel[panel]
	if provider == "" {
		return nil, 409, "Снимок можно сделать только для диалога с агентом", nil
	}
	state := tab.Object("agentState")
	jsonl := nullish(get(state, "jsonlPath"))
	if jsonl == nil {
		jsonl = nullish(get(tab, "claudeJsonlPath"))
	}
	path, _ := jsonl.(string)
	if !truthy(jsonl) {
		return nil, 409, "У диалога ещё нет транскрипта", nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, 409, "Транскрипт диалога не найден", nil
	}
	if st.Size() > maxTranscript {
		return nil, 413, "Диалог слишком большой для снимка", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, "", err
	}
	content := jscompat.DecodeUTF8(raw)
	var timeline []*jsvalue.Object
	if provider == "claude" {
		timeline = transcript.ParseContent(content)
	} else {
		timeline = transcript.ParseCodexContent(content, transcript.CodexOptions{ImageURL: func(string) string { return "" }})
	}
	var headline any
	if provider == "claude" && rt.Headlines != nil {
		if t := rt.Headlines.Read(path).Title; t != nil {
			headline = *t
		}
	}
	title := CleanTitle(jscompat.String(firstTruthy(headline, get(state, "summary"), get(tab, "lastUserMessage"), get(tab, "title"), get(tab, "name"))))
	var econ *jsvalue.Object
	if opts.Economics {
		e, ok := TranscriptEconomics(content, provider)
		if !ok {
			return nil, 0, "", errNullLine
		}
		econ = e
	}
	snapshot, report, ok := Capture(CaptureInput{Timeline: timeline, Provider: provider, Title: title, OwnerSub: sub, Options: opts,
		Economics: econ, Redactor: rt.redactor(), Now: float64(rt.now().UnixMilli())})
	if !ok {
		return nil, 413, "Снимок больше 5 МБ: выключите диффы или действия агента", nil
	}
	d, err := rt.Store.SaveDraft(tabID, snapshot, report)
	if err != nil {
		return nil, 0, "", err
	}
	return d, 0, "", nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

const errNullLine = errorString("Cannot read properties of null (reading 'timestamp')")

func queryString(r *http.Request, k string) (string, bool) {
	vs := jscompat.Query(r.URL.RawQuery)[k]
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// fileName is the downloaded file's name: the title's words, the capture date (UTC), the format.
func (rt *Route) fileName(title string, capturedAt float64, format string) (string, bool) {
	date, ok := jscompat.ISOString(capturedAt)
	if !ok {
		return "", false
	}
	slug := strings.Trim(nonWord.ReplaceAllString(title, "-"), "-")
	slug = jscompat.Slice(slug, 60)
	if slug == "" {
		slug = "dialog"
	}
	return rt.FilePrefix + "-" + slug + "-" + date[:10] + "." + format, true
}

// encodeURIComponent escapes all but A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func encodeURIComponent(s string) string {
	return strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*", "%7E", "~").
		Replace(url.QueryEscape(s))
}

func (rt *Route) sessionOrEmpty403(r *http.Request) (int, any, http.Header, bool) {
	if status, v, h, refused := rt.APIRefusal(r); refused {
		return status, v, h, true
	}
	if _, ok, status, v, h := body(r); !ok {
		return status, v, h, true
	}
	if !rt.SessionOK(r) {
		return http.StatusForbidden, nil, nil, true
	}
	return 0, nil, nil, false
}

func (rt *Route) liveDraft(r *http.Request, id string) (*jsvalue.Object, int, any, http.Header, bool) {
	d, err := rt.Store.ReadDraft(id)
	if err != nil {
		status, v, h := internalError(r.URL.Path, err, noStore())
		return nil, status, v, h, false
	}
	if d == nil {
		status, v, h := nodejson.Answer(r, http.StatusNotFound, errorBody("Черновик снимка не найден или устарел"), noStore())
		return nil, status, v, h, false
	}
	return d, 0, nil, nil, true
}

// Download is GET ?format=html|md: the draft as a file. The HTML is the preview page without the banner.
func (rt *Route) Download(r *http.Request, id string) (int, any, http.Header) {
	if status, v, h, done := rt.sessionOrEmpty403(r); done {
		return status, v, h
	}
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return http.StatusMethodNotAllowed, nil, h
	}
	format, _ := queryString(r, "format")
	if format != "html" && format != "md" {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Формат: html или md"), h)
	}
	d, status, v, eh, ok := rt.liveDraft(r, id)
	if !ok {
		return status, v, eh
	}
	snap := d.Object("snapshot")
	if snap == nil {
		return internalError(r.URL.Path, errorString("draft without a snapshot"), h)
	}
	red := rt.redactor()
	name, ok := rt.fileName(snap.String("title"), num(snap, "capturedAt"), format)
	if !ok {
		return internalError(r.URL.Path, errorString("Invalid time value"), h)
	}
	h.Set("Content-Disposition", `attachment; filename="`+rt.FilePrefix+`-snapshot.`+format+`"; filename*=UTF-8''`+encodeURIComponent(name))
	if format == "md" {
		h.Set("X-Content-Type-Options", "nosniff")
		return textAnswer(r, http.StatusOK, "text/markdown; charset=utf-8", rt.Page.RenderMarkdown(snap, red), h)
	}
	for k, vs := range htmlHeaders() {
		h[k] = vs
	}
	return textAnswer(r, http.StatusOK, "text/html; charset=utf-8", rt.Page.RenderHTML(snap, red, RenderOptions{Mode: ModeExport}), h)
}

// Preview is GET: the draft exactly as a colleague will see it, under a banner.
func (rt *Route) Preview(r *http.Request, id string) (int, any, http.Header) {
	if status, v, h, done := rt.sessionOrEmpty403(r); done {
		return status, v, h
	}
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return http.StatusMethodNotAllowed, nil, h
	}
	d, status, v, eh, ok := rt.liveDraft(r, id)
	if !ok {
		return status, v, eh
	}
	snap := d.Object("snapshot")
	if snap == nil {
		return internalError(r.URL.Path, errorString("draft without a snapshot"), h)
	}
	html := rt.Page.RenderHTML(snap, rt.redactor(), RenderOptions{Mode: ModePreview})
	return textAnswer(r, http.StatusOK, "text/html; charset=utf-8", html, htmlHeaders())
}

const notConfigured = "Ссылки для коллег на этом сервере ещё не включены"

func (rt *Route) sessionOrJSON403(r *http.Request) (any, int, any, http.Header, bool) {
	if status, v, h, refused := rt.APIRefusal(r); refused {
		return nil, status, v, h, true
	}
	b, ok, status, v, h := body(r)
	if !ok {
		return nil, status, v, h, true
	}
	if !rt.SessionOK(r) {
		status, v, h := nodejson.Answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
		return nil, status, v, h, true
	}
	return b, 0, nil, nil, false
}

// Shares is GET ?tabId= → {enabled, shares} and POST {draftId, expiresInDays, replaceId?} → 201 {share}.
func (rt *Route) Shares(r *http.Request) (int, any, http.Header) {
	b, status, v, h, done := rt.sessionOrJSON403(r)
	if done {
		return status, v, h
	}
	h = noStore()
	routeBase := ParseShareBase(strings.Join(r.Header.Values(rt.ShareBaseHeader), ", "))
	if r.Method == http.MethodGet {
		var tab *string
		if vs, present := jscompat.Query(r.URL.RawQuery)["tabId"]; present {
			if len(vs) != 1 || !tabIDRe.MatchString(vs[0]) {
				return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверный диалог"), h)
			}
			tab = &vs[0]
		}
		list, err := rt.Store.List(tab)
		if err != nil {
			return internalError(r.URL.Path, err, h)
		}
		return nodejson.Answer(r, http.StatusOK, jsvalue.NewObject("enabled", routeBase != "", "shares", list), h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return http.StatusMethodNotAllowed, nil, h
	}
	if !jsonType(r) {
		return nodejson.Answer(r, http.StatusUnsupportedMediaType, errorBody("Нужен application/json"), h)
	}
	if routeBase == "" {
		return nodejson.Answer(r, http.StatusUnprocessableEntity, errorBody(notConfigured), h)
	}
	draftID, isStr := field(b, "draftId").(string)
	if !isStr || !ShareIDRe.MatchString(draftID) {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверный черновик"), h)
	}
	days := field(b, "expiresInDays")
	if !IsExpiryDays(days) {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Срок: 7, 30 или 90 дней"), h)
	}
	replaceID := ""
	switch rv := field(b, "replaceId").(type) {
	case nil:
	case string:
		if !ShareIDRe.MatchString(rv) {
			return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверная ссылка"), h)
		}
		replaceID = rv
	default:
		if rv != jsvalue.Undefined {
			return nodejson.Answer(r, http.StatusBadRequest, errorBody("Неверная ссылка"), h)
		}
	}
	draft, err := rt.Store.ReadDraft(draftID)
	if err != nil {
		return internalError(r.URL.Path, err, h)
	}
	if draft == nil {
		return nodejson.Answer(r, http.StatusNotFound, errorBody("Черновик устарел: соберите снимок заново"), h)
	}
	m, err := rt.Store.Publish(draft, routeBase, days.(float64), replaceID)
	if err != nil {
		return internalError(r.URL.Path, err, h)
	}
	if m.Share == nil {
		return nodejson.Answer(r, m.Status, errorBody(m.Error), h)
	}
	rt.Store.RemoveDraft(draft.String("id"))
	return nodejson.Answer(r, http.StatusCreated, jsvalue.NewObject("share", m.Share), h)
}

// Share is PATCH {expiresInDays} → {share} (a new date from now, never past 90 days after publication) and DELETE → 204
// (revoked: content removed at once, the record stays as a tombstone).
func (rt *Route) Share(r *http.Request, id string) (int, any, http.Header) {
	b, status, v, h, done := rt.sessionOrJSON403(r)
	if done {
		return status, v, h
	}
	h = noStore()
	if !ShareIDRe.MatchString(id) {
		return nodejson.Answer(r, http.StatusNotFound, errorBody(errLinkMissing), h)
	}
	if r.Method == http.MethodDelete {
		m, err := rt.Store.Revoke(id)
		if err != nil {
			return internalError(r.URL.Path, err, h)
		}
		if m.Share == nil {
			return nodejson.Answer(r, m.Status, errorBody(m.Error), h)
		}
		return http.StatusNoContent, nil, h
	}
	if r.Method != http.MethodPatch {
		h.Set("Allow", "PATCH, DELETE")
		return http.StatusMethodNotAllowed, nil, h
	}
	if !jsonType(r) {
		return nodejson.Answer(r, http.StatusUnsupportedMediaType, errorBody("Нужен application/json"), h)
	}
	days := field(b, "expiresInDays")
	if !IsExpiryDays(days) {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("Срок: 7, 30 или 90 дней"), h)
	}
	m, err := rt.Store.Extend(id, days.(float64))
	if err != nil {
		return internalError(r.URL.Path, err, h)
	}
	if m.Share == nil {
		return nodejson.Answer(r, m.Status, errorBody(m.Error), h)
	}
	return nodejson.Answer(r, http.StatusOK, jsvalue.NewObject("share", m.Share), h)
}

// ShareRead is GET: a published snapshot for the platform's share route only. No session is looked at, only the
// short-lived share-read token signed for exactly this id: 200 HTML (redacted again), 401 no or bad token, 403 a token
// for another id, 404 unknown, 410 revoked or expired.
func (rt *Route) ShareRead(r *http.Request, id string) (int, any, http.Header) {
	if _, ok, status, v, h := body(r); !ok {
		return status, v, h
	}
	h := http.Header{"Cache-Control": {"private, no-store"}}
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return http.StatusMethodNotAllowed, nil, h
	}
	token := strings.Join(r.Header.Values(rt.ShareTokenHeader), ", ")
	viewer, refusal := rt.Token.VerifyToken(token, id, rt.Secret, rt.now())
	if refusal != 0 {
		return refusal, nil, h
	}
	status, record, snap, err := rt.Store.ReadShared(id, viewer)
	if err != nil {
		return internalError(r.URL.Path, err, h)
	}
	if status != http.StatusOK {
		return status, nil, h
	}
	agent.Log("info", "[share] view "+logID(id))
	expires, _ := record.Number("expiresAt")
	html := rt.Page.RenderHTML(snap, rt.redactor(), RenderOptions{Mode: ModeShared, Viewer: viewer, ExpiresAt: expires})
	return textAnswer(r, http.StatusOK, "text/html; charset=utf-8", html, htmlHeaders())
}

// decodeFile is a file read as UTF-8 text, invalid bytes replaced.
func decodeFile(b []byte) string { return jscompat.DecodeUTF8(b) }
