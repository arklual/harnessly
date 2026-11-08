package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// MaxWait is the longest a changes request is held open when there is nothing new.
const MaxWait = 25 * time.Second

// DefaultRoom is the design library of a store that serves one place only (a runtime's own store, a design-sync
// hub): its revisions are in hub.db and its blobs in blobs/ at the top of the data folder.
const DefaultRoom = KindDesign

var (
	// ErrUnauthorized is a request whose caller is not known.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrUnknownCollection is a collection the store does not serve.
	ErrUnknownCollection = errors.New("unknown collection")
)

// Access is what the authorizer decided about a request: the member written into the revisions and leases it makes,
// and the room that holds the collection it asked for.
type Access struct {
	Member string
	Room   string
}

// Authorizer decides who a request comes from and which room the collection it names is for that caller. It returns
// ErrUnauthorized, ErrForbidden or ErrUnknownCollection for a request it refuses.
type Authorizer func(r *http.Request, collection string) (Access, error)

// Server is the store's HTTP API. Who the caller is and which room a collection is come from the Authorizer, never
// from the request's body.
type Server struct {
	meta      MetaStore
	blobs     BlobStore
	authorize Authorizer
	// owned: Close closes the drivers.
	owned bool

	mu    sync.Mutex
	rooms map[string]*Store
}

// NewServer builds the API over opened drivers; the caller keeps them and closes them.
func NewServer(meta MetaStore, blobs BlobStore, authorize Authorizer) *Server {
	return &Server{meta: meta, blobs: blobs, authorize: authorize, rooms: map[string]*Store{}}
}

// OpenFolder opens (creating) a store in a data folder, SQLite next to blobs/: the layout of a store that lives inside
// one runtime, and of a design-sync hub. The server owns the drivers.
func OpenFolder(dir string, authorize Authorizer) (*Server, error) {
	meta, err := openSQLiteMeta(dir)
	if err != nil {
		return nil, err
	}
	blobs, err := NewFileBlobs(dir)
	if err != nil {
		meta.Close()
		return nil, err
	}
	s := NewServer(meta, blobs, authorize)
	s.owned = true
	return s, nil
}

// Meta and Blobs are the drivers (backup, gc, migrate run on them).
func (s *Server) Meta() MetaStore            { return s.meta }
func (s *Server) Blobs() BlobStore           { return s.blobs }
func (s *Server) SetAuthorizer(a Authorizer) { s.authorize = a }

// Close closes the drivers when the server opened them itself.
func (s *Server) Close() {
	if !s.owned {
		return
	}
	s.meta.Close()
	s.blobs.Close()
}

// Room opens a room's store, creating it the first time.
func (s *Server) Room(ctx context.Context, room string) (*Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.rooms[room]; ok {
		return st, nil
	}
	if !ValidRoom(room) {
		return nil, os.ErrNotExist
	}
	rm, err := s.meta.Room(ctx, room)
	if err != nil {
		return nil, err
	}
	st := NewStore(room, rm, s.blobs, PolicyFor(room))
	s.rooms[room] = st
	return st, nil
}

type caller struct {
	member string
	room   string
	store  *Store
}

// Handler routes the API: /v1/collections/{collection}/… for every collection kind.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	const prefix = "/v1/collections/{collection}"
	mux.HandleFunc("GET "+prefix+"/health", s.room(func(w http.ResponseWriter, r *http.Request, c caller) {
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}))
	mux.HandleFunc("HEAD "+prefix+"/blobs/{sha}", s.room(s.headBlob))
	mux.HandleFunc("GET "+prefix+"/blobs/{sha}", s.room(s.getBlob))
	mux.HandleFunc("PUT "+prefix+"/blobs/{sha}", s.room(s.putBlob))
	mux.HandleFunc("POST "+prefix+"/commit", s.room(s.commit))
	mux.HandleFunc("GET "+prefix+"/changes", s.room(s.changes))
	mux.HandleFunc("GET "+prefix+"/current", s.room(s.current))
	mux.HandleFunc("GET "+prefix+"/history", s.room(s.history))
	mux.HandleFunc("GET "+prefix+"/leases", s.room(s.leases))
	mux.HandleFunc("POST "+prefix+"/leases", s.room(s.leaseTake))
	mux.HandleFunc("POST "+prefix+"/leases/release", s.room(s.leaseRelease))
	return mux
}

