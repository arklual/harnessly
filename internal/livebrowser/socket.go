package livebrowser

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The pane's socket, /api/browser/live?tab=<id>: the page's pictures come as binary messages (JPEG), everything else
// as JSON text both ways.
//
//	page → machine: {t:"open", url, w, h, dpr, ack}  the first message: the tab's page, made when there is none;
//	                ack: true says the pane answers each picture it drew with {t:"ack"} (HRN-711)
//	                {t:"nav", url} {t:"back"} {t:"fwd"} {t:"reload"} {t:"size", w, h, dpr} {t:"cast", on}
//	                {t:"mouse", k:"down"|"up"|"move", x, y, b, bs, m, c} {t:"wheel", x, y, dx, dy, m}
//	                {t:"key", k:"down"|"up", key, code, text, vk, m} {t:"text", text} {t:"copy"}
//	                {t:"act", id, op, ref, text, key, value, dy, ms, submit, event, host}  the agent's action (act.go)
//	machine → page: {t:"ready"} {t:"page", url, title, canBack, canForward, loading} {t:"clip", text}
//	                {t:"acted", id, ok, text, rect}  {t:"error", text} {t:"end", why}

const (
	socketReadLimit = 64 << 10
	maxTabID        = 80
	maxTextIn       = 64 << 10
	// A frame the socket cannot send within this time is dropped: the next one is newer anyway.
	frameWrite = 3 * time.Second
)

