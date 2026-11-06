package agentbrowser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChromeScriptedPopupAndFreshContext(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = fmt.Fprint(w, `<body>Fixture</body>`)
			}))
			defer server.Close()
			parent, err := i.AgentNewTab(t.Context(), "chat", server.URL)
			must(t, err)
			other, err := i.AgentNewTab(t.Context(), "other", server.URL)
			must(t, err)
			for _, tab := range []Tab{parent, other} {
				if got := string(evaluate(t, i, tab.ID, "window.opener===null")); got != "true" {
					t.Fatal("chrome_open created a window with an opener")
				}
				raw, err := i.connection().Call(t.Context(), "Target.getTargetInfo", map[string]any{"targetId": tab.ID}, "")
				must(t, err)
				var info struct {
					Target target `json:"targetInfo"`
				}
				must(t, json.Unmarshal(raw, &info))
				if info.Target.Opener != "" {
					t.Fatal("chrome_open target has an opener relation")
				}
			}
			// Named lookup crosses same-origin windows in the same browsing-context
			// group. A fresh chrome_open group must create a third window instead.
			evaluate(t, i, other.ID, "window.name='fixture-window';true")
			must(t, i.MarkScripted("chat", parent.ID))
			evaluate(t, i, parent.ID, `window.open('/popup','fixture-window');true`)
			eventually(t, 5*time.Second, func() bool { return len(i.Tabs("chat")) == 2 })
			popup := i.Tabs("chat")[1]
			if popup.ID == other.ID || popup.Opener != parent.ID || !popup.Scripted {
				t.Fatal("fresh context group or scripted popup inheritance failed")
			}
			if i.Tabs("other")[0].Scripted {
				t.Fatal("unrelated chrome_open context was tainted")
			}
			must(t, i.SetControl(parent.ID, "person"))
			evaluate(t, i, parent.ID, `window.open('/person-popup','_blank');true`)
			eventually(t, 5*time.Second, func() bool { return len(i.Tabs("chat")) == 3 })
			personPopup := i.Tabs("chat")[2]
			if personPopup.Control != "person" || !personPopup.Scripted || personPopup.Conversation != "chat" {
				t.Fatal("popup during person control lost its inherited restrictions")
			}
			// Rediscover an orphan before its parent is attributed, as after a
			// runtime disconnect or a delayed createTarget reply.
			i.mu.Lock()
			delete(i.pages, personPopup.ID)
			i.pages[parent.ID].Conversation = ""
			i.mu.Unlock()
			must(t, i.reconcile(t.Context(), i.connection(), true))
			i.mu.Lock()
			i.pages[parent.ID].Conversation = "chat"
			i.attributeOpenersLocked()
			rediscovered := i.pages[personPopup.ID].Tab
			i.mu.Unlock()
			if rediscovered.Control != "person" || !rediscovered.Scripted || rediscovered.Opener != parent.ID || rediscovered.Conversation != "chat" {
				t.Fatal("reconciliation exposed an orphan person popup")
			}
		})
	}
}
