package livebrowser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"
)

// Info is what the pane shows of the page besides its picture.
type Info struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	CanBack    bool   `json:"canBack"`
	CanForward bool   `json:"canForward"`
	Loading    bool   `json:"loading"`
}

// Frame is one picture of the page: JPEG bytes and the page's CSS size they show.
type Frame struct {
	Data   []byte
	Width  int
	Height int
}

// Tab is one page of the browser, shown in one browser tab of the pane.
type Tab struct {
	ID        string
	conn      *conn
	targetID  string
	sessionID string
	mainFrame string

	// gate says why an address may not open ("" when it may); the browser's connections are held to the same rule
	// by the service's egress.
	gate func(string) string

	mu      sync.Mutex
	width   int
	height  int
	scale   float64
	loading bool
	casting bool
	closed  bool
	onClose func()
	// actCtx is the isolated world of the agent's actions (act.go), 0 before the first.
	actCtx int
	// leaveTimer closes the page when no pane came back for it.
	leaveTimer *time.Timer

	// The pictures' pace (HRN-711). A pane that acks what it drew (paced) gets at most inflightMax pictures on the way:
	// the browser's next one is held (chromeAck) until the pane catches up, so nothing piles up in the buffers between.
	// While the page stands still, one still shot in the screen's own scale replaces the screencast's 1x picture
	// (stillShown), and the screencast's pictures of a page that only blinks its caret are not sent over it.
	paced      bool
	inflight   int
	held       []int
	slot       chan struct{}
	holdSince  time.Time
	lastAt     time.Time
	touchedAt  time.Time
	lastFrame  []byte
	still      *time.Timer
	stillShown bool
	stillAt    time.Time
	lastStill  []byte
	baseSize   int
	changed    bool

	// frames carries the latest picture only: a pane that lags gets the newest, not the backlog. updates carries the
	// latest Info the same way. ended is closed when the tab is gone, with the reason in why.
	frames  chan Frame
	updates chan Info
	ended   chan struct{}
	endOnce sync.Once
	why     string
	stop    func()
}

const (
	callTimeout = 15 * time.Second
	// The screencast's pictures are the moving ones: lighter, the still shot is the sharp one.
	jpegQuality  = 60
	stillQuality = 85
	// inflightMax pictures may be on the way to a pane that acks them; a pane silent for holdMax is let go on.
	inflightMax = 2
	holdMax     = 3 * time.Second
	// stillAfter without a new picture or a touch means the page stands: time for the still shot. Pictures closer
	// than motionGap are motion; a still page's own blinks (a caret) come slower. A page that changes while its still
	// shot is shown gets a new shot after stillAgain at the earliest; a change is a picture whose size differs from
	// the one before the shot by more than caretPart (a caret alone changes a few bytes).
	stillAfter = 350 * time.Millisecond
	motionGap  = 300 * time.Millisecond
	stillAgain = 2 * time.Second
	caretPart  = 0.02
	// The shot itself stirs the compositor for a moment: pictures within shotSettle of it mean nothing on their own.
	shotSettle = time.Second
	minSize    = 200
	maxSize    = 4096
)

func clampSize(n, fallback int) int {
	if n <= 0 {
		return fallback
	}
	return int(math.Max(minSize, math.Min(maxSize, float64(n))))
}

func clampScale(s float64) float64 {
	if s <= 0 || math.IsNaN(s) || math.IsInf(s, 0) {
		return 1
	}
	return math.Max(1, math.Min(3, s))
}

func newTab(ctx context.Context, c *conn, id string, width, height int, scale float64, gate func(string) string) (*Tab, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	t := &Tab{ID: id, conn: c, gate: gate, width: clampSize(width, 1280), height: clampSize(height, 800), scale: clampScale(scale),
		frames: make(chan Frame, 1), updates: make(chan Info, 1), ended: make(chan struct{}), slot: make(chan struct{}, 1)}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := c.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return nil, err
	}
	t.targetID = created.TargetID
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.targetID, "flatten": true}, &attached); err != nil {
		_ = c.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
		return nil, err
	}
	t.sessionID = attached.SessionID
	events, stop := c.Listen(t.sessionID, 64)
	t.stop = stop
	go t.listen(events)
	for _, step := range []struct {
		method string
		params map[string]any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}},
		{"Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}},
	} {
		if err := c.Call(ctx, t.sessionID, step.method, step.params, nil); err != nil && step.method != "Browser.setDownloadBehavior" {
			t.Close()
			return nil, err
		}
	}
	if err := t.metrics(ctx); err != nil {
		t.Close()
		return nil, err
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := c.Call(ctx, t.sessionID, "Page.getFrameTree", nil, &tree); err == nil {
		t.mainFrame = tree.FrameTree.Frame.ID
	}
	return t, nil
}

