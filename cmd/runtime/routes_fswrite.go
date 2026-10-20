package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/gitsync"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Disk writes without agent state (slice fswrite): attachments uploaded into a conversation and their clean-up, an
// editor save, a sync of a tab's repository with its upstream, and stopping a heavy process from the load panel.
// Only the methods that write are answered here (POST; PUT for the editor), never shadowed.

func init() { routeGroups = append(routeGroups, fswriteRoutes) }

// uploadsDir is where attachments are kept: RUNTIME_UPLOADS_DIR, else <data>/uploads.
func uploadsDir(d deps) string {
	return env("RUNTIME_UPLOADS_DIR", filepath.Join(d.DataDir, "uploads"))
}

// uploadHeader is an upload's header under the first of the deployment's prefixes it was sent with (the current
// prefix, then the old one); present is false when it was sent under none. Repeated, its values are joined.
func uploadHeader(r *http.Request, prefixes []string, name string) (string, bool) {
	for _, p := range prefixes {
		if vs, ok := r.Header[http.CanonicalHeaderKey(p+name)]; ok {
			return strings.Join(vs, ", "), true
		}
	}
	return "", false
}

func jsonAnswer(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	return nodejson.Answer(r, status, v, h)
}

func errorAnswer(r *http.Request, status int, text string, h http.Header) (int, any, http.Header) {
	return nodejson.Answer(r, status, jsvalue.NewObject("error", text), h)
}

func textRefusal(status int, text string) (int, any, http.Header) {
	return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
}

// readUpload is the whole body, or the status and text the upload is refused with.
func readUpload(r *http.Request, limit int64, tooLarge string) ([]byte, int, string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, http.StatusBadRequest, err.Error()
	}
	if int64(len(data)) > limit {
		return nil, http.StatusRequestEntityTooLarge, tooLarge
	}
	if len(data) == 0 {
		return nil, http.StatusBadRequest, "Empty body"
	}
	return data, 0, ""
}

// fetchSite is the browser's Sec-Fetch-Site, repeated values joined; present is false without one.
func fetchSite(r *http.Request) (site string, present bool) {
	vs, ok := r.Header["Sec-Fetch-Site"]
	return strings.Join(vs, ", "), ok
}

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// contentHasLoneSurrogate says whether the "content" member of a JSON object (the last one, as JSON.parse keeps it)
// escapes a lone UTF-16 surrogate, which JSON.parse kept and encoding/json replaces.
func contentHasLoneSurrogate(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return false
	}
	var content json.RawMessage
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return false
		}
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return false
		}
		if k == "content" {
			content = v
		}
	}
	return loneSurrogate(content)
}

func loneSurrogate(raw []byte) bool {
	unit := func(i int) (int, bool) {
		if i+6 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
			return 0, false
		}
		n := 0
		for _, c := range raw[i+2 : i+6] {
			switch {
			case c >= '0' && c <= '9':
				n = n<<4 | int(c-'0')
			case c >= 'a' && c <= 'f':
				n = n<<4 | int(c-'a'+10)
			case c >= 'A' && c <= 'F':
				n = n<<4 | int(c-'A'+10)
			default:
				return 0, false
			}
		}
		return n, true
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		u, ok := unit(i)
		if !ok {
			i++ // another escape: its second byte is not a quote or backslash to look at
			continue
		}
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if low, ok := unit(i + 6); ok && low >= 0xDC00 && low <= 0xDFFF {
				i += 11
				continue
			}
			return true
		case u >= 0xDC00 && u <= 0xDFFF:
			return true
		}
		i += 5
	}
	return false
}

// newUploads is the uploads folder the routes write; tests fix its clock and names.
var newUploads = func(d deps) *files.Uploads { return &files.Uploads{Dir: uploadsDir(d)} }

