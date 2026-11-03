package design

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The recorder. The page runs on a clock of our own: the browser is chrome-headless-shell with
// --deterministic-mode and --enable-begin-frame-control, so it draws a frame only when HeadlessExperimental.beginFrame
// asks, and the time of that frame (frameTimeTicks) is the one we give. A frame is then one step of 1000/fps ms of the
// page's own time, however long the machine takes to draw it: CSS animations, transitions and requestAnimationFrame all
// follow it, and no frame is missed under load, which the real-time capture of a screen cannot promise.
//
// An input event is sent without waiting for its answer: the browser answers it in the next frame, and the next frame
// is ours to ask for, so waiting would hold both.

// RecordOptions is what a recording takes besides its script.
type RecordOptions struct {
	// Dir is the project's folder; the clip and the log go to Dir/records.
	Dir string
	// Name overrides the script's name.
	Name string
	// CRF of the clip: 0 is lossless; 8 or lower is without visible loss.
	CRF int
	// Preset of the x264: slower is smaller.
	Preset string
	// Getenv gives a secret step its value; nil: the process environment.
	Getenv func(string) string
	// Log is told what the recording is doing now.
	Log func(string)
}

// RecordResult is what a recording made.
type RecordResult struct {
	Video, LogFile string
	Frames         int
	Dropped        int
	DurMs          int
	Wall           time.Duration
	Secrets        int
}

// FindHeadlessShell is the chrome-headless-shell this machine has: RUNTIME_DESIGN_HEADLESS_SHELL, else on PATH, else in
// the folders such a browser is installed to (~/.cache/chrome-headless-shell, puppeteer's and playwright's caches, /opt).
func FindHeadlessShell() string {
	if p := os.Getenv("RUNTIME_DESIGN_HEADLESS_SHELL"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
	if p := lookPathOr("chrome-headless-shell"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	var roots []string
	if home != "" {
		roots = append(roots,
			filepath.Join(home, ".cache", "chrome-headless-shell"),
			filepath.Join(home, ".cache", "puppeteer", "chrome-headless-shell"),
			filepath.Join(home, ".cache", "ms-playwright"))
	}
	roots = append(roots, "/opt/chrome-headless-shell", "/usr/local/share/chrome-headless-shell")
	best := ""
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && strings.Count(strings.TrimPrefix(p, root), string(filepath.Separator)) > 4 {
				return filepath.SkipDir
			}
			if !d.IsDir() && (d.Name() == "chrome-headless-shell" || d.Name() == "headless_shell") && p > best {
				best = p // the newest version sorts last
			}
			return nil
		})
	}
	return best
}

type recordReq struct {
	typ   string
	start time.Time
}

// recordBusy: the page is still loading when one of its requests for something it draws is on its way. A request that
// stays open (an event stream, a long poll) is the page's own, not a load: those are left out by type and by age.
func (r *recorder) busyLocked() bool {
	for _, q := range r.reqs {
		switch q.typ {
		case "EventSource", "WebSocket", "Ping", "Prefetch", "CSPViolationReport", "Preflight", "SignedExchange":
			continue
		}
		if time.Since(q.start) < 4*time.Second {
			return true
		}
	}
	return false
}

type recordKey struct {
	key, code string
	vk        int
	text      string
}

var recordKeys = map[string]recordKey{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
	"Space":      {" ", "Space", 32, " "},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"Home":       {"Home", "Home", 36, ""},
	"End":        {"End", "End", 35, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
}

// recorder is one recording in progress.
type recorder struct {
	sc   *RecordScenario
	opt  RecordOptions
	pipe *cdpPipe
	br   *browser

	session string
	dt      float64 // ms of the page's time per frame
	base    float64 // the browser's clock at frame 0, ms
	tick    int     // frames asked for, hidden or not
	frames  int     // frames recorded
	dropped int
	last    []byte
	rec     bool // frames are recorded now

	shots  chan []byte
	ffDone chan error
	ffIn   io.WriteCloser

	px, py float64 // the cursor
	placed bool
	cursor [][3]float64
	events []RecordEvent
	secret []string

	mu      sync.Mutex
	reqs    map[string]recordReq // requests of the page that have not finished
	netAt   time.Time
	loaded  bool
	pending []<-chan cdpReply
}

func (r *recorder) say(format string, a ...any) {
	if r.opt.Log != nil {
		r.opt.Log(fmt.Sprintf(format, a...))
	}
}

// post sends a command and goes on without waiting for its answer; wait collects the answers.
func (p *cdpPipe) post(method string, params map[string]any, session string) <-chan cdpReply {
	ch := make(chan cdpReply, 1)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		ch <- cdpReply{err: errors.New("CDP closed: " + method)}
		return ch
	}
	id := p.nextID
	p.nextID++
	p.pending[id] = pendingCall{method: method, ch: ch}
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	_, err := p.in.Write(append(b, 0))
	p.mu.Unlock()
	if err != nil {
		p.close(err)
	}
	return ch
}

