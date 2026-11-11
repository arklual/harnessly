package design

// The proof of a migrated board: the board and the board built from its page, each drawn by the project's own engine
// in Chrome from the project's folder, the same size, the animations stopped at their start, compared pixel by pixel.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	goimage "image"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	compareWidth     = 1280
	compareHeight    = 800
	compareMaxHeight = 6000
	compareSettle    = 400 * time.Millisecond
	compareMaxWait   = 15 * time.Second
	compareShotTries = 12
	compareShotGap   = 250 * time.Millisecond
)

const compareSameWorld = `(() => {
  let seed = 20260101;
  Math.random = () => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed / 2147483648; };
  const RealDate = Date, at = Date.UTC(2026, 0, 15, 9, 30);
  function SameDate(...args) {
    if (!new.target) return new RealDate(at).toString();
    return args.length ? new RealDate(...args) : new RealDate(at);
  }
  SameDate.prototype = RealDate.prototype;
  SameDate.now = () => at;
  SameDate.UTC = RealDate.UTC;
  SameDate.parse = RealDate.parse;
  window.Date = SameDate;
})();`

type chromeComparer struct {
	l       *Library
	dir     string
	realDir string
	prefix  string
	b       *browser
	session string
	ln      net.Listener
	srv     *http.Server
	base    string

	mu      sync.Mutex
	overlay map[string][]byte
	shots   map[string][]byte // the original board's picture, by file
}

// newChromeComparer starts a browser and a server of the project's folder on the loopback.
func (l *Library) newChromeComparer(dir string) (*chromeComparer, error) {
	bin := l.findChrome()
	if bin == "" {
		return nil, errors.New("a migration proves each page against its board in Chrome, and this machine has none (set RUNTIME_DESIGN_CHROME)")
	}
	c := &chromeComparer{l: l, dir: dir, overlay: map[string][]byte{}, shots: map[string][]byte{}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c.ln = ln
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	c.realDir = real
	// Only this browser knows the address: the server answers under a random prefix.
	c.prefix = "/" + randomHex(16) + "/"
	c.base = "http://" + ln.Addr().String() + c.prefix
	c.srv = &http.Server{Handler: http.HandlerFunc(c.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = c.srv.Serve(ln) }()
	b, err := l.launchBrowser(bin)
	if err != nil {
		c.Close()
		return nil, err
	}
	c.b = b
	t, err := b.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		c.Close()
		return nil, err
	}
	a, err := b.cdp.send("Target.attachToTarget", map[string]any{"targetId": t["targetId"], "flatten": true}, "")
	if err != nil {
		c.Close()
		return nil, err
	}
	c.session, _ = a["sessionId"].(string)
	for _, d := range []string{"Page", "Runtime"} {
		if _, err := b.cdp.send(d+".enable", nil, c.session); err != nil {
			c.Close()
			return nil, err
		}
	}
	// Both drawings get the same clock and the same random numbers, so a board that shows the time or random data
	// draws the same twice.
	if _, err := b.cdp.send("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": compareSameWorld}, c.session); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *chromeComparer) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, c.prefix) {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, c.prefix)), "/")
	c.mu.Lock()
	body, over := c.overlay[rel]
	c.mu.Unlock()
	if over {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
		return
	}
	// Only the project's own files: a symbolic link that leads out of it is not followed.
	file, err := filepath.EvalSymlinks(filepath.Join(c.realDir, filepath.FromSlash(rel)))
	if err != nil || !insideProject(c.realDir, file) {
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(file)
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(rel, ".dc.html") {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeFile(w, r, file)
}

func (c *chromeComparer) Close() {
	if c.b != nil {
		c.b.close()
	}
	if c.srv != nil {
		_ = c.srv.Close()
	}
}

// shot draws the board file (or the bytes given for it) and returns its picture.
func (c *chromeComparer) shot(boardFile string, body []byte) ([]byte, error) {
	c.mu.Lock()
	c.overlay = map[string][]byte{}
	if body != nil {
		c.overlay[boardFile] = body
	}
	c.mu.Unlock()
	cdp, s := c.b.cdp, c.session
	if _, err := cdp.send("Emulation.setDeviceMetricsOverride", map[string]any{"width": compareWidth, "height": compareHeight, "deviceScaleFactor": 1, "mobile": false}, s); err != nil {
		return nil, err
	}
	loaded := make(chan struct{})
	var once sync.Once
	stop := cdp.on(func(ev cdpEvent) {
		if ev.SessionID == s && ev.Method == "Page.loadEventFired" {
			once.Do(func() { close(loaded) })
		}
	})
	defer stop()
	u := c.base + (&url.URL{Path: boardFile}).EscapedPath()
	if _, err := cdp.send("Page.navigate", map[string]any{"url": u}, s); err != nil {
		return nil, err
	}
	select {
	case <-loaded:
	case <-time.After(checkLoadTimeout):
		return nil, fmt.Errorf("%s did not load in %s", boardFile, checkLoadTimeout)
	}
	// Drawn when the page stops changing: the same DOM and every font and picture loaded, twice in a row.
	const probe = `(() => { const r = document.getElementById('dc-root') || document.body;
	  const imgs = Array.prototype.every.call(document.images, (i) => i.complete);
	  return (r ? r.innerHTML.length + ':' + r.querySelectorAll('*').length : 'none') + ':' + document.fonts.status + ':' + imgs; })()`
	last, same := "", 0
	deadline := time.Now().Add(compareMaxWait)
	for time.Now().Before(deadline) {
		time.Sleep(compareSettle)
		r, err := cdp.send("Runtime.evaluate", map[string]any{"expression": probe, "returnByValue": true}, s)
		if err != nil {
			return nil, err
		}
		v := evalString(r)
		if v == last && strings.HasSuffix(v, ":loaded:true") && !strings.HasPrefix(v, "0:") {
			same++
			if same >= 2 {
				break
			}
		} else {
			same = 0
		}
		last = v
	}
	// Every animation at its start, so two loads of a moving board give one picture.
	const freeze = `document.getAnimations().forEach((a) => { try { a.pause(); a.currentTime = 0; } catch (e) {} });
	  document.querySelectorAll('video').forEach((v) => { try { v.pause(); v.currentTime = 0; } catch (e) {} }); true`
	if _, err := cdp.send("Runtime.evaluate", map[string]any{"expression": freeze}, s); err != nil {
		return nil, err
	}
	// A chart drawn by a script moves without changing the DOM: the picture is taken once it stays the same.
	var prev []byte
	for i := 0; i < compareShotTries; i++ {
		time.Sleep(compareShotGap)
		pic, err := c.capture()
		if err != nil {
			return nil, err
		}
		if prev != nil && bytes.Equal(prev, pic) {
			return pic, nil
		}
		prev = pic
		if _, err := cdp.send("Runtime.evaluate", map[string]any{"expression": freeze}, s); err != nil {
			return nil, err
		}
	}
	return prev, nil
}

// capture is the whole page as PNG, at most compareMaxHeight high.
func (c *chromeComparer) capture() ([]byte, error) {
	cdp, s := c.b.cdp, c.session
	m, err := cdp.send("Page.getLayoutMetrics", nil, s)
	if err != nil {
		return nil, err
	}
	w, h := float64(compareWidth), float64(compareHeight)
	if cs, ok := m["cssContentSize"].(map[string]any); ok {
		if v, ok := cs["width"].(float64); ok && v > w {
			w = v
		}
		if v, ok := cs["height"].(float64); ok && v > h {
			h = v
		}
	}
	if h > compareMaxHeight {
		h = compareMaxHeight
	}
	if w > 4*compareWidth {
		w = 4 * compareWidth
	}
	pic, err := cdp.send("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true,
		"clip": map[string]any{"x": 0, "y": 0, "width": w, "height": h, "scale": 1}}, s)
	if err != nil {
		return nil, err
	}
	data, _ := pic["data"].(string)
	return base64.StdEncoding.DecodeString(data)
}

