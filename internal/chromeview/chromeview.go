// Package chromeview shows the machine's Chrome that agents drive over CDP (a chrome-devtools MCP pointed at
// --browserUrl) in the right pane, live: the tab in front (the one the agent works in: its select_page brings it to
// front) is cast with Page.startScreencast, and the person's clicks, wheel and keys go back through Input.*, so a sign-in
// the agent waits for is done in the pane. One CDP connection serves every page that looks, and only while one does.
package chromeview

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// FollowEvery: how often the tab in front is looked for.
	FollowEvery = 800 * time.Millisecond
	// Linger: the connection to Chrome outlives the last page that looked by this (a pane tab switched back and forth).
	Linger      = 15 * time.Second
	retryEvery  = 2 * time.Second
	callTimeout = 3 * time.Second
)

// Config is where the Chrome is; CDP "" turns the viewer off.
type Config struct {
	// CDP is the browser's remote debugging address, e.g. http://127.0.0.1:9222.
	CDP string
	Log func(level, msg string, kv ...any)
}

// Tab is a page of the browser as the pane lists it.
type Tab struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// Viewer is the machine's Chrome as pages see it.
type Viewer struct {
	cfg Config

	mu    sync.Mutex
	subs  map[*sub]bool
	stop  context.CancelFunc
	gen   int
	sess  *session
	up    bool
	err   string
	tabs  []Tab
	frame []byte
}

type sub struct {
	text  chan []byte
	frame chan []byte
}

// New is a viewer of the Chrome at cfg.CDP.
func New(cfg Config) *Viewer {
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	cfg.CDP = strings.TrimRight(cfg.CDP, "/")
	return &Viewer{cfg: cfg, subs: map[*sub]bool{}}
}

// On says whether the machine has a Chrome to show.
func (v *Viewer) On() bool { return v != nil && v.cfg.CDP != "" }

func textMsg(kind string, fields map[string]any) []byte {
	fields["t"] = kind
	b, _ := json.Marshal(fields)
	return b
}

func (v *Viewer) statusMsg() []byte {
	return textMsg("status", map[string]any{"up": v.up, "error": v.err})
}

func (v *Viewer) tabsMsg() []byte { return textMsg("tabs", map[string]any{"tabs": v.tabs}) }

// broadcast sends a text message to every page; one that does not keep up loses it.
func (v *Viewer) broadcast(msg []byte) {
	for s := range v.subs {
		select {
		case s.text <- msg:
		default:
		}
	}
}

func (v *Viewer) setStatus(up bool, err string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.up == up && v.err == err {
		return
	}
	v.up, v.err = up, err
	if !up {
		v.tabs, v.frame = nil, nil
		v.broadcast(v.tabsMsg())
	}
	v.broadcast(v.statusMsg())
}

func (v *Viewer) setTabs(tabs []Tab) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if slices.Equal(v.tabs, tabs) {
		return
	}
	v.tabs = tabs
	v.broadcast(v.tabsMsg())
}

// setFrame keeps the newest frame for each page: one that is slow gets the latest, not a queue.
func (v *Viewer) setFrame(f []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frame = f
	for s := range v.subs {
		select {
		case <-s.frame:
		default:
		}
		s.frame <- f
	}
}

func (v *Viewer) join(s *sub) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.subs[s] = true
	v.gen++
	s.text <- v.statusMsg()
	s.text <- v.tabsMsg()
	if v.frame != nil {
		s.frame <- v.frame
	}
	if v.stop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		v.stop = cancel
		go v.run(ctx)
	}
}

func (v *Viewer) leave(s *sub) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.subs, s)
	if len(v.subs) > 0 {
		return
	}
	gen := v.gen
	time.AfterFunc(Linger, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if len(v.subs) == 0 && v.gen == gen && v.stop != nil {
			v.stop()
			v.stop = nil
			v.up, v.err, v.tabs, v.frame = false, "", nil, nil
		}
	})
}

