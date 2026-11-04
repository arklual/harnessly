package tools

import (
	"bytes"
	"context"
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
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// These are hard-failing regressions derived from the architect's adversarial Chrome probes.
// Private CDP calls supply fixture/person input or inspect booleans; attacks use public tools.
func TestChromeSecretAdversary(t *testing.T) {
	chrome, err := agentbrowser.FindChrome()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			exe, err := os.Executable()
			must(t, err)
			manager := agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode})
			access := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(data, ""), func(conv string) (agentbrowser.ConversationIdentity, bool) {
				return agentbrowser.ConversationIdentity{Project: "alpha", Title: "Adversary"}, conv != ""
			})
			access.Manager, access.Wait = manager, time.Millisecond
			store := secrets.Open(filepath.Join(data, "safe"), filepath.Join(data, "key"))
			service := &Service{Manager: manager, Secrets: store, Access: access, Identity: func(conv string) (secrets.Use, error) {
				return secrets.Use{Conv: conv, Project: "alpha", Title: "Adversary"}, nil
			}}
			t.Cleanup(func() {
				service.Close()
				manager.Close()
				cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile))
			})
			var pageURL string
			var submitted atomic.Bool
			var expectedValue atomic.Value
			expectedValue.Store("")
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch r.URL.Path {
				case "/submit":
					if r.FormValue("credential") != "" && r.FormValue("credential") == expectedValue.Load().(string) {
						submitted.Store(true)
						http.SetCookie(w, &http.Cookie{Name: "signed", Value: "yes", Path: "/"})
					}
					http.Redirect(w, r, "/home", http.StatusSeeOther)
				case "/home":
					fmt.Fprint(w, `<h1>Home</h1><button onclick="history.back()">Back</button>`)
				case "/landing":
					fmt.Fprint(w, `<h1>Landing</h1><p>quiet query</p>`)
				case "/popup":
					fmt.Fprint(w, `<h1>Popup</h1><button onclick="window.open('/popup')">Next</button>`)
				case "/paste":
					fmt.Fprint(w, `<label>Paste<input oninput="document.title=btoa(this.value)"></label>`)
				case "/race":
					fmt.Fprintf(w, `<input id="outside" aria-label="Outside"><iframe src="%s/login?type=password"></iframe><script>setInterval(()=>document.getElementById('outside').focus(),5)</script>`, pageURL)
				case "/offscreen-frame":
					fmt.Fprintf(w, `<iframe style="position:absolute;left:-9999px" src="%s/login"></iframe>`, pageURL)
				case "/shadow":
					fmt.Fprint(w, `<div id="host"></div><script>host.attachShadow({mode:'open'}).innerHTML='<label>Credential<input type="password"></label>'</script>`)
				default:
					kind := r.URL.Query().Get("type")
					if kind == "" {
						kind = "password"
					}
					extra := ""
					if r.URL.Query().Get("max") != "" {
						extra = `maxlength="10"`
					}
					if r.URL.Path == "/offscreen" {
						extra = `style="position:absolute;left:-9999px"`
					}
					if r.URL.Path == "/focus" {
						extra = `onfocus="document.getElementById('outside').focus()"`
					}
					if r.URL.Path == "/readonly-onfocus" {
						extra = `onfocus="this.readOnly=true"`
					}
					if r.URL.Path == "/dialog-input" {
						extra = `oninput="alert('pause')"`
					}
					method, action := "post", "/submit"
					if r.URL.Path == "/getform" {
						method, action = "get", "/landing"
					}
					fmt.Fprintf(w, `<form action="%s" method="%s"><label>Credential<input name="credential" type="%s" %s></label><button>Sign in</button></form>
<input id="outside"><a href="/popup" target="_blank" rel="opener">Help</a>
<button onclick="document.querySelector('[name=credential]').type='text'">Show</button>
<button onclick="document.querySelector('[name=credential]').value=''">Clear</button>
<button onclick="document.querySelector('[name=credential]').remove()">Remove</button>
<script>console.log('before-secret-recording');</script>`, action, method, kind, extra)
					if r.URL.Path == "/react" {
						fmt.Fprint(w, `<script>
const input=document.querySelector('[name=credential]'); let state='';
const descriptor=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');
Object.defineProperty(input,'value',{get(){return descriptor.get.call(this)},set(v){descriptor.set.call(this,v)}});
input.addEventListener('input',e=>{state=e.target.value;window.trusted=e.isTrusted&&e.inputType==='insertText'});
document.querySelector('form').onsubmit=e=>{e.preventDefault();fetch('/submit',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({credential:state})}).then(()=>location.href='/home')};
</script>`)
					}
				}
			}))
			defer page.Close()
			pageURL = page.URL
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
				mcp := mcpserver.Server{Name: "adversary", Sets: []mcpserver.Toolset{Tools{S: service, Caller: r.Header.Get("X-Test-Chat"), Cwd: data}}}
				_, body := mcp.Handle(r.Context(), v)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			run := func(name string, f func(*secretAttack)) {
				t.Run(name, func(t *testing.T) {
					seed := make([]byte, 20)
					_, e := rand.Read(seed)
					must(t, e)
					value := "s" + hex.EncodeToString(seed) + "z"
					expectedValue.Store(value)
					_, _, e = store.Put("probe", value)
					must(t, e)
					_, e = store.Metadata("probe", []string{pageURL}, []string{"alpha"})
					must(t, e)
					c := mcpClient{t, server.URL, name, value, server.Client()}
					a := &secretAttack{t: t, c: c, manager: manager, service: service, store: store, url: pageURL, value: value}
					t.Cleanup(func() {
						i, e := manager.InstanceFor(context.Background(), name)
						if e != nil {
							return
						}
						for _, tab := range i.Tabs(name) {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							_ = i.CloseTab(ctx, tab.ID)
							cancel()
						}
					})
					f(a)
				})
			}
			run("tool-handoff-gate", func(a *secretAttack) {
				a.open("/login")
				i, tab := a.instance(), a.tab()
				unlock := sync.OnceFunc(i.LockConversation(a.c.chat))
				defer unlock()
				finished := make(chan *jsvalue.Object, 1)
				go func() {
					set := Tools{S: service, Caller: a.c.chat}
					finished <- set.Call(a.t.Context(), "browser_evaluate", jsvalue.NewObject("expression", "1"))
				}()
				select {
				case <-finished:
					a.t.Fatal("tool dispatched while the handoff conversation gate was held")
				case <-time.After(200 * time.Millisecond):
				}
				// Install the handoff's new marker before its gate opens. The waiting
				// read must check this seal, rather than a pre-handoff snapshot.
				must(a.t, i.Seal(a.c.chat, tab, "handoff:unknown"))
				unlock()
				select {
				case result := <-finished:
					if raw := string(jsvalue.Stringify(result, "")); !strings.Contains(raw, "sealed") || !strings.Contains(raw, `"isError":true`) {
						a.t.Fatal("waiting tool did not enforce the completed handoff seal")
					}
				case <-time.After(5 * time.Second):
					a.t.Fatal("tool did not resume after handoff")
				}
			})
			run("scripted-hooks", func(a *secretAttack) {
				for _, hook := range []string{
					`const p=document.createElement('p');document.body.append(p);document.querySelector('input').addEventListener('input',e=>p.textContent=btoa(e.target.value));1`,
					`document.querySelector('input').addEventListener('input',e=>document.title=btoa(e.target.value));1`,
				} {
					a.open("/login")
					a.c.text("evaluate", map[string]any{"expression": hook})
					a.refuseFill("JavaScript")
					a.assertPrivate(`document.querySelector('input').value===''`)
					a.c.text("tabs", nil)
					a.c.text("snapshot", nil)
					a.c.text("close_tab", nil)
				}
			})
			run("opener-scripted-chain", func(a *secretAttack) {
				a.open("/login")
				root := a.tab()
				a.c.text("click", map[string]any{"id": a.c.node("Help")})
				popup := a.tab()
				a.c.text("evaluate", map[string]any{"expression": `opener.eval("document.querySelector('input').addEventListener('input',e=>document.title=btoa(e.target.value))");1`})
				a.c.text("close_tab", map[string]any{"tab": popup})
				a.c.text("snapshot", map[string]any{"tab": root})
				a.refuseFill("JavaScript")
				a.c.text("navigate", map[string]any{"url": pageURL + "/login"})
				a.refuseFill("JavaScript")
				// A new browser_open target has neither an opener nor access from the old component.
				a.open("/login")
				a.assertPrivate(`opener===null`)
				a.fill()
				a.c.refuses("evaluate", map[string]any{"tab": root, "expression": "1"}, "sealed")
			})
			run("chat-barrier-and-popup", func(a *secretAttack) {
				a.open("/login")
				root := a.tab()
				a.fill()
				a.c.text("click", map[string]any{"id": a.c.node("Help")})
				popup := a.tab()
				if popup == root {
					a.t.Fatal("fixture did not open a popup")
				}
				for _, tool := range []string{"evaluate", "console", "network", "screenshot"} {
					a.c.refuses(tool, map[string]any{"expression": `btoa(opener.document.querySelector('input').value)`}, "sealed")
				}
				other := mcpClient{a.t, server.URL, "independent", a.value, server.Client()}
				other.text("open", map[string]any{"url": pageURL + "/home"})
				other.eval("opener===null", "true")
				other.text("console", nil)
				other.text("network", nil)
				other.call("screenshot", nil)
				i := a.instance()
				for _, tab := range i.Tabs("") {
					if tab.ID == popup && tab.Conversation != a.c.chat {
						a.t.Fatal("popup escaped chat ownership")
					}
				}
				other.refuses("snapshot", map[string]any{"tab": popup}, "not available")
				other.text("close_tab", nil)
			})
			for _, kind := range []string{"password", "text"} {
				for _, how := range []string{"submit", "navigate"} {
					run("history-"+kind+"-"+how, func(a *secretAttack) {
						a.open("/login?type=" + kind)
						a.fill()
						if how == "submit" {
							a.c.text("click", map[string]any{"id": a.c.node("Sign in")})
						} else {
							a.c.text("navigate", map[string]any{"url": pageURL + "/home"})
						}
						a.evalExact("history.length", "1")
						a.c.text("evaluate", map[string]any{"expression": "history.back();1"})
						time.Sleep(100 * time.Millisecond)
						a.c.eval(`location.pathname==='/home' && !document.querySelector('[name=credential]')`, "true")
						if strings.Contains(a.c.text("console", nil), "before-secret-recording") {
							a.t.Fatal("old console recording survived unseal")
						}
					})
				}
			}
			run("clear-keeps-seal", func(a *secretAttack) {
				a.open("/login?type=text")
				a.fill()
				a.c.text("click", map[string]any{"id": a.c.node("Clear")})
				a.c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
				a.c.text("click", map[string]any{"id": a.c.node("Remove")})
				a.evalExact("history.length", "1")
			})
			for _, kind := range []string{"text", "email", "password"} {
				run("partial-snapshot-"+kind, func(a *secretAttack) {
					a.open("/login?type=" + kind)
					a.fill()
					if kind == "password" {
						a.c.text("click", map[string]any{"id": a.c.node("Show")})
					}
					a.c.text("click", map[string]any{"id": a.c.node("Credential")})
					a.c.text("press_key", map[string]any{"key": "End"})
					a.c.text("press_key", map[string]any{"key": "Backspace"})
					text := a.c.text("snapshot", nil)
					if strings.Contains(text, a.value[:len(a.value)-1]) || strings.Contains(text, ` value=`) {
						a.t.Fatal("editable value survived sealed snapshot")
					}
				})
			}
			run("atomic-rejection", func(a *secretAttack) {
				for _, path := range []string{"/login?type=text&max=10", "/offscreen", "/focus", "/readonly-onfocus"} {
					a.open(path)
					a.refuseFill("refused")
					a.assertPrivate(`Array.from(document.querySelectorAll('input')).every(e=>e.value==='')`)
					if tabSealed(a.instance(), a.c.chat, a.tab()) {
						a.t.Fatal("known rejection left a seal")
					}
					if a.instance().Redact(a.value) != a.value {
						a.t.Fatal("known rejection retained its mask")
					}
					a.c.text("close_tab", nil)
				}
			})
			run("uncertain-input", func(a *secretAttack) {
				a.open("/dialog-input")
				a.refuseFill("remains sealed")
				if !tabSealed(a.instance(), a.c.chat, a.tab()) || a.instance().Redact(a.value) == a.value {
					a.t.Fatal("uncertain insertion failed open")
				}
				a.c.text("dialog", map[string]any{"accept": true})
				a.c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
			})
			run("offscreen-frame", func(a *secretAttack) {
				a.open("/offscreen-frame")
				a.refuseFill("visible")
				if tabSealed(a.instance(), a.c.chat, a.tab()) {
					a.t.Fatal("offscreen rejection sealed the tab")
				}
			})
			run("clipboard-modifiers", func(a *secretAttack) {
				a.open("/login?type=text")
				a.fill()
				for _, key := range []string{"Control+a", "Control+c", "Ctrl+x", "Meta+c", "Alt+ArrowLeft", "Shift+Delete", "Shift+Insert"} {
					a.c.refuses("press_key", map[string]any{"key": key}, "sealed")
				}
				a.c.text("press_key", map[string]any{"key": "Tab"})
				a.c.text("navigate", map[string]any{"url": strings.Replace(pageURL, "127.0.0.1", "localhost", 1) + "/paste"})
				a.c.text("click", map[string]any{"id": a.c.node("Paste")})
				a.c.text("press_key", map[string]any{"key": "Control+v"})
				a.assertPrivate(`document.querySelector('input').value===''`)
			})
			run("form-encoding", func(a *secretAttack) {
				a.value = "x*y z~!'()" + a.value
				_, _, e := store.Put("probe", a.value)
				must(a.t, e)
				a.open("/getform?type=text")
				a.fill()
				a.c.text("click", map[string]any{"id": a.c.node("Sign in")})
				encoded := formEncodeForTest(a.value)
				for _, tool := range []string{"tabs", "network"} {
					text := a.c.text(tool, nil)
					if strings.Contains(text, encoded) {
						a.t.Fatal("form-encoded value leaked")
					}
				}
				login := a.c.text("request_login", map[string]any{"url": pageURL + "/landing?credential=" + encoded, "why": "Check URL masking"})
				if strings.Contains(login, encoded) {
					a.t.Fatal("manual login answer exposed encoded value")
				}
			})
			run("atomic-iframe-and-controlled-input", func(a *secretAttack) {
				a.c.text("open", map[string]any{"url": strings.Replace(pageURL, "127.0.0.1", "localhost", 1) + "/race"})
				a.fill()
				a.assertPrivate(`document.getElementById('outside').value===''`)
				a.c.text("close_tab", nil)
				a.open("/shadow")
				a.fill()
				a.c.text("close_tab", nil)
				submitted.Store(false)
				a.open("/react?type=text")
				a.fill()
				a.assertPrivate(`window.trusted===true`)
				a.c.text("click", map[string]any{"id": a.c.node("Sign in")})
				if !submitted.Load() {
					a.t.Fatal("controlled input did not submit its state")
				}
			})
			run("person-handoff", func(a *secretAttack) {
				a.c.text("request_login", map[string]any{"url": pageURL + "/login", "why": "Manual credentials"})
				i := a.instance()
				tab := a.tab()
				_, e := i.Call(a.t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('input').focus()`})
				must(a.t, e)
				_, e = i.Call(a.t.Context(), tab, "Input.insertText", map[string]any{"text": a.value})
				must(a.t, e)
				must(a.t, i.SetControl(tab, "agent"))
				for _, tool := range []string{"snapshot", "evaluate", "console", "network", "screenshot"} {
					a.c.refuses(tool, map[string]any{"expression": "1"}, "sealed")
				}
				if i.Redact(a.value) == a.value {
					a.t.Fatal("person password was not masked")
				}
				a.c.text("navigate", map[string]any{"url": pageURL + "/home"})
				a.evalExact("history.length", "1")
				a.c.text("evaluate", map[string]any{"expression": "history.back();1"})
				time.Sleep(100 * time.Millisecond)
				a.c.eval(`location.pathname==='/home'`, "true")
			})
			run("person-replaces-tracked-field", func(a *secretAttack) {
				a.open("/login?type=text")
				a.fill()
				i, tab := a.instance(), a.tab()
				must(a.t, i.SetControl(tab, "person"))
				_, e := i.Call(a.t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('[name=credential]').remove();const p=document.createElement('input');p.type='password';document.body.append(p);p.focus()`})
				must(a.t, e)
				_, e = i.Call(a.t.Context(), tab, "Input.insertText", map[string]any{"text": a.value + "-new"})
				must(a.t, e)
				must(a.t, i.SetControl(tab, "agent"))
				a.c.refuses("snapshot", map[string]any{}, "sealed")
				a.c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
			})

			for _, takeover := range []bool{true, false} {
				name := "control-return-interrupts-wait"
				if takeover {
					name = "control-takeover-interrupts-wait"
				}
				run(name, func(a *secretAttack) {
					a.open("/login")
					i, first := a.instance(), a.tab()
					a.open("/landing")
					other := a.tab()
					var before int64
					for _, tab := range i.Tabs(a.c.chat) {
						if tab.ID == first {
							before = tab.AgentAt
						}
					}
					finished := make(chan *jsvalue.Object, 1)
					go func() {
						set := Tools{S: service, Caller: a.c.chat}
						finished <- set.Call(a.t.Context(), "browser_wait_for", jsvalue.NewObject(
							"tab", first, "text", "never appears", "timeout_ms", 20000))
					}()
					deadline := time.Now().Add(3 * time.Second)
					started := false
					for time.Now().Before(deadline) && !started {
						for _, tab := range i.Tabs(a.c.chat) {
							if tab.ID == first && tab.AgentAt > before {
								started = true
							}
						}
						if !started {
							time.Sleep(10 * time.Millisecond)
						}
					}
					if !started {
						a.t.Fatal("wait tool did not start")
					}
					began := time.Now()
					holder, target, limit, reason := "agent", other, 2*time.Second, "Interrupted"
					if takeover {
						holder, target, limit, reason = "person", first, time.Second, "The person has control"
					}
					must(a.t, i.SetControl(target, holder))
					a.t.Logf("control change took %s", time.Since(began))
					if time.Since(began) >= limit {
						a.t.Fatalf("control change blocked for %v", time.Since(began))
					}
					select {
					case result := <-finished:
						raw := string(jsvalue.Stringify(result, ""))
						if !strings.Contains(raw, `"isError":true`) || !strings.Contains(raw, reason) {
							a.t.Fatalf("wait did not return a control error: %s", raw)
						}
					case <-time.After(time.Second):
						a.t.Fatal("wait continued after control change")
					}
				})
			}
			run("csrf-false-positive", func(a *secretAttack) {
				a.open("/login?type=search")
				i, tab := a.instance(), a.tab()
				_, err := i.Call(a.t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `
const form=document.querySelector('form');
for(const type of ['hidden','checkbox','radio','submit','button','reset','file','image','range','color','date','datetime-local','month','time','week']) {
 const input=document.createElement('input'); input.type=type; input.name='authenticity_token';
 input.id='password-'+type; input.autocomplete='current-password';
 if(type!=='file') input.value=type==='submit'?'Continue here':'fixture-'+type+'-value';
 form.append(input);
}`})
				must(a.t, err)
				must(a.t, i.SetControl(tab, "person"))
				a.personType("pizza near me")
				must(a.t, i.SetControl(tab, "agent"))
				if i.Tabs(a.c.chat)[0].Sealed {
					a.t.Fatal("non-text inputs sealed ordinary search")
				}
				if out := a.c.text("snapshot", nil); !strings.Contains(out, "pizza near me") || !strings.Contains(out, "Continue here") {
					a.t.Fatal("ordinary search snapshot was hidden or masked")
				}
				for _, value := range []string{"fixture-hidden-value", "fixture-checkbox-value", "Continue here"} {
					if i.Redact(value) != value {
						a.t.Fatal("non-text input entered the mask")
					}
				}
				a.c.refuses("evaluate", map[string]any{"expression": "1"}, "The person typed")
			})
			run("person-document-custom-editor", func(a *secretAttack) {
				a.open("/landing")
				i, tab := a.instance(), a.tab()
				_, err := i.Call(a.t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": "const e=document.createElement('div');e.contentEditable='true';e.setAttribute('role','textbox');document.body.append(e);e.oninput=()=>{window.savedBySite=e.textContent;e.textContent=''};e.focus()"})
				must(a.t, err)
				_, err = i.Call(a.t.Context(), tab, "Input.insertText", map[string]any{"text": a.value})
				must(a.t, err)
				a.c.refuses("evaluate", map[string]any{"expression": "window.savedBySite"}, "The person typed")
				if i.Tabs(a.c.chat)[0].Sealed {
					a.t.Fatal("custom editor unexpectedly relied on the input classifier")
				}
			})
			run("person-document-copy-clear", func(a *secretAttack) {
				a.open("/login")
				i, parent := a.instance(), a.tab()
				_, err := i.Call(a.t.Context(), parent, "Runtime.evaluate", map[string]any{"expression": `
const input=document.querySelector('input');
input.addEventListener('input',()=>{window.savedBySite=input.value;input.value=''});`})
				must(a.t, err)
				a.personType(a.value)
				a.c.refuses("evaluate", map[string]any{"expression": `window.savedBySite.split('').reverse().join('')`}, "The person typed")
				if tab := i.Tabs(a.c.chat)[0]; tab.Sealed || tab.Scripted || tab.PersonDocument == "" {
					a.t.Fatal("document guard was lost or confused with a seal")
				}
				a.c.text("snapshot", nil)
				a.c.call("screenshot", nil)
				a.c.text("console", nil)
				a.c.text("network", nil)
				_, err = i.Call(a.t.Context(), parent, "Runtime.evaluate", map[string]any{"expression": `history.pushState({},'', '/spa')`})
				must(a.t, err)
				a.c.refuses("evaluate", map[string]any{"expression": `window.savedBySite`}, "The person typed")
				a.c.text("click", map[string]any{"id": a.c.node("Help")})
				popup := a.tab()
				if popup == parent {
					a.t.Fatal("fixture did not create a popup")
				}
				a.c.refuses("evaluate", map[string]any{"expression": `opener.savedBySite`}, "The person typed")
				for _, tab := range i.Tabs(a.c.chat) {
					if tab.Scripted {
						a.t.Fatal("refusal marked opener component scripted")
					}
				}
				a.open("/landing")
				a.evalExact("1+1", "2")
				// Reattach the same surviving Chrome after a runtime restart.
				service.Close()
				manager.Close()
				manager = agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode})
				access.Manager = manager
				service = &Service{Manager: manager, Secrets: store, Access: access, Identity: service.Identity}
				a.manager, a.service = manager, service
				a.c.refuses("evaluate", map[string]any{"tab": parent, "expression": `window.savedBySite`}, "The person typed")
				// Reloading the current address changes the loader even when the URL is identical.
				a.c.text("navigate", map[string]any{"tab": parent, "url": pageURL + "/spa"})
				if out := a.c.text("evaluate", map[string]any{"tab": parent, "expression": "typeof window.savedBySite"}); !strings.Contains(out, `"undefined"`) {
					a.t.Fatal("same-address navigation did not release the document guard")
				}
			})
			run("lazy-person-password", func(a *secretAttack) {
				a.open("/login")
				a.personType(a.value)
				a.c.refuses("evaluate", map[string]any{"expression": `document.querySelector('input').value.split('').reverse().join('')`}, "sealed")
				if tab := a.instance().Tabs(a.c.chat)[0]; !tab.Sealed || tab.PersonInput || tab.Control != "agent" {
					a.t.Fatal("lazy input scan did not consume the guard and seal the agent-owned tab")
				}
				if a.instance().Redact(a.value) == a.value {
					a.t.Fatal("lazy password was not masked")
				}
			})
			run("person-opener-control", func(a *secretAttack) {
				a.open("/login")
				parent := a.tab()
				a.c.text("click", map[string]any{"id": a.c.node("Help")})
				popup := a.tab()
				if popup == parent {
					a.t.Fatal("fixture did not open popup")
				}
				i := a.instance()
				must(a.t, i.SetControl(parent, "person"))
				_, err := i.Call(a.t.Context(), parent, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('input').focus()`})
				must(a.t, err)
				_, err = i.Call(a.t.Context(), parent, "Input.insertText", map[string]any{"text": a.value})
				must(a.t, err)
				a.c.refuses("evaluate", map[string]any{"tab": popup, "expression": `opener.document.querySelector('input').value`}, "person")
				for _, tab := range i.Tabs(a.c.chat) {
					if tab.Scripted {
						a.t.Fatal("refused related evaluation marked the person's page scripted")
					}
				}
				// Independent tabs in the same chat remain usable during person control.
				a.open("/landing")
				a.evalExact("1+1", "2")
				must(a.t, i.SetControl(parent, "agent"))
				a.c.refuses("evaluate", map[string]any{"tab": popup, "expression": "1"}, "sealed")
			})

			run("lazy-person-history", func(a *secretAttack) {
				a.open("/login")
				// Capture the button before input, like a tool already planning its next action.
				button := a.c.node("Sign in")
				a.personType(a.value)
				a.c.text("click", map[string]any{"id": button})
				a.evalExact("history.length", "1")
				a.c.text("evaluate", map[string]any{"expression": "history.back();1"})
				time.Sleep(100 * time.Millisecond)
				a.c.eval(`location.pathname==='/home' && document.querySelector('input')===null`, "true")
			})
			run("lazy-person-submitted", func(a *secretAttack) {
				a.open("/login")
				a.personType(a.value)
				_, e := a.instance().Call(a.t.Context(), a.tab(), "Runtime.evaluate", map[string]any{
					"expression": `document.querySelector('form').requestSubmit()`})
				must(a.t, e)
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					raw, e := a.instance().Call(a.t.Context(), a.tab(), "Runtime.evaluate", map[string]any{"expression": `location.pathname==='/home'`, "returnByValue": true})
					if e == nil && strings.Contains(string(raw), `"value":true`) {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				a.evalExact("history.length", "1")
				a.c.text("evaluate", map[string]any{"expression": "history.back();1"})
				time.Sleep(100 * time.Millisecond)
				a.c.eval(`location.pathname==='/home' && document.querySelector('input')===null`, "true")
			})

			run("lazy-short-password", func(a *secretAttack) {
				a.open("/login")
				a.personType("q")
				a.c.refuses("evaluate", map[string]any{"expression": "1"}, "sealed")
				a.open("/landing")
				if a.instance().Redact("quiet query") != "quiet query" {
					a.t.Fatal("one-character password corrupted unrelated output")
				}
				// Read tools are chat-wide sealed, while snapshot of the untouched neighbor remains available.
				if !strings.Contains(a.c.text("snapshot", nil), "quiet query") {
					a.t.Fatal("neighbor snapshot was corrupted")
				}
			})
			for _, login := range []bool{false, true} {
				for _, kind := range []string{"name", "ordinary", "css", "autocomplete", "aria", "placeholder", "id"} {
					name := fmt.Sprintf("manual-reveal-%s-login-%t", kind, login)
					run(name, func(a *secretAttack) {
						if login {
							a.c.text("request_login", map[string]any{"url": pageURL + "/login", "why": "Manual sign in"})
						} else {
							a.open("/login")
							must(a.t, a.instance().SetControl(a.tab(), "person"))
						}
						a.personType(a.value)
						expression := `const p=document.querySelector('input');p.type='text';`
						switch kind {
						case "name":
							expression += `p.name='password';`
						case "css":
							expression += `p.style.webkitTextSecurity='disc';`
						case "autocomplete":
							expression += `p.autocomplete='one-time-code';`
						case "aria":
							expression += `p.setAttribute('aria-label','PIN');`
						case "placeholder":
							expression += `p.placeholder='пароль';`
						case "id":
							expression += `p.id='access-token';`
						}
						_, e := a.instance().Call(a.t.Context(), a.tab(), "Runtime.evaluate", map[string]any{"expression": expression})
						must(a.t, e)
						must(a.t, a.instance().SetControl(a.tab(), "agent"))
						if kind != "ordinary" || login {
							a.c.refuses("evaluate", map[string]any{"expression": `document.querySelector('input').value.split('').reverse().join('')`}, "sealed")
						} else {
							// Ordinary text stays visible to snapshots, but not agent JavaScript.
							// This value intentionally is ordinary content, not a secret for this scenario.
							client := a.c
							client.password = ""
							if !strings.Contains(client.text("snapshot", nil), a.value) {
								a.t.Fatal("ordinary text missing from snapshot")
							}
							a.c.refuses("evaluate", map[string]any{"expression": "1"}, "The person typed")
						}
						masked := a.instance().Redact(a.value) != a.value
						if masked != (kind != "ordinary") {
							a.t.Fatal("manual input classification applied the wrong mask policy")
						}
						a.assertPrivate(`history.length===1`)
						a.c.text("navigate", map[string]any{"url": pageURL + "/home"})
						if kind != "ordinary" || login {
							a.evalExact("history.length", "1")
						}
					})
				}
			}

			if mode == "headless" {
				run("profile-persistence", func(a *secretAttack) {
					a.open("/login?type=text")
					a.fill()
					a.c.text("click", map[string]any{"id": a.c.node("Sign in")})
					time.Sleep(21 * time.Second)
					a.c.text("close_tab", nil)
					wide := []byte{}
					for _, r := range utf16.Encode([]rune(a.value)) {
						wide = append(wide, byte(r), byte(r>>8))
					}
					root := filepath.Join(data, "agent-browser", "profiles")
					must(a.t, filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
						if e != nil {
							return e
						}
						if !info.Mode().IsRegular() {
							return nil
						}
						raw, e := os.ReadFile(path)
						if os.IsNotExist(e) {
							return nil
						}
						if e != nil {
							return e
						}
						if bytes.Contains(raw, []byte(a.value)) || bytes.Contains(raw, wide) {
							a.t.Errorf("profile persisted input in %s", strings.TrimPrefix(path, root))
						}
						return nil
					}))
				})
			}
		})
	}
}

type secretAttack struct {
	t          *testing.T
	c          mcpClient
	manager    *agentbrowser.Manager
	service    *Service
	store      *secrets.Store
	url, value string
}

func (a *secretAttack) instance() *agentbrowser.Instance {
	a.t.Helper()
	i, e := a.manager.InstanceFor(a.t.Context(), a.c.chat)
	must(a.t, e)
	return i
}
func (a *secretAttack) tab() string { return a.instance().Current(a.c.chat) }
func (a *secretAttack) open(path string) {
	a.t.Helper()
	a.c.text("open", map[string]any{"url": a.url + path})
}
func (a *secretAttack) fill() {
	a.t.Helper()
	a.c.text("fill_secret", map[string]any{"id": a.c.node("Credential"), "secret": "probe"})
}
func (a *secretAttack) refuseFill(reason string) {
	a.t.Helper()
	a.c.refuses("fill_secret", map[string]any{"id": a.c.node("Credential"), "secret": "probe"}, reason)
}
func (a *secretAttack) assertPrivate(expression string) {
	a.t.Helper()
	raw, e := a.instance().Call(a.t.Context(), a.tab(), "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
	must(a.t, e)
	var r struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	must(a.t, json.Unmarshal(raw, &r))
	if !r.Result.Value {
		a.t.Fatal("private browser assertion failed: " + expression)
	}
}

// Independent expected WHATWG encoding for the GET-form regression.
func formEncodeForTest(s string) string {
	var out strings.Builder
	for _, b := range []byte(s) {
		switch {
		case b == ' ':
			out.WriteByte('+')
		case b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("*-._", rune(b)):
			out.WriteByte(b)
		default:
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func (a *secretAttack) evalExact(expression, want string) {
	a.t.Helper()
	lines := strings.Split(a.c.text("evaluate", map[string]any{"expression": expression}), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != want {
		a.t.Fatal("unexpected evaluate result for " + expression)
	}
}

// Human CDP goes through the same Call path as panel keys, paste and screen keyboard.
func (a *secretAttack) personType(value string) {
	a.t.Helper()
	i, tab := a.instance(), a.tab()
	_, err := i.Call(a.t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('input').focus()`})
	must(a.t, err)
	_, err = i.Call(a.t.Context(), tab, "Input.insertText", map[string]any{"text": value})
	must(a.t, err)
}
