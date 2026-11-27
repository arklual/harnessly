package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// fsTimeout is how long one file operation waits for the device.
const fsTimeout = 60 * time.Second

// FS runs one file operation on a device; a device that is offline or does not answer gives ENOTCONN or ETIMEDOUT.
func (s *Server) FS(ctx context.Context, device string, req *FSReq) *FSResp {
	s.init()
	s.mu.Lock()
	d := s.find(device)
	var l *srvLink
	if d != nil {
		l = s.links[d.ID]
	}
	if l == nil {
		s.mu.Unlock()
		return &FSResp{Errno: "ENOTCONN"}
	}
	id := s.nextSID.Add(1)
	ch := make(chan *FSResp, 1)
	s.fsWait[id] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.fsWait, id)
		s.mu.Unlock()
	}
	if !l.sendMsg(Msg{T: "fs", SID: strconv.FormatUint(id, 10), FS: req}) {
		forget()
		return &FSResp{Errno: "ENOTCONN"}
	}
	t := time.NewTimer(fsTimeout)
	defer t.Stop()
	select {
	case r := <-ch:
		return r
	case <-l.done:
		forget()
		return &FSResp{Errno: "ENOTCONN"}
	case <-t.C:
		forget()
		return &FSResp{Errno: "ETIMEDOUT"}
	case <-ctx.Done():
		forget()
		return &FSResp{Errno: "EINTR"}
	}
}

// ServeFS is the mount's socket (the caller checks the cli-token): many file operations at once, each {t:"fs", sid,
// device, fs} answered by {t:"fsr", sid, fsr}.
func (s *Server) ServeFS(w http.ResponseWriter, r *http.Request) {
	s.init()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var wmu sync.Mutex
	for {
		typ, b, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m Msg
		if typ != websocket.MessageText || json.Unmarshal(b, &m) != nil || m.T != "fs" || m.FS == nil {
			continue
		}
		go func() {
			resp := s.FS(ctx, m.Device, m.FS)
			out, _ := json.Marshal(Msg{T: "fsr", SID: m.SID, FSR: resp})
			wmu.Lock()
			_ = ws.Write(ctx, websocket.MessageText, out)
			wmu.Unlock()
		}()
	}
}