func (r *recorder) input(method string, params map[string]any) {
	r.pending = append(r.pending, r.pipe.post(method, params, r.session))
}

// settleInput looks at the answers of the events sent since the last frame; the frame that followed has answered them all.
func (r *recorder) settleInput() error {
	for _, ch := range r.pending {
		select {
		case a := <-ch:
			if a.err != nil {
				return a.err
			}
		case <-time.After(30 * time.Second):
			return errors.New("an input event got no answer")
		}
	}
	r.pending = r.pending[:0]
	return nil
}

func (r *recorder) cdp(method string, params map[string]any) (map[string]any, error) {
	return r.pipe.sendLong(method, params, r.session, 90*time.Second)
}

// eval runs a script of ours in the page through the DevTools protocol (Runtime.evaluate, not JavaScript eval): the
// scripts are constants of this file and JSON-quoted selectors.
func (r *recorder) eval(expr string) (any, error) {
	res, err := r.cdp("Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	if ex, ok := res["exceptionDetails"].(map[string]any); ok {
		msg, _ := ex["text"].(string)
		if e, ok := ex["exception"].(map[string]any); ok {
			if d, ok := e["description"].(string); ok {
				msg = d
			}
		}
		return nil, errors.New(line(msg))
	}
	v, _ := res["result"].(map[string]any)
	return v["value"], nil
}

// t is the clip's time in ms at the next recorded frame.
func (r *recorder) t() int { return int(math.Round(float64(r.frames) * 1000 / float64(r.sc.FPS))) }

// frame asks the browser for one frame of the page's time. When it is recorded, the picture goes to the encoder (the
// previous one again when the page did not change, which is how the browser says nothing was damaged).
func (r *recorder) frame() error {
	ticks := r.base + float64(r.tick)*r.dt
	params := map[string]any{"frameTimeTicks": ticks, "interval": r.dt}
	if r.rec {
		params["screenshot"] = map[string]any{"format": "png", "optimizeForSpeed": true}
	} else {
		params["noDisplayUpdates"] = true
	}
	res, err := r.pipe.sendLong("HeadlessExperimental.beginFrame", params, r.session, 120*time.Second)
	if err != nil {
		return err
	}
	r.tick++
	if err := r.settleInput(); err != nil {
		return err
	}
	if !r.rec {
		return nil
	}
	if s, _ := res["screenshotData"].(string); s != "" {
		png, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return err
		}
		r.last = png
	} else if r.last == nil {
		// the first frame of a page nobody has changed since it was drawn: ask again after a change that damages it
		if _, err := r.eval(`document.documentElement.style.setProperty('--tk-rec', String(Math.random()))`); err == nil {
			return r.frame()
		}
	}
	if r.last == nil {
		r.dropped++
		return nil
	}
	select {
	case r.shots <- r.last:
	case err := <-r.ffDone:
		r.ffDone <- err
		return fmt.Errorf("ffmpeg stopped: %w", err)
	}
	r.cursor = append(r.cursor, [3]float64{float64(r.t()), roundTo(r.px, 1), roundTo(r.py, 1)})
	r.frames++
	return nil
}

// hold lets ms of the page's time pass.
func (r *recorder) hold(ms float64) error {
	n := int(math.Round(ms / r.dt))
	for i := 0; i < n; i++ {
		if err := r.frame(); err != nil {
			return err
		}
	}
	return nil
}

func (r *recorder) mouse(typ string, x, y float64, extra map[string]any) {
	p := map[string]any{"type": typ, "x": x, "y": y, "button": "none", "modifiers": 0}
	for k, v := range extra {
		p[k] = v
	}
	r.input("Input.dispatchMouseEvent", p)
}

func smooth(p float64) float64 {
	// ease in and out: quick through the middle, soft at both ends
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	return p * p * p * (p*(p*6-15) + 10)
}

