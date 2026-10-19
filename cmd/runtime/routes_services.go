package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"harnessly/internal/agent"
	"harnessly/internal/files"
	"harnessly/internal/gate"
	"harnessly/internal/jsvalue"
	"harnessly/internal/preflight"
	"harnessly/internal/services"
)

// Routes that call external services with the person's tokens (slice "services"; their GETs also under
// "services-shadow"): a session's GitLab pushes and merge requests, a merge request or commit with its diff, link
// previews, tracker avatars, project keys and task cards, site icons, and the person's organizer file. A route whose
// service the deployment does not name stays with the old runtime.

func init() {
	routeGroups = append(routeGroups, serviceRoutes)
	bodyRouteGroups = append(bodyRouteGroups, serviceBodyRoutes)
}

var (
	servicesMu  sync.Mutex
	servicesFor = map[*gate.OldRuntime]*services.Handlers{}
)

// serviceConfig is what the deployment names; empty values leave their routes to the old runtime.
func serviceConfig(d deps) services.Config {
	cfg := services.Config{
		Home:          d.Home,
		SecretDirs:    splitList(os.Getenv("RUNTIME_SECRET_CONFIG_DIRS")),
		GitLabHosts:   splitList(os.Getenv("RUNTIME_GITLAB_HOSTS")),
		TrackerURL:    os.Getenv("RUNTIME_TRACKER_URL"),
		TrackerHosts:  splitList(os.Getenv("RUNTIME_TRACKER_HOSTS")),
		DocsHost:      os.Getenv("RUNTIME_DOCS_HOST"),
		ReaderHost:    os.Getenv("RUNTIME_PLATFORM_HOST"),
		ReaderPath:    env("RUNTIME_DOCS_READER_PATH", "/agents/docs/"),
		AvatarPath:    platformAPI + "/link-meta/avatar",
		IconAgent:     os.Getenv("RUNTIME_ICON_USER_AGENT"),
		OrganizerFile: os.Getenv("RUNTIME_ORGANIZER_FILE"),
	}
	if re := os.Getenv("RUNTIME_TASK_KEY_RE"); re != "" {
		if compiled, err := regexp.Compile(re); err == nil {
			cfg.TaskKey = compiled
		} else {
			agent.Log("warn", "RUNTIME_TASK_KEY_RE does not compile", agent.F{K: "error", V: err.Error()})
		}
	}
	if cfg.OrganizerFile != "" && !filepath.IsAbs(cfg.OrganizerFile) {
		cfg.OrganizerFile = filepath.Join(d.DataDir, cfg.OrganizerFile)
	}
	return cfg
}

// handlersFor is one set of handlers per runtime, so the JSON and the body routes share their caches.
func handlersFor(d deps) (*services.Handlers, services.Config) {
	servicesMu.Lock()
	defer servicesMu.Unlock()
	cfg := serviceConfig(d)
	if h := servicesFor[d.Old]; h != nil {
		return h, cfg
	}
	h := &services.Handlers{S: services.New(cfg), Session: d.SessionOK}
	servicesFor[d.Old] = h
	return h, cfg
}

// viaMemory runs a handler into memory: the JSON route's answer, its headers and its body as written.
func viaMemory(d deps, handler http.HandlerFunc) func(*http.Request) (int, any, http.Header) {
	return func(r *http.Request) (int, any, http.Header) {
		if status, v, h, refused := d.APIRefusal(r); refused {
			return status, v, h
		}
		rec := &memoryWriter{h: http.Header{}}
		handler(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		h := rec.h.Clone()
		if len(rec.body) == 0 {
			return rec.status, nil, h
		}
		return rec.status, gate.Raw{ContentType: rec.h.Get("Content-Type"), Body: rec.body}, h
	}
}

type memoryWriter struct {
	h      http.Header
	status int
	body   []byte
}

func (m *memoryWriter) Header() http.Header { return m.h }

func (m *memoryWriter) WriteHeader(code int) {
	if m.status == 0 {
		m.status = code
	}
}

func (m *memoryWriter) Write(b []byte) (int, error) {
	if m.status == 0 {
		m.status = http.StatusOK
	}
	m.body = append(m.body, b...)
	return len(b), nil
}

func routeAt(route string) func(*http.Request) bool {
	return func(r *http.Request) bool { return files.RouteIs(r, route) }
}

// bootstrapFor is the page's start data from this machine's files.
func bootstrapFor(d deps) *services.Bootstrap {
	tools := d.Tools
	if tools == nil {
		tools = preflight.New(d.Home)
	}
	return &services.Bootstrap{
		DataDir:    d.DataDir,
		ConfigFile: d.ConfigFile,
		HostLocked: strings.TrimSpace(env("RUNTIME_ACCESS_HOST", os.Getenv("HOST"))) != "",
		BindLocal:  bindsLoopback(stateSettings(d)),
		Owner: func(r *http.Request) any {
			if d.Claims == nil {
				return nil
			}
			sub, ok := d.Claims(r)["sub"]
			if !ok {
				return nil
			}
			raw, _ := json.Marshal(sub)
			v, err := jsvalue.Parse(raw)
			if err != nil {
				return nil
			}
			return v
		},
		CreateFirst: d.FirstWorkspace,
		Tools: func() bool {
			r := tools.Runtime()
			return r.Tmux.Installed && r.Tmux.Compatible && r.Git.Installed
		},
	}
}

func serviceRoutes(d deps) []gate.JSONRoute {
	h, cfg := handlersFor(d)
	boot := bootstrapFor(d)
	out := []gate.JSONRoute{{
		Slice: "services",
		// An empty machine gets its first workspace from the layout store: while the core does not run here, that page
		// load stays where the store is.
		Match: func(r *http.Request) bool {
			return files.RouteIs(r, platformAPI+"/bootstrap") && (r.Method != http.MethodGet || boot.HasWorkspaces() ||
				d.CoreRunning != nil && d.CoreRunning())
		},
		Compute: viaMemory(d, boot.Serve),
	}}
	if len(cfg.GitLabHosts) > 0 {
		out = append(out,
			gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/gitlab"), Compute: viaMemory(d, h.GitLab)},
			gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/gitlab-view"), Compute: viaMemory(d, h.GitLabView), Shape: true},
		)
	}
	if cfg.TrackerURL != "" && len(cfg.GitLabHosts) > 0 && cfg.DocsHost != "" {
		out = append(out,
			gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/link-meta"), Compute: viaMemory(d, h.LinkMeta), Shape: true},
			gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/link-meta/projects"), Compute: viaMemory(d, h.Projects)},
		)
	}
	if cfg.TrackerURL != "" && cfg.TaskKey != nil {
		out = append(out, gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/task"), Compute: viaMemory(d, h.Task), Shape: true})
	}
	if cfg.OrganizerFile != "" {
		out = append(out, gate.JSONRoute{Slice: "services", Match: routeAt(platformAPI + "/organizer"), Compute: viaMemory(d, h.Organizer)})
	}
	return out
}

func serviceBodyRoutes(d deps) []gate.BodyRoute {
	h, cfg := handlersFor(d)
	refusing := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if _, _, _, refused := d.APIRefusal(r); refused {
				files.WriteUnauthorized(w)
				return
			}
			next(w, r)
		}
	}
	out := []gate.BodyRoute{{Slice: "services", Match: routeAt(platformAPI + "/favicon"), Serve: refusing(h.Favicon)}}
	if cfg.TrackerURL != "" {
		out = append(out, gate.BodyRoute{Slice: "services", Match: routeAt(platformAPI + "/link-meta/avatar"), Serve: refusing(h.Avatar)})
	}
	return out
}
