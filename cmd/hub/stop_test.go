package main

import (
	"context"
	"slices"
	"testing"
	"time"
)

func recordingSteps(settle, window time.Duration, order *[]string) stopSteps {
	add := func(step string) { *order = append(*order, step) }
	return stopSteps{
		grace: time.Minute, settle: settle, window: window,
		unready:        func() { add("unready") },
		wait:           func(_ context.Context, d time.Duration) { add("settle " + d.String()) },
		drainLinks:     func(_ context.Context, w time.Duration) { add("links " + w.String()) },
		stopBackground: func() { add("background") },
		closeStreams:   func() { add("streams") },
		shutdown: func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); !ok {
				add("shutdown without a deadline")
				return nil
			}
			add("shutdown")
			return nil
		},
	}
}

// A stop first takes the hub out of rotation, lets the balancer hear it, moves the machines away and only then closes
// what is left and waits for the requests under way.
func TestStopDrainsBeforeItCloses(t *testing.T) {
	var order []string
	if err := stopHub(recordingSteps(5*time.Second, 10*time.Second, &order)); err != nil {
		t.Fatal(err)
	}
	want := []string{"unready", "settle 5s", "links 10s", "background", "streams", "shutdown"}
	if !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}

// A single process has nowhere to send its machines: it stops at once, as it did, only saying first it is not ready.
func TestStopOfASingleProcessDoesNotWait(t *testing.T) {
	var order []string
	if err := stopHub(recordingSteps(0, 0, &order)); err != nil {
		t.Fatal(err)
	}
	want := []string{"unready", "background", "streams", "shutdown"}
	if !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}
