package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
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

type mcpClient struct {
	t                   *testing.T
	url, chat, password string
	client              *http.Client
}

func (c mcpClient) rpc(method string, params any) map[string]any {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(c.t.Context(), "POST", c.url, bytes.NewReader(raw))
	must(c.t, err)
	req.Header.Set("X-Test-Chat", c.chat)
	res, err := c.client.Do(req)
	must(c.t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	must(c.t, err)
	if c.password != "" && bytes.Contains(body, []byte(c.password)) {
		c.t.Fatal("password leaked into MCP output")
	}
	var out map[string]any
	must(c.t, json.Unmarshal(body, &out))
	if out["error"] != nil {
		c.t.Fatalf("RPC error: %s", body)
	}
	result, ok := out["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("no result: %s", body)
	}
	return result
}
func (c mcpClient) call(name string, args map[string]any) map[string]any {
	c.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	r := c.rpc("tools/call", map[string]any{"name": "browser_" + name, "arguments": args})
	if r["isError"] == true {
		c.t.Fatalf("%s: %s", name, jsonText(r))
	}
	return r
}
func resultText(r map[string]any) string {
	var out string
	for _, v := range r["content"].([]any) {
		item := v.(map[string]any)
		if item["type"] == "text" {
			out += item["text"].(string)
		}
	}
	return out
}
func (c mcpClient) text(name string, args map[string]any) string {
	c.t.Helper()
	return resultText(c.call(name, args))
}
func (c mcpClient) refuses(name string, args map[string]any, contains string) {
	c.t.Helper()
	r := c.rpc("tools/call", map[string]any{"name": "browser_" + name, "arguments": args})
	if r["isError"] != true || !strings.Contains(resultText(r), contains) {
		c.t.Fatalf("expected refusal %s: %s", contains, jsonText(r))
	}
}
func (c mcpClient) node(label string) string {
	c.t.Helper()
	s := c.text("snapshot", nil)
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, `"`+label+`"`) && !strings.Contains(line, "StaticText") && !strings.Contains(line, "InlineTextBox") {
			m := regexp.MustCompile(`\[(e\d+)\]`).FindStringSubmatch(line)
			if len(m) > 1 {
				return m[1]
			}
		}
	}
	c.t.Fatalf("no node %q in %s", label, s)
	return ""
}
func (c mcpClient) eval(expression, want string) {
	c.t.Helper()
	s := c.text("evaluate", map[string]any{"expression": expression})
	if !strings.Contains(s, want) {
		c.t.Fatalf("evaluate %s: want %s got %s", expression, want, s)
	}
}
func cleanupHost(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "host.pid"))
	if err != nil {
		return
	}
	pid, _ := strconv.Atoi(string(b))
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(300 * time.Millisecond)
	for _, name := range []string{"chrome", "xvfb"} {
		b, e := os.ReadFile(filepath.Join(dir, name+".pid"))
		if e == nil {
			pid, _ = strconv.Atoi(string(b))
			if pid > 0 {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
	}
}
func TestChromeMCP(t *testing.T) {
	if testing.Short() {
		t.Skip("drives an isolated Chrome")
	}
	chrome, err := agentbrowser.FindChrome()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "xvfb" {
				_, err = exec.LookPath("Xvfb")
				must(t, err)
			}
			data := t.TempDir()
			exe, err := os.Executable()
			must(t, err)
			m := agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode})
			service := &Service{Manager: m}
			t.Cleanup(func() {
				service.Close()
				m.Close()
				cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile))
			})
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			i, err := m.InstanceFor(ctx, "chat-a")
			must(t, err)
			var pageURL string
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch r.URL.Path {
				case "/login":
					fmt.Fprint(w, `<form action="/submit" method="post"><label>User<input name="user"></label><label>Password<input name="password" type="password"></label><button>Sign in</button></form>`)
				case "/submit":
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "fixture", Path: "/"})
					http.Redirect(w, r, "/home", http.StatusSeeOther)
				case "/home":
					fmt.Fprint(w, `<h1>Signed in</h1>`)
				case "/frame":
					fmt.Fprint(w, `<style>body{margin:17px}button{margin:30px}</style><button onclick="document.body.dataset.clicked='yes'">Frame action</button><label>Frame input<input oninput="document.body.dataset.typed=this.value"></label>`)
				case "/popup":
					fmt.Fprint(w, `<style>body{background:rgb(0,0,240)}</style><h1>Popup page</h1><button onclick="document.body.dataset.clicked='yes'">Popup action</button>`)
				case "/api":
					w.Header().Set("Set-Cookie", "fixture=private")
					fmt.Fprint(w, `bounded response body`)
				default:
					fmt.Fprintf(w, `<style>iframe{margin:70px 40px;border:8px solid green;width:600px;height:250px}body{height:1600px;background:rgb(240,0,0)}</style>
<a target="_blank" href="/popup">New tab</a>
<button onmouseover="document.body.dataset.hovered='yes'" onclick="document.body.dataset.clicked='yes'">Main action</button>
<label>Choice<select onchange="document.body.dataset.selected=this.value"><option value="one">One</option><option value="two">Two</option></select></label>
<label>Attachment<input type="file" onchange="document.body.dataset.uploaded=this.files[0].name"></label>
<button onclick="alert('hello')">Alert action</button><button onclick="document.body.dataset.confirm=confirm('continue?')">Confirm action</button>
<button onclick="document.body.dataset.prompt=prompt('name?','initial')">Prompt action</button>
<iframe src="%s/frame"></iframe><div id="shadow"></div>
<script>shadow.attachShadow({mode:'open'}).innerHTML='<button onclick="this.textContent=\'Shadow clicked\'">Shadow action</button>';
setTimeout(()=>{const h=document.createElement('h2');h.textContent='Delayed content';document.body.append(h)},700);</script>`, strings.Replace(pageURL, "127.0.0.1", "localhost", 1))
				}
			}))
			defer page.Close()
			pageURL = page.URL
			mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					http.Error(w, e.Error(), 400)
					return
				}
				v, e := jsvalue.Parse(raw)
				if e != nil {
					http.Error(w, e.Error(), 400)
					return
				}
				set := Tools{S: service, Caller: r.Header.Get("X-Test-Chat"), Cwd: data}
				server := mcpserver.Server{Name: "browser-test", Sets: []mcpserver.Toolset{set}}
				status, b := server.Handle(r.Context(), v)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write(b)
			}))
			defer mcp.Close()
			c := mcpClient{t, mcp.URL, "chat-a", "fixture-password-" + mode, mcp.Client()}
			if list := c.rpc("tools/list", nil)["tools"].([]any); len(list) != 18 {
				t.Fatalf("tools: %d", len(list))
			}
			c.text("navigate", map[string]any{"url": page.URL + "/login"})
			first := i.Current("chat-a")
			t.Run("login_password", func(t *testing.T) {
				c.t = t
				c.text("type", map[string]any{"id": c.node("User"), "text": "agent"})
				c.text("press_key", map[string]any{"key": "Tab"})
				c.eval("document.activeElement.type", `"password"`)
				c.text("type", map[string]any{"id": c.node("Password"), "text": c.password})
				c.text("snapshot", nil)
				c.text("evaluate", map[string]any{"expression": "document.querySelector('[type=password]').value"})
				c.text("evaluate", map[string]any{"expression": "console.log(document.querySelector('[type=password]').value)"})
				c.text("console", nil)
				c.text("click", map[string]any{"id": c.node("Sign in")})
				c.eval("[location.pathname,document.cookie]", `/home`)
				c.eval("document.cookie", "session=fixture")
			})
			c.t = t
			c.text("navigate", map[string]any{"url": page.URL})
			t.Run("cross_site_frame", func(t *testing.T) {
				c.t = t
				frames, e := i.AgentFrames(t.Context(), "chat-a", first)
				must(t, e)
				if len(frames) == 0 {
					t.Fatal("fixture did not create an out-of-process iframe")
				}
				t.Logf("separate iframe sessions: %d", len(frames))
				c.text("click", map[string]any{"id": c.node("Frame action")})
				frame := frames[0]
				raw, e := i.AgentFrameCall(t.Context(), "chat-a", first, frame.Session, "Runtime.evaluate", map[string]any{"expression": "document.body.dataset.clicked", "returnByValue": true})
				must(t, e)
				if !bytes.Contains(raw, []byte(`"yes"`)) {
					t.Fatalf("frame click missed: %s", raw)
				}
				c.text("type", map[string]any{"id": c.node("Frame input"), "text": "inside frame"})
				raw, e = i.AgentFrameCall(t.Context(), "chat-a", first, frame.Session, "Runtime.evaluate", map[string]any{"expression": "document.body.dataset.typed", "returnByValue": true})
				must(t, e)
				if !bytes.Contains(raw, []byte("inside frame")) {
					t.Fatalf("frame input missed: %s", raw)
				}
			})
			c.t = t
			t.Run("select_shadow_wait", func(t *testing.T) {
				c.t = t
				c.text("hover", map[string]any{"id": c.node("Main action")})
				c.eval("document.body.dataset.hovered", `"yes"`)
				c.text("select", map[string]any{"id": c.node("Choice"), "value": "two"})
				c.eval("document.body.dataset.selected", `"two"`)
				c.text("click", map[string]any{"id": c.node("Shadow action")})
				c.text("wait_for", map[string]any{"text": "Shadow clicked"})
				c.text("evaluate", map[string]any{"expression": "setTimeout(()=>{document.body.append('New delayed marker')},4000);true"})
				c.text("wait_for", map[string]any{"text": "New delayed marker"})
			})
			c.t = t
			t.Run("dialogs", func(t *testing.T) {
				c.t = t
				for _, dialog := range []struct {
					label, key string
					accept     bool
					text       string
				}{{"Alert action", "", true, ""}, {"Confirm action", "confirm", false, "false"}, {"Prompt action", "prompt", true, "answer"}} {
					c.text("click", map[string]any{"id": c.node(dialog.label)})
					c.refuses("snapshot", map[string]any{}, "browser_dialog")
					c.text("dialog", map[string]any{"accept": dialog.accept, "text": dialog.text})
					if dialog.key != "" {
						c.eval("document.body.dataset."+dialog.key, dialog.text)
					}
				}
			})
			c.t = t
			t.Run("upload_paths", func(t *testing.T) {
				c.t = t
				must(t, os.WriteFile(filepath.Join(data, "upload.txt"), []byte("fixture"), 0600))
				outside := filepath.Join(t.TempDir(), "outside.txt")
				must(t, os.WriteFile(outside, []byte("outside"), 0600))
				must(t, os.Symlink(outside, filepath.Join(data, "escape")))
				id := c.node("Attachment")
				c.refuses("upload", map[string]any{"id": id, "files": []string{outside}}, "inside the chat folder")
				c.refuses("upload", map[string]any{"id": id, "files": []string{"escape"}}, "inside the chat folder")
				c.text("upload", map[string]any{"id": id, "files": []string{"upload.txt"}})
				c.eval("document.body.dataset.uploaded", "upload.txt")
			})
			c.t = t
			t.Run("popup_hidden_screenshots", func(t *testing.T) {
				c.t = t
				c.text("resize", map[string]any{"width": 1800, "height": 1000})
				c.eval("[innerWidth,innerHeight]", "[1800,1000]")
				result := c.text("click", map[string]any{"id": c.node("New tab")})
				if !strings.Contains(result, "New working tab") {
					t.Fatal(result)
				}
				popup := i.Current("chat-a")
				if popup == first {
					t.Fatal("popup was not selected")
				}
				c.text("click", map[string]any{"id": c.node("Popup action")})
				c.eval("document.body.dataset.clicked", `"yes"`)
				for _, tab := range []string{first, popup} {
					shot := c.call("screenshot", map[string]any{"tab": tab})
					content := shot["content"].([]any)
					image := content[1].(map[string]any)
					if image["type"] != "image" || image["mimeType"] != "image/jpeg" {
						t.Fatal("not an MCP JPEG image")
					}
					b, e := base64.StdEncoding.DecodeString(image["data"].(string))
					must(t, e)
					size, e := jpeg.DecodeConfig(bytes.NewReader(b))
					must(t, e)
					if size.Width > 1568 || size.Height > 1568 {
						t.Fatalf("uncapped JPEG: %+v", size)
					}
					t.Logf("MCP JPEG %dx%d (%d bytes)", size.Width, size.Height, len(b))
					img, e := jpeg.Decode(bytes.NewReader(b))
					must(t, e)
					red, _, blue, _ := img.At(size.Width-20, size.Height-20).RGBA()
					if tab == first && red < blue || tab == popup && blue < red {
						t.Fatalf("screenshot shows wrong tab: red=%d blue=%d", red, blue)
					}
				}
				c.text("snapshot", map[string]any{"tab": first})
				c.text("click", map[string]any{"id": c.node("Main action")})
				c.eval("document.body.dataset.clicked", `"yes"`)
				c.call("screenshot", map[string]any{"tab": first, "path": "saved.jpg"})
				if _, e := os.Stat(filepath.Join(data, "saved.jpg")); e != nil {
					t.Fatal(e)
				}
				c.text("close_tab", map[string]any{"tab": popup})
			})
			c.t = t
			t.Run("ownership_control_urls_stale", func(t *testing.T) {
				c.t = t
				b := c
				b.chat = "chat-b"
				if strings.Contains(b.text("tabs", nil), first) {
					t.Fatal("other chat saw tab")
				}
				b.refuses("snapshot", map[string]any{"tab": first}, "not available to this chat")
				b.refuses("dialog", map[string]any{"tab": first, "accept": true}, "not available to this chat")
				must(t, i.SetControl(first, "person"))
				c.refuses("snapshot", map[string]any{"tab": first}, "person has control")
				must(t, i.SetControl(first, "agent"))
				for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/html,hi", "chrome://version", "view-source:http://localhost"} {
					for _, tool := range []string{"open", "navigate"} {
						c.refuses(tool, map[string]any{"url": u}, "URL refused")
					}
				}
				old := c.node("Main action")
				c.text("navigate", map[string]any{"url": page.URL + "/home"})
				c.refuses("click", map[string]any{"id": old}, "refresh the snapshot")
			})
			c.t = t
			t.Run("network_console_bounds", func(t *testing.T) {
				c.t = t
				c.text("evaluate", map[string]any{"expression": "for(let n=0;n<520;n++)console.log('entry-'+n);fetch('/api',{headers:{Authorization:'fixture-hidden'}}).then(r=>r.text())"})
				out := c.text("console", nil)
				if strings.Contains(out, `entry-0"`) {
					t.Fatal("ring retained oldest entries")
				}
				network := c.text("network", nil)
				if strings.Contains(network, "fixture-hidden") || strings.Contains(network, "fixture=private") {
					t.Fatal("headers leaked")
				}
				if strings.Contains(network, "bounded response body") {
					t.Fatal("implicit response body")
				}
				var request string
				for _, line := range strings.Split(network, "\n") {
					var entry map[string]any
					if json.Unmarshal([]byte(line), &entry) == nil && strings.HasSuffix(fmt.Sprint(entry["url"]), "/api") {
						request, _ = entry["request_id"].(string)
					}
				}
				if request == "" {
					t.Fatal("no recorded request")
				}
				if body := c.text("network", map[string]any{"request_id": request}); !strings.Contains(body, "bounded response body") {
					t.Fatal(body)
				}
				large := c.text("evaluate", map[string]any{"expression": "'x'.repeat(40000)"})
				if len(large) > resultLimit+256 || !strings.Contains(large, "truncated") {
					t.Fatal("evaluation limit missing")
				}
			})
		})
	}
}
