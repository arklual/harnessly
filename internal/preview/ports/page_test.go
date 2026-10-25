package ports

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParsePage(t *testing.T) {
	doc := `<!doctype html><html><head>
<title>
  Acme &amp; Files </title>
<meta name="viewport" content="width=device-width">
<meta name='description' content='Files of the VM'>
<link rel="apple-touch-icon" href="/touch.png">
<link rel="shortcut icon" href="/fav.svg?v=1&amp;x=2">
</head><body></body></html>`
	title, description, icon := ParsePage(doc)
	if title != "Acme & Files" || description != "Files of the VM" || icon != "/fav.svg?v=1&x=2" {
		t.Fatalf("got %q %q %q", title, description, icon)
	}
	title, description, icon = ParsePage(`<html><meta property="og:title" content="Shared"><body>`)
	if title != "Shared" || description != "" || icon != "" {
		t.Fatalf("og: %q %q %q", title, description, icon)
	}
}

func TestFetchPageInlinesSmallSameServerIcon(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<title>App</title><link rel="icon" href="/i.svg"><link rel="x" href="http://elsewhere.invalid/">`)
	})
	mux.HandleFunc("/i.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	page := FetchPage(port, 2*time.Second)
	if page.Title != "App" || !strings.HasPrefix(page.Icon, "data:image/svg+xml;base64,") {
		t.Fatalf("page: %+v", page)
	}
}

func TestIsPageStatus(t *testing.T) {
	for status, want := range map[int]bool{200: true, 302: true, 401: true, 403: true, 400: false, 404: false, 500: false} {
		if isPageStatus(status) != want {
			t.Errorf("%d: want %v", status, want)
		}
	}
}

// The platform's own listeners and a page's title come through the scan; an error page is not a site.
func TestScanHidesPlatformAndNamesSites(t *testing.T) {
	ss := "LISTEN 0 4096 127.0.0.1:28150 0.0.0.0:* users:((\"runtime\",pid=11,fd=5))\n" +
		"LISTEN 0 4096 127.0.0.1:3000 0.0.0.0:* users:((\"node\",pid=22,fd=5))\n" +
		"LISTEN 0 4096 127.0.0.1:4000 0.0.0.0:* users:((\"self\",pid=33,fd=5))\n"
	s := &Scanner{
		Home: "/home/u", Worktrees: "/home/u/.worktrees", Self: 33,
		SS:    func() (string, error) { return ss, nil },
		Cwd:   func(int) string { return "" },
		Probe: func(int) (string, bool) { return "site", true },
		Page:  func(port int) Page { return Page{Title: "Port " + strconv.Itoa(port)} },
	}
	got, err := s.List(nil)
	if err != nil || len(got) != 1 || got[0].Port != 3000 || got[0].Title == nil || *got[0].Title != "Port 3000" {
		t.Fatalf("got %+v %v", got, err)
	}
}
