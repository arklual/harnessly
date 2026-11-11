package storelink

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

// A machine that is not in a space yet is refused a store session. The refusal is kept and told once: the hub is not
// asked by every loop every few seconds, but again after a growing pause, and the machine takes the session as soon as
// the hub gives one.
func TestRefusedSessionIsAskedAgainWithGrowingPauses(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	var asked atomic.Int32
	var connected atomic.Bool
	hc := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		asked.Add(1)
		if !connected.Load() {
			return reply(http.StatusForbidden, ""), nil
		}
		return reply(http.StatusOK, `{"token":"tok","path":"/store","expires_at":"2099-01-01T00:00:00Z"}`), nil
	})}
	clock := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	s := &sessions{hc: hc, machine: Machine{ID: "m1", DialURL: "wss://hub.example.org/agent/v1", Key: key}, now: func() time.Time { return clock }}
	ctx := context.Background()

	for range 20 { // many loops, one clock tick: the hub is asked once
		if _, err := s.token(ctx, false); !errors.Is(err, ErrNotConnected) {
			t.Fatalf("err = %v, want ErrNotConnected", err)
		}
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("the hub was asked %d times in a row, want 1", n)
	}

	clock = clock.Add(denyFirst + time.Second) // the first pause is over: asked again, the next pause is longer
	_, _ = s.token(ctx, false)
	if n := asked.Load(); n != 2 {
		t.Fatalf("after the first pause the hub was asked %d times, want 2", n)
	}
	if got := s.retryIn(); got <= denyFirst {
		t.Fatalf("the second pause is %v, want longer than %v", got, denyFirst)
	}

	connected.Store(true) // the machine is connected to a space
	clock = clock.Add(denyMax + time.Second)
	tok, err := s.token(ctx, false)
	if err != nil || tok != "tok" {
		t.Fatalf("token = %q, %v after the connection, want tok", tok, err)
	}
	if s.retryIn() != 0 {
		t.Fatalf("a connected machine still waits %v", s.retryIn())
	}
}