// run keeps a connection to Chrome while ctx lives, dialing again after a failure.
func (v *Viewer) run(ctx context.Context) {
	for ctx.Err() == nil {
		c, err := dial(ctx, v.cfg.CDP)
		if err != nil {
			v.setStatus(false, err.Error())
		} else {
			s := &session{v: v, c: c, pages: map[string]*page{}, visible: map[string]bool{}}
			c.onEvent = s.event
			v.mu.Lock()
			v.sess = s
			v.mu.Unlock()
			v.setStatus(true, "")
			go s.follow(ctx)
			go func() { _, _ = c.call(ctx, "Target.setDiscoverTargets", map[string]any{"discover": true}, "") }()
			err = c.read(ctx)
			v.mu.Lock()
			v.sess = nil
			v.mu.Unlock()
			_ = c.ws.Close(websocket.StatusNormalClosure, "")
			if ctx.Err() == nil {
				v.cfg.Log("warn", "chrome: the connection to Chrome ended", "error", err)
				v.setStatus(false, "disconnected")
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(retryEvery):
		}
	}
}

// ServeHTTP is the pane's socket: text messages status and tabs and binary frames (width and height as two big-endian
// uint32, then the JPEG) go to the page; the page's input comes back as text.
func (v *Viewer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !v.On() {
		http.NotFound(w, r)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s := &sub{text: make(chan []byte, 64), frame: make(chan []byte, 1)}
	v.join(s)
	defer v.leave(s)
	go func() {
		defer cancel()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var in Input
			if json.Unmarshal(data, &in) == nil {
				v.input(ctx, in)
			}
		}
	}()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			_ = ws.Close(websocket.StatusNormalClosure, "")
			return
		case m := <-s.text:
			err = ws.Write(ctx, websocket.MessageText, m)
		case f := <-s.frame:
			err = ws.Write(ctx, websocket.MessageBinary, f)
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err = ws.Ping(pctx)
			pcancel()
		}
		if err != nil {
			return
		}
	}
}

// Input is what the person does in the pane.
type Input struct {
	T          string  `json:"t"` // mouse, wheel, key, text, tab, nav
	Type       string  `json:"type,omitempty"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	DX         float64 `json:"dx,omitempty"`
	DY         float64 `json:"dy,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	KeyCode    int     `json:"keyCode,omitempty"`
	Text       string  `json:"text,omitempty"`
	ID         string  `json:"id,omitempty"`
	Action     string  `json:"action,omitempty"`
	URL        string  `json:"url,omitempty"`
	Alt        bool    `json:"alt,omitempty"`
	Ctrl       bool    `json:"ctrl,omitempty"`
	Meta       bool    `json:"meta,omitempty"`
	Shift      bool    `json:"shift,omitempty"`
}

func (in Input) modifiers() int {
	m := 0
	if in.Alt {
		m |= 1
	}
	if in.Ctrl {
		m |= 2
	}
	if in.Meta {
		m |= 4
	}
	if in.Shift {
		m |= 8
	}
	return m
}

var mouseTypes = map[string]bool{"mousePressed": true, "mouseReleased": true, "mouseMoved": true}
var buttons = map[string]bool{"none": true, "left": true, "middle": true, "right": true}
var schemeRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)

// Calls is what an input asks of Chrome, in order; nothing for one the pane cannot send.
func (in Input) Calls() []Call {
	mods := in.modifiers()
	switch in.T {
	case "mouse":
		if !mouseTypes[in.Type] {
			return nil
		}
		b := in.Button
		if !buttons[b] {
			b = "none"
		}
		return []Call{{"Input.dispatchMouseEvent", map[string]any{"type": in.Type, "x": in.X, "y": in.Y, "button": b,
			"buttons": in.Buttons, "clickCount": in.ClickCount, "modifiers": mods}}}
	case "wheel":
		return []Call{{"Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": in.X, "y": in.Y,
			"deltaX": in.DX, "deltaY": in.DY, "modifiers": mods}}}
	case "text":
		if in.Text == "" {
			return nil
		}
		return []Call{{"Input.insertText", map[string]any{"text": in.Text}}}
	case "key":
		if in.Key == "" {
			return nil
		}
		base := map[string]any{"key": in.Key, "code": in.Code, "windowsVirtualKeyCode": in.KeyCode,
			"nativeVirtualKeyCode": in.KeyCode, "modifiers": mods}
		text := ""
		if len([]rune(in.Key)) == 1 && !in.Ctrl && !in.Meta && !in.Alt {
			text = in.Key
		} else if in.Key == "Enter" {
			text = "\r"
		}
		down := map[string]any{"type": "rawKeyDown"}
		if text != "" {
			down = map[string]any{"type": "keyDown", "text": text, "unmodifiedText": text}
		}
		for k, val := range base {
			down[k] = val
		}
		up := map[string]any{"type": "keyUp"}
		for k, val := range base {
			up[k] = val
		}
		return []Call{{"Input.dispatchKeyEvent", down}, {"Input.dispatchKeyEvent", up}}
	case "nav":
		switch in.Action {
		case "reload":
			return []Call{{"Page.reload", map[string]any{}}}
		case "back", "forward":
			return []Call{{"Runtime.evaluate", map[string]any{"expression": "history." + in.Action + "()"}}}
		case "go":
			u := strings.TrimSpace(in.URL)
			if u == "" {
				return nil
			}
			if !schemeRe.MatchString(u) {
				u = "https://" + u
			}
			return []Call{{"Page.navigate", map[string]any{"url": u}}}
		}
	}
	return nil
}

// Call is one CDP method and its params.
type Call struct {
	Method string
	Params map[string]any
}

