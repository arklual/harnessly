//go:build linux

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// The right pane's browser of a runtime run on its own, with no gateway in front: the pane opens a running dev server,
// the slot's enter page sets the slot's cookie and leads to the page, which comes through the preview proxy from an
// origin other than the app's. A dev server that is down is told apart from a runtime that has no preview, and a slot
// page cannot drive the app.
func TestStandalonePreviewEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the runtime")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "runtime")
	if out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".acme-ai")
	tmuxDir := filepath.Join(root, "tmux")
	for _, d := range []string{home, tmuxDir} {
		_ = os.MkdirAll(d, 0o700)
	}
	bridge := filepath.Join(root, "bridge.js")
	_ = os.WriteFile(bridge, []byte("/* the page bridge */"), 0o644)

	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cookies" {
			_, _ = io.WriteString(w, "cookies="+r.Header.Get("Cookie"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "dev-session", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>Dev</title></head><body>hello from the dev server at `+r.URL.RequestURI()+`</body></html>`)
	}))
	defer dev.Close()
	devPort := portOf(strings.TrimPrefix(dev.URL, "http://"))

	socket := "hs-test-pv-" + strconv.Itoa(os.Getpid())
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	env := []string{
		"HOME=" + home, "USER=agent", "LOGNAME=agent", "SHELL=/bin/bash", "LANG=C.UTF-8", "TMUX_TMPDIR=" + tmuxDir,
		"PATH=/usr/local/bin:/usr/bin:/bin", "RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "RUNTIME_GATE_UPSTREAM=",
		"AGENT_DATA_DIR=" + data, "RUNTIME_TMUX_SOCKET=" + socket, "RUNTIME_SESSION_PREFIX=hs-", "RUNTIME_GATE_COUNT_SECONDS=3600",
		"RUNTIME_PREVIEW_LISTEN=127.0.0.1:0", "RUNTIME_PREVIEW_BRIDGE_FILE=" + bridge,
	}
	logFile, _ := os.Create(filepath.Join(root, "gate.log"))
	defer logFile.Close()
	gate := exec.Command(bin)
	gate.Env, gate.Stdout, gate.Stderr = env, logFile, logFile
	gate.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := gate.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gate.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = gate.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = gate.Process.Kill()
			<-done
		}
		kill := exec.Command("tmux", "-L", socket, "kill-server")
		kill.Env = env
		_ = kill.Run()
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(root, "gate.log"))
			t.Logf("gate log:\n%s", tail(string(raw), 6000))
		}
	})

	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type answer struct {
		code   int
		body   string
		header http.Header
	}
	do := func(method, address, body string, header http.Header) answer {
		t.Helper()
		req, _ := http.NewRequest(method, address, strings.NewReader(body))
		for k, v := range header {
			req.Header[k] = v
		}
		res, err := client.Do(req)
		if err != nil {
			return answer{body: err.Error()}
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return answer{res.StatusCode, string(raw), res.Header}
	}
	for deadline := time.Now().Add(30 * time.Second); do("GET", base+"/api/health", "", nil).code != 200; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the runtime did not come up")
		}
	}
	var config struct {
		AuthSecret string `json:"authSecret"`
	}
	raw, _ := os.ReadFile(filepath.Join(data, "config.json"))
	_ = json.Unmarshal(raw, &config)
	cookie := "session-token=" + agent.SignHS256([]byte(config.AuthSecret), "user", "", time.Hour)
	page := http.Header{"Cookie": {cookie}, "Origin": {base}, "Content-Type": {"application/json"}}

	type opened struct {
		Slot     int    `json:"slot"`
		Origin   string `json:"origin"`
		EnterURL string `json:"enterUrl"`
		Error    string `json:"error"`
	}
	open := func(body string, header http.Header) (int, opened) {
		t.Helper()
		a := do("POST", base+"/api/preview/open", body, header)
		var o opened
		_ = json.Unmarshal([]byte(a.body), &o)
		return a.code, o
	}
	portBody := func(p, slot int, path string) string {
		return `{"target":{"kind":"port","port":` + strconv.Itoa(p) + `},"slot":` + strconv.Itoa(slot) + `,"path":"` + path + `"}`
	}

	// Only the app's own session opens a preview.
	if code, _ := open(portBody(devPort, 1, "/"), http.Header{"Origin": {base}, "Content-Type": {"application/json"}}); code == 200 {
		t.Fatal("opened without a session")
	}
	code, o := open(portBody(devPort, 1, "/hello?x=1"), page)
	if code != 200 || o.Slot != 1 || !strings.HasPrefix(o.EnterURL, o.Origin+"/__tk/enter?t=") {
		t.Fatalf("open: %d %+v", code, o)
	}
	slot, _ := url.Parse(o.Origin)
	if slot.Scheme != "http" || slot.Hostname() != "127.0.0.1" || slot.Port() == strconv.Itoa(port) {
		t.Fatalf("the slot is not an origin of its own: %s", o.Origin)
	}
	// /agents/api answers the same.
	if a := do("POST", base+"/agents/api/preview/open", portBody(devPort, 2, "/"), page); a.code != 200 {
		t.Fatalf("/agents/api/preview/open: %d %s", a.code, a.body)
	}

	// The enter page spends the ticket: the slot's cookie, a clean origin, then the page asked for.
	enter := do("GET", o.EnterURL, "", nil)
	if enter.code != 302 || enter.header.Get("Location") != "/hello?x=1" || enter.header.Get("Clear-Site-Data") == "" {
		t.Fatalf("enter: %d %v", enter.code, enter.header)
	}
	var slotCookie string
	for _, c := range (&http.Response{Header: enter.header}).Cookies() {
		if strings.HasPrefix(c.Name, "hl_preview_") {
			slotCookie = c.Name + "=" + c.Value
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("slot cookie flags: %+v", c)
			}
		}
	}
	if slotCookie == "" {
		t.Fatalf("no slot cookie: %v", enter.header)
	}
	if again := do("GET", o.EnterURL, "", nil); again.code != 401 {
		t.Fatalf("a ticket entered twice: %d", again.code)
	}

	// The page itself, from the dev server through the proxy, framed only by the app.
	got := do("GET", o.Origin+"/hello?x=1", "", http.Header{"Cookie": {slotCookie + "; " + cookie}})
	if got.code != 200 || !strings.Contains(got.body, "hello from the dev server at /hello?x=1") {
		t.Fatalf("page: %d %s", got.code, got.body)
	}
	if !strings.Contains(got.body, `src="/__tk/bridge.js"`) || !strings.Contains(got.body, `data-parent="`+base+`"`) {
		t.Fatalf("no bridge in the page: %s", got.body)
	}
	if csp := strings.Join(got.header.Values("Content-Security-Policy"), "; "); !strings.Contains(csp, "frame-ancestors "+base+" 'self'") {
		t.Fatalf("frame-ancestors: %q", csp)
	}
	if cc := got.header.Get("Cache-Control"); !strings.HasPrefix(cc, "private") || strings.Contains(cc, "public") {
		t.Fatalf("cache-control: %q", cc)
	}
	if got.header.Get("X-Robots-Tag") == "" {
		t.Fatal("no X-Robots-Tag")
	}
	var devCookie string
	for _, c := range (&http.Response{Header: got.header}).Cookies() {
		if strings.HasSuffix(c.Name, "sid") {
			devCookie = c.Name + "=" + c.Value
		}
	}
	if devCookie == "" || devCookie == "sid=dev-session" {
		t.Fatalf("the dev server's cookie is not kept to its target: %v", got.header.Values("Set-Cookie"))
	}
	// The dev server gets its own cookie back and never the app's session nor the slot's.
	echo := do("GET", o.Origin+"/cookies", "", http.Header{"Cookie": {slotCookie + "; " + cookie + "; " + devCookie}})
	if echo.code != 200 || !strings.Contains(echo.body, "sid=dev-session") || strings.Contains(echo.body, "session-token") || strings.Contains(echo.body, "hl_preview_") {
		t.Fatalf("cookies at the dev server: %d %s", echo.code, echo.body)
	}
	if a := do("GET", o.Origin+"/__tk/bridge.js", "", http.Header{"Cookie": {slotCookie}}); a.code != 200 || !strings.Contains(a.body, "the page bridge") {
		t.Fatalf("bridge script: %d %s", a.code, a.body)
	}

	// No cookie, another origin, a way out of the slot, an unknown runtime path: refused.
	for _, c := range []struct {
		path   string
		header http.Header
		want   int
	}{
		{"/hello", nil, 401},
		{"/hello", http.Header{"Cookie": {slotCookie}, "Origin": {"http://evil.example"}}, 403},
		{"/__tk/whatever", http.Header{"Cookie": {slotCookie}}, 404},
		{"/a/%2e%2e/b", http.Header{"Cookie": {slotCookie}}, 400},
		{"/sw.js", http.Header{"Cookie": {slotCookie}, "Service-Worker": {"script"}}, 403},
	} {
		if a := do("GET", o.Origin+c.path, "", c.header); a.code != c.want {
			t.Fatalf("%s %v: %d, want %d", c.path, c.header, a.code, c.want)
		}
	}

	// A slot page cannot drive the app on its port: the app's listener refuses what comes from a slot.
	for _, h := range []http.Header{
		{"Cookie": {cookie}, "Origin": {o.Origin}},
		{"Cookie": {cookie}, "Referer": {o.Origin + "/hello"}},
		{"Cookie": {cookie}, "Origin": {o.Origin}, "Connection": {"Upgrade"}, "Upgrade": {"websocket"}, "Sec-WebSocket-Version": {"13"},
			"Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}},
	} {
		if a := do("GET", base+"/api/terminal", "", h); a.code != 403 {
			t.Fatalf("app from a slot %v: %d", h, a.code)
		}
	}
	if code, _ := open(portBody(devPort, 3, "/"), http.Header{"Cookie": {cookie}, "Origin": {o.Origin}, "Content-Type": {"application/json"}}); code != 403 {
		t.Fatalf("a slot opened a preview: %d", code)
	}

	// A dev server that is down: the preview opens, and the slot says the server does not answer.
	down := freePort(t)
	code, o3 := open(portBody(down, 3, "/"), page)
	if code != 200 {
		t.Fatalf("open a stopped port: %d %+v", code, o3)
	}
	enter3 := do("GET", o3.EnterURL, "", nil)
	var cookie3 string
	for _, c := range (&http.Response{Header: enter3.header}).Cookies() {
		cookie3 = c.Name + "=" + c.Value
	}
	if a := do("GET", o3.Origin+"/", "", http.Header{"Cookie": {cookie3}}); a.code != 502 {
		t.Fatalf("a stopped dev server: %d %s", a.code, a.body)
	}

	// What the pane may not ask for.
	for body, want := range map[string]string{
		`{"target":{"kind":"url","origin":"https://example.com"},"slot":1}`: "invalid slot",
		portBody(devPort, 7, "/"):                      "invalid slot",
		`{"target":{"kind":"port","port":0},"slot":1}`: "invalid target",
		portBody(devPort, 1, "//evil.example/"):        "invalid path",
		`{"target":{"kind":"port","port":` + strconv.Itoa(devPort) + `},"slot":1,"neighbours":[` + strconv.Itoa(devPort) + `]}`: "invalid neighbours",
	} {
		if code, o := open(body, page); code != 400 || o.Error != want {
			t.Fatalf("%s: %d %q, want %q", body, code, o.Error, want)
		}
	}
	// An external site goes to slots 7 and 8.
	if code, o := open(`{"target":{"kind":"url","origin":"https://example.com"},"slot":7,"path":"/"}`, page); code != 200 || o.Slot != 7 {
		t.Fatalf("open a site: %d %+v", code, o)
	}
}
