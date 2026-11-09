package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/datamove"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// The move of this machine's own data into the hub's store (internal/datamove, docs/migrations/0001-data-to-hub.md,
// HRN-883): under a hub, the runtime moves by itself what an older version kept only on its disk, and keeps the
// journal on the disk and in the hub. `harnessly migrate` and the page's «Машины» read and steer it here:
//
//	GET  /api/cli/migrate                  (CLI token)  the status
//	POST /api/cli/migrate/retry|skip       {objects?: [{kind, object}]}
//	POST /api/cli/migrate/verify           every verified object asked of the store again
//	GET  <platform>/machine/migration      (session)    the status, for the page
//	POST <platform>/machine/migration/retry|skip

// moving is the runtime's move, nil while there is none (no hub, RUNTIME_DATA_MOVE=off, before the store is linked).
var moving atomic.Pointer[datamove.Mover]

// storeTarget is the hub's store as the move puts into it.
type storeTarget struct{ l *storelink.Link }

func moveErr(err error) error {
	if errors.Is(err, filesync.ErrUnauthorized) {
		return fmt.Errorf("%w: %v", datamove.ErrUnauthorized, err)
	}
	return err
}

func (t storeTarget) HasBlob(ctx context.Context, kind, sha string) (bool, error) {
	ok, err := t.l.Client(kind).HasBlob(ctx, sha)
	return ok, moveErr(err)
}

func (t storeTarget) PutBlob(ctx context.Context, kind, sha string, data []byte) error {
	return moveErr(t.l.Client(kind).PutBlob(ctx, sha, data))
}

// hubJournal is the hub's journal of the move.
type hubJournal struct{ l *storelink.Link }

func (j hubJournal) Report(ctx context.Context, rep hubproto.MigrationReport) (hubproto.MigrationAnswer, error) {
	ans, err := j.l.Report(ctx, rep)
	if errors.Is(err, storelink.ErrNoJournal) {
		return ans, datamove.ErrNoJournal
	}
	return ans, err
}

// moveDelay lets the runtime settle after a start before the move reads the disk (RUNTIME_DATA_MOVE_DELAY, a Go
// duration: the browser tests start it at once).
var moveDelay = 30 * time.Second

func moveDelayOf() time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv("RUNTIME_DATA_MOVE_DELAY"))); err == nil && d >= 0 {
		return d
	}
	return moveDelay
}

// wakeMove has the move take a pass now (a handoff note was just written): nothing when there is no move.
func wakeMove() {
	if m := moving.Load(); m != nil {
		m.Wake()
	}
}

// startMove starts the move under a hub's store, unless RUNTIME_DATA_MOVE=off.
func (f *runtimeGate) startMove(ctx context.Context, l *storelink.Link, dataDir string) func() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_DATA_MOVE"))) {
	case "off", "false", "0", "no":
		return func() {}
	}
	if l == nil || l.Kind() != storelink.KindHub {
		return func() {}
	}
	m := datamove.New(datamove.Options{
		Version:   env("AGENT_VERSION", agent.BuildVersion),
		StateFile: filepath.Join(dataDir, "migrations", datamove.ID+".json"),
		Sources: []datamove.Source{
			datamove.Uploads{Dir: filepath.Join(dataDir, "v2", "uploads")},
			datamove.Handoffs{Dir: filepath.Join(dataDir, "v2", "handoffs")},
			&datamove.Delivered{Transcripts: f.transcripts},
			datamove.Sidebar{Read: f.sidebar, Send: hubSidebar{l}.send},
		},
		Target: storeTarget{l}, Journal: hubJournal{l}, MaxBytes: deliveredMax, Log: slog.Default(),
	})
	moving.Store(m)
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := l.Ready(run, 10*time.Minute); err != nil {
			return
		}
		select {
		case <-run.Done():
			return
		case <-time.After(moveDelayOf()):
		}
		m.Run(run)
	}()
	return func() {
		stop()
		<-done
		moving.Store(nil)
	}
}

// transcripts are the transcripts of this runtime's chats (the records of the files their agents handed over).
func (f *runtimeGate) transcripts(ctx context.Context) []string {
	rt := peerLocal{c: f.core.running()}.rt()
	if rt == nil {
		return nil
	}
	snap, err := rt.Conversations(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(snap.Conversations))
	for _, c := range snap.Conversations {
		if c.TranscriptPath != "" {
			out = append(out, c.TranscriptPath)
		}
	}
	return out
}

// errNoRuntime: the chats are not up yet; the sidebar waits for the next pass.
var errNoRuntime = errors.New("the runtime's chats are not up yet")

