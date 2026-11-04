package agentbrowser

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "browser-host" {
		if err := host.Main(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func eventually(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func pidAt(t *testing.T, dir, name string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name+".pid"))
	must(t, err)
	pid, err := strconv.Atoi(string(b))
	must(t, err)
	return pid
}
func cleanupHost(t *testing.T, dir string) {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(dir, "host.pid"))
	if e != nil {
		return
	}
	pid, _ := strconv.Atoi(string(b))
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(300 * time.Millisecond)
	for _, name := range []string{"chrome", "xvfb"} {
		if b, e = os.ReadFile(filepath.Join(dir, name+".pid")); e == nil {
			p, _ := strconv.Atoi(string(b))
			if p > 0 {
				_ = syscall.Kill(-p, syscall.SIGKILL)
			}
		}
	}
}
func evaluate(t *testing.T, i *Instance, id, expr string) json.RawMessage {
	t.Helper()
	b, e := i.Call(t.Context(), id, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "userGesture": true})
	must(t, e)
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception any `json:"exceptionDetails"`
	}
	must(t, json.Unmarshal(b, &r))
	if r.Exception != nil {
		t.Fatalf("evaluate: %s", b)
	}
	return r.Result.Value
}
func TestChromeLifecycle(t *testing.T) {
	chrome, e := FindChrome()
	if e != nil {
		t.Skip(e)
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
			data := t.TempDir()
			exe, e := os.Executable()
			must(t, e)
			cfg := Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode}
			dir := filepath.Join(data, "agent-browser", "profiles", DefaultProfile)
			t.Cleanup(func() { cleanupHost(t, dir) })
			m := New(cfg)
			t.Cleanup(func() { m.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			i, e := m.Instance(ctx, DefaultProfile)
			if e != nil {
				log, _ := os.ReadFile(filepath.Join(dir, "host.log"))
				t.Fatalf("startup: %v\n%s", e, log)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<meta name="viewport" content="width=device-width,initial-scale=1"><script>let n=0;setInterval(()=>{document.body.style.background=n++%2?'red':'blue'},80)</script><body>browser fixture</body>`)
			}))
			defer server.Close()
			a, e := i.NewTab(ctx, "chat-a", "agent", server.URL)
			must(t, e)
			b, e := i.NewTab(ctx, "chat-b", "person", server.URL)
			must(t, e)
			t.Run("parallel_screencasts", func(t *testing.T) {
				va, e := i.Watch(ctx, a.ID)
				must(t, e)
				defer va.Close()
				vb, e := i.Watch(ctx, b.ID)
				must(t, e)
				defer vb.Close()
				for n := 0; n < 3; n++ {
					for _, v := range []*Viewer{va, vb} {
						select {
						case f := <-v.Frames():
							if len(f.JPEG) < 100 {
								t.Fatal("empty JPEG")
							}
						case <-time.After(8 * time.Second):
							t.Fatal("no screencast frame")
						}
					}
				}
			})
			t.Run("viewport", func(t *testing.T) {
				neighbor := string(evaluate(t, i, b.ID, "[innerWidth,innerHeight,devicePixelRatio]"))
				for _, v := range []Viewport{{"fixed", 1280, 800, 1}, {"fixed", 2200, 1300, 1}, {"mobile", 390, 844, 1}, {"fixed", 1280, 800, 1}} {
					must(t, i.SetViewport(ctx, a.ID, v))
					got := string(evaluate(t, i, a.ID, "[innerWidth,innerHeight]"))
					want := fmt.Sprintf("[%d,%d]", v.W, v.H)
					t.Logf("%s: inner=%s outer=%s", v.Mode, got, evaluate(t, i, a.ID, "[outerWidth,outerHeight]"))
					if got != want {
						t.Fatalf("%s: got %s want %s", v.Mode, got, want)
					}
					if got = string(evaluate(t, i, b.ID, "[innerWidth,innerHeight,devicePixelRatio]")); got != neighbor {
						t.Fatalf("neighbor resized: %s != %s", got, neighbor)
					}
				}
			})
			t.Run("popup_owner", func(t *testing.T) {
				evaluate(t, i, a.ID, "window.open('"+server.URL+"/popup','_blank','width=600,height=500');true")
				eventually(t, 5*time.Second, func() bool { return len(i.Tabs("chat-a")) == 2 })
			})
			t.Run("reconnect_preserves_js_and_registry", func(t *testing.T) {
				evaluate(t, i, a.ID, "window.survives=42")
				must(t, i.SetViewport(ctx, a.ID, Viewport{"mobile", 390, 844, 1}))
				before := pidAt(t, dir, "chrome")
				m.Close()
				// A page created while the runtime is absent must not be mistaken for the service window.
				conn, err := net.Dial("unix", host.SocketPath(filepath.Join(data, "agent-browser", "run"), DefaultProfile))
				must(t, err)
				external := cdp.New(cdp.NewStream(conn), nil)
				go func() { _ = external.Run(ctx) }()
				_, err = external.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank", "newWindow": true}, "")
				must(t, err)
				_ = external.Close()
				m = New(cfg)
				i, e = m.Instance(ctx, DefaultProfile)
				must(t, e)
				if pidAt(t, dir, "chrome") != before {
					t.Fatal("Chrome restarted")
				}
				if got := string(evaluate(t, i, a.ID, "window.survives")); got != "42" {
					t.Fatalf("lost JavaScript state: %s", got)
				}
				if len(i.Tabs("chat-a")) != 2 || len(i.Tabs("chat-b")) != 1 {
					t.Fatalf("ownership lost: %+v", i.Tabs(""))
				}
				unknown := 0
				for _, tab := range i.Tabs("") {
					if tab.Conversation == "" && tab.URL == "about:blank" {
						unknown++
					}
				}
				if unknown != 1 {
					t.Fatalf("external blank tab was hidden: %+v", i.Tabs(""))
				}

				if got := string(evaluate(t, i, a.ID, "[innerWidth,innerHeight]")); got != "[390,844]" {
					t.Fatalf("override lost: %s", got)
				}
			})
			t.Run("last_tab", func(t *testing.T) {
				for _, tab := range i.Tabs("") {
					must(t, i.CloseTab(ctx, tab.ID))
				}
				if len(i.Tabs("")) != 0 {
					t.Fatal("hidden startup window leaked")
				}
				a, e = i.NewTab(ctx, "chat-a", "agent", server.URL)
				must(t, e)
			})
			t.Run("crashed_tab_state", func(t *testing.T) {
				crash, err := i.NewTab(ctx, "chat-a", "agent", server.URL)
				must(t, err)
				done := make(chan struct{})
				go func() {
					defer close(done)
					callCtx, cancel := context.WithTimeout(ctx, time.Second)
					defer cancel()
					_, _ = i.Call(callCtx, crash.ID, "Page.crash", nil)
				}()
				eventually(t, 5*time.Second, func() bool {
					for _, tab := range i.Tabs("") {
						if tab.ID == crash.ID {
							return tab.Crashed
						}
					}
					return false
				})
				<-done
				must(t, i.CloseTab(ctx, crash.ID))
			})

			t.Run("hard_kill_recovers_persistent_cookie", func(t *testing.T) {
				evaluate(t, i, a.ID, `document.cookie="persistent=kept; Max-Age=3600; Path=/"`)
				// Chrome batches cookie writes. SIGKILL cannot preserve data it has not flushed.
				cookieFile := filepath.Join(dir, "chrome", "Default", "Cookies")
				eventually(t, 50*time.Second, func() bool {
					db, err := sql.Open("sqlite", "file:"+cookieFile+"?mode=ro")
					if err != nil {
						return false
					}
					defer db.Close()
					var n int
					err = db.QueryRow("SELECT count(*) FROM cookies WHERE name = 'persistent'").Scan(&n)
					return err == nil && n == 1
				})
				oldHost := pidAt(t, dir, "host")
				oldChrome := pidAt(t, dir, "chrome")
				oldX := 0
				if mode == "xvfb" {
					oldX = pidAt(t, dir, "xvfb")
				}
				must(t, syscall.Kill(oldHost, syscall.SIGKILL))
				_ = syscall.Kill(-oldChrome, syscall.SIGKILL)
				if oldX > 0 {
					_ = syscall.Kill(-oldX, syscall.SIGKILL)
				}
				eventually(t, 20*time.Second, func() bool {
					attempt, stop := context.WithTimeout(ctx, time.Second)
					defer stop()
					next, err := m.Instance(attempt, DefaultProfile)
					if err != nil {
						return false
					}
					state, _ := next.State()
					return state == "up" && pidAt(t, dir, "host") != oldHost
				})
				a, e = i.NewTab(ctx, "chat-a", "agent", server.URL)
				must(t, e)
				eventually(t, 5*time.Second, func() bool {
					return strings.Contains(string(evaluate(t, i, a.ID, "document.cookie")), "persistent=kept")
				})
			})
			t.Run("native_last_window_probe", func(t *testing.T) {
				i.op.Lock()
				defer i.op.Unlock()
				c := i.conn
				raw, err := c.Call(ctx, "Target.getTargets", nil, "")
				must(t, err)
				var r struct {
					Targets []target `json:"targetInfos"`
				}
				must(t, json.Unmarshal(raw, &r))
				for _, target := range r.Targets {
					if target.Type == "page" {
						_, _ = c.Call(ctx, "Target.closeTarget", map[string]any{"targetId": target.ID}, "")
					}
				}
				select {
				case <-c.Done():
					t.Log("Chrome exited after closing its last native window; the service window is required")
				case <-time.After(2 * time.Second):
					_, err = c.Call(ctx, "Browser.getVersion", nil, "")
					must(t, err)
					t.Log("Chrome remained alive without native windows in this mode")
				}
			})

		})
	}
}