// moveTo carries the cursor to (x, y) along an arc: the page sees the same positions, frame by frame, as the log.
func (r *recorder) moveTo(x, y float64, ms int) error {
	if !r.placed {
		r.px, r.py, r.placed = float64(r.sc.Viewport.Width)*0.62, float64(r.sc.Viewport.Height)*0.78, true
	}
	dx, dy := x-r.px, y-r.py
	dist := math.Hypot(dx, dy)
	if dist < 1 {
		return nil
	}
	if ms <= 0 {
		ms = int(math.Min(1500, math.Max(420, 300+dist*0.7)))
	}
	n := int(math.Max(2, math.Round(float64(ms)/r.dt)))
	// the control point leans away from the straight line by a tenth of the way, always to the same side of the travel
	sign := 1.0
	if dx < 0 {
		sign = -1
	}
	cx := r.px + dx/2 - dy*0.1*sign
	cy := r.py + dy/2 + dx*0.1*sign
	sx, sy := r.px, r.py
	for i := 1; i <= n; i++ {
		p := smooth(float64(i) / float64(n))
		q := 1 - p
		r.px = q*q*sx + 2*q*p*cx + p*p*x
		r.py = q*q*sy + 2*q*p*cy + p*p*y
		r.mouse("mouseMoved", r.px, r.py, nil)
		if err := r.frame(); err != nil {
			return err
		}
	}
	return nil
}

type recordTarget struct {
	Found, Editable, InView bool
	X, Y, W, H              float64
	Tag, Type, Label        string
}

// findJS is the page's side of finding a target: a selector or text=...; the box is in the viewport's CSS pixels.
const findJS = `(function (q) {
  function visible(el) {
    var r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    var s = getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none' && Number(s.opacity) > 0;
  }
  function textOf(el) {
    var t = el.tagName;
    if (t === 'INPUT' || t === 'TEXTAREA') return (el.value && el.type !== 'password' ? el.value + ' ' : '') + (el.placeholder || '') + ' ' + (el.getAttribute('aria-label') || '');
    return (el.innerText || '') + ' ' + (el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('title') || '');
  }
  function norm(s) { return s.replace(/\s+/g, ' ').trim(); }
  var el = null;
  if (q.indexOf('text=') === 0) {
    var want = q.slice(5), re = null;
    var m = /^\/(.+)\/([a-z]*)$/.exec(want);
    if (m) re = new RegExp(m[1], m[2]);
    var lw = want.toLowerCase();
    var all = document.body ? document.body.querySelectorAll('*') : [];
    var best = null, bestKey = null;
    for (var i = 0; i < all.length; i++) {
      var e = all[i];
      if (/^(SCRIPT|STYLE|NOSCRIPT|HEAD|META|LINK|TITLE|HTML|BODY)$/.test(e.tagName) || !visible(e)) continue;
      var t = norm(textOf(e));
      var hit = re ? re.test(t) : t.toLowerCase().indexOf(lw) >= 0;
      if (!hit) continue;
      var exact = !re && norm(e.innerText || '').toLowerCase() === lw ? 0 : 1;
      var r = e.getBoundingClientRect();
      var key = [exact, r.width * r.height];
      if (!bestKey || key[0] < bestKey[0] || (key[0] === bestKey[0] && key[1] < bestKey[1])) { best = e; bestKey = key; }
    }
    el = best;
    if (el) { var up = el.closest('a,button,[role=button],[role=tab],[role=menuitem],[role=link],label,summary,select'); if (up && visible(up)) el = up; }
  } else {
    var list;
    try { list = document.querySelectorAll(q); } catch (err) { return {error: 'bad selector: ' + q}; }
    for (var j = 0; j < list.length; j++) { if (visible(list[j])) { el = list[j]; break; } }
  }
  if (!el) return {found: false};
  var b = el.getBoundingClientRect();
  var vw = innerWidth, vh = innerHeight;
  var inView = b.top >= 0 && b.left >= 0 && b.bottom <= vh && b.right <= vw;
  var label = norm(textOf(el)).slice(0, 60);
  var tag = el.tagName.toLowerCase();
  return {found: true, x: b.left, y: b.top, w: b.width, h: b.height, tag: tag, type: el.type || '', label: el.type === 'password' ? '' : label,
    editable: !!(el.isContentEditable || tag === 'input' || tag === 'textarea'), inView: inView};
})(`

func (r *recorder) find(sel string) (*recordTarget, error) {
	q, _ := json.Marshal(sel)
	v, err := r.eval(findJS + string(q) + `)`)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	if e, _ := m["error"].(string); e != "" {
		return nil, errors.New(e)
	}
	t := &recordTarget{}
	if ok, _ := m["found"].(bool); !ok {
		return t, nil
	}
	raw, _ := json.Marshal(m)
	var d struct {
		X, Y, W, H float64
		Tag, Type  string
		Label      string
		Editable   bool
		InView     bool
	}
	_ = json.Unmarshal(raw, &d)
	t.Found, t.X, t.Y, t.W, t.H, t.Tag, t.Type, t.Label, t.Editable, t.InView = true, d.X, d.Y, d.W, d.H, d.Tag, d.Type, d.Label, d.Editable, d.InView
	return t, nil
}

