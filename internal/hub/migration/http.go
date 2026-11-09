package migration

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Session is the machine a store session names: its space and the person who made it.
type Session struct {
	MachineID string
	SpaceID   string
	OwnerID   string
}

// SessionMachine is the machine whose live store session the request carries (storehub.Store.SessionMachine).
type SessionMachine func(r *http.Request) (Session, error)

// MayOpen checks the person may open the machine (machines.Service.Authorize and Visible); MayChange that they may
// change what it does (its person, or an owner of its space).
type MayOpen func(r *http.Request, p *auth.Principal, machineID string) error

// Blobs is what the hub checks a machine's report against: the space's collections in the store.
type Blobs interface {
	HasBlob(ctx context.Context, spaceID, kind, sha string) (bool, error)
}

// Service is the journal's HTTP side.
type Service struct {
	j         *Journal
	auth      *auth.Service
	machine   SessionMachine
	mayOpen   MayOpen
	mayChange MayOpen
	blobs     Blobs
	// sidebars is the database of the people's sidebars, nil on a hub that keeps none (no history).
	sidebars *store.DB
	now      func() int64
}

// NewService serves the journal: to the machines with their store sessions, to the people who may open a machine.
func NewService(j *Journal, a *auth.Service, machine SessionMachine, mayOpen, mayChange MayOpen, blobs Blobs) *Service {
	return &Service{j: j, auth: a, machine: machine, mayOpen: mayOpen, mayChange: mayChange, blobs: blobs}
}

// UseSidebars lets the machines merge their projects and favorites into their people's sidebars (history.Sidebar).
func (s *Service) UseSidebars(db *store.DB, nowMs func() int64) {
	s.sidebars, s.now = db, nowMs
}

// Journal is the journal the service serves.
func (s *Service) Journal() *Journal { return s.j }

// Mount adds the routes:
//
//	POST <hubproto.MigrationPath>/report              the machine's MigrationReport → MigrationAnswer
//	GET  <hubproto.MigrationPath>/status              the machine's own MigrationStatus
//	POST <hubproto.MigrationPath>/sidebar             the machine's MigrationSidebar into its person's sidebar
//	GET  /api/machines/{machineId}/migration          MigrationStatus, for a person who may open the machine
//	POST /api/machines/{machineId}/migration/retry    {objects?: [{kind, object}]}: try the failed ones again (none: all),
//	                                                  for the machine's person or an owner of its space
//	POST /api/machines/{machineId}/migration/skip     {objects?: …}: let them stay on the machine only, the same people
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("POST "+hubproto.MigrationPath+"/report", httpx.Handler(s.report))
	mux.Handle("GET "+hubproto.MigrationPath+"/status", httpx.Handler(s.own))
	mux.Handle("POST "+hubproto.MigrationPath+"/sidebar", httpx.Handler(s.sidebar))
	mux.Handle("GET /api/machines/{machineId}/migration", s.auth.Require(s.status))
	mux.Handle("POST /api/machines/{machineId}/migration/retry", s.auth.Require(s.ask(AskRetry)))
	mux.Handle("POST /api/machines/{machineId}/migration/skip", s.auth.Require(s.ask(AskSkip)))
}

var errNoMove = httpx.New(http.StatusNotFound, "no_migration", "Машина ещё не переносила данные")

func badReport(err error) error {
	if errors.Is(err, ErrBadReport) {
		return httpx.New(http.StatusBadRequest, "invalid_request", err.Error())
	}
	if errors.Is(err, ErrNoMove) {
		return errNoMove
	}
	return err
}

