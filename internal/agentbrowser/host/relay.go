package host

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

type client struct {
	conn     net.Conn
	frames   chan []byte
	done     chan struct{}
	once     sync.Once
	sessions map[string]bool // guarded by relay.mu
}

func (c *client) close() { c.once.Do(func() { close(c.done); _ = c.conn.Close() }) }

type request struct {
	client *client
	id     json.RawMessage
	method string
	// reply takes the answer to a call of the host's own (call); client is nil then.
	reply chan map[string]json.RawMessage
}

type relay struct {
	mu      sync.Mutex
	current *client
	chrome  *cdp.Stream
	next    uint32
	pending map[uint32]request
	closed  bool
	// alone is since when no client has been connected (zero while one is).
	alone time.Time

	// build is this host's build, answered to Host.hello: a runtime of another build replaces the host.
	build string
	// beforeClose runs before a client's Browser.close reaches Chrome (the session cookies are kept then).
	beforeClose func()
	// event takes the browser's own events (no session): the downloads.
	event func(method string, params json.RawMessage)
}

// HelloMethod is answered by the host itself, never by Chrome: {"build": "..."}. A host too old to know it answers
// with Chrome's "method not found", which is a different build too.
const HelloMethod = "Host.hello"

// refusal is the answer the relay gives in place of Chrome's to a command it does not let through.
func refusal(id json.RawMessage, message string) []byte {
	raw, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": message}})
	return raw
}

// screen is the relay's own answer to a client's command, or nil to pass it to Chrome. A page is opened only at an
// address AllowedNavigation lets through; nothing a client sends turns the egress, the download rule or a page's
// access to the protocol off; Host.hello is answered here.
//
// The params of a screened command go on as the relay read them (rewritten): with a key given twice, Chrome gets the
// value that was checked.
func (r *relay) screen(id json.RawMessage, method string, raw json.RawMessage) (answer, rewritten []byte) {
	if method == HelloMethod {
		answer, _ = json.Marshal(map[string]any{"id": id, "result": map[string]string{"build": r.build}})
		return answer, nil
	}
	if refused[method] {
		return refusal(id, method+" is not available in the agent's browser"), nil
	}
	if !screened[method] {
		return nil, nil
	}
	var params map[string]json.RawMessage
	if len(raw) > 0 && json.Unmarshal(raw, &params) != nil {
		return refusal(id, "invalid params"), nil
	}
	// Chrome reads keys as written; a key only a case apart could be read by one parser and not the other.
	seen := map[string]bool{}
	for k := range params {
		lower := strings.ToLower(k)
		if seen[lower] {
			return refusal(id, "duplicate parameter "+k), nil
		}
		seen[lower] = true
	}
	switch method {
	case "Page.navigate", "Target.createTarget":
		for k, v := range params {
			if !strings.EqualFold(k, "url") {
				continue
			}
			var u string
			if k != "url" || json.Unmarshal(v, &u) != nil || u != "" && !AllowedNavigation(u) {
				return refusal(id, "URL refused: the agent's browser opens http(s) addresses and about:blank only"), nil
			}
		}
	}
	if params == nil {
		return nil, nil
	}
	rewritten, _ = json.Marshal(params)
	return nil, rewritten
}

// screened are the commands whose params the relay reads.
var screened = map[string]bool{"Page.navigate": true, "Target.createTarget": true}

// refused are the commands no client may send: a page talking to the protocol, a session around the relay's
// screening (the non-flat mode), the download rule set by the host, a browser context (its own proxy, its own
// downloads; the runtime uses the default one).
var refused = map[string]bool{
	"Target.sendMessageToTarget": true, "Target.exposeDevToolsProtocol": true,
	"Browser.setDownloadBehavior": true, "Page.setDownloadBehavior": true, "Target.createBrowserContext": true,
}

// allocate gives a request the relay's next wire ID; r.mu is held.
func (r *relay) allocate(req request) uint32 {
	if r.pending == nil {
		r.pending = make(map[uint32]request)
	}
	// Chrome uses signed 32-bit IDs. The relay, not the reconnecting client, owns their namespace.
	for {
		r.next++
		if r.next >= 1<<31 {
			r.next = 1
		}
		if _, used := r.pending[r.next]; !used {
			break
		}
	}
	r.pending[r.next] = req
	return r.next
}

