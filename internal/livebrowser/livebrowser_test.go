package livebrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRefusal(t *testing.T) {
	s := New(Config{DataDir: t.TempDir(), Lookup: func(host string) ([]string, error) {
		switch host {
		case "example.com":
			return []string{"93.184.216.34"}, nil
		case "intranet.local":
			return []string{"10.1.2.3"}, nil
		}
		return nil, errors.New("no such host")
	}})
	for raw, want := range map[string]string{
		"https://example.com/a?b=c":   "",
		"http://example.com":          "",
		"ftp://example.com":           "http(s)",
		"https://user:pw@example.com": "password",
		"https://127.0.0.1:3000/":     "proxy",
		"https://intranet.local/":     "proxy",
		"https://nowhere.example/":    "resolve",
		"not an address":              "http(s)",
	} {
		got := s.Refusal(raw)
		if (want == "" && got != "") || (want != "" && !strings.Contains(got, want)) {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}

func TestNoBrowserOnTheMachine(t *testing.T) {
	s := New(Config{DataDir: t.TempDir()})
	s.lookup = func(string) (string, error) { return "", errors.New("nope") }
	if s.Available() {
		t.Fatal("available without a binary")
	}
	if _, err := s.Open(context.Background(), "t1", 800, 600, 1); err == nil {
		t.Fatal("opened without a binary")
	}
}

// The rest runs the real browser: skipped where the machine has none (CI).
func chromeOrSkip(t *testing.T) *Service {
	t.Helper()
	s := New(Config{DataDir: t.TempDir(), IdleAfter: 200 * time.Millisecond, Log: func(level, msg string, kv ...any) { t.Logf("%s: %s %v", level, msg, kv) }})
	if !s.Available() {
		t.Skip("no Chrome on this machine")
	}
	// The tests' pages live on the machine, which the real rule refuses; a host of "refused." stands for what it would.
	s.resolve = func(host string) (string, string) {
		if strings.HasPrefix(host, "refused.") {
			return "", "refused by the test gate"
		}
		return "127.0.0.1", ""
	}
	t.Cleanup(s.Close)
	return s
}

// A page's own requests, redirects and windows are held to the gate like the address typed.
func TestPageRequestsAreGated(t *testing.T) {
	s := chromeOrSkip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Access-Control-Allow-Origin", "*")
			_, _ = w.Write([]byte("ok"))
		case "/redirect":
			http.Redirect(w, r, "http://refused.test/landing", http.StatusFound)
		case "/frame":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<script>fetch('http://refused.test:1/f').then(()=>{window.r='ok'},()=>{window.r='denied'})</script>`))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<title>Gate</title><body><iframe id="f" src="/frame"></iframe><script>
				window.out = {};
				fetch('/ok').then(r => r.text()).then(t => { window.out.ok = t }, () => { window.out.ok = 'denied' });
				fetch('http://refused.test:1/x').then(() => { window.out.refused = 'reached' }, () => { window.out.refused = 'denied' });
				fetch('/redirect').then(() => { window.out.redirect = 'reached' }, () => { window.out.redirect = 'denied' });
				// A name only the egress resolves: it answers when the traffic goes through it.
				fetch('http://via.test:' + location.port + '/ok').then(r => r.text()).then(t => { window.out.via = t }, () => { window.out.via = 'denied' });
				const ws = new WebSocket('ws://refused.test:1/');
				ws.onopen = () => { window.out.ws = 'reached' };
				ws.onerror = () => { window.out.ws = 'denied' };
			</script></body>`))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tab, err := s.Open(ctx, "gate", 640, 480, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Close()
	if err := tab.Navigate("http://refused.test/"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("navigate to a refused address: %v", err)
	}
	if err := tab.Navigate(srv.URL + "/"); err != nil {
		t.Fatal(err)
	}
	var out struct{ Ok, Refused, Redirect, Via, Ws string }
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := tab.Evaluate(`window.out || {}`, &out); err == nil && out.Ok != "" && out.Refused != "" && out.Redirect != "" && out.Via != "" && out.Ws != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if out.Ok != "ok" || out.Refused != "denied" || out.Redirect != "denied" || out.Via != "ok" || out.Ws != "denied" {
		t.Fatalf("requests: %+v", out)
	}
	// The frame's own request went through the gate too.
	var inFrame string
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := tab.Evaluate(`(document.getElementById('f').contentWindow.r || '')`, &inFrame); err == nil && inFrame != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if inFrame != "denied" {
		t.Fatalf("frame request: %q", inFrame)
	}
	// A window to a refused address closes and this page stays; one to an allowed address moves this page there.
	if err := tab.Evaluate(`window.open('http://refused.test/pop'), true`, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if info, _ := tab.Info(); info.URL != srv.URL+"/" {
		t.Fatalf("after a refused window: %+v", info)
	}
	if err := tab.Evaluate(`window.open('`+srv.URL+`/ok'), true`, nil); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	var info Info
	for time.Now().Before(deadline) {
		info, _ = tab.Info()
		if info.URL == srv.URL+"/ok" && !info.Loading {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info.URL != srv.URL+"/ok" {
		t.Fatalf("after an allowed window: %+v", info)
	}
}

func TestTabNavigatesAndCasts(t *testing.T) {
	s := chromeOrSkip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Проба</title><body style="background:#fff"><h1>Привет</h1><input id="q" autofocus><a href="/two">two</a></body>`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tab, err := s.Open(ctx, "t1", 640, 480, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Open(ctx, "t1", 640, 480, 1); again != tab {
		t.Fatal("a second open of the same tab made another page")
	}
	if err := tab.StartScreencast(); err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(srv.URL + "/"); err != nil {
		t.Fatal(err)
	}
	var info Info
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, _ = tab.Info()
		if info.Title == "Проба" && !info.Loading {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info.Title != "Проба" || info.URL != srv.URL+"/" {
		t.Fatalf("info %+v", info)
	}
	select {
	case f := <-tab.Frames():
		if len(f.Data) < 100 || f.Data[0] != 0xff || f.Data[1] != 0xd8 {
			t.Fatalf("not a JPEG: %d bytes", len(f.Data))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no frame")
	}
	// Typing lands in the focused field; a paste too.
	if err := tab.Key("keyDown", "a", "KeyA", "a", 65, 0); err != nil {
		t.Fatal(err)
	}
	_ = tab.Key("keyUp", "a", "KeyA", "", 65, 0)
	if err := tab.InsertText("бв"); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := tab.Evaluate(`document.getElementById('q').value`, &value); err != nil || value != "aбв" {
		t.Fatalf("typed %q (%v)", value, err)
	}
	// A click on the link moves the page; back returns.
	var box struct{ X, Y, W, H float64 }
	if err := tab.Evaluate(`(function(){const r=document.querySelector('a').getBoundingClientRect();return {X:r.x,Y:r.y,W:r.width,H:r.height}})()`, &box); err != nil {
		t.Fatal(err)
	}
	x, y := box.X+box.W/2, box.Y+box.H/2
	_ = tab.Mouse("mouseMoved", x, y, "none", 0, 0, 0)
	_ = tab.Mouse("mousePressed", x, y, "left", 1, 0, 1)
	_ = tab.Mouse("mouseReleased", x, y, "left", 0, 0, 1)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, _ = tab.Info()
		if strings.HasSuffix(info.URL, "/two") && !info.Loading {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.HasSuffix(info.URL, "/two") || !info.CanBack {
		t.Fatalf("after the click: %+v", info)
	}
	if err := tab.Back(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, _ = tab.Info()
		if info.URL == srv.URL+"/" && !info.Loading {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if info.URL != srv.URL+"/" || !info.CanForward {
		t.Fatalf("after back: %+v", info)
	}
	if err := tab.Resize(800, 600, 2); err != nil {
		t.Fatal(err)
	}
	tab.Close()
	select {
	case <-tab.Ended():
	case <-time.After(5 * time.Second):
		t.Fatal("the tab did not end")
	}
	// Without tabs the browser idles out.
	deadline = time.Now().Add(5 * time.Second)
	for s.Running() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if s.Running() {
		t.Fatal("the browser stayed up past IdleAfter")
	}
}

func TestSocketOpensAndSendsFrames(t *testing.T) {
	s := chromeOrSkip(t)
	sk := &Socket{S: s}
	srv := httptest.NewServer(sk)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/browser/live?tab=t2", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(cdpReadLimit)
	send := func(v any) {
		raw, _ := json.Marshal(v)
		if err := ws.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"t": "open", "url": "", "w": 500, "h": 400, "dpr": 1})
	var gotReady, gotPage, gotFrame, gotRefusal, gotActed bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !(gotReady && gotPage && gotFrame && gotRefusal && gotActed) {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageBinary {
			gotFrame = len(data) > 100
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		switch m["t"] {
		case "ready":
			gotReady = true
			// A refused address (a server of the machine, in the real gate) comes back as an error.
			send(map[string]any{"t": "nav", "url": "http://refused.test/"})
			send(map[string]any{"t": "mouse", "k": "move", "x": 10, "y": 10, "b": "none"})
			send(map[string]any{"t": "size", "w": 600, "h": 400, "dpr": 1})
			// The agent's action goes through the same socket and comes back with its id.
			send(map[string]any{"t": "act", "id": "s1", "op": "snapshot"})
		case "acted":
			if m["id"] == "s1" && m["ok"] == true && strings.HasPrefix(m["text"].(string), "url: ") {
				gotActed = true
			}
		case "page":
			gotPage = true
		case "error":
			if strings.Contains(m["text"].(string), "refused") {
				gotRefusal = true
			}
		}
	}
	if !gotReady || !gotPage || !gotFrame || !gotRefusal || !gotActed {
		t.Fatalf("ready %v page %v frame %v refusal %v acted %v", gotReady, gotPage, gotFrame, gotRefusal, gotActed)
	}
	if s.Tabs() != 1 {
		t.Fatalf("tabs %d", s.Tabs())
	}
}

// A pane that acks its pictures gets at most two on the way (HRN-711): a page that never stops moving sends no more
// until the acks come. A page that stands gets one still shot in the screen's own scale, and its caret's blinks do not
// take the sharp picture away.
func TestFramesArePacedAndStillShotIsSharp(t *testing.T) {
	s := chromeOrSkip(t)
	sk := &Socket{S: s}
	srv := httptest.NewServer(sk)
	defer srv.Close()
	pages := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/moving" {
			_, _ = w.Write([]byte(`<body><div style="width:80px;height:80px;background:#f06;animation:a .4s linear infinite alternate"></div>` +
				`<style>@keyframes a{from{transform:translateX(0)}to{transform:translateX(300px)}}</style></body>`))
			return
		}
		_, _ = w.Write([]byte(`<body style="font:16px system-ui;padding:24px"><h1>Тихая страница</h1><p>Текст.</p><input autofocus></body>`))
	}))
	defer pages.Close()
	// The pages' server by a name: the gate refuses an address of the machine itself, the test resolver maps the name.
	site := "http://pages.test:" + strings.TrimPrefix(pages.URL, "http://127.0.0.1:")
	// A long leash: the machine may be busy with other work while the browser starts.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/browser/live?tab=paced", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(cdpReadLimit)
	send := func(v any) {
		raw, _ := json.Marshal(v)
		if err := ws.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	// A reader goroutine: a read with its own deadline would close the socket. frames takes what came within d and
	// acks it when asked; the values are the JPEGs' pixel widths.
	pictures := make(chan []byte, 64)
	go func() {
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				close(pictures)
				return
			}
			if kind == websocket.MessageBinary {
				pictures <- data
			}
		}
	}()
	frames := func(d time.Duration, ack bool) (widths []int) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				return widths
			case data, ok := <-pictures:
				if !ok {
					t.Fatal("the socket closed")
				}
				cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
				if err != nil {
					t.Fatalf("not a jpeg: %v", err)
				}
				widths = append(widths, cfg.Width)
				if ack {
					send(map[string]any{"t": "ack"})
				}
			}
		}
	}
	send(map[string]any{"t": "open", "url": site + "/moving", "w": 500, "h": 400, "dpr": 2, "ack": true})
	// The browser starts first: its first picture is waited for and acked.
	for start := time.Now(); len(frames(500*time.Millisecond, true)) == 0; {
		if time.Since(start) > 20*time.Second {
			t.Fatal("no first picture")
		}
	}
	frames(500*time.Millisecond, true)
	// Without acks the machine stops after two pictures, however much the page moves.
	if got := frames(2500*time.Millisecond, false); len(got) == 0 || len(got) > inflightMax {
		t.Fatalf("unacked pictures: %d (%v)", len(got), got)
	}
	// Acks let them go on.
	if got := frames(1500*time.Millisecond, true); len(got) < 3 {
		t.Fatalf("acked pictures: %d", len(got))
	}
	// A still page: the screencast's 1x pictures first, then the still shot in the screen's scale (2x), and no 1x
	// picture after it while only the caret blinks.
	send(map[string]any{"t": "nav", "url": site + "/still"})
	got := frames(4*time.Second, true)
	last := -1
	for i, w := range got {
		if w == 1000 {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("no still shot among %v", got)
	}
	for _, w := range got[last+1:] {
		if w != 1000 {
			t.Fatalf("a 1x picture after the still shot: %v", got)
		}
	}
	// A key brings the screencast back: a 1x picture comes (a still shot taken before the key may land first).
	send(map[string]any{"t": "key", "k": "down", "key": "a", "code": "KeyA", "text": "a"})
	send(map[string]any{"t": "key", "k": "up", "key": "a", "code": "KeyA"})
	if got := frames(1200*time.Millisecond, true); !slices.Contains(got, 500) {
		t.Fatalf("after a key: %v", got)
	}
}