// room authenticates the caller and opens the room of the collection it asks for.
func (s *Server) room(h func(http.ResponseWriter, *http.Request, caller)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		coll := r.PathValue("collection")
		if !KnownKind(coll) {
			http.Error(w, "unknown collection", http.StatusNotFound)
			return
		}
		if s.authorize == nil {
			http.Error(w, "the store takes no requests", http.StatusUnauthorized)
			return
		}
		acc, err := s.authorize(r, coll)
		switch {
		case errors.Is(err, ErrUnauthorized):
			http.Error(w, "unknown caller", http.StatusUnauthorized)
			return
		case errors.Is(err, ErrForbidden):
			http.Error(w, "this caller may not open collection "+coll, http.StatusForbidden)
			return
		case errors.Is(err, ErrUnknownCollection):
			http.Error(w, "unknown collection", http.StatusNotFound)
			return
		case err != nil:
			slog.Error("store authorize", "collection", coll, "err", err)
			http.Error(w, "cannot authorize", http.StatusInternalServerError)
			return
		case acc.Member == "" || !ValidRoom(acc.Room):
			http.Error(w, "unknown caller", http.StatusUnauthorized)
			return
		}
		st, err := s.Room(r.Context(), acc.Room)
		if err != nil {
			slog.Error("open room", "room", acc.Room, "err", err)
			http.Error(w, "cannot open the collection", http.StatusInternalServerError)
			return
		}
		h(w, r, caller{member: acc.Member, room: acc.Room, store: st})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) headBlob(w http.ResponseWriter, r *http.Request, c caller) {
	ok, err := c.store.HasBlob(r.Context(), r.PathValue("sha"))
	switch {
	case err != nil:
		slog.Error("head blob", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) getBlob(w http.ResponseWriter, r *http.Request, c caller) {
	sha := r.PathValue("sha")
	f, err := c.store.OpenBlob(r.Context(), sha)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("get blob", "err", err)
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	// A hash names the same bytes forever.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+sha+`"`)
	_, _ = io.Copy(w, f)
}

func (s *Server) putBlob(w http.ResponseWriter, r *http.Request, c caller) {
	err := c.store.PutBlob(r.Context(), r.PathValue("sha"), http.MaxBytesReader(w, r.Body, MaxFileSize+1024))
	if errors.Is(err, ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "bigger than the store takes", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		slog.Error("put blob", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commit(w http.ResponseWriter, r *http.Request, c caller) {
	var req CommitRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	e, conflict, err := c.store.Commit(r.Context(), req, c.member)
	switch {
	case errors.Is(err, ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNoBlob):
		http.Error(w, err.Error(), http.StatusPreconditionRequired)
	case err != nil:
		slog.Error("commit", "path", req.Path, "err", err)
		http.Error(w, "cannot commit", http.StatusInternalServerError)
	case conflict:
		slog.Info("store conflict", "room", c.room, "path", req.Path, "member", c.member, "base", req.Base, "current", e.Rev)
		writeJSON(w, http.StatusConflict, CommitResponse{Entry: e, Conflict: true})
	default:
		if e.Rev > req.Base {
			slog.Debug("store commit", "room", c.room, "path", e.Path, "rev", e.Rev, "deleted", e.Deleted, "member", c.member)
		}
		writeJSON(w, http.StatusOK, CommitResponse{Entry: e})
	}
}

// changes answers at once when something changed after `since`, otherwise holds the request until a write lands or
// `wait` seconds pass: a machine learns about another machine's edit within a round trip.
func (s *Server) changes(w http.ResponseWriter, r *http.Request, c caller) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	wait := time.Duration(0)
	if v, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && v > 0 {
		wait = min(time.Duration(v)*time.Second, MaxWait)
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		wake := c.store.Wake()
		out, err := c.store.Changes(r.Context(), since, 2000)
		if err != nil {
			slog.Error("changes", "err", err)
			http.Error(w, "cannot read", http.StatusInternalServerError)
			return
		}
		if len(out.Entries) > 0 || wait == 0 || out.Rev < since {
			writeJSON(w, http.StatusOK, out)
			return
		}
		select {
		case <-wake:
		case <-deadline.C:
			writeJSON(w, http.StatusOK, out)
			return
		case <-r.Context().Done():
			return
		}
	}
}

// current is the latest revision of one path: 404 when the path never existed.
func (s *Server) current(w http.ResponseWriter, r *http.Request, c caller) {
	p := r.URL.Query().Get("path")
	if !c.store.policy.Synced(p) {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	e, err := c.store.Current(r.Context(), p)
	if err != nil {
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return
	}
	if e == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request, c caller) {
	p := r.URL.Query().Get("path")
	if !c.store.policy.Synced(p) {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	out, err := c.store.History(r.Context(), p, 200)
	if err != nil {
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// leases lists the live leases of the room.
func (s *Server) leases(w http.ResponseWriter, r *http.Request, c caller) {
	ls, err := c.store.meta.Leases(r.Context(), time.Now().UnixMilli())
	if err != nil {
		slog.Error("leases", "err", err)
		http.Error(w, "cannot read leases", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, Leases{Leases: ls})
}

// leaseTake gives the caller's chat the lease on a key, or answers 409 with the live holder. The machine is the one
// the caller is, whatever the body says.
func (s *Server) leaseTake(w http.ResponseWriter, r *http.Request, c caller) {
	var req LeaseRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req); err != nil || req.Key == "" || req.Chat == "" || len(req.Key) > 512 {
		http.Error(w, "bad json: key and chat are required", http.StatusBadRequest)
		return
	}
	l, held, err := c.store.meta.LeaseTake(r.Context(), req, c.member, time.Now().UnixMilli())
	switch {
	case err != nil:
		slog.Error("lease", "key", req.Key, "err", err)
		http.Error(w, "cannot take the lease", http.StatusInternalServerError)
	case held:
		writeJSON(w, http.StatusConflict, LeaseResponse{Lease: l, Held: true})
	default:
		writeJSON(w, http.StatusOK, LeaseResponse{Lease: l})
	}
}

// leaseRelease lets go of the caller's lease, or (force) of anyone's: the person said the chat holding it is hung.
func (s *Server) leaseRelease(w http.ResponseWriter, r *http.Request, c caller) {
	var req LeaseRelease
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, "bad json: key is required", http.StatusBadRequest)
		return
	}
	dropped, err := c.store.meta.LeaseDrop(r.Context(), req, c.member, time.Now().UnixMilli())
	if err != nil {
		slog.Error("lease release", "key", req.Key, "err", err)
		http.Error(w, "cannot release the lease", http.StatusInternalServerError)
		return
	}
	if dropped != nil && req.Force {
		slog.Warn("lease forced off", "room", c.room, "key", req.Key, "by", c.member, "holder", dropped.Machine+"/"+dropped.Chat)
	}
	writeJSON(w, http.StatusOK, map[string]any{"dropped": dropped})
}
