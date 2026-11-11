package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The keeper's view is gone through when something asks for it, and by the clock only as a rare catch-all: with
// nothing asking, a pass every syncEvery, not every two seconds.
func TestSyncPassesRunOnAskAndSeldomByTheClock(t *testing.T) {
	if syncEvery < 30*time.Second {
		t.Fatalf("the sync passes by the clock every %v", syncEvery)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kick := make(chan struct{}, 1)
	var passes atomic.Int32
	done := make(chan struct{})
	go func() {
		syncPasses(ctx, kick, time.Hour, time.Millisecond, func() { passes.Add(1) })
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	if n := passes.Load(); n != 0 {
		t.Fatalf("%d passes with nothing asking", n)
	}
	kick <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for passes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := passes.Load(); n != 1 {
		t.Fatalf("an ask made %d passes", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the passes did not end with their context")
	}
}

// By the clock alone the passes still come, so what the keeper found by itself is not lost.
func TestSyncPassesComeByTheClock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var passes atomic.Int32
	go syncPasses(ctx, make(chan struct{}), 10*time.Millisecond, time.Millisecond, func() { passes.Add(1) })
	deadline := time.Now().Add(5 * time.Second)
	for passes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if passes.Load() < 2 {
		t.Fatal("no pass by the clock")
	}
}
