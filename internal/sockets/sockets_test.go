package sockets

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type fakeSource struct {
	mu        sync.Mutex
	limits    any
	dismissed []string
	excluded  []bool
	acks      []float64
	applied   [][]string
	warned    []string
}

func (f *fakeSource) RateLimits() any { f.mu.Lock(); defer f.mu.Unlock(); return f.limits }
func (f *fakeSource) Tabs() any       { return map[string]any{"tab-a": map[string]any{"cliState": "idle"}} }
func (f *fakeSource) Background() any { return map[string]any{} }
func (f *fakeSource) Queues() any     { return map[string]any{} }
func (f *fakeSource) Intents() any    { return map[string]any{} }
func (f *fakeSource) History() any    { return []any{map[string]any{"id": "h1"}} }
func (f *fakeSource) Dismiss(tabID string, exclude *Client) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dismissed = append(f.dismissed, tabID)
	f.excluded = append(f.excluded, exclude != nil)
}
func (f *fakeSource) Ack(tabID string, seq float64) {
	f.mu.Lock()
	f.acks = append(f.acks, seq)
	f.mu.Unlock()
}
func (f *fakeSource) IntentsApplied(tabID string, ids []string, device string) {
	f.mu.Lock()
	f.applied = append(f.applied, append([]string{tabID, device}, ids...))
	f.mu.Unlock()
}
func (f *fakeSource) Warn(msg string) { f.mu.Lock(); f.warned = append(f.warned, msg); f.mu.Unlock() }

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(4 << 20)
	return c
}

func read(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

func types(t *testing.T, c *websocket.Conn, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, read(t, c)["type"].(string))
	}
	return out
}

func write(t *testing.T, c *websocket.Conn, s string) {
	if err := c.Write(context.Background(), websocket.MessageText, []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func TestStatusSnapshotOrderAndMessages(t *testing.T) {
	src := &fakeSource{limits: map[string]any{"ts": 1.0}}
	s := &StatusServer{Hub: NewHub(), Source: src}
	srv := httptest.NewServer(s)
	defer srv.Close()
	page := dial(t, srv.URL+"/api/status")
	defer page.CloseNow()
	got := types(t, page, 5)
	if strings.Join(got[:3], ",") != "rate-limits:update,status:sync,background:sync" {
		t.Fatalf("connect order %v", got)
	}
	write(t, page, PingFrame)
	if m := read(t, page); m["type"] != "pong" {
		t.Fatalf("%v", m)
	}
	write(t, page, `{"type":"status:request-sync"}`)
	if got := types(t, page, 5); strings.Join(got[:3], ",") != "status:sync,background:sync,rate-limits:update" {
		t.Fatalf("request-sync order %v", got)
	}
	write(t, page, `{"type":"session-history:request"}`)
	if m := read(t, page); m["type"] != "session-history:sync" {
		t.Fatalf("%v", m)
	}
	relay := dial(t, srv.URL+"/api/status?relay=1")
	defer relay.CloseNow()
	types(t, relay, 5)
	write(t, page, `{"type":"status:tab-dismissed","tabId":"tab-a"}`)
	write(t, relay, `{"type":"status:tab-dismissed","tabId":"tab-b"}`)
	write(t, page, `{"type":"status:ack-notification","tabId":"tab-a","seq":3}`)
	write(t, page, `{"type":"pane-intents:applied","tabId":"tab-a","ids":["0b8f5c2e-1d2a-4e6f-9a7b-3c4d5e6f7a8b","NOT"],"device":"phone"}`)
	write(t, page, `{"type":"pane-intents:applied","tabId":"tab a","ids":["0b8f5c2e-1d2a-4e6f-9a7b-3c4d5e6f7a8b"],"device":"phone"}`)
	write(t, page, `{"type":"nope"}`)
	write(t, page, `not json`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		src.mu.Lock()
		done := len(src.warned) == 1 && len(src.dismissed) == 2
		src.mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.dismissed) != 2 || !src.excluded[0] && !src.excluded[1] || src.excluded[0] == src.excluded[1] {
		t.Fatalf("dismissals %v excluded %v", src.dismissed, src.excluded)
	}
	for i, tab := range src.dismissed {
		if (tab == "tab-a") != src.excluded[i] {
			t.Fatalf("a page's dismissal must exclude it, the relay's must not: %v %v", src.dismissed, src.excluded)
		}
	}
	if len(src.acks) != 1 || src.acks[0] != 3 {
		t.Fatalf("acks %v", src.acks)
	}
	if len(src.applied) != 1 || strings.Join(src.applied[0], " ") != "tab-a phone 0b8f5c2e-1d2a-4e6f-9a7b-3c4d5e6f7a8b" {
		t.Fatalf("applied %v", src.applied)
	}
}

func TestASlowClientMissesMessagesThenGetsASnapshot(t *testing.T) {
	c := &Client{wake: make(chan struct{}, 1), done: make(chan struct{})}
	big := []byte(strings.Repeat("x", backpressure))
	if !c.Send(big) {
		t.Fatal("the first message must be queued")
	}
	if c.Send([]byte("dropped")) {
		t.Fatal("a client a megabyte behind must miss the message")
	}
	resynced := make(chan struct{}, 1)
	c.resync = func(*Client) { resynced <- struct{}{} }
	c.mu.Lock()
	c.queue, c.queued = nil, 0
	c.mu.Unlock() // as if the big message went out; its wake-up is still pending
	go c.writer()
	defer c.Close(websocket.StatusNormalClosure, "")
	select {
	case <-resynced:
	case <-time.After(5 * time.Second):
		t.Fatal("a client that missed messages got no snapshot once it drained")
	}
}

func TestSyncReplaysToastsAndBroadcasts(t *testing.T) {
	s := &SyncServer{Hub: NewHub()}
	srv := httptest.NewServer(s)
	defer srv.Close()
	s.Toast("a", map[string]any{"type": "system-toast", "key": "a", "message": "1"})
	s.Toast("b", map[string]any{"type": "system-toast", "key": "b", "message": "2"})
	s.Toast("a", map[string]any{"type": "system-toast", "key": "a", "message": "3"})
	c := dial(t, srv.URL)
	defer c.CloseNow()
	if a, b := read(t, c), read(t, c); a["message"] != "3" || b["message"] != "2" {
		t.Fatalf("toasts replayed as %v %v", a, b)
	}
	for s.Hub.Count() != 1 {
		time.Sleep(5 * time.Millisecond)
	}
	s.Broadcast(map[string]any{"type": "layout", "workspaceId": "ws-1"})
	if m := read(t, c); m["type"] != "layout" {
		t.Fatalf("%v", m)
	}
	write(t, c, PingFrame)
	if m := read(t, c); m["type"] != "pong" {
		t.Fatalf("%v", m)
	}
}