func (s *Service) report(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.machine(r)
	if err != nil {
		return err
	}
	var rep hubproto.MigrationReport
	if err := httpx.Decode(r, &rep); err != nil {
		return err
	}
	if err := s.check(r.Context(), sess, rep.Objects); err != nil {
		return err
	}
	ans, err := s.j.Report(r.Context(), sess.MachineID, sess.OwnerID, rep)
	if err != nil {
		return badReport(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, ans)
	return nil
}

// check is the hub's own look at what a machine says it moved: an object it calls verified is in the space's collection
// under that sha256, or it is a failure (MIG-VERIFY) whatever the machine says; an upload is bound to the machine's
// person only once its blob is there. The machine's sidebar is verified once its person's sidebar lists it as merged.
func (s *Service) check(ctx context.Context, sess Session, objs []hubproto.MigrationObject) error {
	for i := range objs {
		o := &objs[i]
		if o.State != hubproto.MigrationVerified {
			continue
		}
		if o.Kind == hubproto.MigrationSidebarKind {
			merged := false
			if s.sidebars != nil {
				var err error
				if merged, err = history.MachineMerged(ctx, s.sidebars, sess.OwnerID, sess.MachineID); err != nil {
					return err
				}
			}
			if !merged {
				o.State, o.Code, o.Error = hubproto.MigrationFailed, "MIG-VERIFY", "the person's sidebar does not list this machine"
			}
			continue
		}
		if s.blobs == nil {
			continue
		}
		has := false
		if sha256Hex(o.Sha256) && validKind(o.Kind) {
			var err error
			if has, err = s.blobs.HasBlob(ctx, sess.SpaceID, o.Kind, o.Sha256); err != nil {
				return err
			}
		}
		if !has {
			o.State, o.Code, o.Error = hubproto.MigrationFailed, "MIG-VERIFY", "the hub does not have this object under its sha256"
		}
	}
	return nil
}

var errNoSidebar = httpx.New(http.StatusNotFound, "no_sidebar", "Хаб не хранит сайдбар")

// sidebar merges the machine's projects and favorites into its person's sidebar, on the revision it read.
func (s *Service) sidebar(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.machine(r)
	if err != nil {
		return err
	}
	if s.sidebars == nil {
		return errNoSidebar
	}
	var in hubproto.MigrationSidebar
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if len(in.Projects) > 500 || in.Favorites != nil && len(in.Favorites.Groups) > 100 {
		return httpx.New(http.StatusBadRequest, "invalid_request", "too many projects or groups")
	}
	projects := make([]history.MachineProject, 0, len(in.Projects))
	for _, p := range in.Projects {
		projects = append(projects, history.MachineProject{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt})
	}
	var favs *history.ProfileFavorites
	if in.Favorites != nil {
		favs = &history.ProfileFavorites{Loose: in.Favorites.Loose}
		for _, g := range in.Favorites.Groups {
			favs.Groups = append(favs.Groups, history.ProfileGroup{ID: g.ID, Name: g.Name, Chats: g.Chats})
		}
	}
	merged, err := history.MergeMachineInto(r.Context(), s.sidebars, sess.OwnerID, sess.MachineID, projects, favs, s.now())
	if errors.Is(err, history.ErrSidebarBusy) {
		return httpx.New(http.StatusConflict, "sidebar_busy", err.Error())
	}
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, hubproto.MigrationSidebarAnswer{Merged: merged})
	return nil
}

func (s *Service) own(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.machine(r)
	if err != nil {
		return err
	}
	st, err := s.j.Status(r.Context(), sess.MachineID, r.URL.Query().Get("migration"))
	if err != nil {
		return badReport(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

func (s *Service) status(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	id := r.PathValue("machineId")
	if err := s.mayOpen(r, p, id); err != nil {
		return err
	}
	st, err := s.j.Status(r.Context(), id, "")
	if err != nil {
		return badReport(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

type askRequest struct {
	Objects []hubproto.MigrationKey `json:"objects"`
}

func (s *Service) ask(what string) func(http.ResponseWriter, *http.Request, *auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		id := r.PathValue("machineId")
		if err := s.mayChange(r, p, id); err != nil {
			return err
		}
		var req askRequest
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		n, err := s.j.Ask(r.Context(), id, what, req.Objects)
		if err != nil {
			return badReport(err)
		}
		st, err := s.j.Status(r.Context(), id, "")
		if err != nil {
			return badReport(err)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"marked": n, "status": st})
		return nil
	}
}
