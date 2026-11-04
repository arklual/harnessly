package agentbrowser

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestChromeReview(t *testing.T) {
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "xvfb" {
				if _, err := exec.LookPath("Xvfb"); err != nil {
					t.Skip(err)
				}
			}
			data := t.TempDir()
			exe, err := os.Executable()
			must(t, err)
			cfg := Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode}
			m := New(cfg)
			t.Cleanup(func() { m.Close(); cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", DefaultProfile)) })
			i, err := m.Instance(t.Context(), DefaultProfile)
			must(t, err)
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<title>fixture</title><meta name="viewport" content="width=device-width,initial-scale=1"><body>
<a target="_blank" href="/child" style="position:absolute;left:10px;top:10px;width:200px;height:80px">new tab</a>
<textarea id="paste" style="position:absolute;top:200px"></textarea>
<script>
let frame=0;
function paint(){
 document.body.style.background='hsl('+(frame++%360)+' 80% 70%)';
 requestAnimationFrame(paint);
}
paint();
</script>`)
			}))
			defer page.Close()
			a, err := i.NewTab(t.Context(), "chat", "person", page.URL)
			must(t, err)
			b, err := i.NewTab(t.Context(), "neighbor", "agent", page.URL)
			must(t, err)
			t.Run("title_writes_and_frame_budget", func(t *testing.T) {
				must(t, i.SetViewport(t.Context(), b.ID, Viewport{"fixed", 800, 600, 1}))
				v, err := i.Watch(t.Context(), b.ID)
				must(t, err)
				defer v.Close()
				measure := func(duration time.Duration) int {
					timer := time.NewTimer(duration)
					defer timer.Stop()
					n := 0
					for {
						select {
						case f := <-v.Frames():
							if f.Width != 800 || f.Height != 600 {
								t.Fatalf("CSS dimensions: %dx%d", f.Width, f.Height)
							}
							n++
						case <-timer.C:
							return n
						}
					}
				}
				// Discard frames captured before the asynchronous window resize settled.
				time.Sleep(150 * time.Millisecond)
				select {
				case <-v.Frames():
				default:
				}
				baseline := measure(2 * time.Second)
				i.mu.Lock()
				before := i.writes
				i.mu.Unlock()
				evaluate(t, i, b.ID, "window.titleTimer=setInterval(()=>document.title='tick-'+Date.now(),50);true")
				count := measure(3 * time.Second)
				evaluate(t, i, b.ID, "clearInterval(window.titleTimer);document.title='final-title'")
				i.mu.Lock()
				writes := i.writes - before
				i.mu.Unlock()
				if baseline < 10 || count > 75 || count < baseline {
					t.Fatalf("baseline=%d/2s title=%d/3s registry=%+v actual=%s",
						baseline, count, i.Tabs(""), evaluate(t, i, b.ID, "document.title"))
				}
				if writes == 0 || writes > 4 {
					t.Fatalf("too many writes: %d", writes)
				}
				t.Logf("baseline=%d frames/2s; 50ms title changes=%d frames/3s; registry writes=%d", baseline, count, writes)
			})
			t.Run("target_blank_hidden_resize_and_reconnect", func(t *testing.T) {
				_, err := i.connection().Call(t.Context(), "Target.activateTarget", map[string]any{"targetId": a.ID}, "")
				must(t, err)
				for _, kind := range []string{"mousePressed", "mouseReleased"} {
					_, err = i.Call(t.Context(), a.ID, "Input.dispatchMouseEvent",
						map[string]any{"type": kind, "x": 40, "y": 40, "button": "left", "clickCount": 1})
					must(t, err)
				}
				eventually(t, 5*time.Second, func() bool { return len(i.Tabs("chat")) == 2 })
				var popup Tab
				for _, tab := range i.Tabs("chat") {
					if tab.ID != a.ID {
						popup = tab
					}
				}
				if popup.Conversation != "chat" {
					t.Fatal("popup owner missing")
				}
				eventually(t, 3*time.Second, func() bool {
					tabs := i.Tabs("chat")
					popup = tabs[1]
					return tabs[0].WindowID != 0 && tabs[0].WindowID == tabs[1].WindowID
				})
				hidden := string(evaluate(t, i, a.ID, "document.visibilityState"))
				t.Logf("opener visibility with focus emulation: %s", hidden)
				_, err = i.Call(t.Context(), a.ID, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": false})
				must(t, err)
				hidden = string(evaluate(t, i, a.ID, "document.visibilityState"))
				if hidden != `"hidden"` {
					t.Fatalf("expected native hidden opener, got %s", hidden)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				started := time.Now()
				if err = i.SetViewport(ctx, a.ID, Viewport{"fixed", 1280, 800, 1}); err != nil {
					state, why := i.State()
					t.Fatalf("hidden resize: %v state=%s %s", err, state, why)
				}
				cancel()
				t.Logf("real target=_blank click shared window %d; hidden resize=%s",
					popup.WindowID, time.Since(started).Round(time.Millisecond))
				_, err = i.connection().Call(t.Context(), "Target.activateTarget", map[string]any{"targetId": popup.ID}, "")
				must(t, err)
				m.Close()
				m = New(cfg)
				ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
				i, err = m.Instance(ctx, DefaultProfile)
				must(t, err)
				cancel()
				// Binding waits only for this page's restoration, never for visibility-dependent rAF.
				got := string(evaluate(t, i, a.ID, "[innerWidth,innerHeight]"))
				if got != "[1280,800]" {
					t.Fatal(got)
				}
				socket := httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
				defer socket.Close()
				ws, _, err := websocket.Dial(t.Context(), "ws"+socket.URL[4:]+"?conv=chat", nil)
				must(t, err)
				defer ws.CloseNow()
				ws.SetReadLimit(8 << 20)
				send := func(in Input) { raw, _ := json.Marshal(in); must(t, ws.Write(t.Context(), websocket.MessageText, raw)) }
				var previous uint32
				for _, id := range []string{popup.ID, a.ID, popup.ID, a.ID} {
					send(Input{T: "tab.select", ID: id})
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					var gen uint32
					for {
						typ, raw, e := ws.Read(ctx)
						must(t, e)
						if typ == websocket.MessageText {
							var tabs TabsMessage
							must(t, json.Unmarshal(raw, &tabs))
							if tabs.T == "tabs" && tabs.Current == id {
								gen = tabs.Gen
							}
							continue
						}
						if gen == 0 {
							continue
						}
						frameGen := binary.BigEndian.Uint32(raw[8:])
						if frameGen != gen || gen <= previous {
							t.Fatalf("generation: frame=%d tabs=%d previous=%d", frameGen, gen, previous)
						}
						previous = gen
						break
					}
					cancel()
				}
				t.Logf("reconnected with hidden viewport; both shared-window targets streamed; final gen=%d", previous)
				// Commands larger than the old 64 KiB ceiling remain usable; oversized messages report an error.
				evaluate(t, i, a.ID, "document.getElementById('paste').focus();true")
				send(Input{T: "text", Text: strings.Repeat("x", 70<<10)})
				eventually(t, 5*time.Second, func() bool {
					return string(evaluate(t, i, a.ID, "document.getElementById('paste').value.length")) == "71680"
				})
				send(Input{T: "text", Text: strings.Repeat("x", panelReadLimit+1)})
				ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				for {
					typ, raw, e := ws.Read(ctx)
					must(t, e)
					if typ != websocket.MessageText {
						continue
					}
					var notice Notice
					must(t, json.Unmarshal(raw, &notice))
					if notice.T == "notice" && strings.Contains(notice.Message, "1 MiB") {
						break
					}
				}
				send(Input{T: "tab.new", URL: page.URL})
				for {
					typ, raw, e := ws.Read(ctx)
					must(t, e)
					if typ != websocket.MessageText {
						continue
					}
					var tabs TabsMessage
					must(t, json.Unmarshal(raw, &tabs))
					if tabs.T == "tabs" && len(tabs.Tabs) == 4 {
						break
					}
				}
				t.Log("70 KiB paste accepted; >1 MiB message returned notice; socket created another tab afterwards")
			})
			t.Run("transient_popup_churn", func(t *testing.T) {
				c := i.connection()
				monitor, stop := context.WithCancel(t.Context())
				defer stop()
				type result struct {
					samples int
					err     error
				}
				done := make(chan result, 1)
				go func() {
					samples := 0
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-monitor.Done():
							done <- result{samples, nil}
							return
						case <-c.Done():
							done <- result{samples, fmt.Errorf("connection closed during popup churn")}
							return
						case <-ticker.C:
							state, reason := i.State()
							if state != "up" || i.connection() != c {
								done <- result{samples, fmt.Errorf("churn disconnected profile: %s %s", state, reason)}
								return
							}
							samples++
						}
					}
				}()
				for n := 0; n < 30; n++ {
					// Each popup needs a fresh user gesture; one timer's activation only permits the first popup.
					opened := evaluate(t, i, b.ID, `(()=>{
   const w=window.open('/brief','_blank','width=500,height=400');
   if(w)setTimeout(()=>w.close(),5);
   return !!w;
  })()`)
					if string(opened) != "true" {
						t.Fatalf("popup %d was blocked", n)
					}
					time.Sleep(20 * time.Millisecond)
				}
				time.Sleep(300 * time.Millisecond)
				stop()
				observed := <-done
				must(t, observed.err)
				t.Logf("30 short-lived popups; %d up-state samples; connection unchanged", observed.samples)
			})
			t.Run("close_flushes_last_title", func(t *testing.T) {
				evaluate(t, i, b.ID, "document.title='last-before-close'")
				eventually(t, 5*time.Second, func() bool {
					for _, tab := range i.Tabs("") {
						if tab.ID == b.ID {
							return tab.Title == "last-before-close"
						}
					}
					return false
				})
				m.Close()
				restored := newInstance(m, DefaultProfile)
				for _, tab := range restored.Tabs("") {
					if tab.ID == b.ID && tab.Title == "last-before-close" {
						t.Log("Close flushed final title to tabs.json")
						return
					}
				}
				t.Fatal("final title was not persisted")
			})
		})
	}
}
