package chromeview

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPickFollowsTheTabThatCameToFront(t *testing.T) {
	order := []string{"a", "b", "c"}
	set := func(ids ...string) map[string]bool {
		m := map[string]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	cases := []struct {
		before, now  map[string]bool
		active, want string
	}{
		{set(), set("a"), "", "a"},
		{set("a"), set("a"), "a", ""},
		{set("a"), set("b"), "a", "b"},
		{set("a"), set("a", "c"), "a", "c"}, // a second window came up
		{set("a", "c"), set("a", "c"), "a", ""},
		{set("a", "c"), set("c"), "a", "c"},
		{set("a"), set(), "a", ""},
		{set(), set("b"), "b", ""},
	}
	for i, c := range cases {
		if got := Pick(order, c.before, c.now, c.active); got != c.want {
			t.Errorf("%d: %q, want %q", i, got, c.want)
		}
	}
}

func TestInputCalls(t *testing.T) {
	key := Input{T: "key", Key: "a", Code: "KeyA", KeyCode: 65}.Calls()
	if len(key) != 2 || key[0].Params["type"] != "keyDown" || key[0].Params["text"] != "a" || key[1].Params["type"] != "keyUp" {
		t.Fatal(key)
	}
	ctrl := Input{T: "key", Key: "a", KeyCode: 65, Ctrl: true}.Calls()
	if ctrl[0].Params["type"] != "rawKeyDown" || ctrl[0].Params["text"] != nil || ctrl[0].Params["modifiers"] != 2 {
		t.Fatal(ctrl)
	}
	if enter := (Input{T: "key", Key: "Enter", KeyCode: 13}).Calls(); enter[0].Params["text"] != "\r" {
		t.Fatal(enter)
	}
	if c := (Input{T: "mouse", Type: "mouseDragged"}).Calls(); c != nil {
		t.Fatal(c)
	}
	if c := (Input{T: "mouse", Type: "mousePressed", Button: "evil", X: 3}).Calls(); c[0].Params["button"] != "none" {
		t.Fatal(c)
	}
	if c := (Input{T: "nav", Action: "go", URL: " example.org/x "}).Calls(); c[0].Params["url"] != "https://example.org/x" {
		t.Fatal(c)
	}
	if c := (Input{T: "nav", Action: "go", URL: "about:blank"}).Calls(); c[0].Params["url"] != "about:blank" {
		t.Fatal(c)
	}
	if c := (Input{T: "nav", Action: "eval"}).Calls(); c != nil {
		t.Fatal(c)
	}
}

// fakeChrome answers CDP as Chrome does for two tabs, b in front, and casts one frame on startScreencast.
type fakeChrome struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeChrome) server(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/x"})
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		send := func(m map[string]any) { b, _ := json.Marshal(m); _ = ws.Write(ctx, websocket.MessageText, b) }
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var m struct {
				ID        int64          `json:"id"`
				Method    string         `json:"method"`
				SessionID string         `json:"sessionId"`
				Params    map[string]any `json:"params"`
			}
			_ = json.Unmarshal(data, &m)
			f.mu.Lock()
			f.calls = append(f.calls, m.Method+"@"+m.SessionID)
			f.mu.Unlock()
			result := map[string]any{}
			switch m.Method {
			case "Target.setDiscoverTargets":
				for _, id := range []string{"a", "b"} {
					send(map[string]any{"method": "Target.targetCreated", "params": map[string]any{"targetInfo": map[string]any{
						"targetId": id, "type": "page", "title": "Tab " + id, "url": "https://" + id + ".example/"}}})
				}
				send(map[string]any{"method": "Target.targetCreated", "params": map[string]any{"targetInfo": map[string]any{
					"targetId": "w", "type": "service_worker", "title": "sw", "url": "https://a.example/sw.js"}}})
			case "Target.attachToTarget":
				result["sessionId"] = "s-" + m.Params["targetId"].(string)
			case "Runtime.evaluate":
				state := "hidden"
				if m.SessionID == "s-b" {
					state = "visible"
				}
				result["result"] = map[string]any{"type": "string", "value": state}
			case "Page.startScreencast":
				send(map[string]any{"method": "Page.screencastFrame", "sessionId": m.SessionID, "params": map[string]any{
					"data": base64.StdEncoding.EncodeToString([]byte("JPEG")), "sessionId": 1,
					"metadata": map[string]any{"deviceWidth": 1440, "deviceHeight": 812}}})
			}
			send(map[string]any{"id": m.ID, "result": result})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeChrome) saw(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func TestThePaneSeesTheTabInFrontAndDrivesIt(t *testing.T) {
	f := &fakeChrome{}
	chrome := f.server(t)
	v := New(Config{CDP: chrome.URL + "/"})
	pane := httptest.NewServer(v)
	t.Cleanup(pane.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(pane.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	var tabs []Tab
	var frame []byte
	for frame == nil || len(tabs) != 2 || !tabs[1].Active {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("tabs %v, frame %v: %v", tabs, frame, err)
		}
		if typ == websocket.MessageBinary {
			frame = data
			continue
		}
		var m struct {
			T    string `json:"t"`
			Tabs []Tab  `json:"tabs"`
		}
		_ = json.Unmarshal(data, &m)
		if m.T == "tabs" {
			tabs = m.Tabs
		}
	}
	if w, h := binary.BigEndian.Uint32(frame[0:4]), binary.BigEndian.Uint32(frame[4:8]); w != 1440 || h != 812 || string(frame[8:]) != "JPEG" {
		t.Fatal(w, h, string(frame[8:]))
	}
	if tabs[0].Title != "Tab a" || tabs[0].Active {
		t.Fatal(tabs)
	}
	b, _ := json.Marshal(Input{T: "mouse", Type: "mousePressed", Button: "left", X: 10, Y: 20, ClickCount: 1})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	for !f.saw("Input.dispatchMouseEvent@s-b") {
		select {
		case <-ctx.Done():
			t.Fatal("the click did not reach the tab in front")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !f.saw("Page.screencastFrameAck@s-b") {
		t.Fatal("the frame was not acknowledged")
	}
}

func TestOffWithoutAnAddress(t *testing.T) {
	v := New(Config{})
	if v.On() {
		t.Fatal("on")
	}
	rec := httptest.NewRecorder()
	v.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chrome", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatal(rec.Code)
	}
}
