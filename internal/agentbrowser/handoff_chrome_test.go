package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChromePersonHandoff(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			value := "generated-" + strings.Repeat("k7", 18)
			encoded, err := json.Marshal(value)
			must(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if r.URL.Path == "/frame" {
					_, _ = fmt.Fprintf(w, `<body><div id="host"></div><script>
const root = document.querySelector('#host').attachShadow({mode:'closed'});
const input = document.createElement('input'); input.type='password'; input.value=%s; root.append(input); parent.postMessage('frame-ready','*');
</script>`, encoded)
					return
				}
				_, _ = fmt.Fprint(w, `<body>Fixture</body>`)
			}))
			defer server.Close()
			for _, test := range []struct {
				name, script string
				secret       bool
			}{
				{"empty", `document.body.innerHTML='<input type="password">';true`, false},
				{"ordinary_text", `document.body.innerHTML='<input type="text" value="ordinary">';true`, false},
				{"password", `document.body.innerHTML='<input type="password">';document.querySelector('input').value=` + string(encoded) + `;true`, true},
				{"replaced_tracked_password", `document.body.innerHTML='<input type="password">';document.querySelector('input').value=` + string(encoded) + `;true`, true},
				{"closed_shadow", `const host=document.createElement('div');document.body.append(host);const root=host.attachShadow({mode:'closed'});root.innerHTML='<input type="password">';root.querySelector('input').value=` + string(encoded) + `;true`, true},
				{"same_origin_frame", `window.addEventListener('message',e=>{if(e.data==='frame-ready')window.frameReady=true});const f=document.createElement('iframe');f.src='/frame';document.body.append(f);true`, true},
				{"cross_origin_frame", `window.addEventListener('message',e=>{if(e.data==='frame-ready')window.frameReady=true});const f=document.createElement('iframe');f.src=` + fmt.Sprintf("%q", strings.Replace(server.URL, "127.0.0.1", "localhost", 1)+"/frame") + `;document.body.append(f);true`, true},
				{"hostile_main_world", `document.body.innerHTML='<input type="password">';document.querySelector('input').value=` + string(encoded) + `;Object.defineProperty(HTMLInputElement.prototype,'value',{get(){throw Error('blocked')}});document.querySelectorAll=()=>[];true`, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					tab, err := i.AgentNewTab(t.Context(), "chat", server.URL)
					must(t, err)
					if test.name == "replaced_tracked_password" {
						evaluate(t, i, tab.ID, `document.body.innerHTML='<input id="old" type="password">';true`)
						must(t, i.Seal("chat", tab.ID, "tracked-document"))
						if i.Tabs("chat")[0].SecretUntracked {
							t.Fatal("fresh tool seal unexpectedly lost tracked provenance")
						}
					}
					must(t, i.SetControl(tab.ID, "person"))
					eventually(t, 5*time.Second, func() bool { return string(evaluate(t, i, tab.ID, "document.readyState")) == `"complete"` })
					evaluate(t, i, tab.ID, test.script)
					if strings.Contains(test.name, "frame") {
						eventually(t, 5*time.Second, func() bool {
							return string(evaluate(t, i, tab.ID, "window.frameReady===true")) == "true"
						})
					}
					_, found, scanErr := i.inspectPersonPage(t.Context(), tab.ID)
					must(t, scanErr)
					if test.name == "cross_origin_frame" {
						i.mu.Lock()
						attached := false
						for _, frame := range i.agentFrames {
							attached = attached || frame.tab == tab.ID && frame.conn == i.conn
						}
						i.mu.Unlock()
						if !attached {
							t.Fatal("cross-origin fixture did not exercise an out-of-process frame")
						}
					}
					if found != test.secret {
						t.Fatal("isolated scan missed or invented a password")
					}
					must(t, i.SetControl(tab.ID, "agent"))
					i.mu.Lock()
					result := i.pages[tab.ID].Tab
					i.mu.Unlock()
					if result.Sealed != test.secret || result.SecretTouched != test.secret {
						t.Fatalf("handoff %s: sealed=%v touched=%v", test.name, result.Sealed, result.SecretTouched)
					}
					if test.secret {
						if !result.SecretUntracked {
							t.Fatal("person handoff retained stale tool field provenance")
						}
						if result.SecretDocument == "" || result.SecretDocument == unknownHandoffDocument {
							t.Fatal("handoff did not capture the current browser document")
						}
						if i.Redact(value) != "[redacted]" {
							t.Fatal("person password was not registered for metadata redaction")
						}
						i.save()
						raw, err := os.ReadFile(filepath.Join(i.dir, "tabs.json"))
						must(t, err)
						if strings.Contains(string(raw), value) {
							t.Fatal("registry contains a person password")
						}
					}
					must(t, i.CloseTab(t.Context(), tab.ID))
				})
			}
		})
	}
}

