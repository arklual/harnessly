package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHubConsoleDeliversTheLoopbackCallbackOnce(t *testing.T) {
	c, _ := consoleRig(t)
	var got atomic.Value
	var hits atomic.Int32
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		got.Store(r.Host + " " + r.URL.RequestURI())
		_, _ = w.Write([]byte("signed in, code=secret-in-the-answer"))
	}))
	defer login.Close()
	_, portText, _ := net.SplitHostPort(login.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	c.ports = func(string) []int { return []int{port} }
	c.claims = func(r *http.Request) map[string]any { return map[string]any{"sub": r.Header.Get("X-Test-Sub")} }
	vera := map[string]string{"X-Test-Session": "yes", "X-Test-Sub": "vera"}
	anna := map[string]string{"X-Test-Session": "yes", "X-Test-Sub": "anna"}

	rec := c.do("POST", "/agents/api/agent/terminal-tab", `{"command":"echo waiting-for-the-browser; read -r x"}`, asAgent)
	var tab struct {
		TabID string `json:"tabId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tab); err != nil || tab.TabID == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	screen := "/agents/api/console/" + tab.TabID + "/screen"
	cb := "/agents/api/console/" + tab.TabID + "/callback"

	// The first person who opens the console holds it, and is told which port waits.
	rec = c.do("GET", screen, "", vera)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ports":[`+portText+`]`) || !strings.Contains(rec.Body.String(), `"used":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := c.do("GET", screen, "", anna); rec.Code != 403 {
		t.Errorf("another person read the console: %d", rec.Code)
	}
	good := `{"url":"http://localhost:` + portText + `/callback?code=abc&state=xyz"}`
	if rec := c.do("POST", cb, good, anna); rec.Code != 403 {
		t.Errorf("another person sent the callback: %d", rec.Code)
	}

	// Only a loopback address, on a port of this console's processes, from the same site.
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"url":"http://example.com:` + portText + `/callback?code=abc"}`, 400},
		{`{"url":"https://localhost:` + portText + `/callback"}`, 400},
		{`{"url":"http://user@localhost:` + portText + `/callback"}`, 400},
		{`{"url":"http://localhost/callback"}`, 400},
		{`{"url":"http://127.0.0.1:1/callback"}`, 409},
		{`nope`, 400},
	} {
		if rec := c.do("POST", cb, tc.body, vera); rec.Code != tc.want {
			t.Errorf("%s: %d %s", tc.body, rec.Code, rec.Body.String())
		}
	}
	cross := map[string]string{"X-Test-Session": "yes", "X-Test-Sub": "vera", "Sec-Fetch-Site": "cross-site"}
	if rec := c.do("POST", cb, good, cross); rec.Code != 403 {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if hits.Load() != 0 {
		t.Fatal("a refused callback reached the listener")
	}

	// The one request reaches the listener with its path and query; the answer's body does not come back.
	rec = c.do("POST", cb, good, vera)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":200`) || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if g, _ := got.Load().(string); g != "localhost:"+portText+" /callback?code=abc&state=xyz" {
		t.Errorf("the listener got %q", g)
	}
	if rec := c.do("POST", cb, good, vera); rec.Code != 409 {
		t.Errorf("a second callback: %d", rec.Code)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests reached the listener", hits.Load())
	}
	rec = c.do("GET", screen, "", vera)
	if !strings.Contains(rec.Body.String(), `"used":true`) {
		t.Errorf("the page is not told it was sent: %s", rec.Body.String())
	}
}

func TestLoopbackTarget(t *testing.T) {
	for raw, want := range map[string]int{
		"http://localhost:8085/cb?code=1":   8085,
		"http://127.0.0.1:53682/?state=s":   53682,
		"http://[::1]:9000/oauth2/callback": 9000,
		" http://LOCALHOST:1234/x ":         1234,
	} {
		if port, _, err := loopbackTarget(raw); err != nil || port != want {
			t.Errorf("%q: %d %v", raw, port, err)
		}
	}
	for _, raw := range []string{"http://localhost.example.com:80/", "http://10.0.0.1:80/", "ftp://localhost:21/", "http://localhost:0/", "http://localhost:99999/", "localhost:80"} {
		if _, _, err := loopbackTarget(raw); err == nil {
			t.Errorf("%q passed", raw)
		}
	}
}