// need waits (in frames) until the target is on the page and visible, up to timeout ms of the machine's time.
func (r *recorder) need(sel string, timeout time.Duration) (*recordTarget, error) {
	deadline := time.Now().Add(timeout)
	for {
		t, err := r.find(sel)
		if err != nil {
			return nil, err
		}
		if t.Found {
			return t, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(locale.Pick("nothing visible matches %q after %s", "ничего видимого по %q за %s"), sel, timeout)
		}
		if err := r.frame(); err != nil {
			return nil, err
		}
	}
}

func (t *recordTarget) center(vw, vh int) (float64, float64) {
	l, rr := math.Max(t.X, 0), math.Min(t.X+t.W, float64(vw))
	tp, b := math.Max(t.Y, 0), math.Min(t.Y+t.H, float64(vh))
	return (l + rr) / 2, (tp + b) / 2
}

func (r *recorder) emit(e RecordEvent) { r.events = append(r.events, e) }

func recF(v float64) *float64 { v = roundTo(v, 1); return &v }

func bbox(t *recordTarget) []float64 {
	return []float64{roundTo(t.X, 1), roundTo(t.Y, 1), roundTo(t.W, 1), roundTo(t.H, 1)}
}

// reach scrolls the target into the middle of the view when it is out, by wheel turns the page sees as a person's.
func (r *recorder) reach(sel string, t *recordTarget) (*recordTarget, error) {
	for i := 0; i < 4 && !t.InView; i++ {
		cy := t.Y + t.H/2
		if err := r.scroll(int(cy-float64(r.sc.Viewport.Height)/2), 0); err != nil {
			return nil, err
		}
		if err := r.hold(120); err != nil {
			return nil, err
		}
		var err error
		if t, err = r.find(sel); err != nil || !t.Found {
			return nil, fmt.Errorf("%q: %v", sel, err)
		}
	}
	return t, nil
}

const scrollJS = `(function (dy, x, y) {
  var e = document.elementFromPoint(x, y);
  while (e && e !== document.documentElement && e !== document.body) {
    var o = getComputedStyle(e).overflowY;
    if ((o === 'auto' || o === 'scroll') && e.scrollHeight > e.clientHeight) { e.scrollBy({top: dy, behavior: 'instant'}); return; }
    e = e.parentElement;
  }
  window.scrollBy({top: dy, behavior: 'instant'});
})(%f, %f, %f)`

// scroll turns the wheel by dy CSS pixels over ms, eased, at the cursor.
func (r *recorder) scroll(dy, ms int) error {
	if dy == 0 {
		return nil
	}
	if !r.placed {
		r.px, r.py, r.placed = float64(r.sc.Viewport.Width)/2, float64(r.sc.Viewport.Height)/2, true
	}
	if ms <= 0 {
		ms = int(math.Min(1600, math.Max(500, math.Abs(float64(dy))*1.2)))
	}
	n := int(math.Max(2, math.Round(float64(ms)/r.dt)))
	t0 := r.t()
	done := 0.0
	for i := 1; i <= n; i++ {
		goal := float64(dy) * smooth(float64(i)/float64(n))
		d := goal - done
		done = goal
		// The wheel event itself is answered only once the scroll is handled on the compositor, which waits for a frame of
		// ours; the page is scrolled from inside instead, the element under the cursor if it scrolls, else the window.
		if _, err := r.eval(fmt.Sprintf(scrollJS, d, r.px, r.py)); err != nil {
			return err
		}
		if err := r.frame(); err != nil {
			return err
		}
	}
	e := RecordEvent{T: t0, End: r.t(), Type: "scroll", X: recF(r.px), Y: recF(r.py), By: dy}
	r.emit(e)
	return nil
}

