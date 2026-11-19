package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mediagen"
)

// Video and pictures made by a media provider (internal/mediagen, Higgsfield): the jobs an agent's generate_video, a Sinopia
// design or the page start, and `<platform>/media` that lists, reads, starts, prices and cancels them. The clips are files
// the viewer shows (`<platform>/file-preview?path=`): the jobs' folder is among its roots (pictureReadable).

func init() {
	bodyRouteGroups = append(bodyRouteGroups, mediaRoutes)
}

var (
	mediaJobsOnce sync.Once
	mediaJobsSvc  *mediagen.Service
)

// mediaDir is where the jobs' clips are kept.
func mediaDir(d deps) string { return filepath.Join(d.DataDir, "media") }

// mediaJobs is the machine's one media service: its providers are the registry's, its key of last resort the safe's.
func mediaJobs(d deps) *mediagen.Service {
	mediaJobsOnce.Do(func() {
		mediaJobsSvc = &mediagen.Service{
			Dir:      mediaDir(d),
			Registry: extProvidersFor(d),
			Secrets:  extSecrets{store: safeFor(d).store},
		}
	})
	return mediaJobsSvc
}

func mediaRoutes(d deps) []gate.BodyRoute {
	return []gate.BodyRoute{{
		Slice: "fsread",
		Match: func(r *http.Request) bool { return files.RouteIs(r, platformAPI+"/media") },
		Serve: func(w http.ResponseWriter, r *http.Request) {
			if _, _, _, no := d.APIRefusal(r); no {
				files.WriteUnauthorized(w)
				return
			}
			if !d.SessionOK(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			serveMedia(mediaJobs(d), pictureReadable(d, workspaceAccess(d)), w, r)
		},
	}}
}

// serveMedia answers <platform>/media: GET ?id= one job, GET ?conv=&owner= the jobs of a chat or an owner; POST {kind, model,
// prompt, frames, seconds, …} a new job, with ?estimate=1 only its price, and {cancel: <id>} a cancel.
func serveMedia(s *mediagen.Service, readable func(string) bool, w http.ResponseWriter, r *http.Request) {
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
		var body struct {
			mediagen.Request
			Cancel string `json:"cancel"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeImagesJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is not a media request"})
			return
		}
		ctx, stop := context.WithTimeout(r.Context(), 30*time.Second)
		defer stop()
		if body.Cancel != "" {
			snap, err := s.Cancel(ctx, body.Cancel)
			if err != nil {
				writeImagesJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeImagesJSON(w, http.StatusOK, snap)
			return
		}
		req := body.Request
		req.CopyTo, req.Name, req.SaveOK, req.FrameOK = "", "", nil, readable
		if q.Get("estimate") != "" {
			usd, model, err := s.Estimate(ctx, req)
			if err != nil {
				writeMediaError(w, err)
				return
			}
			writeImagesJSON(w, http.StatusOK, map[string]any{"usd": usd, "model": model})
			return
		}
		snap, err := s.Start(req)
		if err != nil {
			writeMediaError(w, err)
			return
		}
		writeImagesJSON(w, http.StatusOK, snap)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeMediaError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, mediagen.ErrBadRequest):
		status = http.StatusBadRequest
	case errors.Is(err, mediagen.ErrNoKey):
		status = http.StatusFailedDependency
		err = errors.New(mediagen.NoKeyMessage)
	}
	writeImagesJSON(w, status, map[string]string{"error": err.Error()})
}
