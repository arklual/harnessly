package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// Read-only file routes (slice fsread): the editor's folder listing and file, both sides of a changed file, the
// viewer (and its check of which files can be opened), the «@»-context picker, a cited memory note and uploaded
// attachments. The editor's save is the fswrite slice's; the other methods' refusals are in routes_methods.go.

func init() {
	routeGroups = append(routeGroups, fileRoutes)
	bodyRouteGroups = append(bodyRouteGroups, fileBodyRoutes)
}

// sharedProjects is the shared project mount the viewer and the picker also open.
const sharedProjects = "/work"

var editorSession = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func getRoute(route string) func(*http.Request) bool {
	return func(r *http.Request) bool { return r.Method == http.MethodGet && files.RouteIs(r, route) }
}

// sessionDir is the folder a tab's shell is in: its process's working folder, else tmux's idea of it.
func sessionDir(t *tmux.Tmux, session string) string {
	if pid := t.PanePID(session); pid > 0 {
		if c, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil && c != "" {
			return c
		}
	}
	return t.PanePath(session)
}

// splitList is a comma-separated list without empty items.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// memoryParents: Claude Code's projects, then the folders of synced session copies (RUNTIME_SESSION_MEMORY_DIRS).
func memoryParents(home string) []string {
	return append([]string{filepath.Join(machine.ClaudeDir(home), "projects")}, splitList(os.Getenv("RUNTIME_SESSION_MEMORY_DIRS"))...)
}

func fileRoutes(d deps) []gate.JSONRoute {
	answer := files.Answer
	editor := func(r *http.Request, list bool) (int, any, http.Header) {
		noStore := func() http.Header { return http.Header{"Cache-Control": {"no-store"}} }
		fail := func(status int, text string) (int, any, http.Header) {
			return answer(r, status, files.Error(text), noStore())
		}
		if !d.SessionOK(r) {
			return fail(401, locale.Pick("Sign in to the workspace.", "Войдите в рабочее пространство."))
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
		var (
			v   files.Object
			err *files.EditorError
		)
		if list {
			relative := ""
			if values := q["path"]; len(values) > 1 {
				return fail(400, locale.Pick("Invalid path.", "Некорректный путь."))
			} else if len(values) == 1 {
				relative = values[0]
			}
			v, err = files.ListProject(root, relative)
		} else {
			relative, ok := q.String("path")
			if !ok || relative == "" {
				return fail(400, locale.Pick("No file is selected.", "Не выбран файл."))
			}
			v, err = files.ReadProjectFile(root, relative)
		}
		if err != nil {
			return fail(err.Status, err.Text)
		}
		return answer(r, 200, v, noStore())
	}
	return []gate.JSONRoute{
		{
			Slice: "fsread",
			Match: getRoute("/api/editor/files"),
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				return editor(r, true)
			},
		},
		{
			Slice: "fsread",
			Match: getRoute("/api/editor/file"),
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				return editor(r, false)
			},
		},
		{
			Slice: "fsread",
			Match: getRoute("/api/layout/file-content"),
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				q := files.RequestQuery(r)
				session, ok := q.String("session")
				if !ok || session == "" {
					return answer(r, 400, files.Error("session parameter required"), nil)
				}
				oldPath, hasOld := files.RepoPath(q["oldPath"])
				newPath, hasNew := files.RepoPath(q["newPath"])
				oldRef, okOld := files.Ref(q["oldRef"], "HEAD")
				newRef, okNew := files.Ref(q["newRef"], "WORKTREE")
				if !okOld || !okNew {
					return answer(r, 400, files.Error("Invalid ref"), nil)
				}
				if !hasOld && !hasNew {
					return answer(r, 400, files.Error("oldPath or newPath required"), nil)
				}
				if !d.Tmux.HasSession(session) {
					return answer(r, 404, files.Error("Session not found"), nil)
				}
				cwd := sessionDir(d.Tmux, session)
				if cwd == "" {
					return answer(r, 500, files.Error("Failed to get CWD"), nil)
				}
				oldSide := make(chan files.Side, 1)
				go func() { oldSide <- files.LoadSide(cwd, oldRef, oldPath, hasOld) }()
				newSide := files.LoadSide(cwd, newRef, newPath, hasNew)
				return answer(r, 200, files.FileContent(<-oldSide, newSide), nil)
			},
		},
		{
			Slice: "fsread",
			Match: getRoute(platformAPI + "/context-files"),
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if !d.SessionOK(r) {
					return answer(r, 403, files.Error("Forbidden"), nil)
				}
				noStore := http.Header{"Cache-Control": {"no-store"}}
				q := files.RequestQuery(r)
				cwd, _ := q.String("cwd")
				query, _ := q.String("q")
				if cwd == "" || !strings.HasPrefix(cwd, "/") || strings.Contains(cwd, "\x00") {
					return answer(r, 400, files.Error(locale.Pick("An absolute path is required", "Нужен абсолютный путь")), noStore)
				}
				root, err := files.Realpath(cwd)
				if err != nil {
					return answer(r, 404, files.Error(locale.Pick("Folder not found", "Папка не найдена")), noStore)
				}
				if !files.AllowedContextRoot(root, []string{d.WorkspaceRoot, sharedProjects}) {
					return answer(r, 403, files.Error(locale.Pick("The folder is outside the workspace", "Папка вне рабочего пространства")), noStore)
				}
				if st, err := os.Stat(root); err != nil || !st.IsDir() {
					return answer(r, 400, files.Error(locale.Pick("This is not a folder", "Это не папка")), noStore)
				}
				return answer(r, 200, files.Object{{K: "files", V: files.SearchContext(root, files.QueryUnits(query))}}, noStore)
			},
		},
		{
			Slice: "fsread",
			// Any method reads the note, as the old handler never looked at it.
			Match: func(r *http.Request) bool { return files.RouteIs(r, platformAPI+"/memory-note") },
			Compute: func(r *http.Request) (int, any, http.Header) {
				if status, v, h, refused := d.APIRefusal(r); refused {
					return status, v, h
				}
				if _, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb"); status != 0 {
					return status, gate.Raw{ContentType: "text/plain", Body: []byte(text)}, nil
				}
				if !d.SessionOK(r) {
					return http.StatusForbidden, nil, nil
				}
				noStore := http.Header{"Cache-Control": {"no-store"}}
				name, _ := files.RequestQuery(r).String("name")
				note, ok := files.ReadMemoryNote(name, memoryParents(d.Home))
				if !ok {
					return answer(r, 404, files.Error(locale.Pick("There is no such memory on this machine", "Такой памяти на этой машине нет")), noStore)
				}
				return answer(r, 200, note, noStore)
			},
		},
	}
}

