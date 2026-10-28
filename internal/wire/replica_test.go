package wire_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type replicaFrame struct {
	K             string            `json:"k"`
	Epoch         string            `json:"epoch"`
	Ch            string            `json:"ch"`
	Seq           int64             `json:"seq"`
	E             json.RawMessage   `json:"e"`
	Conversations []json.RawMessage `json:"conversations"`
}

func (g *rig) replica() *websocket.Conn {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(g.http.URL, "http")+"/api/v2/replica", nil)
	if err != nil {
		g.t.Fatal(err)
	}
	ws.SetReadLimit(8 << 20)
	g.t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}

func replicaRead(t *testing.T, ws *websocket.Conn) replicaFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f replicaFrame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return f
}

// The replica names the epoch, then carries what the chats publish with its seq, and the chat list when asked.
func TestReplicaCarriesTheEventsOfTheChannelsAndTheChatList(t *testing.T) {
	g := newRig(t, 5*time.Millisecond)
	ws := g.replica()
	hello := replicaRead(t, ws)
	if hello.K != "hello" || hello.Epoch != g.hub.Epoch() {
		t.Fatalf("hello %+v", hello)
	}
	ctx := context.Background()
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"k":"convs"}`)); err != nil {
		t.Fatal(err)
	}
	list := replicaRead(t, ws)
	if list.K != "convs" || len(list.Conversations) == 0 {
		t.Fatalf("list %+v", list)
	}

	seq := g.hub.Publish(wire.ChannelConvs, wire.ConversationSeenEv{ID: "tab-s4Qrea", SeenAt: 5, Rev: 5})
	ev := replicaRead(t, ws)
	if ev.K != "ev" || ev.Ch != "convs" || ev.Seq != seq {
		t.Fatalf("event %+v", ev)
	}
	var e struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(ev.E, &e); err != nil || e.Type != "conversation_seen" || e.ID != "tab-s4Qrea" {
		t.Fatalf("body %s %v", ev.E, err)
	}
	if n := g.hub.Subscribers(wire.ConvChannel("tab-s4Qrea")); n != 0 {
		t.Fatalf("the replica holds a chat as a page does: %d", n)
	}
	g.hub.Publish(wire.ChannelMachine, wire.ConversationSeenEv{ID: "x", SeenAt: 1, Rev: 1}) // dropped by the hub: not a machine event
}

// A new epoch ends the replica: the seqs it carried no longer mean anything, and the agent comes back for a new hello.
func TestReplicaEndsWithItsEpoch(t *testing.T) {
	g := newRig(t, 5*time.Millisecond)
	ws := g.replica()
	replicaRead(t, ws)
	g.hub.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := ws.Read(ctx); err == nil {
		t.Fatal("the replica went on over a new epoch")
	}
}