// call sends a command of the host's own to Chrome (the browser's session) and waits for its result.
func (r *relay) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reply := make(chan map[string]json.RawMessage, 1)
	r.mu.Lock()
	id := r.allocate(request{method: method, reply: reply})
	r.mu.Unlock()
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	raw, _ := json.Marshal(msg)
	forget := func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }
	if err := r.chrome.Write(ctx, raw); err != nil {
		forget()
		return nil, err
	}
	select {
	case answer := <-reply:
		if answer["error"] != nil {
			return nil, errors.New(string(answer["error"]))
		}
		return answer["result"], nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// idle is how long no client has been connected.
func (r *relay) idle(now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil || r.alone.IsZero() {
		return 0
	}
	return now.Sub(r.alone)
}

func (r *relay) detach(sessions []string) {
	for _, session := range sessions {
		// Detaching alone leaves a native modal dialog without a DevTools handler.
		// Cancel it first so a later client can attach and use the renderer again.
		dialog, _ := json.Marshal(map[string]any{"id": 0, "sessionId": session,
			"method": "Page.handleJavaScriptDialog", "params": map[string]bool{"accept": false}})
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		_ = r.chrome.Write(closeCtx, dialog)
		closeCancel()
		raw, _ := json.Marshal(map[string]any{"id": 0, "method": "Target.detachFromTarget",
			"params": map[string]string{"sessionId": session}})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = r.chrome.Write(ctx, raw)
		cancel()
	}
}

func (r *relay) disconnect(c *client) {
	c.close()
	r.mu.Lock()
	if r.current == c {
		r.current = nil
		r.alone = time.Now()
	}
	sessions := make([]string, 0, len(c.sessions))
	for session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = nil
	for id, req := range r.pending {
		if req.client == c && req.method != "Target.attachToTarget" {
			delete(r.pending, id)
		}
	}
	r.mu.Unlock()
	// Detach stops the old client's screencasts without closing any browser tabs.
	r.detach(sessions)
}

func (r *relay) replace(conn net.Conn) {
	c := &client{conn: conn, frames: make(chan []byte, 32), done: make(chan struct{}), sessions: map[string]bool{}}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = conn.Close()
		return
	}
	if r.current != nil {
		r.current.close()
	}
	r.current = c
	r.alone = time.Time{}
	r.mu.Unlock()
	stream := cdp.NewStream(conn)
	go func() {
		defer c.close()
		for {
			select {
			case <-c.done:
				return
			case raw := <-c.frames:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := stream.Write(ctx, raw)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()
	go func() {
		defer r.disconnect(c)
		for {
			raw, err := stream.Read(context.Background())
			if err != nil {
				return
			}
			var msg map[string]json.RawMessage
			if json.Unmarshal(raw, &msg) != nil || msg["id"] == nil {
				continue
			}
			var method string
			_ = json.Unmarshal(msg["method"], &method)
			answer, params := r.screen(msg["id"], method, msg["params"])
			if params != nil {
				msg["params"] = params
			}
			if answer != nil {
				select {
				case c.frames <- answer:
					continue
				case <-c.done:
					return
				}
			}
			if method == "Browser.close" && r.beforeClose != nil {
				r.beforeClose()
			}
			r.mu.Lock()
			if r.current != c {
				r.mu.Unlock()
				return
			}
			wireID := r.allocate(request{client: c, id: msg["id"], method: method})
			msg["id"], _ = json.Marshal(wireID)
			raw, _ = json.Marshal(msg)
			r.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = r.chrome.Write(ctx, raw)
			cancel()
			if err != nil {
				return
			}
		}
	}()
}

func (r *relay) read(ctx context.Context) error {
	for {
		raw, err := r.chrome.Read(ctx)
		if err != nil {
			return err
		}
		r.mu.Lock()
		c := r.current
		var msg map[string]json.RawMessage
		if json.Unmarshal(raw, &msg) == nil && msg["id"] != nil {
			var id uint32
			_ = json.Unmarshal(msg["id"], &id)
			req, known := r.pending[id]
			delete(r.pending, id)
			if !known {
				r.mu.Unlock()
				continue
			}
			if req.reply != nil {
				r.mu.Unlock()
				req.reply <- msg
				continue
			}
			active := req.client == c
			select {
			case <-req.client.done:
				active = false
			default:
			}
			if req.method == "Target.attachToTarget" {
				var result struct {
					Session string `json:"sessionId"`
				}
				_ = json.Unmarshal(msg["result"], &result)
				if result.Session != "" {
					if active {
						c.sessions[result.Session] = true
					} else {
						go r.detach([]string{result.Session})
					}
				}
			}
			if !active {
				r.mu.Unlock()
				continue
			}
			msg["id"] = req.id
			raw, _ = json.Marshal(msg)
		} else if msg != nil && msg["sessionId"] == nil && r.event != nil {
			var method string
			_ = json.Unmarshal(msg["method"], &method)
			if strings.HasPrefix(method, "Browser.download") {
				go r.event(method, msg["params"])
			}
		}
		r.mu.Unlock()
		// Backpressure preserves bursts; replacement and write deadlines can still close this client.
		if c != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.done:
			case c.frames <- raw:
			}
		}
	}
}
