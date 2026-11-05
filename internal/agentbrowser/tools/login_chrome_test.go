package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestChromeLoginToolControl(t *testing.T) {
	if testing.Short() {
		t.Skip("drives isolated Chrome")
	}
	chrome, err := agentbrowser.FindChrome()
	must(t, err)
	exe, err := os.Executable()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			access := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(data, ""), func(conv string) (agentbrowser.ConversationIdentity, bool) {
				return agentbrowser.ConversationIdentity{Title: "Login task"}, conv == "chat"
			})
			access.Wait = time.Millisecond
			manager := agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode, LookupProfile: access.Store.Lookup, ProfileOf: access.ProfileOf})
			access.Manager = manager
			service := &Service{Manager: manager, Access: access}
			t.Cleanup(func() {
				service.Close()
				manager.Close()
				cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile))
			})
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<title>Private login title</title><h1>Private account contents</h1>`))
			}))
			defer page.Close()
			set := Tools{S: service, Caller: "chat"}
			askArgs := jsvalue.NewObject("url", page.URL, "why", "Sign in to continue")
			result := set.Call(t.Context(), "chrome_request_login", askArgs)
			if raw := string(jsvalue.Stringify(result, "")); !strings.Contains(raw, "waiting") || strings.Contains(raw, `"isError":true`) {
				t.Fatal(raw)
			}
			asks := access.Logins.Pending()
			if len(asks) != 1 {
				t.Fatal(asks)
			}
			tab := asks[0].Tab
			instance, err := manager.InstanceFor(t.Context(), "chat")
			must(t, err)
			ready := false
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				raw, e := instance.Call(t.Context(), tab, "Runtime.evaluate", map[string]any{
					"expression": `location.protocol==='http:' && document.readyState==='complete'`, "returnByValue": true,
				})
				if e == nil && strings.Contains(string(raw), `"value":true`) {
					ready = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("manual login document did not commit")
			}
			_, err = instance.Call(t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `window.open('/popup');true`, "userGesture": true})
			must(t, err)
			popupDeadline := time.Now().Add(5 * time.Second)
			var popup string
			for time.Now().Before(popupDeadline) && popup == "" {
				for _, candidate := range instance.Tabs("chat") {
					if candidate.ID != tab {
						popup = candidate.ID
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if popup == "" {
				t.Fatal("person-controlled page did not open its popup")
			}
			for _, tt := range []struct {
				name string
				args *jsvalue.Object
			}{
				{"navigate", jsvalue.NewObject("url", page.URL)},
				{"close_tab", jsvalue.NewObject()},
				{"resize", jsvalue.NewObject("width", float64(800), "height", float64(600))},
				{"snapshot", jsvalue.NewObject()},
				{"screenshot", jsvalue.NewObject()},
				{"wait_for", jsvalue.NewObject("text", "Private")},
				{"evaluate", jsvalue.NewObject("expression", "document.body.textContent")},
				{"console", jsvalue.NewObject()},
				{"network", jsvalue.NewObject()},
				{"click", jsvalue.NewObject("id", "e1")},
				{"hover", jsvalue.NewObject("id", "e1")},
				{"type", jsvalue.NewObject("id", "e1", "text", "test")},
				{"press_key", jsvalue.NewObject("key", "Enter")},
				{"select", jsvalue.NewObject("id", "e1", "value", "test")},
				{"upload", jsvalue.NewObject("id", "e1", "files", []any{"fixture.txt"})},
				{"dialog", jsvalue.NewObject("accept", false)},
				{"fill_secret", jsvalue.NewObject("id", "e1", "secret", "test-login")},
			} {
				for _, target := range []string{tab, popup} {
					tt.args.Set("tab", target)
					out := set.Call(t.Context(), "chrome_"+tt.name, tt.args)
					raw := string(jsvalue.Stringify(out, ""))
					if !strings.Contains(raw, `"isError":true`) || !strings.Contains(raw, "person") {
						t.Fatalf("%s on %s: %s", tt.name, target, raw)
					}
				}
			}
			must(t, instance.CloseTab(t.Context(), popup))
			_, err = instance.Call(t.Context(), tab, "Runtime.evaluate", map[string]any{"expression": `document.title='Private login title';setTimeout(()=>prompt('Private dialog','Private answer'),10);true`})
			must(t, err)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) && instance.Tabs("chat")[0].Dialog == nil {
				time.Sleep(10 * time.Millisecond)
			}
			if instance.Tabs("chat")[0].Dialog == nil {
				t.Fatal("fixture dialog did not open")
			}
			out := set.Call(t.Context(), "chrome_tabs", jsvalue.NewObject())
			if strings.Contains(string(jsvalue.Stringify(out, "")), "Private") {
				t.Fatal("person-controlled title or dialog leaked through chrome_tabs")
			}
			must(t, instance.HandleDialog(t.Context(), tab, false, ""))
			must(t, access.AnswerLogin(t.Context(), asks[0].ID, "done"))
			out = set.Call(t.Context(), "chrome_request_login", askArgs)
			if raw := string(jsvalue.Stringify(out, "")); !strings.Contains(raw, "done") || strings.Contains(raw, "Private") {
				t.Fatal(raw)
			}
			out = set.Call(t.Context(), "chrome_snapshot", jsvalue.NewObject())
			if !strings.Contains(string(jsvalue.Stringify(out, "")), "Private account contents") {
				t.Fatal("Done did not return access to the agent")
			}
		})
	}
}
