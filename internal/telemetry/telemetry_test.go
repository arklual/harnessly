package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The previous runtime's batchBody for the same event (its module run under node).
const previousBody = `{"api_key":"key-1","batch":[{"event":"platform_status_mismatch","timestamp":"2026-10-19T09:00:00.123Z","properties":{"app":"ai","$lib":"status","stand":false,"release":"9.9.9","runtime_commit":null,"host":"box","kind":"stuck-settled","shown":"busy","actual":null,"provider":"claude","duration_ms":1500,"unhooked":false,"distinct_id":"ann@example.org","$process_person_profile":false}}]}`

func event() Event {
	return Event{Name: "platform_status_mismatch", At: time.UnixMilli(1792400400123),
		Properties: jsvalue.NewObject("kind", "stuck-settled", "shown", "busy", "actual", nil, "provider", "claude", "duration_ms", 1500.0, "unhooked", false)}
}

func common() *jsvalue.Object {
	return jsvalue.NewObject("app", "ai", "$lib", "status", "stand", false, "release", "9.9.9", "runtime_commit", nil, "host", "box")
}

func TestBodyAsThePreviousRuntime(t *testing.T) {
	if got := string(Body("key-1", "ann@example.org", common(), []Event{event()})); got != previousBody {
		t.Fatalf("\n%s\n%s", got, previousBody)
	}
}

// Events wait for the delay and go in batches of at most MaxEvents; a full batch goes at once.
func TestQueue(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
	}))
	defer srv.Close()
	q := New(Config{URL: srv.URL, Key: "key-1", Common: common(), DistinctID: func() string { return "ann@example.org" }, Delay: 50 * time.Millisecond})
	q.Push(event())
	wait := func(n int) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			mu.Lock()
			got := len(bodies)
			mu.Unlock()
			if got >= n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d requests, want %d", got, n)
			}
		}
	}
	wait(1)
	if bodies[0] != previousBody {
		t.Fatal(bodies[0])
	}
	q.cfg.Delay = time.Hour
	for i := 0; i < MaxEvents; i++ {
		q.Push(event())
	}
	wait(2)
	if n := strings.Count(bodies[1], `"event":`); n != MaxEvents {
		t.Fatalf("a full batch carries %d events", n)
	}
	if (&Queue{}).Enabled() || New(Config{}).Enabled() {
		t.Fatal("no endpoint, nothing sent")
	}
}
