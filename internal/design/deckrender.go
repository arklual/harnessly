package design

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// What the static exports (PDF, PNG, PPTX, HTML) share: the deck's slides in order with their notes, and a browser
// of this machine that opens one slide per tab at the slide's own size. The exports run one at a time (deckjobs.go),
// each with a browser of its own that goes with the job.

const (
	deckDefaultW = 1920
	deckDefaultH = 1080
	// deckLoadWait is how long a slide may take to load; deckSettle lets its fonts and first frame land.
	deckLoadWait = 25 * time.Second
	deckSettle   = 900 * time.Millisecond
	// deckLongCall bounds the calls that take long on a big deck (printing every slide).
	deckLongCall = 4 * time.Minute
)

// deckSlideSrc is a slide to export: its board and its speaker notes.
type deckSlideSrc struct {
	ID    string
	Title string
	Path  string
	W, H  int
	Notes string
}

// deckSlides are the slides of a deck in the order of its pages, with the notes written for them.
func (l *Library) deckSlides(artifact string) ([]deckSlideSrc, error) {
	a := l.ReadArtifact(artifact)
	if a == nil {
		return nil, errors.New(locale.Pick("The design is gone", "Макета больше нет"))
	}
	out := make([]deckSlideSrc, 0, len(a.Boards))
	for _, b := range a.Boards {
		s := deckSlideSrc{ID: b.ID, Title: b.Title, Path: b.Path, W: deckDefaultW, H: deckDefaultH}
		if b.W != nil && b.H != nil && *b.W > 0 && *b.H > 0 {
			s.W, s.H = *b.W, *b.H
		}
		if deckIDRe.MatchString(b.ID) {
			if raw, err := os.ReadFile(filepath.Join(l.ArtifactDir(artifact), DeckNotesDir, b.ID+".md")); err == nil {
				s.Notes = strings.TrimSpace(string(raw))
			}
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New(locale.Pick("The deck has no slides yet", "В презентации пока нет слайдов"))
	}
	return out, nil
}

// sendLong is send with a wait of its own: printing a deck takes longer than the 15 s of an ordinary call.
func (p *cdpPipe) sendLong(method string, params map[string]any, session string, wait time.Duration) (map[string]any, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("CDP closed: " + method)
	}
	id := p.nextID
	p.nextID++
	ch := make(chan cdpReply, 1)
	p.pending[id] = pendingCall{method: method, ch: ch}
	if params == nil {
		params = map[string]any{}
	}
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
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.result, r.err
	case <-t.C:
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("CDP timeout: " + method)
	}
}

// deckShooter is the browser of one export.
type deckShooter struct {
	l        *Library
	br       *browser
	base     string
	artifact string
}

// errNoChrome is the reason a job gives on a machine without a browser.
func errNoChrome() error {
	return errors.New(locale.Pick("Chrome is not installed on this machine: install google-chrome or set RUNTIME_DESIGN_CHROME",
		"На машине нет Chrome: поставьте google-chrome или задайте RUNTIME_DESIGN_CHROME"))
}

func (l *Library) newDeckShooter(artifact string) (*deckShooter, error) {
	bin := l.findChrome()
	if bin == "" {
		return nil, errNoChrome()
	}
	base, err := libraryBase(l)
	if err != nil {
		return nil, err
	}
	br, err := l.launchBrowser(bin)
	if err != nil {
		return nil, err
	}
	return &deckShooter{l: l, br: br, base: base, artifact: artifact}, nil
}

func (s *deckShooter) close() { s.br.close() }

// url is where a slide's board is served.
func (s *deckShooter) url(sl deckSlideSrc) string { return s.base + BoardHref(s.artifact, sl.Path) }

// deckTab is one tab of the export's browser.
type deckTab struct {
	s       *deckShooter
	target  string
	session string
}

func (s *deckShooter) tab() (*deckTab, error) {
	t, err := s.br.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return nil, err
	}
	target, _ := t["targetId"].(string)
	a, err := s.br.cdp.send("Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, "")
	if err != nil {
		return nil, err
	}
	session, _ := a["sessionId"].(string)
	tab := &deckTab{s: s, target: target, session: session}
	for _, d := range []string{"Page", "Runtime"} {
		if _, err := tab.call(d+".enable", nil); err != nil {
			tab.close()
			return nil, err
		}
	}
	return tab, nil
}

