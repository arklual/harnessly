package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The replica socket: GET /api/v2/replica, read by the machine agent next door over loopback, on the page's
// session like the socket. It streams every event of the conversation channels (`convs`, `conv:<id>`) as it is published
// and, on request, the whole chat list — what the agent forwards to the cluster, which keeps the chats of the person
// whether this machine is on or not. It subscribes to no channel: a chat nobody has open stays unwatched, and a replica
// that falls behind is closed, not waited for (the agent reconnects and takes the chats again from their pages).
//
// Frames to the agent, JSON text:
//
//	{"k":"hello","epoch":"ep-…"}                      first; the epoch the seqs below belong to
//	{"k":"ev","ch":"conv:tab-x","seq":12,"e":{…}}     one event, the same as the "ev" frame of the socket
//	{"k":"convs","conversations":[…]}                 the answer to {"k":"convs"}: every chat's record
//
// Frames from the agent: {"k":"convs"} asks for the list; {"k":"ping"} is answered with {"k":"pong"}.
const (
	replicaQueue = 8192
	replicaIdle  = 60 * time.Second
)

type replicaEv struct {
	K   string          `json:"k"`
	Ch  Channel         `json:"ch"`
	Seq int64           `json:"seq"`
	E   json.RawMessage `json:"e"`
}

// Replica serves GET /api/v2/replica.
func (s *Server) Replica(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	defer ws.CloseNow()

	// What waits for the writer: a frame as the hub made it (parsed by the writer, not under the hub's lock) or one of the replica's own.
	type item struct {
		ch    Channel
		seq   int64
		frame []byte // the hub's frame; own is used when nil
		own   []byte
	}
	queue := make(chan item, replicaQueue)
	var closeOnce sync.Once
	end := make(chan struct{})
	finish := func() { closeOnce.Do(func() { close(end) }) }
	push := func(it item) {
		select {
		case queue <- it:
		default:
			finish() // too far behind: closed, not waited for
		}
	}
	own := func(b []byte) { push(item{own: b}) }
	// The epoch is read together with the tap, so the seqs that follow belong to it.
	s.Hub.mu.Lock()
	epoch := s.Hub.epoch
	s.Hub.mu.Unlock()
	hello, _ := json.Marshal(map[string]string{"k": "hello", "epoch": epoch})
	own(hello)
	stop := s.Hub.Tap(func(ch Channel, seq int64, frame []byte) { push(item{ch: ch, seq: seq, frame: frame}) }, finish)
	defer stop()

	go func() {
		defer cancel()
		for {
			var out []byte
			select {
			case <-ctx.Done():
				return
			case <-end:
				_ = ws.Close(websocket.StatusGoingAway, "epoch ended or reader too far behind")
				return
			case it := <-queue:
				out = it.own
				if it.frame != nil {
					// The frame is {"f":"ev","ch":…,"seq":…,"e":{…}}: the replica's own names the same parts.
					var f struct {
						E json.RawMessage `json:"e"`
					}
					if json.Unmarshal(it.frame, &f) != nil {
						continue
					}
					out, _ = json.Marshal(replicaEv{K: "ev", Ch: it.ch, Seq: it.seq, E: f.E})
				}
			}
			wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
			err := ws.Write(wctx, websocket.MessageText, out)
			wcancel()
			if err != nil {
				return
			}
		}
	}()

	for {
		rctx, rcancel := context.WithTimeout(ctx, replicaIdle)
		typ, b, err := ws.Read(rctx)
		rcancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var req struct {
			K string `json:"k"`
		}
		if json.Unmarshal(b, &req) != nil {
			continue
		}
		switch req.K {
		case "ping":
			own([]byte(`{"k":"pong"}`))
		case "convs":
			go s.replicaConvs(ctx, own)
		}
	}
}

// replicaConvs answers {"k":"convs"}: the records of all chats, as the list snapshot has them (not cut to the page's budget).
func (s *Server) replicaConvs(ctx context.Context, send func([]byte)) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	snap, err := s.Service.Conversations(ctx)
	if err != nil {
		jsonlog.Log("warn", "v2 replica: the chat list failed", jsonlog.F{K: "error", V: err.Error()})
		return
	}
	b, err := json.Marshal(map[string]any{"k": "convs", "conversations": snap.Conversations})
	if err != nil {
		return
	}
	send(b)
}