func (t *Tab) call(method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return t.conn.Call(ctx, t.sessionID, method, params, result)
}

func (t *Tab) metrics(ctx context.Context) error {
	t.mu.Lock()
	w, h, s := t.width, t.height, t.scale
	t.mu.Unlock()
	return t.conn.Call(ctx, t.sessionID, "Emulation.setDeviceMetricsOverride",
		map[string]any{"width": w, "height": h, "deviceScaleFactor": s, "mobile": false}, nil)
}

// Frames is the page's pictures, the newest only.
func (t *Tab) Frames() <-chan Frame { return t.frames }

// Updates is what changed about the page, the newest only.
func (t *Tab) Updates() <-chan Info { return t.updates }

// Ended is closed when the tab is gone; Why says why.
func (t *Tab) Ended() <-chan struct{} { return t.ended }

func (t *Tab) Why() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.why
}

func (t *Tab) end(why string) {
	t.endOnce.Do(func() {
		t.mu.Lock()
		t.why = why
		t.closed = true
		onClose := t.onClose
		if t.leaveTimer != nil {
			t.leaveTimer.Stop()
		}
		t.mu.Unlock()
		if t.stop != nil {
			t.stop()
		}
		close(t.ended)
		if onClose != nil {
			onClose()
		}
	})
}

// gone: the browser died under the tab.
func (t *Tab) gone() { t.end("the browser ended") }

// leaveGrace is how long a page stays after its pane left: a reload of the pane finds it again.
const leaveGrace = 2 * time.Minute

// leave: the pane's socket closed; the page stays for leaveGrace, its pictures stop.
func (t *Tab) leave() {
	t.mu.Lock()
	if t.leaveTimer != nil {
		t.leaveTimer.Stop()
	}
	t.leaveTimer = time.AfterFunc(leaveGrace, t.Close)
	t.mu.Unlock()
	_ = t.StopScreencast()
}

// rejoin: a pane took the page again.
func (t *Tab) rejoin() {
	t.mu.Lock()
	if t.leaveTimer != nil {
		t.leaveTimer.Stop()
		t.leaveTimer = nil
	}
	t.mu.Unlock()
}

// Close ends the page.
func (t *Tab) Close() {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if !closed {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = t.conn.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
		cancel()
	}
	t.end("closed")
}

func (t *Tab) listen(events <-chan Event) {
	for {
		select {
		case <-t.ended:
			return
		case <-t.conn.Done():
			t.gone()
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			t.handle(ev)
		}
	}
}

