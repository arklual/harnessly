package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// The hub's error-level lines go as $exception with the error scrubbed and the route as a property; info lines and
// the configuration's warnings stay in the log only, and the log itself keeps every line.
func TestHubErrorsReported(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer srv.Close()

	var out bytes.Buffer
	q := telemetry.New(telemetry.Config{URL: srv.URL, Key: "k", Delay: time.Hour, DistinctID: func() string { return "id" }})
	log := slog.New(&reportHandler{next: slog.NewTextHandler(&out, nil), rep: &telemetry.Reporter{Queue: q}})

	log.Info("hub started")
	log.Error("config warning", notReported)
	log.With("method", "GET").Error("request failed", "route", "/api/v2/chats/{id}",
		"error", errors.New("open /home/ann/data/x.db: permission denied"))
	q.Flush() // sends in the background

	if n := strings.Count(out.String(), "\n"); n != 3 {
		t.Fatalf("log lines = %d, want 3:\n%s", n, out.String())
	}
	var all string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		all = strings.Join(bodies, "\n")
		mu.Unlock()
		if all != "" {
			break
		}
	}
	if strings.Count(all, `"$exception"`) != 1 {
		t.Fatalf("want one $exception, got: %s", all)
	}
	for _, want := range []string{`"hub.http"`, `request failed: open ~/data/x.db: permission denied`, `"route":"/api/v2/chats/{id}"`, `"method":"GET"`} {
		if !strings.Contains(all, want) {
			t.Errorf("batch lacks %s: %s", want, all)
		}
	}
	for _, leak := range []string{"ann", "config warning", "hub started"} {
		if strings.Contains(all, leak) {
			t.Errorf("batch carries %q: %s", leak, all)
		}
	}
}

// Without a target (a build from a checkout) the hub's log is left as it was; HARNESSLY_TELEMETRY_URL sets one.
func TestHubTelemetryTarget(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if got := withHubTelemetry(context.Background(), cfg, log, getenv); got != log {
		t.Fatal("no target: the log must stay as it was")
	}
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
	}))
	defer server.Close()
	env["HARNESSLY_TELEMETRY_URL"] = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got := withHubTelemetry(ctx, cfg, log, getenv); got == log {
		t.Fatal("a target set: the hub's errors must be reported")
	}
	cancel()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("the final heartbeat was not flushed")
	}
}