func fswriteRoutes(d deps) []gate.JSONRoute {
	store := newUploads(d)
	prefixes := splitList(env("RUNTIME_UPLOAD_HEADER_PREFIXES", "x-harnessly-"))
	post := func(path string) func(*http.Request) bool {
		return func(r *http.Request) bool { return r.Method == http.MethodPost && files.RouteIs(r, path) }
	}
	upload := func(image bool) func(*http.Request) (int, any, http.Header) {
		return func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			mime := ""
			limit, tooLarge, failed := int64(files.FileMaxBytes), "File exceeds 50MB", "Failed to save file"
			if image {
				mime, _, _ = strings.Cut(r.Header.Get("Content-Type"), ";")
				mime = strings.TrimSpace(mime)
				if !files.ImageType(mime) {
					return errorAnswer(r, http.StatusBadRequest, "Unsupported image type", nil)
				}
				limit, tooLarge, failed = files.ImageMaxBytes, "Image exceeds 10MB", "Failed to save image"
			}
			data, status, text := readUpload(r, limit, tooLarge)
			if status != 0 {
				var h http.Header
				if status == http.StatusRequestEntityTooLarge {
					h = http.Header{"Connection": {"close"}} // the rest of the body is not read
				}
				return errorAnswer(r, status, text, h)
			}
			wsID, _ := uploadHeader(r, prefixes, "ws-id")
			tabID, _ := uploadHeader(r, prefixes, "tab-id")
			var original []rune
			if raw, _ := uploadHeader(r, prefixes, "filename"); raw != "" {
				decoded, ok := files.DecodeName(raw)
				if !ok {
					return errorAnswer(r, http.StatusInternalServerError, failed, nil)
				}
				original = decoded
			}
			var name string
			if image {
				name = store.ImageName(original, mime)
			} else {
				name = store.FileName(original)
			}
			full, err := store.Save(data, wsID, tabID, name)
			if err != nil {
				return errorAnswer(r, http.StatusInternalServerError, failed, nil)
			}
			return jsonAnswer(r, 200, jsvalue.NewObject("path", full, "filename", name), nil)
		}
	}
	return []gate.JSONRoute{
		{Slice: "fswrite", Match: post("/api/upload-file"), Compute: upload(false)},
		{Slice: "fswrite", Match: post("/api/upload-image"), Compute: upload(true)},
		{
			Slice: "fswrite",
			Match: func(r *http.Request) bool { return post("/api/uploads/cleanup")(r) && nodejson.BodyUTF8(r) },
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				body, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb")
				if status != 0 {
					return textRefusal(status, text)
				}
				all := false
				if o, ok := body.(*jsvalue.Object); ok {
					mode, _ := o.Get("mode")
					all = mode == "all"
				}
				res := store.Clean(all)
				mode := "expired"
				if all {
					mode = "all"
				}
				return jsonAnswer(r, 200, jsvalue.NewObject("mode", mode, "deleted", res.Deleted, "freedBytes", res.FreedBytes), nil)
			},
		},
		{
			Slice: "fswrite",
			Match: func(r *http.Request) bool {
				return r.Method == http.MethodPut && files.RouteIs(r, "/api/editor/file") && nodejson.BodyUTF8(r)
			},
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				body, raw, status, text := nodejson.ParseBody(r, 6<<20, "6mb")
				if status != 0 {
					return textRefusal(status, text)
				}
				noStore := func() http.Header { return http.Header{"Cache-Control": {"no-store"}} }
				fail := func(status int, text string) (int, any, http.Header) {
					return files.Answer(r, status, files.Error(text), noStore())
				}
				if !d.SessionOK(r) {
					return fail(401, locale.Pick("Sign in to the workspace.", "Войдите в рабочее пространство."))
				}
				if site, _ := fetchSite(r); site == "cross-site" {
					return fail(403, locale.Pick("Saving is only available from the workspace.", "Сохранение доступно только из рабочего пространства."))
				}
				q := files.RequestQuery(r)
				session, ok := q.String("session")
				if !ok || !editorSession.MatchString(session) {
					return fail(400, locale.Pick("No active session is selected.", "Не выбрана активная сессия."))
				}
				if !d.Tmux.HasSession(session) {
					return fail(404, locale.Pick("The session no longer exists.", "Сессия больше не существует."))
				}
				root := sessionDir(d.Tmux, session)
				if root == "" {
					return fail(404, locale.Pick("Could not determine the project folder.", "Не удалось определить папку проекта."))
				}
				relative, ok := q.String("path")
				if !ok || relative == "" {
					return fail(400, locale.Pick("No file is selected.", "Не выбран файл."))
				}
				if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					return fail(415, locale.Pick("The file text in JSON is expected.", "Ожидается текст файла в JSON."))
				}
				var content, revision any
				if o, isObject := body.(*jsvalue.Object); isObject {
					content, _ = o.Get("content")
					revision, _ = o.Get("revision")
				}
				c, okContent := content.(string)
				rev, okRevision := revision.(string)
				if !okContent || !okRevision || !revisionPattern.MatchString(rev) {
					return fail(400, locale.Pick("The file's content and version were not sent.", "Не переданы содержимое и версия файла."))
				}
				saved, ee := files.WriteProjectFile(root, relative, c, rev, contentHasLoneSurrogate(raw))
				if ee != nil {
					return fail(ee.Status, ee.Text)
				}
				return files.Answer(r, 200, saved, noStore())
			},
		},
		{
			Slice: "fswrite",
			Match: func(r *http.Request) bool {
				return r.Method == http.MethodPost && files.RouteIs(r, "/api/git/sync") && nodejson.BodyUTF8(r)
			},
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if _, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb"); status != 0 {
					return textRefusal(status, text)
				}
				session := files.RequestQuery(r)["session"]
				if len(session) == 0 || len(session) == 1 && session[0] == "" {
					return errorAnswer(r, 400, "session parameter required", nil)
				}
				if len(session) > 1 {
					return http.StatusInternalServerError, handlerFailed, nil // tmux asked about an array threw
				}
				if !d.Tmux.HasSession(session[0]) {
					return errorAnswer(r, 404, "Session not found", nil)
				}
				cwd := sessionDir(d.Tmux, session[0])
				if cwd == "" {
					return errorAnswer(r, 500, "Failed to get CWD", nil)
				}
				status, v := gitsync.Sync(cwd)
				return jsonAnswer(r, status, v, nil)
			},
		},
	}
}