func (t *Tab) handle(ev Event) {
	switch ev.Method {
	case "Page.screencastFrame":
		var p struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
			Metadata  struct {
				DeviceWidth  float64 `json:"deviceWidth"`
				DeviceHeight float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		data, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			_ = t.call("Page.screencastFrameAck", map[string]any{"sessionId": p.SessionID}, nil)
			return
		}
		if t.ackNow(p.SessionID) {
			_ = t.call("Page.screencastFrameAck", map[string]any{"sessionId": p.SessionID}, nil)
		}
		if t.takeFrame(data) {
			t.offer(Frame{Data: data, Width: int(p.Metadata.DeviceWidth), Height: int(p.Metadata.DeviceHeight)})
		}
	case "Page.frameStartedLoading", "Page.frameStoppedLoading", "Page.frameNavigated", "Page.navigatedWithinDocument", "Page.loadEventFired":
		var p struct {
			FrameID string `json:"frameId"`
			Frame   struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(ev.Params, &p)
		id := p.FrameID
		if id == "" {
			id = p.Frame.ID
		}
		if ev.Method == "Page.frameNavigated" && p.Frame.ParentID != "" {
			return
		}
		if id != "" && t.mainFrame != "" && id != t.mainFrame && ev.Method != "Page.loadEventFired" {
			return
		}
		t.mu.Lock()
		switch ev.Method {
		case "Page.frameStartedLoading":
			t.loading = true
		case "Page.frameNavigated":
			// A new document: the agent's world and its refs went with the old one.
			t.actCtx = 0
		case "Page.frameStoppedLoading", "Page.loadEventFired":
			t.loading = false
		}
		casting := t.casting
		t.mu.Unlock()
		t.Touched()
		if ev.Method == "Page.frameNavigated" && casting {
			// A page of another site comes in another process: the pictures are asked for again there.
			go func() { _ = t.StartScreencast() }()
		}
		go t.tell()
	case "Page.javascriptDialogOpening":
		// The page's own dialogs have no window here: an alert is read and closed, a question answered «no».
		var p struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(ev.Params, &p)
		_ = t.call("Page.handleJavaScriptDialog", map[string]any{"accept": p.Type == "alert"}, nil)
	}
}

// adoptWindow takes a window this page opened (target=_blank, window.open): it closes, and this page goes to its
// address instead, through the same gate as any navigation. Nothing opens out of the pane's sight.
func (t *Tab) adoptWindow(targetID, url string) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	for wait := time.Now().Add(1500 * time.Millisecond); (url == "" || url == "about:blank") && time.Now().Before(wait); {
		// The window's address comes a moment after it opens.
		time.Sleep(100 * time.Millisecond)
		var info struct {
			TargetInfo struct {
				URL string `json:"url"`
			} `json:"targetInfo"`
		}
		if t.conn.Call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": targetID}, &info) != nil {
			break
		}
		url = info.TargetInfo.URL
	}
	_ = t.conn.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": targetID}, nil)
	if url != "" && url != "about:blank" {
		_ = t.Navigate(url)
	}
}

// ackNow says whether the browser may be told at once that its picture was taken: not while a paced pane has
// inflightMax pictures on the way; then the ack waits its turn for the pane's (Acked), and the browser, which keeps
// only a couple of pictures in flight itself, stops drawing more. The pump holds the pictures the same way (WaitSlot).
func (t *Tab) ackNow(sessionID int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.paced || t.inflight < inflightMax {
		return true
	}
	if t.holdSince.IsZero() {
		t.holdSince = time.Now()
	}
	if len(t.held) < 8 {
		t.held = append(t.held, sessionID)
		return false
	}
	return true
}

// WaitSlot waits until the pane has room for one more picture: at once for a pane that does not ack; a pane silent
// for holdMax is not waited for any longer. False when the context ended.
func (t *Tab) WaitSlot(ctx context.Context) bool {
	for {
		t.mu.Lock()
		room := !t.paced || t.inflight < inflightMax
		t.mu.Unlock()
		if room {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-t.slot:
		case <-time.After(holdMax):
			t.letGo()
		}
	}
}

// letGo gives up on the pane's acks: the pictures go on as to a pane that does not ack.
func (t *Tab) letGo() {
	t.mu.Lock()
	t.inflight = 0
	t.holdSince = time.Time{}
	held := t.held
	t.held = nil
	t.mu.Unlock()
	for _, id := range held {
		_ = t.call("Page.screencastFrameAck", map[string]any{"sessionId": id}, nil)
	}
}

// Sent: a picture went out to the pane.
func (t *Tab) Sent() {
	t.mu.Lock()
	if t.paced {
		t.inflight++
	}
	t.mu.Unlock()
}

// Acked: the pane drew (or dropped) a picture; the pump may send the next, and the browser hears of one more taken.
func (t *Tab) Acked() {
	t.mu.Lock()
	if t.inflight > 0 {
		t.inflight--
	}
	t.holdSince = time.Time{}
	held := 0
	if len(t.held) > 0 && t.inflight < inflightMax {
		held = t.held[0]
		t.held = t.held[1:]
	}
	t.mu.Unlock()
	select {
	case t.slot <- struct{}{}:
	default:
	}
	if held != 0 {
		_ = t.call("Page.screencastFrameAck", map[string]any{"sessionId": held}, nil)
	}
}

// Touched: the person or the agent did something, or the page moved: the page is in motion, its pictures go on.
func (t *Tab) Touched() {
	t.mu.Lock()
	t.touchedAt = time.Now()
	t.stillShown = false
	t.mu.Unlock()
}