// uploadsRoute: /api/uploads/<segments…>, all but the cleanup route.
func uploadsRoute(r *http.Request) ([]string, string, bool) {
	if r.Method != http.MethodGet {
		return nil, "", false
	}
	parts, apiPath, ok := files.APIParts(r)
	if !ok || len(parts) < 3 || parts[0] != "api" || parts[1] != "uploads" || len(parts) == 3 && parts[2] == "cleanup" {
		return nil, "", false
	}
	return parts[2:], apiPath, true
}

func fileBodyRoutes(d deps) []gate.BodyRoute {
	access := files.NewAccess([]string{d.WorkspaceRoot, sharedProjects, files.NodeTempDir(), "/tmp"}, splitList(os.Getenv("RUNTIME_SECRET_CONFIG_DIRS")))
	uploads := os.Getenv("RUNTIME_UPLOADS_DIR")
	if uploads == "" {
		uploads = filepath.Join(d.DataDir, "uploads")
	}
	refused := func(w http.ResponseWriter, r *http.Request) bool {
		if _, _, _, no := d.APIRefusal(r); no {
			files.WriteUnauthorized(w)
			return true
		}
		return false
	}
	return []gate.BodyRoute{
		{
			Slice: "fsread",
			// POST says which files can be opened or saved; any other method serves one, as the old handler did.
			Match: func(r *http.Request) bool { return files.RouteIs(r, platformAPI+"/file-preview") },
			Serve: func(w http.ResponseWriter, r *http.Request) {
				if refused(w, r) {
					return
				}
				body, _, status, text := nodejson.ParseBody(r, 1<<20, "1mb")
				if status != 0 {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(text))
					return
				}
				if !d.SessionOK(r) {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if r.Method == http.MethodPost {
					w.Header().Set("Cache-Control", "no-store")
					files.WriteJSON(w, r, http.StatusOK, previewChecks(access, body))
					return
				}
				access.ServePreview(w, r, files.RequestQuery(r))
			},
		},
		{
			Slice: "fsread",
			Match: func(r *http.Request) bool { _, _, ok := uploadsRoute(r); return ok },
			Serve: func(w http.ResponseWriter, r *http.Request) {
				segments, apiPath, _ := uploadsRoute(r)
				if !files.OpenWithoutSession(apiPath) && refused(w, r) {
					return
				}
				files.ServeUpload(w, r, uploads, segments)
			},
		},
	}
}

// previewChecks answers POST file-preview {paths[, details]}: which of the paths can be opened, or with details each
// one's size and whether it can be opened and saved.
func previewChecks(access *files.Access, body any) files.Object {
	o, _ := body.(*jsvalue.Object)
	var paths []string
	for _, p := range o.Array("paths") {
		if s, ok := p.(string); ok && len(paths) < 200 {
			paths = append(paths, s)
		}
	}
	if details, _ := o.Get("details"); details == true {
		list := make([]any, 0, len(paths))
		for _, p := range paths {
			dl, pv := access.Download(p), access.Preview(p)
			if dl.OK {
				list = append(list, files.Object{{K: "path", V: p}, {K: "size", V: dl.Size}, {K: "preview", V: pv.OK}, {K: "download", V: true}})
			} else {
				list = append(list, files.Object{{K: "path", V: p}, {K: "preview", V: false}, {K: "download", V: false}, {K: "error", V: dl.Text}})
			}
		}
		return files.Object{{K: "files", V: list}}
	}
	available := []any{}
	for _, p := range paths {
		if access.Preview(p).OK {
			available = append(available, p)
		}
	}
	return files.Object{{K: "available", V: available}}
}