func evalString(r map[string]any) string {
	res, _ := r["result"].(map[string]any)
	v, _ := res["value"].(string)
	return v
}

// Compare draws the board as it is on disk and as built, and counts the pixels that differ; -1 when the board
// itself does not draw the same twice.
func (c *chromeComparer) Compare(boardFile string, original, built []byte) (int, error) {
	c.mu.Lock()
	before, ok := c.shots[boardFile]
	c.mu.Unlock()
	if !ok {
		var err error
		before, err = c.shot(boardFile, original)
		if err != nil {
			return 0, err
		}
		c.mu.Lock()
		c.shots[boardFile] = before
		c.mu.Unlock()
	}
	after, err := c.shot(boardFile, built)
	if err != nil {
		return 0, err
	}
	n, err := pixelsDiffer(before, after)
	if err != nil || n == 0 {
		return n, err
	}
	again, err := c.shot(boardFile, original)
	if err != nil {
		return 0, err
	}
	if m, err := pixelsDiffer(before, again); err != nil || m != 0 {
		return -1, err
	}
	return n, nil
}

// pixelsDiffer counts the pixels two PNG pictures differ in; pictures of different sizes differ in all of the larger.
func pixelsDiffer(a, b []byte) (int, error) {
	if bytes.Equal(a, b) {
		return 0, nil
	}
	x, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return 0, err
	}
	y, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	bx, by := x.Bounds(), y.Bounds()
	if bx.Dx() != by.Dx() || bx.Dy() != by.Dy() {
		ax, ay := bx.Dx()*bx.Dy(), by.Dx()*by.Dy()
		if ay > ax {
			ax = ay
		}
		return ax, nil
	}
	n := 0
	for yy := 0; yy < bx.Dy(); yy++ {
		for xx := 0; xx < bx.Dx(); xx++ {
			if !samePixel(x, y, bx.Min.X+xx, bx.Min.Y+yy, by.Min.X+xx, by.Min.Y+yy) {
				n++
			}
		}
	}
	return n, nil
}

func samePixel(x, y goimage.Image, x1, y1, x2, y2 int) bool {
	r1, g1, b1, a1 := x.At(x1, y1).RGBA()
	r2, g2, b2, a2 := y.At(x2, y2).RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