// stopRoute: POST <platform API>/machine/stop {pid, starttime}, from the load panel.
func stopRoute(d deps, stopper *machines.Stopper) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: "fswrite",
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && files.RouteIs(r, platformAPI+"/machine/stop") && nodejson.BodyUTF8(r)
		},
		Compute: func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			body, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb")
			if status != 0 {
				return textRefusal(status, text)
			}
			if !d.SessionOK(r) {
				return errorAnswer(r, 403, "unauthorized", nil)
			}
			// SameSite=Lax still sends the cookie from a sibling subdomain: only the platform's own page may kill.
			if site, present := fetchSite(r); present && site != "same-origin" && site != "none" {
				return errorAnswer(r, 403, "cross-site", nil)
			}
			noStore := http.Header{"Cache-Control": {"no-store"}}
			kind, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
			req, ok := machines.StopRequest{}, false
			if strings.ToLower(strings.TrimSpace(kind)) == "application/json" {
				req, ok = machines.ParseStopRequest(body)
			}
			if !ok {
				return errorAnswer(r, 400, "bad-request", noStore)
			}
			res, err := stopper.Stop(req)
			if errors.Is(err, machines.ErrScanTimeout) {
				return errorAnswer(r, 503, "scan-timeout", noStore)
			}
			if err != nil {
				return errorAnswer(r, 500, "stop failed", noStore)
			}
			return jsonAnswer(r, res.Status, res.Body, noStore)
		},
	}
}

// newStopper stops what the load monitor lists, with the same runtime processes and release root.
func newStopper(load *machines.LoadMonitor, runtimePIDs, panePIDs func() []int) *machines.Stopper {
	listed := func() ([]machines.LoadProcess, error) {
		l, err := load.Load()
		if err != nil {
			return nil, err
		}
		return l.Top, nil
	}
	return &machines.Stopper{Listed: listed, UID: os.Getuid(), RuntimePIDs: runtimePIDs, PanePIDs: panePIDs,
		ReleaseRoot: os.Getenv("RUNTIME_RELEASE_ROOT"),
		Kill:        func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		Sleep:       time.Sleep}
}