// sidebar is this machine's projects and favorites, as its own sidebar keeps them, to merge into its person's on the hub.
func (f *runtimeGate) sidebar(ctx context.Context) (*hubproto.MigrationSidebar, error) {
	rt := peerLocal{c: f.core.running()}.rt()
	if rt == nil {
		return nil, errNoRuntime
	}
	snap, err := rt.Conversations(ctx)
	if err != nil {
		return nil, err
	}
	side := &hubproto.MigrationSidebar{Projects: make([]hubproto.MigrationProject, 0, len(snap.Projects))}
	for _, p := range snap.Projects {
		side.Projects = append(side.Projects, hubproto.MigrationProject{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt})
	}
	if fv := snap.Favorites; fv != nil {
		side.Favorites = &hubproto.MigrationFavorites{Groups: make([]hubproto.MigrationFavGroup, 0, len(fv.Groups)), Loose: fv.Loose}
		for _, g := range fv.Groups {
			side.Favorites.Groups = append(side.Favorites.Groups, hubproto.MigrationFavGroup{ID: g.ID, Name: g.Name, Chats: g.Chats})
		}
	}
	return side, nil
}

// hubSidebar is the person's sidebar on the hub.
type hubSidebar struct{ l *storelink.Link }

func (h hubSidebar) send(ctx context.Context, side hubproto.MigrationSidebar) (bool, error) {
	ans, err := h.l.Sidebar(ctx, side)
	if errors.Is(err, storelink.ErrNoSidebar) {
		return false, datamove.ErrNotKept
	}
	return ans.Merged, moveErr(err)
}

// moveAnswer is the status of the move, or that there is none and why.
type moveAnswer struct {
	*datamove.Status
	Off string `json:"off,omitempty"`
}

func moveStatus() moveAnswer {
	m := moving.Load()
	if m == nil {
		return moveAnswer{Off: "no move on this machine: it is not linked to a hub's store, or RUNTIME_DATA_MOVE=off"}
	}
	st := m.Status()
	return moveAnswer{Status: &st}
}

type moveAsk struct {
	Objects []hubproto.MigrationKey `json:"objects"`
}

// serveMove answers one route of the move; base is the route's root.
func serveMove(w http.ResponseWriter, r *http.Request, base string) {
	act := strings.TrimPrefix(strings.TrimPrefix(routePath(r), base), "/")
	if act == "" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		peerJSON(w, http.StatusOK, moveStatus())
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	m := moving.Load()
	if m == nil {
		peerJSON(w, http.StatusConflict, moveStatus())
		return
	}
	var ask moveAsk
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		err = json.Unmarshal(raw, &ask)
	}
	if err != nil {
		peerJSON(w, http.StatusBadRequest, map[string]string{"error": "objects"})
		return
	}
	switch act {
	case "retry":
		n := m.Retry(ask.Objects)
		peerJSON(w, http.StatusOK, map[string]any{"marked": n, "status": moveStatus()})
	case "skip":
		if len(ask.Objects) == 0 {
			peerJSON(w, http.StatusBadRequest, map[string]string{"error": "name the objects to leave on the machine"})
			return
		}
		n := m.Skip(ask.Objects)
		peerJSON(w, http.StatusOK, map[string]any{"marked": n, "status": moveStatus()})
	case "verify":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		v, err := m.Verify(ctx)
		if err != nil {
			peerJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		peerJSON(w, http.StatusOK, map[string]any{"verify": v, "status": moveStatus()})
	default:
		peerJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

// routePath is the request's path without the app's /agents prefix (pathIs takes both).
func routePath(r *http.Request) string {
	if p, ok := strings.CutPrefix(r.URL.Path, "/agents"); ok && strings.HasPrefix(p, "/") {
		return p
	}
	return r.URL.Path
}

func underPath(r *http.Request, base string) bool {
	p := routePath(r)
	return p == base || strings.HasPrefix(p, base+"/")
}

// moveRoutes are the move's routes: the CLI's with its token, the page's with the session (the page does not verify).
func moveRoutes(d deps) []gate.Route {
	cli := "/api/cli/migrate"
	page := platformAPI + "/machine/migration"
	return []gate.Route{
		{
			Match: func(r *http.Request) bool { return underPath(r, cli) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if d.CLI == nil || !d.CLI.OK(r) {
					peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
					return
				}
				serveMove(w, r, cli)
			}),
		},
		{
			Match: func(r *http.Request) bool { return underPath(r, page) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !d.SessionOK(r) {
					peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				if strings.HasSuffix(routePath(r), "/verify") {
					peerJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
					return
				}
				serveMove(w, r, page)
			}),
		},
	}
}
