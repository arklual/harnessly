package docs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type healthRig struct {
	s     *Service
	down  *atomic.Bool
	now   *time.Time
	mails *[]string
	db    *store.DB
}

func newHealthRig(t *testing.T, boards bool) healthRig {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	u := &store.User{Email: "owner@example.com", Name: "Owner", IsAdmin: true}
	if err := db.S().CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := db.S().CreateUser(ctx, &store.User{Email: "mate@example.com", Name: "Mate"}); err != nil {
		t.Fatal(err)
	}
	down := new(atomic.Bool)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(svc.Close)
	cfg := &config.Config{Docs: true, ArtifactsURL: svc.URL, ArtifactsToken: "t", AdminEmails: []string{"ops@example.com", "owner@example.com"}}
	if boards {
		cfg.BoardsURL, cfg.BoardsToken = svc.URL, "t"
	}
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	var mails []string
	s := New(Deps{Config: cfg, DB: db, Now: func() time.Time { return now },
		Notice: func(_ context.Context, to, subject, _, _ string) error {
			mails = append(mails, to+": "+subject)
			return nil
		}})
	return healthRig{s: s, down: down, now: &now, mails: &mails, db: db}
}

func TestHealthStatusFollowsTheService(t *testing.T) {
	r := newHealthRig(t, true)
	if got := r.s.Status().Services; got != nil {
		t.Fatalf("before the first probe the status names no service, got %v", got)
	}
	r.s.healthPass(context.Background())
	st := r.s.Status().Services
	if !st[ServiceDocuments].Up || !st[ServiceBoards].Up {
		t.Fatalf("both services answer, got %+v", st)
	}
	r.down.Store(true)
	*r.now = r.now.Add(probeEvery)
	r.s.healthPass(context.Background())
	st = r.s.Status().Services
	if st[ServiceDocuments].Up || !st[ServiceDocuments].Since.Equal(*r.now) {
		t.Fatalf("documents went down now, got %+v", st[ServiceDocuments])
	}
}

func TestHealthAlertsOnceAfterFiveMinutesAndOnceOnReturn(t *testing.T) {
	r := newHealthRig(t, false)
	ctx := context.Background()
	r.down.Store(true)
	r.s.healthPass(ctx)
	*r.now = r.now.Add(alertAfter - time.Second)
	r.s.healthPass(ctx)
	if len(*r.mails) != 0 {
		t.Fatalf("no mail before %v, got %v", alertAfter, *r.mails)
	}
	*r.now = r.now.Add(time.Second)
	r.s.healthPass(ctx)
	want := []string{"ops@example.com: Сервис документов недоступен", "owner@example.com: Сервис документов недоступен"}
	if len(*r.mails) != 2 || (*r.mails)[0] != want[0] || (*r.mails)[1] != want[1] {
		t.Fatalf("each administrator once, got %v", *r.mails)
	}
	*r.now = r.now.Add(10 * time.Minute)
	r.s.healthPass(ctx)
	if len(*r.mails) != 2 {
		t.Fatalf("a service still down is not mailed again, got %v", *r.mails)
	}
	r.down.Store(false)
	*r.now = r.now.Add(probeEvery)
	r.s.healthPass(ctx)
	r.s.healthPass(ctx)
	if len(*r.mails) != 4 || (*r.mails)[2] != "ops@example.com: Сервис документов снова работает" {
		t.Fatalf("one mail on return, got %v", *r.mails)
	}
}

func TestHealthShortOutageIsNotMailed(t *testing.T) {
	r := newHealthRig(t, false)
	ctx := context.Background()
	r.down.Store(true)
	r.s.healthPass(ctx)
	*r.now = r.now.Add(time.Minute)
	r.down.Store(false)
	r.s.healthPass(ctx)
	if len(*r.mails) != 0 {
		t.Fatalf("a minute down is not an alert, got %v", *r.mails)
	}
}

func TestHealthOnlyTheLeaseHolderMails(t *testing.T) {
	r := newHealthRig(t, false)
	ctx := context.Background()
	if ok, err := r.db.S().TakeLease(ctx, alertLease, "another replica", *r.now, time.Hour); err != nil || !ok {
		t.Fatalf("lease: %v %v", ok, err)
	}
	r.down.Store(true)
	r.s.healthPass(ctx)
	*r.now = r.now.Add(alertAfter)
	r.s.healthPass(ctx)
	if len(*r.mails) != 0 {
		t.Fatalf("the replica without the lease stays silent, got %v", *r.mails)
	}
}

func TestProbeReadyRefusesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := ProbeReady(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("503 is not ready")
	}
	srv.Close()
	if err := ProbeReady(context.Background(), http.DefaultClient, srv.URL); err == nil {
		t.Fatal("a closed service is not ready")
	}
}

func TestHealthServiceWithoutReadyzRouteIsNotDown(t *testing.T) {
	r := newHealthRig(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	r.s.cfg.ArtifactsURL, r.s.cfg.BoardsURL = srv.URL, srv.URL
	ctx := context.Background()
	r.s.healthPass(ctx)
	if got := r.s.Status().Services; got != nil {
		t.Fatalf("an address that does not route /readyz says nothing about the service, got %v", got)
	}
	*r.now = r.now.Add(alertAfter + probeEvery)
	r.s.healthPass(ctx)
	if len(*r.mails) != 0 {
		t.Fatalf("nobody is mailed for a missing /readyz route, got %v", *r.mails)
	}
	if err := ProbeReady(ctx, srv.Client(), srv.URL); !errors.Is(err, ErrNoReadyz) {
		t.Fatalf("the doctor still sees a 404 as a failure, got %v", err)
	}
}