func (r *recorder) click(sel string, timeout time.Duration, typ string) (*recordTarget, error) {
	t, err := r.need(sel, timeout)
	if err != nil {
		return nil, err
	}
	if t, err = r.reach(sel, t); err != nil {
		return nil, err
	}
	x, y := t.center(r.sc.Viewport.Width, r.sc.Viewport.Height)
	if err := r.moveTo(x, y, 0); err != nil {
		return nil, err
	}
	// the page may have moved while the cursor was on its way
	if t2, err := r.find(sel); err == nil && t2.Found {
		t = t2
	}
	if err := r.hold(3 * r.dt); err != nil {
		return nil, err
	}
	e := RecordEvent{T: r.t(), Type: typ, X: recF(r.px), Y: recF(r.py), BBox: bbox(t), On: sel, Tag: t.Tag, Label: t.Label}
	if typ == "click" {
		r.mouse("mousePressed", r.px, r.py, map[string]any{"button": "left", "buttons": 1, "clickCount": 1})
		if err := r.hold(3 * r.dt); err != nil {
			return nil, err
		}
		r.mouse("mouseReleased", r.px, r.py, map[string]any{"button": "left", "buttons": 0, "clickCount": 1})
		if err := r.frame(); err != nil {
			return nil, err
		}
	}
	e.End = r.t()
	r.emit(e)
	return t, nil
}

