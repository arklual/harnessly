package agentbrowser

import (
	"context"
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

// Exercise selection cancellation while Chrome is starting or stopping its stream.
func TestChromeScreencastAfterCanceledWatches(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `<body>fixture<script>setInterval(()=>document.body.style.background=Math.random()<.5?'red':'blue',80)</script>`)
			}))
			defer server.Close()
			var tabs []Tab
			for range 2 {
				tab, err := i.NewTab(t.Context(), "chat", "person", server.URL)
				must(t, err)
				tabs = append(tabs, tab)
			}
			canceled := 0
			for n := range 30 {
				ctx, cancel := context.WithCancel(t.Context())
				stop := time.AfterFunc(time.Duration(n%5+1)*time.Millisecond, cancel)
				v, err := i.Watch(ctx, tabs[n%2].ID)
				stop.Stop()
				cancel()
				if err != nil {
					canceled++
				} else {
					v.Close()
				}
			}
			t.Logf("canceled %d of 30 watch attempts", canceled)
			for _, tab := range tabs {
				v, err := i.Watch(t.Context(), tab.ID)
				must(t, err)
				select {
				case <-v.Frames():
				case <-time.After(3 * time.Second):
					t.Fatal("no frame after canceled watches")
				}
				v.Close()
			}
		})
	}
}

func TestChromeScreencastAdoptsUnclaimedStream(t *testing.T) {
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			_, i := contractBrowser(t, mode)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `<body>fixture<script>let n=0;setInterval(()=>document.body.style.background=n++%2?'red':'blue',80)</script>`)
			}))
			defer server.Close()
			for _, fixture := range []struct{ name, url string }{{"static", "about:blank"}, {"animated", server.URL}} {
				t.Run(fixture.name, func(t *testing.T) {
					tab, err := i.NewTab(t.Context(), "chat", "person", fixture.url)
					must(t, err)
					// A canceled Watch may leave Chrome streaming without registering a viewer.
					_, err = i.Call(t.Context(), tab.ID, "Page.startScreencast", map[string]any{"format": "jpeg"})
					must(t, err)
					started := time.Now()
					v, err := i.Watch(t.Context(), tab.ID)
					must(t, err)
					defer v.Close()
					select {
					case frame := <-v.Frames():
						t.Logf("first adopted frame after %s", time.Since(started))
						if len(frame.JPEG) < 2 || frame.JPEG[0] != 0xff || frame.JPEG[1] != 0xd8 {
							t.Fatal("invalid JPEG from the existing screencast")
						}
					case <-time.After(3 * time.Second):
						// Keep the strict assertion, but distinguish a late frame from a lost subscription.
						t.Error("no frame from the existing screencast within 3 seconds")
						select {
						case <-v.Frames():
							t.Logf("late adopted frame after %s", time.Since(started))
						case <-time.After(27 * time.Second):
							t.Log("no adopted frame after 30 seconds")
						}
					}
				})
			}
		})
	}
}
