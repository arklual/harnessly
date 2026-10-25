package services

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Handlers answer the routes as the previous runtime's dispatcher and handlers did, after the caller passed the API's
// own check (a page session or the cli-token): the body read first (413, "Invalid JSON"), then each handler's session
// check, statuses, headers, JSON with its ETag and 304, and the plain 500 of a handler that threw.
type Handlers struct {
	S *Service
	// Session: the request carries a page session (a cli-token alone is not a person).
	Session func(*http.Request) bool
	// Now is the clock of the organizer's timestamps; nil is time.Now.
	Now func() time.Time
}

const jsonType = "application/json; charset=utf-8"

func writeEmpty(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

func writeText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

// failed is the dispatcher's answer to a handler that threw; headers the handler had set stay.
func failed(w http.ResponseWriter) { writeText(w, 500, "Internal Server Error") }

// writeBody is res.send of bytes or res.json's string: an ETag over them, 304 to a caller that holds them.
func writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	h := w.Header()
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	etag := nodejson.ETag(body)
	h.Set("ETag", etag)
	if nodejson.Fresh(r.Header, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	writeBody(w, r, status, jsonType, jsvalue.Stringify(v, ""))
}

func errorBody(text string) *jsvalue.Object { return obj("error", text) }

const bodyLimit = 1 << 20

// readBody is the dispatcher's body parser: JSON (empty is {}), a form as an object, anything else as text; ok is
// false after it answered a body it cannot read.
func readBody(w http.ResponseWriter, r *http.Request) (any, bool) {
	if r.ContentLength > bodyLimit {
		writeText(w, 413, "Body exceeded 1mb limit")
		return nil, false
	}
	var raw []byte
	if r.Body != nil {
		var err error
		if raw, err = io.ReadAll(io.LimitReader(r.Body, bodyLimit+1)); err != nil {
			writeText(w, 400, "Invalid body")
			return nil, false
		}
	}
	if len(raw) > bodyLimit {
		writeText(w, 413, "Body exceeded 1mb limit")
		return nil, false
	}
	text := files.DecodeUTF8(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, _ := strings.Cut(ct, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if text == "" {
			return obj(), true
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			writeText(w, 400, "Invalid JSON")
			return nil, false
		}
		return v, true
	case "application/x-www-form-urlencoded":
		o := obj()
		q := files.ParseQuery(text)
		for _, pair := range strings.Split(text, "&") {
			k, _, _ := strings.Cut(pair, "=")
			k = formDecode(k)
			vs, ok := q[k]
			if !ok || o.Has(k) {
				continue
			}
			if len(vs) == 1 {
				o.Set(k, vs[0])
			} else {
				o.Set(k, jsvalue.StringArray(vs))
			}
		}
		return o, true
	}
	return text, true
}

// queryParam is req.query[name]: absent, one value, or several.
func queryParam(r *http.Request, name string) jscompat.Param {
	return jscompat.Param{Values: files.RequestQuery(r)[name]}
}

// queryString is req.query[name] when it is one string.
func queryString(r *http.Request, name string) (string, bool) {
	p := queryParam(r, name)
	if len(p.Values) == 1 {
		return p.Values[0], true
	}
	return "", false
}

func stringsOf(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// GitLab: POST {paths, jsonlPath, mrs}, a session's pushes and merge requests with live pipeline status.
func (h *Handlers) GitLab(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	if !h.Session(r) {
		writeEmpty(w, 403)
		return
	}
	if r.Method != http.MethodPost {
		writeEmpty(w, 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var since *float64
	if t, ok := h.S.sessionStart(opt(body, "jsonlPath")); ok {
		since = &t
	}
	out, err := h.S.SessionGitLab(r.Context(), stringsOf(opt(body, "paths")), since, stringsOf(opt(body, "mrs")))
	if err != nil {
		failed(w)
		return
	}
	writeJSON(w, r, 200, out)
}

// GitLabView: GET ?url=, a merge request or a commit with its diff.
func (h *Handlers) GitLabView(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeJSON(w, r, 403, errorBody("Forbidden"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeEmpty(w, 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	url, _ := queryString(r, "url")
	if !h.S.IsGitLabRef(url) {
		writeJSON(w, r, 400, errorBody(locale.Pick("A link to a GitLab merge request or commit is required", "Нужна ссылка на merge request или коммит GitLab")))
		return
	}
	view, err := h.S.GitLabView(r.Context(), url)
	if err != nil {
		failed(w)
		return
	}
	if view == nil {
		writeJSON(w, r, 404, errorBody(locale.Pick("GitLab did not return this object", "GitLab не отдал этот объект")))
		return
	}
	writeJSON(w, r, 200, view)
}

// ChangeView: GET ?url=, a merge request, pull request, commit or issue of GitLab or GitHub with its checks, discussion
// and diff. A GitHub link that cannot be read answers a card with the reason (200), so the tab is never empty.
func (h *Handlers) ChangeView(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeJSON(w, r, 403, errorBody("Forbidden"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeEmpty(w, 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	url, _ := queryString(r, "url")
	if !h.S.IsChangeRef(url) {
		writeJSON(w, r, 400, errorBody(locale.Pick("A link to a merge request, pull request, commit or issue is required", "Нужна ссылка на merge request, pull request, коммит или задачу")))
		return
	}
	view, err := h.S.ChangeView(r.Context(), url)
	if err != nil {
		failed(w)
		return
	}
	if view == nil {
		writeJSON(w, r, 404, errorBody(locale.Pick("GitLab did not return this object", "GitLab не отдал этот объект")))
		return
	}
	writeJSON(w, r, 200, view)
}

// LinkMeta: GET ?url= (a tracker issue, a merge request, a published document) or ?key= (a bare task key).
func (h *Handlers) LinkMeta(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeJSON(w, r, 403, errorBody("Forbidden"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeEmpty(w, 405)
		return
	}
	var target *linkTarget
	if u, ok := queryString(r, "url"); ok {
		target = h.S.parseLinkTarget(u)
	} else if k, ok := queryString(r, "key"); ok {
		var err error
		if target, err = h.S.issueTargetFromKey(r.Context(), k); err != nil {
			failed(w)
			return
		}
	}
	if target == nil {
		writeJSON(w, r, 400, errorBody("Unsupported url"))
		return
	}
	meta := h.S.linkMeta(r.Context(), target)
	w.Header().Set("Cache-Control", "private, max-age=60")
	if meta == nil {
		writeJSON(w, r, 404, errorBody("Not found"))
		return
	}
	writeJSON(w, r, 200, meta)
}

// Projects: the tracker's project keys, any method.
func (h *Handlers) Projects(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeEmpty(w, 403)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=600")
	keys, err := h.S.ProjectKeys(r.Context())
	if err != nil {
		failed(w)
		return
	}
	writeJSON(w, r, 200, obj("keys", keys))
}

func writeImage(w http.ResponseWriter, r *http.Request, img *Image) {
	w.Header().Set("Content-Type", img.Type)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	writeBody(w, r, 200, "", img.Body)
}

// Avatar: ?login=, a tracker user's avatar, any method.
func (h *Handlers) Avatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeEmpty(w, 403)
		return
	}
	login, _ := queryString(r, "login")
	if !LoginRe.MatchString(login) {
		writeEmpty(w, 400)
		return
	}
	img, err := h.S.TrackerAvatar(r.Context(), login)
	if err != nil {
		failed(w)
		return
	}
	if img == nil {
		w.Header().Set("Cache-Control", "private, max-age=300")
		writeEmpty(w, 404)
		return
	}
	writeImage(w, r, img)
}

// Favicon: ?host=, a public site's icon, any method.
func (h *Handlers) Favicon(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeEmpty(w, 403)
		return
	}
	host := jsLower(queryParam(r, "host").String())
	if !ValidIconHost(host) {
		writeEmpty(w, 400)
		return
	}
	img := h.S.SiteIcon(r.Context(), host)
	if img == nil {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeEmpty(w, 404)
		return
	}
	writeImage(w, r, img)
}

var taskStatus = map[string]int{TaskNotFound: 404, TaskNoToken: 503, TaskTracker: 502}

// Task: GET ?key=, a tracker task for the card.
func (h *Handlers) Task(w http.ResponseWriter, r *http.Request) {
	if _, ok := readBody(w, r); !ok {
		return
	}
	if !h.Session(r) {
		writeJSON(w, r, 403, errorBody("Forbidden"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeEmpty(w, 405)
		return
	}
	key, ok := queryString(r, "key")
	if !ok || !h.S.IsTaskKey(key) {
		writeJSON(w, r, 400, errorBody("Unsupported key"))
		return
	}
	task, fail := h.S.Task(r.Context(), key)
	w.Header().Set("Cache-Control", "private, no-store")
	if task == nil {
		writeJSON(w, r, taskStatus[fail], errorBody(fail))
		return
	}
	writeJSON(w, r, 200, task)
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Organizer: GET reads the person's projects and pins, PATCH {ops} changes them.
func (h *Handlers) Organizer(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	if !h.Session(r) {
		writeJSON(w, r, 403, errorBody("Forbidden"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	unreadable := func(err error) {
		if err == ErrOrganizerUnreadable {
			writeJSON(w, r, 500, errorBody("organizer-unreadable"))
			return
		}
		failed(w)
	}
	switch r.Method {
	case http.MethodGet:
		o, err := h.S.Organizer()
		if err != nil {
			unreadable(err)
			return
		}
		writeJSON(w, r, 200, obj("organizer", o))
		return
	case http.MethodPatch:
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			writeJSON(w, r, 415, errorBody("json-only"))
			return
		}
		ops, refusal := ParseOrganizerOps(body)
		if refusal != nil {
			writeJSON(w, r, refusal.Status, refusal.Body)
			return
		}
		o, refusal, err := h.S.ApplyOrganizer(ops, float64(h.now().UnixMilli()))
		if err != nil {
			unreadable(err)
			return
		}
		if refusal != nil {
			writeJSON(w, r, refusal.Status, refusal.Body)
			return
		}
		writeJSON(w, r, 200, obj("organizer", o))
		return
	}
	w.Header().Set("Allow", "GET, PATCH")
	writeJSON(w, r, 405, errorBody("Method not allowed"))
}