// takeFrame says whether a screencast picture goes to the pane. Motion (pictures close together, a touch) goes; a
// page under a still shot that only blinks does not, and one that changed gets a new shot instead.
func (t *Tab) takeFrame(data []byte) bool {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	touched := now.Sub(t.touchedAt) < motionGap
	fast := touched || now.Sub(t.lastAt) < motionGap
	t.lastAt = now
	t.lastFrame = data
	waiting := len(t.held) > 0 || (t.paced && t.inflight >= inflightMax)
	if t.stillShown && !touched && now.Sub(t.stillAt) < shotSettle {
		return false
	}
	if t.stillShown && !fast && !waiting {
		if t.baseSize > 0 && math.Abs(float64(len(data)-t.baseSize)) > caretPart*float64(t.baseSize) {
			t.changed = true
		}
		if t.changed && now.Sub(t.stillAt) > stillAgain {
			t.armStill(0)
		}
		return false
	}
	t.stillShown = false
	if !waiting {
		// Pictures waiting for the pane's ack are not stillness: no shot then.
		t.armStill(stillAfter)
	}
	return true
}

// armStill (under t.mu) takes the still shot after d; a shot already planned is moved.
func (t *Tab) armStill(d time.Duration) {
	if t.still != nil {
		t.still.Stop()
	}
	t.still = time.AfterFunc(d, t.stillShot)
}

// stillShot sends one picture of the page in the screen's own scale: the sharp one while nothing moves.
func (t *Tab) stillShot() {
	t.mu.Lock()
	casting, closed := t.casting, t.closed
	t.mu.Unlock()
	if !casting || closed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var r struct {
		Data string `json:"data"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": stillQuality, "optimizeForSpeed": true,
	}, &r); err != nil {
		return
	}
	data, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil || len(data) == 0 {
		return
	}
	t.mu.Lock()
	// A touch meanwhile: the page is moving again, the shot is stale.
	if time.Since(t.touchedAt) < motionGap || bytes.Equal(data, t.lastStill) && t.stillShown {
		t.mu.Unlock()
		return
	}
	t.lastStill = data
	t.stillShown = true
	t.stillAt = time.Now()
	t.baseSize = len(t.lastFrame)
	t.changed = false
	w, h := t.width, t.height
	t.mu.Unlock()
	t.offer(Frame{Data: data, Width: w, Height: h})
}

// Paced: whether the pane acks the pictures it draws.
func (t *Tab) Paced(on bool) {
	t.mu.Lock()
	t.paced = on
	t.inflight = 0
	t.held = nil
	t.mu.Unlock()
}

func (t *Tab) offer(f Frame) {
	for {
		select {
		case t.frames <- f:
			return
		default:
			select {
			case <-t.frames:
			default:
			}
		}
	}
}

// tell sends the page's current Info to the pane.
func (t *Tab) tell() {
	info, err := t.Info()
	if err != nil {
		return
	}
	for {
		select {
		case t.updates <- info:
			return
		default:
			select {
			case <-t.updates:
			default:
			}
		}
	}
}

// Info reads the page's address, title and history from the browser.
func (t *Tab) Info() (Info, error) {
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"entries"`
	}
	if err := t.call("Page.getNavigationHistory", nil, &h); err != nil {
		return Info{}, err
	}
	t.mu.Lock()
	loading := t.loading
	t.mu.Unlock()
	info := Info{Loading: loading}
	if h.CurrentIndex >= 0 && h.CurrentIndex < len(h.Entries) {
		info.URL = h.Entries[h.CurrentIndex].URL
		info.Title = h.Entries[h.CurrentIndex].Title
		info.CanBack = h.CurrentIndex > 0
		info.CanForward = h.CurrentIndex < len(h.Entries)-1
	}
	return info, nil
}

// Navigate opens an address.
func (t *Tab) Navigate(url string) error {
	if t.gate != nil {
		if why := t.gate(url); why != "" {
			return errors.New(why)
		}
	}
	var r struct {
		ErrorText string `json:"errorText"`
	}
	if err := t.call("Page.navigate", map[string]any{"url": url}, &r); err != nil {
		return err
	}
	if r.ErrorText != "" {
		return errors.New(r.ErrorText)
	}
	return nil
}

// Back and Forward step through the page's history; Reload loads it again.
func (t *Tab) Back() error    { return t.step(-1) }
func (t *Tab) Forward() error { return t.step(1) }
func (t *Tab) Reload() error  { return t.call("Page.reload", nil, nil) }

