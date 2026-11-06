package tools

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/hlcli"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

func TestChromeFillSecret(t *testing.T) {
	chrome, err := agentbrowser.FindChrome()
	if err != nil {
		skipWithoutChrome(t, err)
	}
	for _, mode := range chromeModes() {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			exe, err := os.Executable()
			must(t, err)
			manager := agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode})
			store := secrets.Open(filepath.Join(data, "safe"), filepath.Join(data, "key"))
			service := &Service{Manager: manager, Secrets: store, Identity: func(id string) (secrets.Use, error) {
				return secrets.Use{Conv: id, Project: "alpha", Title: "Login test"}, nil
			}}
			t.Cleanup(func() {
				service.Close()
				manager.Close()
				cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile))
			})
			seed := make([]byte, 24)
			_, err = rand.Read(seed)
			must(t, err)
			value := hex.EncodeToString(seed)
			var submitted atomic.Bool
			var pageURL string
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch r.URL.Path {
				case "/submit":
					if r.FormValue("credential") == value {
						submitted.Store(true)
						http.SetCookie(w, &http.Cookie{Name: "signed", Value: "yes", Path: "/"})
					}
					http.Redirect(w, r, "/home", http.StatusSeeOther)
				case "/home":
					fmt.Fprint(w, `<h1>Home</h1>`)
				case "/iframe-focus", "/iframe-opacity":
					action := "document.getElementById('outside').focus()"
					if r.URL.Path == "/iframe-opacity" {
						action = "document.querySelector('iframe').style.opacity=0"
					}
					fmt.Fprintf(w, `<input id="outside"><iframe src="%s/frame-focus"></iframe><script>addEventListener('message',()=>{%s})</script>`, strings.Replace(pageURL, "127.0.0.1", "localhost", 1), action)
				case "/iframe":
					fmt.Fprintf(w, `<iframe src="%s/login"></iframe>`, strings.Replace(pageURL, "127.0.0.1", "localhost", 1))
				case "/copy":
					fmt.Fprint(w, `<label>Credential<input name="credential" oninput="window.saved=this.value;document.getElementById('copy').textContent=btoa(this.value)"></label><p id="copy"></p><button onclick="document.querySelector('input').value=''">Clear</button><button onclick="window.open('/popup')">Open copy</button>`)
				case "/shadow":
					fmt.Fprint(w, `<div id="host"></div><script>host.attachShadow({mode:'open'}).innerHTML='<label>Credential<input name="credential"></label>'</script>`)
				default:
					extra := ""
					if kind := r.URL.Query().Get("type"); kind != "" {
						switch kind {
						case "password", "text", "email", "tel", "number":
							extra = "type=" + kind
						}
					}
					if r.URL.Path == "/readonly" {
						extra = "readonly"
					}
					if r.URL.Path == "/disabled" {
						extra = "disabled"
					}
					if r.URL.Path == "/frame-focus" {
						extra = `onfocus="parent.postMessage('focus','*')"`
					}
					if r.URL.Path == "/focus" {
						extra = `onfocus="document.getElementById('trap').focus()"`
					}
					fmt.Fprintf(w, `<form action="/submit" method="post"><label>Credential<input name="credential" %s oninput="console.log(this.value); document.title=this.value"></label><input id="trap"><button>Sign in</button></form><button onclick="document.querySelector('[name=credential]').remove()">Remove</button><button onclick="document.querySelector('[name=credential]').value=''">Clear</button>`, extra)
				}
			}))
			defer page.Close()
			pageURL = page.URL
			_, _, err = store.Put("login", value)
			must(t, err)
			_, err = store.Metadata("login", []string{pageURL}, []string{"alpha"})
			must(t, err)
			_, _, err = store.Put("unbound", value)
			must(t, err)
			_, _, err = store.Put("foreign", value)
			must(t, err)
			_, err = store.Metadata("foreign", []string{pageURL}, []string{"beta"})
			must(t, err)
			var answerMu sync.Mutex
			var answers []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					http.Error(w, "read", 400)
					return
				}
				v, e := jsvalue.Parse(raw)
				if e != nil {
					http.Error(w, "parse", 400)
					return
				}
				set := Tools{S: service, Caller: "chat", Cwd: data}
				var body []byte
				if strings.HasPrefix(r.URL.Path, "/api/cli/browser/") {
					body = jsvalue.Stringify(set.Call(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/cli/browser/"), v.(*jsvalue.Object).Object("arguments")), "")
				} else {
					mcp := mcpserver.Server{Name: "safe-test", Sets: []mcpserver.Toolset{set}}
					_, body = mcp.Handle(r.Context(), v)
				}
				answerMu.Lock()
				answers = append(answers, body...)
				answerMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			c := mcpClient{t, server.URL, "chat", value, server.Client()}
			navigate := func(path string) {
				i, e := manager.InstanceFor(t.Context(), "chat")
				must(t, e)
				for _, tab := range i.Tabs("chat") {
					c.text("close_tab", map[string]any{"tab": tab.ID})
				}
				c.text("open", map[string]any{"url": pageURL + path})
			}
			fixtureJS := func(expression string) {
				i, e := manager.InstanceFor(t.Context(), "chat")
				must(t, e)
				raw, e := i.Call(t.Context(), i.Current("chat"), "Runtime.evaluate", map[string]any{"expression": expression})
				must(t, e)
				var result struct {
					Exception json.RawMessage `json:"exceptionDetails"`
				}
				must(t, json.Unmarshal(raw, &result))
				if result.Exception != nil {
					t.Fatal("fixture JavaScript assertion failed")
				}
			}
			fill := func() { c.text("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "login"}) }
			navigate("/login")
			fill()
			for _, tool := range []string{"evaluate", "console", "network", "screenshot"} {
				c.refuses(tool, map[string]any{"expression": "document.querySelector('input').value"}, "sealed")
			}
			c.text("snapshot", nil)
			// Exercise the real CLI parser, request envelope and stdout, not only the MCP client.
			runCLI := func(name string, args map[string]any, want int) string {
				var stdout, stderr bytes.Buffer
				env := hlcli.Env{Home: data, Cwd: data, Client: server.Client(), Stdout: &stdout, Stderr: &stderr,
					Session: func() string { return "hs-test" }, Getenv: func(key string) string {
						switch key {
						case "HARNESSLY_URL":
							return server.URL
						case "HARNESSLY_CLI_TOKEN":
							return "fixture"
						}
						return ""
					}}
				code := hlcli.Run([]string{"browser", name, "--json", jsonText(args)}, env)
				text := stdout.String() + stderr.String()
				if strings.Contains(text, value) {
					t.Fatal("secret in CLI output")
				}
				if code != want {
					t.Fatalf("CLI %s exit %d: %s", name, code, text)
				}
				return text
			}
			if !strings.Contains(runCLI("evaluate", map[string]any{"expression": "document.title"}, 1), "sealed") {
				t.Fatal("CLI bypassed seal")
			}

			c.text("click", map[string]any{"id": c.node("Sign in")})
			if !submitted.Load() {
				t.Fatal("form did not receive secret")
			}
			c.eval("document.cookie.includes('signed=yes')", "true")
			c.text("console", nil)
			c.text("network", nil)
			c.call("screenshot", nil)
			for _, button := range []string{"Remove", "Clear"} {
				navigate("/login")
				runCLI("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "login"}, 0)
				c.refuses("evaluate", map[string]any{"expression": "window.forbiddenRan = true"}, "sealed")
				c.text("click", map[string]any{"id": c.node(button)})
				if button == "Clear" {
					c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
				} else {
					c.eval("window.forbiddenRan === undefined", "true")
				}
			}
			for _, kind := range []string{"password", "text", "email", "tel"} {
				navigate("/login?type=" + kind)
				fixtureJS("document.querySelector('[name=credential]').value='old'")
				fill()
				// A private CDP assertion observes only a boolean, never returning the value through an agent tool.
				i, e := manager.InstanceFor(t.Context(), "chat")
				must(t, e)
				st, e := service.state(t.Context(), i, "chat", i.Current("chat"))
				must(t, e)
				st.mu.Lock()
				field := st.sealed[len(st.sealed)-1]
				st.mu.Unlock()
				var exact bool
				e = (Tools{S: service, Caller: "chat"}).nodeCheck(t.Context(), i, i.Current("chat"), field.ref, field.object,
					"function(){return this.value === "+jsonText(value)+"}", &exact)
				must(t, e)
				if !exact {
					t.Fatal("secret did not replace existing field text")
				}
			}
			navigate("/shadow")
			fill()
			c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
			// A site-owned copy remains masked; clearing does not permit page evaluation.
			navigate("/copy")
			fill()
			if strings.Contains(c.text("snapshot", nil), value) {
				t.Fatal("snapshot leaked a site copy")
			}
			c.text("click", map[string]any{"id": c.node("Clear")})
			c.refuses("evaluate", map[string]any{"expression": "btoa(window.saved)"}, "sealed")
			navigate("/copy")
			fill()
			c.text("click", map[string]any{"id": c.node("Open copy")})
			c.refuses("evaluate", map[string]any{"expression": "btoa(opener.document.querySelector('input').value)"}, "sealed")
			// Wrong port, host, scheme and subdomain are exact mismatches, never suffix matches.
			for _, origin := range []string{pageURL + "1", strings.Replace(pageURL, "127.0.0.1", "localhost", 1), strings.Replace(pageURL, "http:", "https:", 1), "https://sub.example.com"} {
				// The first test may produce an invalid port; use a valid distinct port instead.
				if origin == pageURL+"1" {
					origin = "http://127.0.0.1:1"
				}
				_, err = store.Metadata("login", []string{origin}, []string{"alpha"})
				must(t, err)
				navigate("/login")
				c.refuses("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "login"}, "origin")
				c.eval("document.querySelector('[name=credential]').value === ''", "true")
			}
			_, err = store.Metadata("login", []string{pageURL}, []string{"alpha"})
			must(t, err)
			for _, path := range []string{"/readonly", "/disabled", "/focus", "/hidden"} {
				navigate(path)
				id := c.node("Credential")
				if path == "/hidden" {
					fixtureJS("document.querySelector('[name=credential]').style.display='none'")
				}
				want := "visible"
				if path == "/focus" {
					want = "focus"
				}
				c.refuses("fill_secret", map[string]any{"id": id, "secret": "login"}, want)
				c.eval("[...document.querySelectorAll('input')].every(e=>e.value==='')", "true")
			}
			navigate("/login")
			id := c.node("Credential")
			c.refuses("fill_secret", map[string]any{"id": id, "secret": "unbound"}, "no allowed sites")
			absent := c.rpc("tools/call", map[string]any{"name": "chrome_fill_secret", "arguments": map[string]any{"id": id, "secret": "missing"}})
			foreign := c.rpc("tools/call", map[string]any{"name": "chrome_fill_secret", "arguments": map[string]any{"id": id, "secret": "foreign"}})
			if jsonText(absent) != jsonText(foreign) {
				t.Fatal("foreign entry existence disclosed")
			}
			navigate("/iframe")
			c.refuses("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "login"}, "origin")
			_, err = store.Metadata("login", []string{strings.Replace(pageURL, "127.0.0.1", "localhost", 1)}, []string{"alpha"})
			must(t, err)
			// Input runs in the frame document; its embedding page cannot redirect it by moving focus.
			navigate("/iframe-focus")
			fill()
			fixtureJS("if(document.getElementById('outside').value!=='')throw new Error('unexpected input')")

			navigate("/iframe")
			fill()
			// The frame origin, intentionally not its embedding page, is the authority.
			views, err := store.List()
			must(t, err)
			for _, v := range views {
				if v.Name == "login" && (len(v.UsedBy) == 0 || v.UsedBy[0].Origin != strings.Replace(pageURL, "127.0.0.1", "localhost", 1)) {
					t.Fatal("frame origin not audited")
				}
			}
			// Removing an entire cross-origin iframe also removes its secret field.
			i, e := manager.InstanceFor(t.Context(), "chat")
			must(t, e)
			_, e = i.Call(t.Context(), i.Current("chat"), "Runtime.evaluate", map[string]any{"expression": "document.querySelector('iframe').remove()"})
			must(t, e)
			c.eval("true", "true")
			navigate("/iframe")
			hiddenID := c.node("Credential")
			fixtureJS("document.querySelector('iframe').style.display='none'")
			c.refuses("fill_secret", map[string]any{"id": hiddenID, "secret": "login"}, "visible")
			// Real TLS on loopback tests host and subdomain boundaries without external DNS.
			secure := httptest.NewTLSServer(page.Config.Handler)
			defer secure.Close()
			allowedTLS := strings.Replace(secure.URL, "127.0.0.1", "localhost", 1)
			_, e = i.Call(t.Context(), i.Current("chat"), "Security.setIgnoreCertificateErrors", map[string]any{"ignore": true})
			must(t, e)
			_, err = store.Metadata("login", []string{allowedTLS}, []string{"alpha"})
			must(t, err)
			c.text("navigate", map[string]any{"url": allowedTLS + "/login"})
			fill()
			for _, deniedTLS := range []string{secure.URL, strings.Replace(allowedTLS, "localhost", "sub.localhost", 1)} {
				c.text("close_tab", nil)
				c.text("open", map[string]any{"url": "about:blank"})
				_, e = i.Call(t.Context(), i.Current("chat"), "Security.setIgnoreCertificateErrors", map[string]any{"ignore": true})
				must(t, e)
				c.text("navigate", map[string]any{"url": deniedTLS + "/login"})
				c.refuses("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "login"}, "origin")
				c.eval("document.querySelector('[name=credential]').value === ''", "true")
			}
			// Numeric secrets are supported by native number inputs too.
			numeric := fmt.Sprintf("%06d", int(seed[0])*1000+int(seed[1]))
			_, _, err = store.Put("number", numeric)
			must(t, err)
			_, err = store.Metadata("number", []string{pageURL}, []string{"alpha"})
			must(t, err)
			navigate("/login?type=number")
			c.text("fill_secret", map[string]any{"id": c.node("Credential"), "secret": "number"})
			st, e := service.state(t.Context(), i, "chat", i.Current("chat"))
			must(t, e)
			st.mu.Lock()
			field := st.sealed[len(st.sealed)-1]
			st.mu.Unlock()
			var numericOK bool
			e = (Tools{S: service, Caller: "chat"}).nodeCheck(t.Context(), i, i.Current("chat"), field.ref, field.object,
				"function(){return this.value === "+jsonText(numeric)+"}", &numericOK)
			must(t, e)
			if !numericOK {
				t.Fatal("number input did not receive the numeric secret")
			}
			// Wait for the periodic registry write and inspect every requested metadata file.
			time.Sleep(1200 * time.Millisecond)
			must(t, filepath.Walk(data, func(path string, info os.FileInfo, e error) error {
				if e != nil {
					return e
				}
				if info.IsDir() {
					return nil
				}
				if info.Name() == "tabs.json" || info.Name() == "profiles.json" || strings.HasSuffix(info.Name(), ".log") {
					b, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					if bytes.Contains(b, []byte(value)) {
						t.Errorf("secret persisted in %s", info.Name())
					}
				}
				return nil
			}))
			answerMu.Lock()
			leaked := bytes.Contains(answers, []byte(value)) || bytes.Contains(answers, []byte(numeric))
			answerMu.Unlock()
			if leaked {
				t.Fatal("secret found in MCP or CLI response history")
			}
		})
	}
}