func (t *deckTab) close() {
	_, _ = t.s.br.cdp.send("Target.closeTarget", map[string]any{"targetId": t.target}, "")
}

func (t *deckTab) call(method string, params map[string]any) (map[string]any, error) {
	return t.s.br.cdp.send(method, params, t.session)
}

func (t *deckTab) callLong(method string, params map[string]any) (map[string]any, error) {
	return t.s.br.cdp.sendLong(method, params, t.session, deckLongCall)
}

// eval runs a script of ours in the page through the browser's DevTools protocol (Runtime.evaluate, not JavaScript's
// eval; the scripts are constants of this package or built from numbers). A promise is awaited and and gives back its value; a thrown error is an error.
func (t *deckTab) eval(expr string) (any, error) {
	r, err := t.s.br.cdp.sendLong("Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true},
		t.session, deckLongCall)
	if err != nil {
		return nil, err
	}
	if ex, ok := r["exceptionDetails"].(map[string]any); ok {
		msg, _ := ex["text"].(string)
		if e, ok := ex["exception"].(map[string]any); ok {
			if d, ok := e["description"].(string); ok {
				msg = d
			}
		}
		return nil, errors.New(line(msg))
	}
	res, _ := r["result"].(map[string]any)
	return res["value"], nil
}

// navigate loads a page and waits for its load event.
func (t *deckTab) navigate(url string) error {
	loaded := make(chan struct{})
	var once sync.Once
	stop := t.s.br.cdp.on(func(ev cdpEvent) {
		if ev.SessionID == t.session && ev.Method == "Page.loadEventFired" {
			once.Do(func() { close(loaded) })
		}
	})
	defer stop()
	if _, err := t.call("Page.navigate", map[string]any{"url": url}); err != nil {
		return err
	}
	select {
	case <-loaded:
	case <-time.After(deckLoadWait):
	}
	return nil
}

// view sets the size of the tab and the scale of its pixels. Motion is reduced so that a slide that fades in is shown
// as it ends up.
func (t *deckTab) view(w, h int, scale float64) error {
	if _, err := t.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": scale, "mobile": false}); err != nil {
		return err
	}
	_, err := t.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]any{{"name": "prefers-reduced-motion", "value": "reduce"}}})
	return err
}

const deckReady = `(async () => {
  document.querySelectorAll('[data-tk-library-bar]').forEach((n) => n.remove());
  if (document.fonts && document.fonts.ready) await document.fonts.ready;
  return true;
})()`

// open shows a slide in the tab at its own size, fonts loaded and the library's bar gone.
func (t *deckTab) open(sl deckSlideSrc, scale float64) error {
	if err := t.view(sl.W, sl.H, scale); err != nil {
		return err
	}
	if err := t.navigate(t.s.url(sl)); err != nil {
		return err
	}
	if _, err := t.eval(deckReady); err != nil {
		return err
	}
	time.Sleep(deckSettle)
	_, err := t.eval(deckReady)
	return err
}

// png is a picture of the tab (the whole viewport).
func (t *deckTab) png() ([]byte, error) {
	r, err := t.callLong("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	data, _ := r["data"].(string)
	if data == "" {
		return nil, errors.New("the browser gave no picture")
	}
	return base64.StdEncoding.DecodeString(data)
}

// deckScale is the scale of pixels that makes a slide w px wide at least 1920 px in a picture (at most 3).
func deckScale(w int) float64 {
	if w <= 0 {
		return 1
	}
	s := float64(deckDefaultW) / float64(w)
	if s < 1 {
		return 1
	}
	if s > 3 {
		return 3
	}
	return s
}

// jsString is a Go string as a JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	// U+2028 and U+2029 end a line in older JavaScript; JSON allows them inside a string.
	return strings.NewReplacer(" ", ` `, " ", ` `).Replace(string(b))
}

func sleepSettle() { time.Sleep(deckSettle) }
