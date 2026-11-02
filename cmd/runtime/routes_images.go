package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/imagegen"
)

// Pictures drawn by Codex (internal/imagegen): the jobs an agent's generate_images, a Sinopia design or the page
// start, and `<platform>/images` that lists, reads and starts them. The pictures themselves are files the viewer
// shows (`<platform>/file-preview?path=`): the jobs' folder and Codex's own are among its roots.

func init() {
	bodyRouteGroups = append(bodyRouteGroups, imageRoutes)
}

var (
	imageJobsOnce sync.Once
	imageJobsSvc  *imagegen.Service
)

// imagesDir is where the jobs' pictures are kept.
func imagesDir(d deps) string { return filepath.Join(d.DataDir, "images") }

// codexImagesDir is where Codex saves the pictures it draws, in any chat.
func codexImagesDir(d deps) string {
	return filepath.Join(env("CODEX_HOME", filepath.Join(d.Home, ".codex")), "generated_images")
}

// imageJobs is the machine's one picture service.
func imageJobs(d deps) *imagegen.Service {
	imageJobsOnce.Do(func() {
		envDir := agentEnvDir(d.DataDir)
		imageJobsSvc = &imagegen.Service{
			Dir:       imagesDir(d),
			CodexHome: env("CODEX_HOME", filepath.Join(d.Home, ".codex")),
			Env:       func() []string { return withAgentEnv(os.Environ(), envDir, agents.Codex) },
		}

	})
	return imageJobsSvc
}

func imageRoutes(d deps) []gate.BodyRoute {
	return []gate.BodyRoute{{
		Slice: "fsread",
		Match: func(r *http.Request) bool { return files.RouteIs(r, platformAPI+"/images") },
		Serve: func(w http.ResponseWriter, r *http.Request) {
			if _, _, _, no := d.APIRefusal(r); no {
				files.WriteUnauthorized(w)
				return
			}
			if !d.SessionOK(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			serveImages(imageJobs(d), pictureReadable(d, workspaceAccess(d)), w, r)
		},
	}}
}

// serveImages answers <platform>/images: GET ?id= one job, GET ?conv=&owner= the jobs of a chat or an owner, POST
// {prompt, count, shape, transparent, conv, owner} a new job, and with {source, mode, format, overlay (a base64 PNG)} a
// change of a picture.
func serveImages(s *imagegen.Service, readable func(string) bool, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	switch r.Method {
	case http.MethodGet:
		if id := q.Get("id"); id != "" {
			snap, ok := s.Get(id)
			if !ok {
				writeImagesJSON(w, http.StatusNotFound, map[string]string{"error": "no such job"})
				return
			}
			writeImagesJSON(w, http.StatusOK, snap)
			return
		}
		writeImagesJSON(w, http.StatusOK, map[string]any{"jobs": s.List(q.Get("conv"), q.Get("owner"))})
	case http.MethodPost:
		if site, present := fetchSite(r); present && site != "same-origin" && site != "none" {
			writeImagesJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request"})
			return
		}
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			writeImagesJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "send JSON"})
			return
		}
		var req imagegen.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 24<<20)).Decode(&req); err != nil {
			writeImagesJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is not a picture request"})
			return
		}
		req.CopyTo, req.Name, req.SaveOK, req.SourceOK = "", "", nil, readable
		snap, err := s.Start(req)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, imagegen.ErrBadRequest):
				status = http.StatusBadRequest
			case errors.Is(err, imagegen.ErrBusy):
				status = http.StatusTooManyRequests
			}
			writeImagesJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeImagesJSON(w, http.StatusOK, snap)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeImagesJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// pictureReadable says whether a picture may be changed: one the file viewer may show, its links followed.
func pictureReadable(d deps, access *files.Access) func(string) bool {
	return func(p string) bool {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return false
		}
		return saveFolderOK(access, real, d.WorkspaceRoot, sharedProjects, imagesDir(d), codexImagesDir(d), files.NodeTempDir(), "/tmp")
	}
}

// saveFolderOK says whether the pictures may be copied into a folder: one inside the workspace or the shared projects,
// its links followed (the deepest part that exists is resolved, the rest is still to be made), not a secret one.
func saveFolderOK(access *files.Access, p string, roots ...string) bool {
	real, ok := resolveExisting(p)
	if !ok || access.Secret(real) || access.Secret(p) {
		return false
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(r, real); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

// resolveExisting follows the links of the deepest part of p that exists and joins the rest to it.
func resolveExisting(p string) (string, bool) {
	p = filepath.Clean(p)
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest), true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}
