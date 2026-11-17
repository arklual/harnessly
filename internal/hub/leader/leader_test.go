package leader

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db := storetest.Open(t)
	return db
}

func fast(l *Leader) *Leader {
	l.ttl, l.every = 600*time.Millisecond, 100*time.Millisecond
	return l
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNilLeads(t *testing.T) {
	var l *Leader
	if !l.Held() {
		t.Fatal("a hub of one process leads always")
	}
	ran := false
	l.Lead(context.Background(), "job", func(context.Context) { ran = true })
	if !ran {
		t.Fatal("job not run")
	}
}

// Of two replicas one leads and runs the job; when it stops, the other takes over and the job moves with the lead.
func TestOneLeadsAndHandsOver(t *testing.T) {
	db := openDB(t)
	a, b := fast(New(db, "a", nil, nil)), fast(New(db, "b", nil, nil))
	actx, astop := context.WithCancel(context.Background())
	bctx, bstop := context.WithCancel(context.Background())
	defer bstop()
	defer astop()
	go a.Run(actx)
	waitFor(t, "a to lead", a.Held)
	go b.Run(bctx)

	var running, runs atomic.Int32
	var both atomic.Bool
	job := func(ctx context.Context) {
		if running.Add(1) > 1 {
			both.Store(true)
		}
		runs.Add(1)
		<-ctx.Done()
		running.Add(-1)
	}
	go a.Lead(actx, "job", job)
	go b.Lead(bctx, "job", job)
	waitFor(t, "the job to run", func() bool { return running.Load() == 1 })
	time.Sleep(700 * time.Millisecond) // more than a lease: b keeps waiting while a renews
	if b.Held() || runs.Load() != 1 {
		t.Fatalf("b leads %v, runs %d: the lead must stay with a", b.Held(), runs.Load())
	}

	astop() // a stops cleanly and gives the lease up
	waitFor(t, "b to lead", b.Held)
	waitFor(t, "the job to run on b", func() bool { return runs.Load() == 2 && running.Load() == 1 })
	if both.Load() {
		t.Fatal("the job ran on both replicas at once")
	}
}

// A job that ends by itself is not started again while the lead stays.
func TestFinishedJobNotRestarted(t *testing.T) {
	db := openDB(t)
	a := fast(New(db, "a", nil, nil))
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go a.Run(ctx)
	waitFor(t, "a to lead", a.Held)
	var runs atomic.Int32
	go a.Lead(ctx, "once", func(context.Context) { runs.Add(1) })
	waitFor(t, "the job to run", func() bool { return runs.Load() == 1 })
	time.Sleep(1500 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("runs %d, want 1", n)
	}
}