type inbound struct {
	T    string  `json:"t"`
	URL  string  `json:"url"`
	W    int     `json:"w"`
	H    int     `json:"h"`
	DPR  float64 `json:"dpr"`
	On   bool    `json:"on"`
	Ack  bool    `json:"ack"`
	K    string  `json:"k"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	B    string  `json:"b"`
	BS   int     `json:"bs"`
	M    int     `json:"m"`
	C    int     `json:"c"`
	DX   float64 `json:"dx"`
	DY   float64 `json:"dy"`
	Key  string  `json:"key"`
	Code string  `json:"code"`
	Text string  `json:"text"`
	VK   int     `json:"vk"`
	// An action's fields (t: act).
	ID     string `json:"id"`
	Op     string `json:"op"`
	Ref    string `json:"ref"`
	Value  string `json:"value"`
	MS     int    `json:"ms"`
	Submit bool   `json:"submit"`
	Event  string `json:"event"`
	Host   string `json:"host"`
}

const (
	maxActID    = 64
	maxActText  = 4000
	maxActKey   = 32
	maxActValue = 500
	maxActDY    = 20000
	maxActMS    = 10000
	// A wait for a video's event (ended, playing, paused) may take longer than one for a text.
	maxActEventMS = 25000
)

// Socket serves the pane's connection to a tab.
type Socket struct {
	S   *Service
	Log func(level, msg string, kv ...any)
}

func (sk *Socket) log(level, msg string, kv ...any) {
	if sk.Log != nil {
		sk.Log(level, msg, kv...)
	}
}

// ServeHTTP upgrades an authenticated request and drives the tab until either side closes.
func (sk *Socket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sameorigin.Refuse(w, r) {
		return
	}
	id := r.URL.Query().Get("tab")
	if id == "" || len(id) > maxTabID {
		http.Error(w, "tab is required", http.StatusBadRequest)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(socketReadLimit)
	c := &paneConn{sk: sk, ws: ws, id: id, out: make(chan []byte, 16), inputReady: make(chan struct{}, 1)}
	c.run(r.Context())
}

type paneConn struct {
	sk  *Socket
	ws  *websocket.Conn
	id  string
	tab *Tab
	out chan []byte
	// The person's input, in order, run beside the socket's reading (HRN-711).
	imu        sync.Mutex
	input      []inbound
	inputReady chan struct{}
}

const maxInputQueue = 256

func (c *paneConn) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.out <- raw:
	default:
	}
}

func (c *paneConn) fail(text string) { c.send(map[string]any{"t": "error", "text": text}) }

func (c *paneConn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = c.ws.Close(websocket.StatusNormalClosure, "") }()
	// The first message opens the tab.
	ctxOpen, cancelOpen := context.WithTimeout(ctx, 30*time.Second)
	_, data, err := c.ws.Read(ctxOpen)
	cancelOpen()
	if err != nil {
		return
	}
	var first inbound
	if json.Unmarshal(data, &first) != nil || first.T != "open" {
		_ = c.ws.Close(websocket.StatusPolicyViolation, "open first")
		return
	}
	fresh := c.sk.S.has(c.id) == nil
	tab, err := c.sk.S.Open(ctx, c.id, first.W, first.H, first.DPR)
	if err != nil {
		c.sk.log("warn", "live browser: open failed", "error", err.Error())
		c.writeNow(map[string]any{"t": "error", "text": err.Error()})
		c.writeNow(map[string]any{"t": "end", "why": "failed"})
		return
	}
	c.tab = tab
	tab.Paced(first.Ack)
	go c.writer(ctx)
	go c.inputs(ctx)
	c.send(map[string]any{"t": "ready"})
	if !fresh {
		if err := tab.Resize(first.W, first.H, first.DPR); err != nil {
			c.fail(err.Error())
		}
	}
	// The pictures start before the first address: a start during a navigation finds no page to attach to.
	if err := tab.StartScreencast(); err != nil {
		c.fail(err.Error())
	}
	if fresh && first.URL != "" {
		c.navigate(first.URL)
	}
	go tab.tell()
	go c.pump(ctx)
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			// The pane left: the page stays for a while, for the pane that comes back (a reload), then idles out.
			c.tab.leave()
			return
		}
		var m inbound
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.T {
		case "mouse", "wheel", "key", "text":
			// The person's input goes through its own queue: a browser call takes a while, and the reading of the
			// socket must not wait for it (HRN-711).
			c.queueInput(m)
		default:
			c.handle(m)
		}
	}
}

// queueInput adds an input to the queue, folding it into the last one when it can: moves replace moves, wheels add up.
func (c *paneConn) queueInput(m inbound) {
	c.imu.Lock()
	if n := len(c.input); n > 0 {
		last := &c.input[n-1]
		switch {
		case m.T == "mouse" && m.K == "move" && last.T == "mouse" && last.K == "move":
			*last = m
			c.imu.Unlock()
			c.wake()
			return
		case m.T == "wheel" && last.T == "wheel" && last.M == m.M:
			last.DX += m.DX
			last.DY += m.DY
			last.X, last.Y = m.X, m.Y
			c.imu.Unlock()
			c.wake()
			return
		}
	}
	if len(c.input) < maxInputQueue {
		c.input = append(c.input, m)
	}
	c.imu.Unlock()
	c.wake()
}

func (c *paneConn) wake() {
	select {
	case c.inputReady <- struct{}{}:
	default:
	}
}

// inputs runs the queued input in order.
func (c *paneConn) inputs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.inputReady:
		}
		for {
			c.imu.Lock()
			if len(c.input) == 0 {
				c.imu.Unlock()
				break
			}
			m := c.input[0]
			c.input = c.input[1:]
			c.imu.Unlock()
			c.handle(m)
		}
	}
}

func (c *paneConn) writeNow(v any) {
	raw, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.ws.Write(ctx, websocket.MessageText, raw)
}

// writer sends what the handlers queue.
func (c *paneConn) writer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case raw := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, frameWrite)
			err := c.ws.Write(wctx, websocket.MessageText, raw)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// pump forwards the tab's frames and updates.
func (c *paneConn) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.tab.Ended():
			c.writeNow(map[string]any{"t": "end", "why": c.tab.Why()})
			_ = c.ws.Close(websocket.StatusNormalClosure, "")
			return
		case f := <-c.tab.Frames():
			// A newer picture may come while this one waits for room: it is the one to send then.
			if !c.tab.WaitSlot(ctx) {
				return
			}
			select {
			case newer := <-c.tab.Frames():
				f = newer
			default:
			}
			wctx, cancel := context.WithTimeout(ctx, frameWrite)
			err := c.ws.Write(wctx, websocket.MessageBinary, f.Data)
			cancel()
			if err != nil {
				return
			}
			c.tab.Sent()
		case info := <-c.tab.Updates():
			c.send(map[string]any{"t": "page", "url": info.URL, "title": info.Title, "canBack": info.CanBack,
				"canForward": info.CanForward, "loading": info.Loading})
		}
	}
}

func (c *paneConn) navigate(raw string) {
	if why := c.sk.S.Refusal(raw); why != "" {
		c.fail(why)
		return
	}
	if err := c.tab.Navigate(raw); err != nil {
		c.fail(err.Error())
	}
}

func (c *paneConn) handle(m inbound) {
	t := c.tab
	var err error
	switch m.T {
	case "ack":
		t.Acked()
		return
	case "nav":
		c.navigate(m.URL)
		return
	case "back":
		err = t.Back()
	case "fwd":
		err = t.Forward()
	case "reload":
		err = t.Reload()
	case "size":
		err = t.Resize(m.W, m.H, m.DPR)
	case "cast":
		if m.On {
			err = t.StartScreencast()
		} else {
			err = t.StopScreencast()
		}
	case "mouse":
		kind := map[string]string{"down": "mousePressed", "up": "mouseReleased", "move": "mouseMoved"}[m.K]
		if kind == "" {
			return
		}
		t.Touched()
		err = t.Mouse(kind, m.X, m.Y, m.B, m.BS, m.M, m.C)
	case "wheel":
		t.Touched()
		err = t.Wheel(m.X, m.Y, m.DX, m.DY, m.M)
	case "key":
		t.Touched()
		kind := map[string]string{"down": "keyDown", "up": "keyUp"}[m.K]
		if kind == "" || len(m.Key) > 32 || len(m.Code) > 32 || len(m.Text) > 16 {
			return
		}
		err = t.Key(kind, m.Key, m.Code, m.Text, m.VK, m.M)
	case "text":
		t.Touched()
		if utf8.RuneCountInString(m.Text) > maxTextIn {
			return
		}
		err = t.InsertText(m.Text)
	case "copy":
		text, cerr := t.Selection()
		if cerr == nil {
			c.send(map[string]any{"t": "clip", "text": text})
		}
		err = cerr
	case "act":
		// The agent's action, bounded as the machine bounds it (browseract): it runs beside the pump, so the person's
		// own pointer and the pictures go on meanwhile.
		if m.ID == "" || len(m.ID) > maxActID || len(m.Op) > 16 || utf8.RuneCountInString(m.Text) > maxActText ||
			len(m.Key) > maxActKey || utf8.RuneCountInString(m.Value) > maxActValue || len(m.Ref) > 8 || len(m.Host) > 253 ||
			len(m.Event) > 16 {
			return
		}
		maxMS := maxActMS
		if m.Event != "" {
			maxMS = maxActEventMS
		}
		a := Act{ID: m.ID, Op: m.Op, Ref: m.Ref, Text: m.Text, Key: m.Key, Value: m.Value,
			DY: clampInt(int(m.DY), -maxActDY, maxActDY), MS: clampInt(m.MS, 0, maxMS), Submit: m.Submit, Event: m.Event, Host: m.Host}
		go func() {
			acted := t.Act(context.Background(), a)
			c.send(map[string]any{"t": "acted", "id": acted.ID, "ok": acted.OK, "text": acted.Text, "rect": acted.Rect})
		}()
		return
	default:
		return
	}
	if err != nil && m.T != "mouse" && m.T != "key" && m.T != "wheel" {
		c.fail(err.Error())
	}
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
