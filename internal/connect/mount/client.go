// Package mount shows a folder of a Harnessly Connect device as a folder of the runtime's machine (FUSE), so the
// agent's own file tools (Read, Edit, Grep) work on the device's files at the same path.
package mount

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/connect"
)

// Client sends file operations to the runtime on one socket, redialled when it drops.
type Client struct {
	Base, Token, Header, Device string

	mu      sync.Mutex
	ws      *websocket.Conn
	wmu     sync.Mutex
	wait    map[uint64]chan *connect.FSResp
	next    atomic.Uint64
	dialing sync.Mutex
}

func (c *Client) conn(ctx context.Context) (*websocket.Conn, error) {
	c.dialing.Lock()
	defer c.dialing.Unlock()
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()
	if ws != nil {
		return ws, nil
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(dctx, strings.Replace(c.Base, "http", "ws", 1)+"/api/cli/connect/fs", &websocket.DialOptions{
		HTTPHeader: http.Header{c.Header: {c.Token}}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(64 << 20)
	c.mu.Lock()
	c.ws = ws
	if c.wait == nil {
		c.wait = map[uint64]chan *connect.FSResp{}
	}
	c.mu.Unlock()
	go c.reader(ws)
	return ws, nil
}

func (c *Client) reader(ws *websocket.Conn) {
	for {
		_, b, err := ws.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			if c.ws == ws {
				c.ws = nil
			}
			for id, ch := range c.wait {
				ch <- &connect.FSResp{Errno: "ENOTCONN"}
				delete(c.wait, id)
			}
			c.mu.Unlock()
			ws.CloseNow()
			return
		}
		var m connect.Msg
		if json.Unmarshal(b, &m) != nil || m.FSR == nil {
			continue
		}
		id, _ := strconv.ParseUint(m.SID, 10, 64)
		c.mu.Lock()
		ch := c.wait[id]
		delete(c.wait, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- m.FSR
		}
	}
}

// Do runs one operation on the device.
func (c *Client) Do(req *connect.FSReq) *connect.FSResp {
	ctx := context.Background()
	ws, err := c.conn(ctx)
	if err != nil {
		return &connect.FSResp{Errno: "ENOTCONN"}
	}
	id := c.next.Add(1)
	ch := make(chan *connect.FSResp, 1)
	c.mu.Lock()
	c.wait[id] = ch
	c.mu.Unlock()
	b, _ := json.Marshal(connect.Msg{T: "fs", SID: strconv.FormatUint(id, 10), Device: c.Device, FS: req})
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	c.wmu.Lock()
	err = ws.Write(wctx, websocket.MessageText, b)
	c.wmu.Unlock()
	cancel()
	if err != nil {
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		ws.CloseNow()
		return &connect.FSResp{Errno: "ENOTCONN"}
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(90 * time.Second):
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		return &connect.FSResp{Errno: "ETIMEDOUT"}
	}
}
