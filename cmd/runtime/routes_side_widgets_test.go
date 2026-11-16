package main

import (
	"testing"
	"time"
)

// a smart widget is answered at most once a minute, and all of them together at most sideSmartHour an hour.
func TestSideSmartGuardHoldsItsFloor(t *testing.T) {
	g := &sideSmartGuard{}
	t0 := time.Unix(1_800_000_000, 0)
	if ok, _ := g.take("a", t0); !ok {
		t.Fatal("the first call is refused")
	}
	if ok, at := g.take("a", t0.Add(10*time.Second)); ok || !at.Equal(t0.Add(sideSmartFloor)) {
		t.Fatalf("a second call within the floor: ok=%v at=%v", ok, at)
	}
	if ok, _ := g.take("b", t0.Add(10*time.Second)); !ok {
		t.Fatal("another widget is held by the first one's floor")
	}
	for i := 0; i < sideSmartHour; i++ {
		g.take(string(rune('c'+i%20))+time.Duration(i).String(), t0.Add(time.Duration(i+20)*time.Second))
	}
	if ok, _ := g.take("z", t0.Add(30*time.Minute)); ok {
		t.Fatal("the hour's limit lets one more through")
	}
	if ok, _ := g.take("z", t0.Add(2*time.Hour)); !ok {
		t.Fatal("the hour is over and the call is still refused")
	}
}
