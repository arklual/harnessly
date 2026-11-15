// Package rollout says whether the hub or the machines' software is being rolled out now, so the pages can say
// «Идёт обновление — возможны перебои с подключением к машинам» while it lasts instead of failing in silence.
//
// The hub is the one source: a rollout is a lease of the table job_leases whose name starts with "rollout:", alive
// until it runs out. Two kinds write one:
//   - the pipeline (or whoever deploys) with POST /api/rollout and the hub's rollout token, at the start and the end
//     of a rollout of the hub or of the runtimes («rollout:hub», «rollout:runtime»); a start that is never ended runs
//     out by itself;
//   - a replica that is told to stop (SIGTERM): it marks «rollout:drain:<replica>» before it takes itself out of
//     rotation, so the replicas that stay answer «active» too, and shortens the mark to a short tail when it exits.
//
// The pages read GET /api/v2/rollout (internal/hub/proxy) when a machine's link drops and once a minute while open.
package rollout

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	prefix = "rollout:"
	// holder of the pipeline's marks: one writer, so a second start extends the first.
	pipeline = "pipeline"
	// cacheFor is how long one answer of the database serves every page that asks.
	cacheFor = 2 * time.Second
	// DefaultFor is how long a pipeline's start lasts when it says nothing; MaxFor the longest it may ask.
	DefaultFor = 15 * time.Minute
	MaxFor     = 2 * time.Hour
)

// What may be rolled out.
var kinds = map[string]bool{"hub": true, "runtime": true}

// Service is the hub's view of the rollouts.
type Service struct {
	DB *store.DB
	// Replica names this process among the replicas; empty for a single process.
	Replica string
	// Token opens POST /api/rollout; empty keeps it shut (404).
	Token string
	Log   *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	draining atomic.Bool
	mu       sync.Mutex
	at       time.Time
	active   bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) drainName() string {
	id := s.Replica
	if id == "" {
		id = "single"
	}
	return prefix + "drain:" + id
}

// Active says whether a rollout is under way: this process is stopping, or a live mark is in the database. A database
// that does not answer is no rollout: the pages then say what they always said.
func (s *Service) Active(ctx context.Context) bool {
	if s == nil {
		return false
	}
	if s.draining.Load() {
		return true
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && now.Sub(s.at) < cacheFor && !now.Before(s.at) {
		return s.active
	}
	if s.DB == nil {
		return false
	}
	names, err := s.DB.S().LiveLeases(ctx, prefix, now)
	if err != nil {
		s.log().Warn("rollout marks not read", "error", err)
		return false
	}
	s.at, s.active = now, len(names) > 0
	return s.active
}

// forget drops the cached answer, so the next Active reads the database.
func (s *Service) forget() {
	s.mu.Lock()
	s.at = time.Time{}
	s.mu.Unlock()
}

// Start marks a rollout of what for d.
func (s *Service) Start(ctx context.Context, what string, d time.Duration) error {
	defer s.forget()
	_, err := s.DB.S().TakeLease(ctx, prefix+what, pipeline, s.now(), d)
	return err
}

// End clears the mark of a rollout of what.
func (s *Service) End(ctx context.Context, what string) error {
	defer s.forget()
	return s.DB.S().ReleaseLease(ctx, prefix+what, pipeline)
}

// Stopping is told that this process is being stopped: from now on it answers «active», and so do the other replicas
// for d (as long as the stop may last).
func (s *Service) Stopping(ctx context.Context, d time.Duration) {
	if s == nil {
		return
	}
	s.draining.Store(true)
	if s.DB == nil {
		return
	}
	if _, err := s.DB.S().TakeLease(ctx, s.drainName(), s.drainName(), s.now(), d); err != nil {
		s.log().Warn("rollout mark of the stop not written", "error", err)
	}
}

// Stopped shortens the mark of this process's stop to tail: the next replica is usually starting by then, and the
// pages keep saying so a little longer instead of blinking between two stops of one rollout.
func (s *Service) Stopped(ctx context.Context, tail time.Duration) {
	if s == nil || s.DB == nil || !s.draining.Load() {
		return
	}
	if _, err := s.DB.S().TakeLease(ctx, s.drainName(), s.drainName(), s.now(), tail); err != nil {
		s.log().Warn("rollout mark of the stop not shortened", "error", err)
	}
}

// ServeState answers GET /api/v2/rollout: {"active": bool}, never cached by the browser.
func (s *Service) ServeState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"active": s.Active(r.Context())})
}

type markBody struct {
	// State is "start" or "end".
	State string `json:"state"`
	// What is "hub" or "runtime".
	What string `json:"what"`
	// Minutes is how long a start lasts if no end comes (DefaultFor when 0).
	Minutes int `json:"minutes"`
}

// ServeMark answers POST /api/rollout {state, what, minutes} with the hub's rollout token as a Bearer.
func (s *Service) ServeMark(w http.ResponseWriter, r *http.Request) error {
	if s.Token == "" {
		return httpx.ErrNotFound
	}
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(given), []byte(s.Token)) != 1 {
		return httpx.ErrUnauthenticated
	}
	var b markBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	if !kinds[b.What] {
		return httpx.Invalid("what", "hub or runtime")
	}
	d := DefaultFor
	if b.Minutes != 0 {
		d = time.Duration(b.Minutes) * time.Minute
	}
	if d <= 0 || d > MaxFor {
		return httpx.Invalid("minutes", "from 1 to 120")
	}
	var err error
	switch b.State {
	case "start":
		err = s.Start(r.Context(), b.What, d)
	case "end":
		err = s.End(r.Context(), b.What)
	default:
		return httpx.Invalid("state", "start or end")
	}
	if err != nil {
		return err
	}
	s.log().Info("rollout marked", "state", b.State, "what", b.What)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"active": s.Active(r.Context())})
	return nil
}