func (v *Viewer) input(ctx context.Context, in Input) {
	v.mu.Lock()
	s := v.sess
	v.mu.Unlock()
	if s == nil {
		return
	}
	if in.T == "tab" {
		s.pick(ctx, in.ID)
		return
	}
	session := s.activeSession()
	if session == "" {
		return
	}
	for _, c := range in.Calls() {
		if _, err := s.c.call(ctx, c.Method, c.Params, session); err != nil {
			return
		}
	}
}

// session is one connection to Chrome: its pages and the one being cast.
type session struct {
	v *Viewer
	c *cdp

	mu      sync.Mutex
	pages   map[string]*page
	order   []string
	active  string
	visible map[string]bool
}

type page struct {
	id, title, url string
	session        string
	attaching      bool
}

// Shown says whether a page of the browser is one to list: not DevTools, an extension or the browser's own popups.
func Shown(url string) bool {
	return !strings.HasPrefix(url, "devtools:") && !strings.HasPrefix(url, "chrome-extension:") && !strings.Contains(url, ".top-chrome")
}

func (s *session) publishTabs() {
	s.mu.Lock()
	tabs := []Tab{}
	for _, id := range s.order {
		p := s.pages[id]
		if Shown(p.url) {
			tabs = append(tabs, Tab{ID: p.id, Title: p.title, URL: p.url, Active: p.id == s.active})
		}
	}
	s.mu.Unlock()
	s.v.setTabs(tabs)
}

func (s *session) activeSession() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pages[s.active]; p != nil {
		return p.session
	}
	return ""
}

// event is what Chrome tells, on its reader: nothing here waits for an answer of Chrome.
func (s *session) event(method, session string, params json.RawMessage) {
	switch method {
	case "Target.targetCreated", "Target.targetInfoChanged":
		var p struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				Title    string `json:"title"`
				URL      string `json:"url"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &p) != nil || p.TargetInfo.Type != "page" {
			return
		}
		t := p.TargetInfo
		s.mu.Lock()
		pg := s.pages[t.TargetID]
		if pg == nil {
			pg = &page{id: t.TargetID}
			s.pages[t.TargetID] = pg
			s.order = append(s.order, t.TargetID)
		}
		pg.title, pg.url = t.Title, t.URL
		s.mu.Unlock()
		s.publishTabs()
	case "Target.targetDestroyed":
		var p struct {
			TargetID string `json:"targetId"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		s.mu.Lock()
		delete(s.pages, p.TargetID)
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == p.TargetID })
		if s.active == p.TargetID {
			s.active = ""
		}
		s.mu.Unlock()
		s.publishTabs()
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		s.mu.Lock()
		for _, pg := range s.pages {
			if pg.session == p.SessionID {
				pg.session = ""
				if pg.id == s.active {
					s.active = "" // attached again and cast again by the next look
				}
			}
		}
		s.mu.Unlock()
	case "Page.screencastFrame":
		var p struct {
			Data      string `json:"data"`
			SessionID int64  `json:"sessionId"`
			Metadata  struct {
				DeviceWidth  float64 `json:"deviceWidth"`
				DeviceHeight float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		go func() {
			_, _ = s.c.call(context.Background(), "Page.screencastFrameAck", map[string]any{"sessionId": p.SessionID}, session)
		}()
		if session == "" || s.activeSession() != session {
			return
		}
		jpeg, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return
		}
		s.v.setFrame(Frame(int(p.Metadata.DeviceWidth), int(p.Metadata.DeviceHeight), jpeg))
	}
}

// Frame is the binary message of a frame: the page's width and height in CSS pixels, then the JPEG.
func Frame(w, h int, jpeg []byte) []byte {
	out := make([]byte, 8+len(jpeg))
	binary.BigEndian.PutUint32(out[0:4], uint32(max(w, 0)))
	binary.BigEndian.PutUint32(out[4:8], uint32(max(h, 0)))
	copy(out[8:], jpeg)
	return out
}

func (s *session) attach(ctx context.Context, pg *page) {
	s.mu.Lock()
	if pg.session != "" || pg.attaching {
		s.mu.Unlock()
		return
	}
	pg.attaching = true
	s.mu.Unlock()
	var r struct {
		SessionID string `json:"sessionId"`
	}
	raw, err := s.c.call(ctx, "Target.attachToTarget", map[string]any{"targetId": pg.id, "flatten": true}, "")
	s.mu.Lock()
	pg.attaching = false
	if err == nil && json.Unmarshal(raw, &r) == nil {
		pg.session = r.SessionID
	}
	s.mu.Unlock()
}