func TestChromeHandoffResetsHistory(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `<body>Fixture</body>`)
			}))
			defer server.Close()
			tab, err := i.AgentNewTab(t.Context(), "chat", server.URL+"/login")
			must(t, err)
			must(t, i.SetControl(tab.ID, "person"))
			value := "generated-" + strings.Repeat("h6", 17)
			encoded, err := json.Marshal(value)
			must(t, err)
			evaluate(t, i, tab.ID, `document.body.innerHTML='<input type="password">';document.querySelector('input').value=`+string(encoded)+`;true`)
			_, err = i.Call(t.Context(), tab.ID, "Page.navigate", map[string]any{"url": server.URL + "/account"})
			must(t, err)
			eventually(t, 5*time.Second, func() bool {
				return string(evaluate(t, i, tab.ID, "location.pathname+':'+document.readyState")) == `"/account:complete"`
			})
			must(t, i.SetControl(tab.ID, "agent"))
			raw, err := i.Call(t.Context(), tab.ID, "Page.getNavigationHistory", nil)
			must(t, err)
			var history struct {
				Current int `json:"currentIndex"`
				Entries []struct {
					URL string `json:"url"`
				} `json:"entries"`
			}
			must(t, json.Unmarshal(raw, &history))
			if len(history.Entries) != 1 || history.Current != 0 || history.Entries[0].URL != server.URL+"/account" {
				t.Fatal("return to agent retained personal login history")
			}
			evaluate(t, i, tab.ID, "history.back();true")
			time.Sleep(100 * time.Millisecond)
			if got := string(evaluate(t, i, tab.ID, "location.pathname")); got != `"/account"` {
				t.Fatal("agent could go back to a person password")
			}
		})
	}
}

func TestChromeHandoffHistoryFailureKeepsPersonControl(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = fmt.Fprint(w, `<body><input type="password"></body>`)
			}))
			defer server.Close()
			tab, err := i.AgentNewTab(t.Context(), "chat", server.URL+"/login")
			must(t, err)
			must(t, i.SetControl(tab.ID, "person"))
			value := "generated-" + strings.Repeat("r3", 17)
			encoded, err := json.Marshal(value)
			must(t, err)
			eventually(t, 5*time.Second, func() bool {
				return string(evaluate(t, i, tab.ID, "document.readyState")) == `"complete"`
			})
			evaluate(t, i, tab.ID, `document.querySelector('input').value=`+string(encoded)+`;true`)
			_, err = i.Call(t.Context(), tab.ID, "Page.navigate", map[string]any{"url": server.URL + "/account"})
			must(t, err)
			eventually(t, 5*time.Second, func() bool {
				return string(evaluate(t, i, tab.ID, "location.pathname+':'+document.readyState")) == `"/account:complete"`
			})
			x := &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
			i.mu.Lock()
			i.pages[tab.ID].login = x
			i.mu.Unlock()
			// Inject failure only at the handoff boundary. The real browser keeps
			// its old documents, so restoring history still exercises real CDP.
			err = i.setControl(tab.ID, "agent", func(context.Context, string) (string, bool, error) {
				return unknownHandoffDocument, false, ErrHandoffHistory
			})
			if !errors.Is(err, ErrHandoffHistory) {
				t.Fatalf("failed history reset returned %v", err)
			}
			result := i.Tabs("chat")[0]
			if result.Control != "person" || !result.Sealed || !result.SecretTouched || !result.SecretUntracked || result.SecretDocument != unknownHandoffDocument {
				t.Fatal("blocked personal history was released to the agent")
			}
			select {
			case <-x.done:
				t.Fatal("history failure completed the login")
			default:
			}
			// A person's navigation may restore the old document, but the failed
			// handoff must still deny all agent calls into it.
			evaluate(t, i, tab.ID, "history.back();true")
			eventually(t, 5*time.Second, func() bool {
				return string(evaluate(t, i, tab.ID, "location.pathname")) == `"/login"`
			})
			_, err = i.AgentCall(t.Context(), "chat", tab.ID, "Runtime.evaluate", map[string]any{"expression": "document.body.innerText"})
			if !errors.Is(err, ErrPersonControl) {
				t.Fatal("history-restored personal document was exposed to the agent")
			}
			if current := i.Tabs("chat")[0]; !current.SecretUntracked || current.Control != "person" || !current.Sealed {
				t.Fatal("restoring history released the failed-handoff seal")
			}
		})
	}
}

func TestChromeHandoffDialogFailureSeals(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			tab, err := i.AgentNewTab(t.Context(), "chat", "about:blank")
			must(t, err)
			must(t, i.SetControl(tab.ID, "person"))
			evaluate(t, i, tab.ID, "setTimeout(()=>alert('Fixture'),0);true")
			eventually(t, 5*time.Second, func() bool {
				return i.Tabs("chat")[0].Dialog != nil
			})
			must(t, i.SetControl(tab.ID, "agent"))
			result := i.Tabs("chat")[0]
			if result.Control != "agent" || !result.Sealed || !result.SecretTouched || !result.SecretUntracked || result.SecretDocument != unknownHandoffDocument {
				t.Fatal("blocked renderer was exposed after returning control")
			}
			must(t, i.HandleDialog(t.Context(), tab.ID, true, ""))
		})
	}
}
