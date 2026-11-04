package main

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
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ly-solutions/harnessly/internal/agentbrowser"
)

func TestAgentBrowserGuards(t *testing.T) {
	m := agentbrowser.New(agentbrowser.Config{Off: true})
	defer m.Close()
	d := deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}
	handler := agentBrowserRoutes(d, m, func(id string) bool { return id == "known" })[0].Handler
	for _, tt := range []struct {
		name, origin, cookie, conv string
		status                     int
	}{{"foreign origin", "https://other.example.org", "session=ok", "known",
		403}, {"no session", "", "", "known", 401}, {"unknown conversation",
		"", "session=ok", "missing", 404}, {"disabled", "", "session=ok",
		"known", 503}} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://example.org/api/agent-browser?conv="+tt.conv, nil)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Cookie", tt.cookie)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestAgentBrowserSocketChrome(t *testing.T) {
	chrome, err := agentbrowser.FindChrome()
	if err != nil {
		t.Skip(err)
	}
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "xvfb" {
				if _, e := exec.LookPath("Xvfb"); e != nil {
					t.Skip(e)
				}
			}
			dir := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			m := agentbrowser.New(agentbrowser.Config{DataDir: dir, Executable: exe, Chrome: chrome, Mode: mode})
			t.Cleanup(func() {
				m.Close()
				profile := filepath.Join(dir, "agent-browser", "profiles", agentbrowser.DefaultProfile)
				b, e := os.ReadFile(filepath.Join(profile, "host.pid"))
				if e != nil {
					return
				}
				pid, _ := strconv.Atoi(string(b))
				_ = syscall.Kill(pid, syscall.SIGTERM)
				time.Sleep(300 * time.Millisecond)
				for _, name := range []string{"chrome", "xvfb"} {
					if b, e = os.ReadFile(filepath.Join(profile, name+".pid")); e == nil {
						pid, _ = strconv.Atoi(string(b))
						if pid > 0 {
							_ = syscall.Kill(-pid, syscall.SIGKILL)
						}
					}
				}
			})
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<meta name="viewport" content="width=device-width"><body><script>
let n=0;
setInterval(()=>{document.body.style.background=n++%2?'red':'blue'},80);
</script>fixture</body>`)
			}))
			defer page.Close()
			authenticated := deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}
			handler := agentBrowserRoutes(authenticated,
				m, func(id string) bool { return id == "known" })[0].Handler
			server := httptest.NewServer(handler)
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			ws, _, err := websocket.Dial(ctx, "ws"+server.URL[4:]+"/api/agent-browser?conv=known",
				&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {"session=ok"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.CloseNow()
			ws.SetReadLimit(8 << 20)
			send := func(in agentbrowser.Input) {
				t.Helper()
				b, _ := json.Marshal(in)
				if e := ws.Write(ctx, websocket.MessageText, b); e != nil {
					t.Fatal(e)
				}
			}
			readTabs := func(count int, current string) agentbrowser.TabsMessage {
				t.Helper()
				for {
					typ, b, e := ws.Read(ctx)
					if e != nil {
						t.Fatal(e)
					}
					if typ != websocket.MessageText {
						continue
					}
					var msg agentbrowser.TabsMessage
					_ = json.Unmarshal(b, &msg)
					if msg.T == "status" {
						var st agentbrowser.Status
						_ = json.Unmarshal(b, &st)
						if st.State == "error" {
							t.Fatalf("browser error: %s", st.Error)
						}
					}
					if msg.T == "tabs" && len(msg.Tabs) == count && (current == "" || msg.Current == current) {
						return msg
					}
				}
			}
			readTabs(0, "")
			send(agentbrowser.Input{T: "tab.new", URL: page.URL})
			a := readTabs(1, "")
			if a.Tabs[0].Owner != "this" || a.Tabs[0].By != "person" {
				t.Fatalf("tab ownership: %+v", a)
			}
			first := a.Current
			send(agentbrowser.Input{T: "tab.new", URL: page.URL})
			b := readTabs(2, "")
			if b.Current == first || b.Gen <= a.Gen {
				t.Fatalf("generation did not advance: %+v -> %+v", a, b)
			}
			send(agentbrowser.Input{T: "tab.select", ID: first})
			selected := readTabs(2, first)
			if selected.Gen <= b.Gen {
				t.Fatal("selection did not advance generation")
			}
			for {
				typ, raw, e := ws.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if typ != websocket.MessageBinary {
					continue
				}
				if len(raw) < 16 || binary.BigEndian.Uint32(raw[8:]) != selected.Gen {
					t.Fatalf("bad frame header: %x", raw[:min(12, len(raw))])
				}
				if binary.BigEndian.Uint32(raw) == 0 || binary.BigEndian.Uint32(raw[4:]) == 0 || raw[12] != 0xff || raw[13] != 0xd8 {
					t.Fatal("bad JPEG frame")
				}
				break
			}
			send(agentbrowser.Input{T: "viewport", Mode: "mobile", W: 390, H: 844, DPR: 1})
			send(agentbrowser.Input{T: "tab.close", ID: first})
			remaining := readTabs(1, "")
			if remaining.Tabs[0].ID == first {
				t.Fatal("closed tab remains")
			}
			send(agentbrowser.Input{T: "tab.close", ID: remaining.Tabs[0].ID})
			readTabs(0, "")
		})
	}
}