func (r *recorder) key(name string) error {
	k := recordKeys[name]
	down := map[string]any{"type": "keyDown", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "nativeVirtualKeyCode": k.vk}
	if k.text != "" {
		down["text"] = k.text
	} else {
		down["type"] = "rawKeyDown"
	}
	r.input("Input.dispatchKeyEvent", down)
	if err := r.frame(); err != nil {
		return err
	}
	r.input("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "nativeVirtualKeyCode": k.vk})
	return r.frame()
}

// typeText sends the text a character at a time at a person's speed (the time between two is a little uneven, the same
// way every time).
func (r *recorder) typeText(text string) error {
	var carry float64
	for i, ch := range []rune(text) {
		s := string(ch)
		if ch == '\n' {
			if err := r.key("Enter"); err != nil {
				return err
			}
		} else {
			r.input("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "text": s, "key": s, "unmodifiedText": s})
			if err := r.frame(); err != nil {
				return err
			}
			r.input("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": s})
		}
		// the frame of the key itself is part of the gap
		carry += 52 + float64((i*37)%29) - r.dt
		frames := math.Floor(carry / r.dt)
		if frames < 0 {
			frames = 0
		}
		carry -= frames * r.dt
		if frames > 0 {
			if err := r.hold(frames * r.dt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *recorder) activeKind() (typ string, editable bool) {
	v, err := r.eval(`(function(){var e=document.activeElement;if(!e)return '';var t=e.tagName.toLowerCase();var ed=!!(e.isContentEditable||t==='input'||t==='textarea');return (ed?'1':'0')+(e.type||t)})()`)
	s, _ := v.(string)
	if err != nil || s == "" {
		return "", false
	}
	return s[1:], s[0] == '1'
}

// open loads a page and waits for it out of the clip: the frames of a load are the page's own time passing, but their
// number depends on the network, so the clip starts when the page has settled.
func (r *recorder) open(target string) error {
	r.mu.Lock()
	r.loaded = false
	r.mu.Unlock()
	ch := r.pipe.post("Page.navigate", map[string]any{"url": target}, r.session)
	wasRec := r.rec
	r.rec = false
	defer func() { r.rec = wasRec }()
	deadline := time.Now().Add(90 * time.Second)
	var reply *cdpReply
	for {
		if err := r.frame(); err != nil {
			return err
		}
		if reply == nil {
			select {
			case a := <-ch:
				if a.err != nil {
					return a.err
				}
				if e, _ := a.result["errorText"].(string); e != "" {
					return fmt.Errorf("%s: %s", target, e)
				}
				reply = &a
			default:
			}
		}
		r.mu.Lock()
		done := r.loaded && !r.busyLocked() && time.Since(r.netAt) > 400*time.Millisecond
		r.mu.Unlock()
		if reply != nil && done {
			v, _ := r.eval(`document.readyState === 'complete' && (!document.fonts || document.fonts.status === 'loaded') && Array.prototype.every.call(document.images, function (i) { return i.complete })`)
			if ok, _ := v.(bool); ok {
				break
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(locale.Pick("%s did not finish loading in 90 s", "%s не загрузилась за 90 с"), target)
		}
	}
	// a few more frames: what the page does on its first draw (a fade, a layout) is over
	for i := 0; i < 8; i++ {
		if err := r.frame(); err != nil {
			return err
		}
	}
	return nil
}

func (r *recorder) onEvent(ev cdpEvent) {
	if ev.SessionID != r.session {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev.Method {
	case "Page.loadEventFired":
		r.loaded = true
		r.netAt = time.Now()
	case "Network.requestWillBeSent":
		id, _ := ev.Params["requestId"].(string)
		typ, _ := ev.Params["type"].(string)
		if r.reqs == nil {
			r.reqs = map[string]recordReq{}
		}
		r.reqs[id] = recordReq{typ: typ, start: time.Now()}
		r.netAt = time.Now()
	case "Network.loadingFinished", "Network.loadingFailed":
		id, _ := ev.Params["requestId"].(string)
		delete(r.reqs, id)
		r.netAt = time.Now()
	}
}

func (r *recorder) startEncoder(out string, o RecordOptions) error {
	ff := DeckFFmpeg()
	if ff == "" {
		return errors.New(locale.Pick("ffmpeg is not installed", "ffmpeg не установлен"))
	}
	// The matrix is bt709 and written into the file: a browser reads an untagged HD clip as bt709 and an SD one as bt601,
	// and the colours of the interface must not move between the page and the clip.
	args := []string{"-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", strconv.Itoa(r.sc.FPS), "-c:v", "png", "-i", "-",
		"-vf", "scale=out_color_matrix=bt709:out_range=tv:flags=accurate_rnd+full_chroma_int,format=yuv420p",
		"-c:v", "libx264", "-preset", o.Preset, "-crf", strconv.Itoa(o.CRF), "-profile:v", "high", "-g", strconv.Itoa(r.sc.FPS), "-bf", "0",
		"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
		"-movflags", "+faststart", "-an", out}
	cmd := recNice(context.Background(), ff, args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r.ffIn = in
	r.shots = make(chan []byte, 6)
	r.ffDone = make(chan error, 1)
	go func() {
		var werr error
		for png := range r.shots {
			if werr == nil {
				_, werr = in.Write(png)
			}
		}
		_ = in.Close()
		err := cmd.Wait()
		if err == nil {
			err = werr
		}
		if err != nil && errBuf.Len() > 0 {
			err = fmt.Errorf("%w: %s", err, line(errBuf.String()))
		}
		r.ffDone <- err
	}()
	return nil
}

// Record plays a script and keeps the clip and the log in opt.Dir/records.
func Record(ctx context.Context, sc *RecordScenario, opt RecordOptions) (*RecordResult, error) {
	started := time.Now()
	bin := FindHeadlessShell()
	if bin == "" {
		return nil, errNoShell()
	}
	if opt.CRF == 0 && opt.Preset == "" {
		opt.CRF = 8
	}
	if opt.Preset == "" {
		opt.Preset = "fast"
	}
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}
	name := sc.Name
	if opt.Name != "" {
		name = opt.Name
	}
	if name == "" {
		name = "record"
	}
	if !recordName.MatchString(name) {
		return nil, recordErr("name is lowercase letters, digits, '.', '_' and '-'", "name: строчные буквы, цифры, '.', '_' и '-'")
	}
	dir := filepath.Join(opt.Dir, RecordsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &recorder{sc: sc, opt: opt, dt: 1000 / float64(sc.FPS)}
	// the values of the secrets are read before anything starts, so a missing one stops the run at once
	for i, s := range sc.Steps {
		if s.Secret == "" {
			continue
		}
		v := opt.Getenv(s.Secret)
		if v == "" {
			return nil, fmt.Errorf(locale.Pick("step %d: the environment variable %s is empty: run with `harnessly secret run --env %s=<name in the safe> -- ...`",
				"шаг %d: переменная окружения %s пуста: запусти через `harnessly secret run --env %s=<имя в сейфе> -- ...`"), i+1, s.Secret, s.Secret)
		}
		r.secret = append(r.secret, v)
	}
	base, err := monotonicMs()
	if err != nil {
		return nil, err
	}
	r.base = base + 5000

	finalVideo := filepath.Join(dir, name+".mp4")
	partVideo := finalVideo + ".part.mp4"
	defer os.Remove(partVideo)

	br, err := launchRecordBrowser(bin, sc.Scale)
	if err != nil {
		return nil, err
	}
	defer br.close()
	r.br, r.pipe = br, br.cdp
	if err := r.attach(); err != nil {
		return nil, err
	}
	if err := r.startEncoder(partVideo, opt); err != nil {
		return nil, err
	}
	encoderClosed := false
	closeEncoder := func() error {
		if encoderClosed {
			return nil
		}
		encoderClosed = true
		close(r.shots)
		return <-r.ffDone
	}
	defer func() { _ = closeEncoder() }()

	stop := r.pipe.on(r.onEvent)
	defer stop()

	steps := sc.Steps
	if sc.URL != "" && steps[0].Do != "open" {
		steps = append([]RecordStep{{Do: "open", URL: sc.URL}}, steps...)
	}
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.say("step %d/%d: %s", i+1, len(steps), s.Do)
		if err := r.play(s); err != nil {
			return nil, fmt.Errorf("%s %d (%s): %w", locale.Pick("step", "шаг"), i+1, s.Do, err)
		}
		if s.Pause != nil {
			if err := r.hold(float64(*s.Pause)); err != nil {
				return nil, err
			}
		}
	}
	if err := r.hold(600); err != nil {
		return nil, err
	}
	if err := closeEncoder(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	if r.frames == 0 {
		return nil, errors.New(locale.Pick("no frame was recorded", "ни одного кадра не записано"))
	}
	lg := RecordLog{V: 1, Name: name, Video: name + ".mp4", FPS: sc.FPS, Frames: r.frames, Dropped: r.dropped, DurMs: r.t(),
		Width: sc.Viewport.Width * sc.Scale, Height: sc.Viewport.Height * sc.Scale, Viewport: [2]int{sc.Viewport.Width, sc.Viewport.Height},
		Scale: sc.Scale, Theme: sc.Theme, Events: r.events, Cursor: r.cursor}
	lg.redact(r.secret)
	raw, err := json.Marshal(lg)
	if err != nil {
		return nil, err
	}
	if leaks(raw, r.secret) {
		return nil, errors.New(locale.Pick("a secret's value got into the log: nothing is kept", "значение секрета попало в журнал: ничего не сохранено"))
	}
	if err := os.Rename(partVideo, finalVideo); err != nil {
		return nil, err
	}
	logFile := filepath.Join(dir, name+".json")
	if err := os.WriteFile(logFile+".tmp", raw, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(logFile+".tmp", logFile); err != nil {
		return nil, err
	}
	return &RecordResult{Video: finalVideo, LogFile: logFile, Frames: r.frames, Dropped: r.dropped, DurMs: lg.DurMs, Wall: time.Since(started), Secrets: len(r.secret)}, nil
}

// attach opens the page under the clock's control.
func (r *recorder) attach() error {
	sc := r.sc
	t, err := r.pipe.sendLong("Target.createTarget", map[string]any{"url": "about:blank", "enableBeginFrameControl": true,
		"width": sc.Viewport.Width, "height": sc.Viewport.Height}, "", 90*time.Second)
	if err != nil {
		return err
	}
	target, _ := t["targetId"].(string)
	a, err := r.pipe.sendLong("Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, "", 90*time.Second)
	if err != nil {
		return err
	}
	r.session, _ = a["sessionId"].(string)
	for _, d := range []string{"Page", "Runtime", "Network"} {
		if _, err := r.cdp(d+".enable", nil); err != nil {
			return err
		}
	}
	if _, err := r.cdp("Emulation.setDeviceMetricsOverride", map[string]any{"width": sc.Viewport.Width, "height": sc.Viewport.Height,
		"deviceScaleFactor": sc.Scale, "mobile": false}); err != nil {
		return err
	}
	if _, err := r.cdp("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]any{
		{"name": "prefers-color-scheme", "value": sc.Theme}, {"name": "prefers-reduced-motion", "value": "no-preference"}}}); err != nil {
		return err
	}
	if sc.Locale != "" {
		_, _ = r.cdp("Emulation.setLocaleOverride", map[string]any{"locale": sc.Locale})
		_, _ = r.cdp("Network.setExtraHTTPHeaders", map[string]any{"headers": map[string]any{"Accept-Language": sc.Locale}})
	}
	// no cursor of the system is in a headless shot; the scene draws one from the log
	return nil
}

func (r *recorder) play(s RecordStep) error {
	timeout := 15 * time.Second
	if s.Timeout > 0 {
		timeout = time.Duration(s.Timeout) * time.Millisecond
	}
	switch s.Do {
	case "open":
		first := !r.rec
		if err := r.open(s.URL); err != nil {
			return err
		}
		r.rec = true
		v, _ := r.eval(`JSON.stringify({u: location.href, t: document.title})`)
		var d struct{ U, T string }
		if str, ok := v.(string); ok {
			_ = json.Unmarshal([]byte(str), &d)
		}
		if first {
			r.px, r.py, r.placed = float64(r.sc.Viewport.Width)*0.62, float64(r.sc.Viewport.Height)*0.78, true
		}
		r.emit(RecordEvent{T: r.t(), Type: "open", URL: d.U, Title: d.T})
		return r.hold(500)
	case "click":
		_, err := r.click(s.On, timeout, "click")
		if err == nil {
			err = r.hold(250)
		}
		return err
	case "hover":
		_, err := r.click(s.On, timeout, "hover")
		if err == nil {
			err = r.hold(450)
		}
		return err
	case "type":
		sel := s.Into
		if sel != "" {
			if _, err := r.click(sel, timeout, "click"); err != nil {
				return err
			}
			if err := r.hold(200); err != nil {
				return err
			}
		}
		kind, editable := r.activeKind()
		if !editable {
			return errors.New(locale.Pick("no field is focused to type in: name it in into", "нет поля в фокусе для ввода: укажи его в into"))
		}
		text := s.Text
		masked := false
		switch {
		case s.Secret != "":
			if kind != "password" {
				return errors.New(locale.Pick("a secret is typed only into a password field: its text would be on the screen", "секрет вводится только в поле пароля: иначе его текст окажется на экране"))
			}
			text, masked = r.opt.Getenv(s.Secret), true
		case kind == "password":
			return errors.New(locale.Pick("a password is typed from a secret (secret: ENV_NAME), not as text", "пароль вводится из секрета (secret: ИМЯ_ПЕРЕМЕННОЙ), а не текстом"))
		}
		e := RecordEvent{T: r.t(), Type: "type", On: sel, Masked: masked, Text: text}
		if masked {
			e.Text = RecordMask
		}
		if x, y := r.px, r.py; r.placed {
			e.X, e.Y = recF(x), recF(y)
		}
		if err := r.typeText(text); err != nil {
			return err
		}
		e.End = r.t()
		r.emit(e)
		return r.hold(250)
	case "key":
		t0 := r.t()
		if err := r.key(s.Key); err != nil {
			return err
		}
		r.emit(RecordEvent{T: t0, End: r.t(), Type: "key", Key: s.Key})
		return r.hold(250)
	case "scroll":
		dy := s.By
		if s.To != "" {
			t, err := r.need(s.To, timeout)
			if err != nil {
				return err
			}
			dy = int(t.Y + t.H/2 - float64(r.sc.Viewport.Height)/2)
		}
		if err := r.scroll(dy, s.Ms); err != nil {
			return err
		}
		return r.hold(300)
	case "wait":
		return r.hold(float64(s.Ms))
	case "wait-for":
		_, err := r.need(s.On, timeout)
		return err
	}
	return nil
}

// launchRecordBrowser starts chrome-headless-shell with its frames under the caller's control, at the lowest priority.
func launchRecordBrowser(bin string, scale int) (*browser, error) {
	type started struct {
		b   *browser
		err error
	}
	ch := make(chan started, 1)
	go func() {
		lowerPriority()
		b, err := launchRecordBrowserNow(bin, scale)
		ch <- started{b, err}
	}()
	s := <-ch
	return s.b, s.err
}

func launchRecordBrowserNow(bin string, scale int) (*browser, error) {
	profile, err := os.MkdirTemp("", "harnessly-record-")
	if err != nil {
		return nil, err
	}
	toChrome, ours, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	theirs, fromChrome, err := os.Pipe()
	if err != nil {
		_ = toChrome.Close()
		_ = ours.Close()
		_ = os.RemoveAll(profile)
		return nil, err
	}
	args := []string{"--headless", "--remote-debugging-pipe", "--user-data-dir=" + profile, "--no-first-run", "--hide-scrollbars", "--mute-audio",
		"--disable-dev-shm-usage", "--force-device-scale-factor=" + strconv.Itoa(scale), "--force-color-profile=srgb", "--font-render-hinting=none",
		"--deterministic-mode", "--enable-begin-frame-control", "--run-all-compositor-stages-before-draw", "--disable-new-content-rendering-timeout",
		"--disable-threaded-animation", "--disable-threaded-scrolling", "--disable-checker-imaging", "--disable-image-animation-resync",
		"--disable-background-timer-throttling", "--disable-renderer-backgrounding", "--disable-backgrounding-occluded-windows"}
	args = append(append(args, strings.Fields(os.Getenv("RUNTIME_DESIGN_CHROME_ARGS"))...), "about:blank")
	cmd := exec.Command(bin, args...)
	cmd.ExtraFiles = []*os.File{toChrome, fromChrome}
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{toChrome, ours, theirs, fromChrome} {
			_ = f.Close()
		}
		_ = os.RemoveAll(profile)
		return nil, err
	}
	_ = toChrome.Close()
	_ = fromChrome.Close()
	b := &browser{cmd: cmd, profile: profile, exited: make(chan struct{}), cdp: newCDPPipe(ours, theirs)}
	go func() {
		_ = cmd.Wait()
		_ = theirs.Close()
		close(b.exited)
	}()
	return b, nil
}
