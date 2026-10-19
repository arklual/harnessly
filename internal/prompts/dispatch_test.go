package prompts

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistryWindowAcrossRoutes(t *testing.T) {
	clock := newFakeClock()
	var logged []string
	reg := NewRegistry(clock, func(level, msg string, kv ...any) { logged = append(logged, level+":"+msg) })
	if !reg.Claim("m1", RouteComposer, "tab-1", "Сделай отчёт") {
		t.Fatal("first claim refused")
	}
	clock.Advance(12 * time.Second)
	if reg.Claim("m1", RoutePromptText, "tab-1", "Сделай отчёт") {
		t.Fatal("the same id went in twice within the window")
	}
	if !reg.Claim("m2", RouteComposer, "tab-1", "Сделай отчёт") {
		t.Fatal("another id refused")
	}
	clock.Advance(DuplicateWindow - 12*time.Second)
	if !reg.Claim("m1", RouteComposer, "tab-1", "Сделай отчёт") {
		t.Fatal("the id is still refused after the window")
	}
	if len(logged) != 4 || !strings.HasPrefix(logged[1], "warn:duplicate prompt refused") || logged[0] != "info:prompt composer → tab-1" {
		t.Errorf("log: %v", logged)
	}
}

func TestRegistryNoIDAndRelease(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	if !reg.Claim("", RouteQueue, "tab-1", "x") || !reg.Claim("", RouteQueue, "tab-1", "x") {
		t.Fatal("a message without an id was refused")
	}
	if !reg.Claim("q1", RouteQueue, "tab-1", "x") {
		t.Fatal("q1")
	}
	reg.Release("q1")
	if !reg.Claim("q1", RouteQueue, "tab-1", "x") {
		t.Fatal("a released id is still refused")
	}
}

func TestComposerClaimCutsTheID(t *testing.T) {
	r := newRig(t, "", Tab{})
	long := strings.Repeat("a", 80)
	if !r.svc.ClaimPrompt(long, testSession, "x") || r.svc.ClaimPrompt(long[:64]+"zzz", testSession, "x") {
		t.Fatal("composer ids are not cut to 64 characters")
	}
}

func TestEnterClock(t *testing.T) {
	clock := newFakeClock()
	e := NewEnterClock(clock)
	if _, ok := e.Take("s"); ok {
		t.Fatal("an Enter never pressed")
	}
	e.Note("s")
	clock.Advance(1200 * time.Millisecond)
	if ms, ok := e.Take("s"); !ok || ms != 1200 {
		t.Fatalf("got %d %v", ms, ok)
	}
	if _, ok := e.Take("s"); ok {
		t.Fatal("taken twice")
	}
	e.Note("s")
	clock.Advance(EnterFresh + time.Millisecond)
	if _, ok := e.Take("s"); ok {
		t.Fatal("a stale Enter was taken")
	}
}

func TestOneJobPerTab(t *testing.T) {
	f := newFlights()
	release := make(chan struct{})
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f.run("tab-a", func() { close(started); <-release })
	}()
	<-started
	if f.run("tab-a", func() { t.Error("second job ran") }) {
		t.Fatal("a second job for the tab was let in")
	}
	ran := false
	if !f.run("tab-b", func() { ran = true }) || !ran {
		t.Fatal("another tab was refused")
	}
	close(release)
	wg.Wait()
	if !f.run("tab-a", func() {}) {
		t.Fatal("the tab is still taken")
	}
	func() {
		defer func() { _ = recover() }()
		f.run("tab-c", func() { panic("tmux") })
	}()
	if !f.run("tab-c", func() {}) {
		t.Fatal("a failed job did not free the tab")
	}
}
