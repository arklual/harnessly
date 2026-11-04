package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestStreamCallsAndUnknownReplies(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	events := make(chan string, 1)
	c := New(NewStream(a), func(method, session string, _ json.RawMessage) { events <- method + ":" + session })
	defer c.Close()
	c.next = 100
	go func() { _ = c.Run(t.Context()) }()
	peer := NewStream(b)
	go func() {
		raw, err := peer.Read(t.Context())
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(raw, &msg)
		_ = peer.Write(t.Context(), []byte(`{"id":1,"result":{"wrong":true}}`))
		_ = peer.Write(t.Context(), []byte(`{"method":"Page.event","sessionId":"flat"}`))
		reply, _ := json.Marshal(map[string]any{"id": msg["id"], "result": map[string]any{"ok": true}})
		// Exercise a record split across writes, including its final delimiter.
		for _, part := range [][]byte{reply[:5], reply[5:], {0}} {
			_, _ = b.Write(part)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	raw, err := c.Call(ctx, "Page.test", map[string]any{}, "flat")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatal(string(raw))
	}
	select {
	case e := <-events:
		if e != "Page.event:flat" {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("no event")
	}
}

func TestCancellationInterruptsBlockedWrite(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := New(NewStream(a), nil)
	defer c.Close()
	go func() { _ = c.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "blocked", nil, ""); err == nil {
		t.Fatal("blocked call succeeded")
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not close partial stream")
	}
}

func TestDialWebSocket(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws" + server.URL[4:] + "/browser"})
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		_, raw, err := ws.Read(r.Context())
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(raw, &msg)
		msg["result"] = map[string]any{"product": "Chrome"}
		raw, _ = json.Marshal(msg)
		_ = ws.Write(r.Context(), websocket.MessageText, raw)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	c, err := Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() { _ = c.Run(ctx) }()
	raw, err := c.Call(ctx, "Browser.getVersion", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"product":"Chrome"}` {
		t.Fatal(string(raw))
	}
}

func TestCloseUnblocksPending(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := New(NewStream(a), nil)
	go func() { _ = c.Run(t.Context()) }()
	peer := NewStream(b)
	go func() { _, _ = peer.Read(t.Context()); _ = b.Close() }()
	_, err := c.Call(t.Context(), "waiting", nil, "")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestCancellationFinishesRecordWithoutClosingConnection(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := New(NewStream(a), nil)
	defer c.Close()
	go func() { _ = c.Run(t.Context()) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "Page.canceled", nil, ""); result <- err }()
	first := make([]byte, 5)
	if _, err := io.ReadFull(b, first); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled caller blocked on transport")
	}
	peer := NewStream(b)
	rest, err := peer.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(append(first, rest...)) {
		t.Fatal("canceled command left an incomplete record")
	}
	go func() {
		raw, err := peer.Read(t.Context())
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(raw, &msg)
		reply, _ := json.Marshal(map[string]any{"id": msg["id"], "result": map[string]bool{"ok": true}})
		_ = peer.Write(t.Context(), reply)
	}()
	if _, err = c.Call(t.Context(), "Browser.getVersion", nil, ""); err != nil {
		t.Fatal(err)
	}
	t.Log("canceled caller returned promptly; full record and subsequent call survived")
}

func TestDefaultDeadlineAndFireAndForget(t *testing.T) {
	ctx, cancel := WithTimeout(t.Context())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > DefaultTimeout || time.Until(deadline) < 9*time.Second {
		t.Fatal("missing default deadline")
	}
	shorter, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	bounded, release := WithTimeout(shorter)
	defer release()
	want, _ := shorter.Deadline()
	if got, _ := bounded.Deadline(); got != want {
		t.Fatal("caller deadline changed")
	}
	a, b := net.Pipe()
	defer b.Close()
	c := New(NewStream(a), nil)
	defer c.Close()
	received := make(chan error, 1)
	go func() { _, err := NewStream(b).Read(ctx); received <- err }()
	if err := c.Send(ctx, "Page.screencastFrameAck", map[string]int{"sessionId": 1}, "session"); err != nil {
		t.Fatal(err)
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}
	t.Log("default deadline is 10s; explicit deadline preserved; acknowledgement did not wait for a reply")
}