func (t *Tab) step(by int) error {
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := t.call("Page.getNavigationHistory", nil, &h); err != nil {
		return err
	}
	at := h.CurrentIndex + by
	if at < 0 || at >= len(h.Entries) {
		return nil
	}
	return t.call("Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[at].ID}, nil)
}

// Resize gives the page a new CSS size and pixel scale; the screencast follows.
func (t *Tab) Resize(width, height int, scale float64) error {
	t.mu.Lock()
	t.width, t.height, t.scale = clampSize(width, t.width), clampSize(height, t.height), clampScale(scale)
	casting := t.casting
	t.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := t.metrics(ctx); err != nil {
		return err
	}
	t.Touched()
	if casting {
		return t.StartScreencast()
	}
	return nil
}

// StartScreencast asks for the page's pictures as they change; one that runs is started over (a new size).
func (t *Tab) StartScreencast() error {
	t.mu.Lock()
	was := t.casting
	t.casting = true
	w, h, s := t.width, t.height, t.scale
	t.mu.Unlock()
	if was {
		_ = t.call("Page.stopScreencast", nil, nil)
	}
	t.Touched()
	return t.call("Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": jpegQuality,
		"maxWidth": int(float64(w) * s), "maxHeight": int(float64(h) * s), "everyNthFrame": 1,
	}, nil)
}

// StopScreencast ends the pictures while nobody looks.
func (t *Tab) StopScreencast() error {
	t.mu.Lock()
	t.casting = false
	if t.still != nil {
		t.still.Stop()
	}
	t.stillShown = false
	t.inflight = 0
	t.held = nil
	t.mu.Unlock()
	return t.call("Page.stopScreencast", nil, nil)
}

// Mouse sends a pointer event: kind is mousePressed, mouseReleased or mouseMoved; x and y are CSS pixels.
func (t *Tab) Mouse(kind string, x, y float64, button string, buttons, modifiers, clicks int) error {
	if button == "" {
		button = "none"
	}
	return t.call("Input.dispatchMouseEvent", map[string]any{
		"type": kind, "x": x, "y": y, "button": button, "buttons": buttons, "modifiers": modifiers, "clickCount": clicks,
	}, nil)
}

// Wheel scrolls at a point.
func (t *Tab) Wheel(x, y, dx, dy float64, modifiers int) error {
	return t.call("Input.dispatchMouseEvent", map[string]any{
		"type": "mouseWheel", "x": x, "y": y, "deltaX": dx, "deltaY": dy, "modifiers": modifiers,
	}, nil)
}

// Key sends a key: kind is keyDown or keyUp; text is what a printable key types.
func (t *Tab) Key(kind, key, code, text string, vk, modifiers int) error {
	params := map[string]any{"type": kind, "key": key, "code": code, "modifiers": modifiers, "windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk}
	if kind == "keyDown" && text != "" {
		params["text"] = text
		params["unmodifiedText"] = text
	}
	return t.call("Input.dispatchKeyEvent", params, nil)
}

// InsertText types text as one piece (a paste, an IME's composition).
func (t *Tab) InsertText(text string) error {
	return t.call("Input.insertText", map[string]any{"text": text}, nil)
}

// Selection is the text the page has selected, for the person's clipboard.
func (t *Tab) Selection() (string, error) {
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	err := t.call("Runtime.evaluate", map[string]any{
		"expression":    "(function(){const a=document.activeElement;if(a&&(a.tagName==='INPUT'||a.tagName==='TEXTAREA')&&a.selectionStart!==a.selectionEnd)return a.value.slice(a.selectionStart,a.selectionEnd);return String(window.getSelection())})()",
		"returnByValue": true,
	}, &r)
	return r.Result.Value, err
}

// Evaluate runs an expression in the page and returns its value as JSON.
func (t *Tab) Evaluate(expression string, result any) error {
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := t.call("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true, "userGesture": true}, &r); err != nil {
		return err
	}
	if r.ExceptionDetails != nil {
		return errors.New("the page's script failed: " + r.ExceptionDetails.Text)
	}
	if result != nil && len(r.Result.Value) > 0 {
		return json.Unmarshal(r.Result.Value, result)
	}
	return nil
}
