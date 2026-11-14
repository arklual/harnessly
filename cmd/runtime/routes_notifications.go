package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/notices"
	"github.com/ly-solutions/harnessly/internal/noticeslot"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// HRN-1421, stage 4. The person's notices (what the window shows, what they read, their switches) and the smart
// notice's open questions live in a database of the runtime (internal/notices): SQLite beside the data, or the
// Postgres of RUNTIME_STORE_DB. A runtime with no hub always serves them; under a hub whose switch hub-notices is on
// the hub answers the same routes from its own database, and the runtime's copy is what an off switch falls back to.

// slotPerson is the store's person for the slot's questions: they belong to the machine, not to a person.
const slotPerson = "_slot"

var (
	noticeOnce sync.Once
	noticeSvc  *notices.Service
)

// noticeStore opens the runtime's notices store once. A database that cannot be opened is said in the log and leaves
// the service nil: the routes then answer 503 and the page keeps its localStorage cache and the old poll.
func noticeStore(dataDir string) *notices.Service {
	noticeOnce.Do(func() {
		var db *sql.DB
		var pg bool
		var err error
		if url := strings.TrimSpace(os.Getenv("RUNTIME_STORE_DB")); strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
			pg = true
			db, err = sql.Open("pgx", url)
		} else {
			if err = os.MkdirAll(dataDir, 0o700); err == nil {
				db, err = sql.Open("sqlite", "file:"+filepath.Join(dataDir, "notices.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
			}
		}
		if err == nil {
			if !pg {
				db.SetMaxOpenConns(1)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = notices.EnsureSchema(ctx, db)
		}
		if err != nil {
			agent.Log("warn", "notices store not opened", agent.F{K: "error", V: err.Error()})
			return
		}
		noticeSvc = notices.New(notices.NewSQL(notices.Wrap(db, pg)))
		noticeSvc.Now = time.Now
		// What a restart left open was asked of a call that is gone: it is over.
		_, _ = noticeSvc.Store.ReadAll(context.Background(), slotPerson, time.Now().UnixMilli())
		_ = noticeSvc.Store.Prune(context.Background(), time.Now().Add(-30*24*time.Hour).UnixMilli())
	})
	return noticeSvc
}

// journalSlot writes the slot's questions into the store as they open and end, in order, off the slot's lock.
func journalSlot(svc *notices.Service) func(event string, n noticeslot.Notice, status string) {
	type job struct {
		event  string
		n      noticeslot.Notice
		status string
	}
	jobs := make(chan job, 256)
	go func() {
		for j := range jobs {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			switch j.event {
			case "open":
				if j.n.Kind.Waits() {
					doc, _ := json.Marshal(j.n)
					_, _ = svc.Store.Put(ctx, slotPerson, notices.Item{Key: j.n.ID, Chat: j.n.Conversation, At: j.n.CreatedAt, Doc: doc})
				}
			case "end":
				_, _ = svc.Store.MarkRead(ctx, slotPerson, []string{j.n.ID}, time.Now().UnixMilli())
			}
			cancel()
		}
	}()
	return func(event string, n noticeslot.Notice, status string) {
		select {
		case jobs <- job{event, n, status}:
		default: // a full queue drops the record; the question itself is unaffected
		}
	}
}

func notificationRoutes(d deps) []gate.Route {
	svc := noticeStore(d.DataDir)
	if svc != nil {
		noticeSlot.Journal = journalSlot(svc)
		prev := noticeSlot.Changed
		noticeSlot.Changed = func() {
			if prev != nil {
				prev()
			}
			svc.Bus.Publish(slotPerson, notices.Event{Type: "slot"})
		}
	}
	base := platformAPI + "/notifications"
	stream := platformAPI + "/notices/stream"
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no-store"})
				return
			}
			if r.Method != http.MethodGet && sameorigin.Refuse(w, r) {
				return
			}
			if !d.SessionOK(r) {
				secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	var handler http.Handler
	if svc != nil {
		handler = svc.Handler(func(r *http.Request) (string, bool) {
			p := strings.TrimSpace(r.URL.Query().Get("person"))
			if p == "" {
				p = "local"
			}
			return p, len(p) <= 200 && p != slotPerson
		})
	}
	strip := func(r *http.Request) *http.Request {
		r2 := r.Clone(r.Context())
		for _, pre := range []string{"/agents" + base, base} {
			if strings.HasPrefix(r.URL.Path, pre) {
				r2.URL.Path = strings.TrimPrefix(r.URL.Path, pre)
				break
			}
		}
		if r2.URL.Path == "" {
			r2.URL.Path = "/"
		}
		r2.URL.RawPath = ""
		return r2
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool {
			return pathIs(r, base) || pathIs(r, "/agents"+base) || strings.HasPrefix(r.URL.Path, base+"/") || strings.HasPrefix(r.URL.Path, "/agents"+base+"/")
		}, Handler: guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, strip(r)) }))},
		{Match: func(r *http.Request) bool { return pathIs(r, stream) || pathIs(r, "/agents"+stream) },
			Handler: guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !svc.Stream(w, r, slotPerson) {
					secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "no-stream"})
				}
			}))},
	}
}
