package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/designrec"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// The records of the library's entities (a project, a design system, a video, a deck) go into the store on
// every change and once when the machine connects: under a hub it is the hub's copy that stays when this machine is off,
// on a runtime of its own it is the embedded store. The hub's row of a project is filled from them under the switch
// hub-design-owner; the runtime writes them whatever the switch says. GET <platform>/design/records-check answers
// whether the store has as many as the machine (designrec.Report).

// libraryRecords is what the machine's library says of each project, in the shape the store keeps it.
func libraryRecords(d deps) []filestore.Record {
	lib := designAPI(d).L
	st := (&v2Designly{file: filepath.Join(d.DataDir, "v2", "designly.json")}).state()
	arts := lib.ListArtifacts()
	out := make([]filestore.Record, 0, len(arts))
	for _, a := range arts {
		if a == nil || a.ID == "" {
			continue
		}
		r := filestore.Record{ID: a.ID, Title: a.Title, Kind: string(kindOf(st, lib, a.ID)), At: a.UpdatedAt}
		if a.Cover != nil {
			r.Preview = *a.Cover
			if len(r.Preview) > 512 {
				r.Preview = ""
			}
		}
		out = append(out, r)
	}
	return out
}

// recordsSession is the running writer of the records, for the check's route.
type recordsSession struct {
	s      *designrec.Syncer
	source func() []filestore.Record
}

var recordsLive atomic.Pointer[recordsSession]

// startRecords writes all records once the store is there, then again a moment after the library changes (and every five
// minutes, in case a change went unseen); it returns when ctx ends. The first push is followed by the check, whose result
// goes to the journal.
func startRecords(ctx context.Context, l *storelink.Link, source func() []filestore.Record, watch func(func()) func(), covers func(context.Context, *designrec.Syncer)) {
	if err := l.Ready(ctx, 10*time.Minute); err != nil {
		return
	}
	sess := &recordsSession{s: &designrec.Syncer{Author: "runtime", Clients: func(kind string) designrec.Store { return l.Client(kind) }}, source: source}
	recordsLive.Store(sess)
	defer recordsLive.CompareAndSwap(sess, nil)
	push := func() {
		if covers != nil {
			covers(ctx, sess.s)
		}
		recs := source()
		if n, err := sess.s.Push(ctx, recs); err != nil {
			if ctx.Err() == nil {
				agent.Log("warn", "design records: not all reached the store", agent.F{K: "error", V: err.Error()})
			}
		} else if n > 0 {
			agent.Log("info", "design records: written", agent.F{K: "count", V: n})
		}
	}
	push()
	if rep, err := sess.s.Reconcile(ctx, source()); err != nil {
		agent.Log("warn", "design records: the check could not run", agent.F{K: "error", V: err.Error()})
	} else if !rep.OK {
		agent.Log("warn", "design records: the store lacks entities the machine has", agent.F{K: "missing", V: len(rep.Missing)})
	} else {
		agent.Log("info", "design records: the store has what the machine has", agent.F{K: "entities", V: len(source())})
	}
	kick := make(chan struct{}, 1)
	if watch != nil {
		if stop := watch(func() {
			select {
			case kick <- struct{}{}:
			default:
			}
		}); stop != nil {
			defer stop()
		}
	}
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			push()
		case <-kick:
			// A moment after the last change: an editor writes many files in a row.
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
			select {
			case <-kick:
			default:
			}
			push()
		}
	}
}

// watchLibrary calls fn on every change of the library; the returned func stops it.
func watchLibrary(d deps) func(func()) func() {
	return func(fn func()) func() {
		stop, ok := designAPI(d).L.Subscribe(func(string) { fn() })
		if !ok {
			return nil
		}
		return stop
	}
}

// designRecordsRoutes is GET <platform>/design/records-check.
func designRecordsRoutes() []gate.Route {
	path := platformAPI + "/design/records-check"
	return []gate.Route{{Slice: "design", Match: func(r *http.Request) bool {
		return r.Method == http.MethodGet && files.RouteIs(r, path)
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := recordsLive.Load()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if sess == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"store_off"}`))
			return
		}
		rep, err := sess.s.Reconcile(r.Context(), sess.source())
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(rep)
	})}}
}

// pushCovers puts the cover picture of each project into the store, apart from the design's revisions.
func pushCovers(d deps) func(context.Context, *designrec.Syncer) {
	return func(ctx context.Context, s *designrec.Syncer) {
		lib := designAPI(d).L
		for _, a := range lib.ListArtifacts() {
			if a == nil || ctx.Err() != nil {
				continue
			}
			p := lib.CoverFile(a.ID)
			if p == "" {
				continue
			}
			if fi, err := os.Stat(p); err != nil || fi.Size() > filestore.MaxCoverBytes {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if _, err := s.PushCover(ctx, a.ID, b); err != nil {
				agent.Log("warn", "design records: a cover did not reach the store", agent.F{K: "error", V: err.Error()})
				return
			}
		}
	}
}
