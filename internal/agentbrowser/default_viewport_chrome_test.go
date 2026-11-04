package agentbrowser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChromeAgentDefaultViewport(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `<script>window.firstSize=[innerWidth,innerHeight]</script><button onclick="window.open('/child')">Open</button>`)
			}))
			defer server.Close()
			tab, err := i.AgentNewTab(t.Context(), "chat", server.URL)
			must(t, err)
			eventually(t, 3*time.Second, func() bool {
				return string(evaluate(t, i, tab.ID, "JSON.stringify(window.firstSize)")) == `"[1280,800]"`
			})
			if tab.Viewport != agentViewport() {
				t.Fatalf("viewport: %+v", tab.Viewport)
			}
			evaluate(t, i, tab.ID, `window.open('/child');true`)
			eventually(t, 3*time.Second, func() bool { return len(i.Tabs("chat")) == 2 })
			child := i.Tabs("chat")[1]
			eventually(t, 3*time.Second, func() bool {
				return string(evaluate(t, i, child.ID, "JSON.stringify([innerWidth,innerHeight])")) == `"[1280,800]"`
			})
			if child.By != "agent" || child.Viewport != agentViewport() {
				t.Fatalf("popup: %+v", child)
			}
			person, err := i.NewTab(t.Context(), "person", "person", "about:blank")
			must(t, err)
			if person.Viewport.Mode != "fit" {
				t.Fatalf("person viewport: %+v", person.Viewport)
			}
		})
	}
}