// follow casts the tab in front: the visible one, and of several (windows) the one that became visible last.
func (s *session) follow(ctx context.Context) {
	t := time.NewTicker(FollowEvery)
	defer t.Stop()
	for {
		s.look(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *session) look(ctx context.Context) {
	s.mu.Lock()
	var list []*page
	for _, id := range s.order {
		if pg := s.pages[id]; Shown(pg.url) {
			list = append(list, pg)
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	var vmu sync.Mutex
	vis := map[string]bool{}
	for _, pg := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.attach(ctx, pg)
			s.mu.Lock()
			session := pg.session
			s.mu.Unlock()
			if session == "" {
				return
			}
			raw, err := s.c.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.visibilityState", "returnByValue": true}, session)
			var r struct {
				Result struct {
					Value any `json:"value"`
				} `json:"result"`
			}
			if err == nil && json.Unmarshal(raw, &r) == nil && r.Result.Value == "visible" {
				vmu.Lock()
				vis[pg.id] = true
				vmu.Unlock()
			}
		}()
	}
	wg.Wait()
	s.mu.Lock()
	pick := Pick(s.order, s.visible, vis, s.active)
	s.visible = vis
	s.mu.Unlock()
	if pick != "" {
		s.cast(ctx, pick)
	}
}

// Pick is the page to cast now, "" to keep the one cast: a page that has just become visible wins (the newest of
// such), else the one cast stays while it is visible, else the first visible one.
func Pick(order []string, before, now map[string]bool, active string) string {
	fresh := ""
	for _, id := range order {
		if now[id] && !before[id] {
			fresh = id
		}
	}
	if fresh != "" {
		if fresh == active {
			return ""
		}
		return fresh
	}
	if now[active] {
		return ""
	}
	for _, id := range order {
		if now[id] {
			return id
		}
	}
	return ""
}

func (s *session) cast(ctx context.Context, id string) {
	s.mu.Lock()
	old := s.pages[s.active]
	pg := s.pages[id]
	if pg == nil || pg.session == "" || s.active == id {
		s.mu.Unlock()
		return
	}
	s.active = id
	session := pg.session
	s.mu.Unlock()
	if old != nil && old.session != "" {
		go func(sid string) { _, _ = s.c.call(context.Background(), "Page.stopScreencast", map[string]any{}, sid) }(old.session)
	}
	s.publishTabs()
	_, _ = s.c.call(ctx, "Page.enable", map[string]any{}, session)
	_, _ = s.c.call(ctx, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 70, "maxWidth": 1920, "maxHeight": 1400}, session)
}

// pick brings a page to front as the person asked: it is cast at once, not at the next look.
func (s *session) pick(ctx context.Context, id string) {
	s.mu.Lock()
	pg := s.pages[id]
	s.mu.Unlock()
	if pg == nil {
		return
	}
	if _, err := s.c.call(ctx, "Target.activateTarget", map[string]any{"targetId": id}, ""); err != nil {
		return
	}
	s.attach(ctx, pg)
	s.mu.Lock()
	s.visible[id] = true
	s.mu.Unlock()
	s.cast(ctx, id)
}

// cdp is a connection to the browser's DevTools socket, with sessions flattened into it.
type cdp struct {
	ws      *websocket.Conn
	mu      sync.Mutex
	next    int64
	pending map[int64]chan reply
	onEvent func(method, session string, params json.RawMessage)
}

type reply struct {
	result json.RawMessage
	err    error
}

var errClosed = errors.New("chrome: connection closed")

func dial(ctx context.Context, base string) (*cdp, error) {
	hctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, base+"/json/version", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ver struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil || ver.WS == "" {
		return nil, errors.New("chrome: no DevTools socket at " + base)
	}
	ws, _, err := websocket.Dial(hctx, ver.WS, &websocket.DialOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(64 << 20)
	return &cdp{ws: ws, pending: map[int64]chan reply{}}, nil
}

func (c *cdp) read(ctx context.Context) error {
	defer func() {
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- reply{err: errClosed}
			delete(c.pending, id)
		}
		c.pending = nil
		c.mu.Unlock()
	}()
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return err
		}
		var m struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			SessionID string          `json:"sessionId"`
			Params    json.RawMessage `json:"params"`
			Result    json.RawMessage `json:"result"`
			Error     *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				r := reply{result: m.Result}
				if m.Error != nil {
					r.err = errors.New(m.Error.Message)
				}
				ch <- r
			}
			continue
		}
		if c.onEvent != nil && m.Method != "" {
			c.onEvent(m.Method, m.SessionID, m.Params)
		}
	}
}

func (c *cdp) call(ctx context.Context, method string, params map[string]any, session string) (json.RawMessage, error) {
	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.next++
	id := c.next
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	forget := func() {
		c.mu.Lock()
		if c.pending != nil {
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}
	wctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.ws.Write(wctx, websocket.MessageText, b); err != nil {
		forget()
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-wctx.Done():
		forget()
		return nil, errors.New("chrome: " + method + " did not answer")
	}
}
