package agentbrowser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChromeScreencastDuringNavigation(t *testing.T) {
	_, i := contractBrowser(t, "headless")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<h1>Page</h1>") }))
	defer server.Close()
	for n := 0; n < 8; n++ {
		tab, err := i.NewTab(t.Context(), "chat", "person", "about:blank")
		must(t, err)
		_, err = i.Call(t.Context(), tab.ID, "Page.navigate", map[string]any{"url": server.URL})
		must(t, err)
		v, err := i.Watch(t.Context(), tab.ID)
		must(t, err)
		select {
		case <-v.Frames():
		case <-time.After(3 * time.Second):
			t.Fatal("no frame after navigation")
		}
		v.Close()
		must(t, i.CloseTab(t.Context(), tab.ID))
	}
}
