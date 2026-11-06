//go:build linux

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// The preview by path, as a company with one host runs it: the runtime has no slot ports, the pane opens a dev server
// under /agents/__pv/<n>-<key>/ of the runtime's own origin, the page comes sandboxed with its root-absolute addresses
// under the prefix, its cookies scoped there, and nothing it sends reaches the runtime's API.
func TestPreviewByPathEndToEnd(t *testing.T) {
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

	// A dev server as Vite and Angular serve one: root-absolute modules, a base href, a style with url(), an API that
	// signs in with a cookie and reads it back.
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/main.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, `import { a } from "/src/dep.js"; const lazy = () => import("/src/lazy.js"); fetch("/api/me");`)
		case "/app.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(w, `body{background:url(/img/bg.png)}`)
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "dev-session", Path: "/api", HttpOnly: true})
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
			w.Header().Set("Clear-Site-Data", `"storage"`)
			_, _ = io.WriteString(w, "ok")
		case "/api/me":
			_, _ = io.WriteString(w, "cookie="+r.Header.Get("Cookie"))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<!doctype html><html><head><base href="/"><link rel="stylesheet" href="/app.css">`+
				`<script type="module" src="/main.js"></script></head><body>dev page at `+r.URL.RequestURI()+`</body></html>`)
		}
	}))
	defer dev.Close()
	devPort := portOf(strings.TrimPrefix(dev.URL, "http://"))

	socket := "hs-test-pvp-" + strconv.Itoa(os.Getpid())
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	env := []string{
		"HOME=" + home, "USER=agent", "LOGNAME=agent", "SHELL=/bin/bash", "LANG=C.UTF-8", "TMUX_TMPDIR=" + tmuxDir,
		"PATH=/usr/local/bin:/usr/bin:/bin", "RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "RUNTIME_GATE_UPSTREAM=",
		"AGENT_DATA_DIR=" + data, "RUNTIME_TMUX_SOCKET=" + socket, "RUNTIME_SESSION_PREFIX=hs-", "RUNTIME_GATE_COUNT_SECONDS=3600",
		"RUNTIME_PREVIEW_MODE=path", "RUNTIME_PREVIEW_BRIDGE_FILE=" + bridge,
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
	session := "session-token=" + agent.SignHS256([]byte(config.AuthSecret), "user", "", time.Hour)
	page := http.Header{"Cookie": {session}, "Origin": {base}, "Content-Type": {"application/json"}}

	body := `{"target":{"kind":"port","port":` + strconv.Itoa(devPort) + `},"slot":1,"path":"/users/7?x=1"}`
	opened := do("POST", base+"/api/preview/open", body, page)
	var o struct {
		Origin   string `json:"origin"`
		EnterURL string `json:"enterUrl"`
		ByPath   bool   `json:"byPath"`
	}
	_ = json.Unmarshal([]byte(opened.body), &o)
	prefix := strings.TrimPrefix(o.Origin, base)
	if opened.code != 200 || !o.ByPath || !strings.HasPrefix(prefix, "/agents/__pv/1-") || o.EnterURL != o.Origin+"/users/7?x=1" {
		t.Fatalf("open: %d %s", opened.code, opened.body)
	}
	if a := do("POST", base+"/api/preview/open", `{"target":{"kind":"url","origin":"https://example.com"},"slot":7}`, page); a.code != 400 {
		t.Fatalf("a site of the outside world by path: %d %s", a.code, a.body)
	}

	// The page: sandboxed, its addresses under the prefix, the bridge first and told the prefix.
	opaque := http.Header{"Origin": {"null"}, "Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"cors"}}
	doc := do("GET", o.EnterURL, "", http.Header{"Sec-Fetch-Dest": {"iframe"}, "Sec-Fetch-Mode": {"navigate"}})
	csp := strings.Join(doc.header.Values("Content-Security-Policy"), "; ")
	if doc.code != 200 || !strings.Contains(doc.body, "dev page at /users/7?x=1") || !strings.Contains(csp, "sandbox allow-scripts") ||
		strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("page: %d %q %s", doc.code, csp, doc.body)
	}
	for _, want := range []string{`<base href="` + prefix + `/">`, `href="` + prefix + `/app.css"`, `src="` + prefix + `/main.js"`,
		`src="` + prefix + `/__tk/bridge.js"`, `data-prefix="` + prefix + `"`} {
		if !strings.Contains(doc.body, want) {
			t.Errorf("page lacks %s: %s", want, doc.body)
		}
	}
	if doc.header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("referrer: %q", doc.header.Get("Referrer-Policy"))
	}
	js := do("GET", o.Origin+"/main.js", "", opaque)
	if js.code != 200 || !strings.Contains(js.body, `from "`+prefix+`/src/dep.js"`) || !strings.Contains(js.body, `import("`+prefix+`/src/lazy.js")`) ||
		js.header.Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("module: %d %v %s", js.code, js.header, js.body)
	}
	if css := do("GET", o.Origin+"/app.css", "", opaque); !strings.Contains(css.body, "url("+prefix+"/img/bg.png)") {
		t.Fatalf("style: %s", css.body)
	}

	// The app's sign-in cookie stays under the slot, and comes back with its own requests; the target's word about
	// the whole origin (Clear-Site-Data, its CORS) does not pass.
	login := do("POST", o.Origin+"/api/login", "", opaque)
	var sid string
	for _, c := range (&http.Response{Header: login.header}).Cookies() {
		if strings.HasSuffix(c.Name, "_sid") {
			sid = c.Name + "=" + c.Value
			if c.Path != prefix+"/api" || !c.Secure || c.SameSite != http.SameSiteNoneMode {
				t.Fatalf("app cookie: %+v", c)
			}
		}
	}
	if sid == "" || login.header.Get("Clear-Site-Data") != "" || login.header.Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("login: %v", login.header)
	}
	if me := do("GET", o.Origin+"/api/me", "", http.Header{"Origin": {"null"}, "Cookie": {sid + "; " + session}}); me.body != "cookie=sid=dev-session" {
		t.Fatalf("the app's cookie, and only it, reaches the dev server: %q", me.body)
	}

	// Preflight answered by the slot, as one origin's requests on localhost never had one.
	pre := do("OPTIONS", o.Origin+"/api/me", "", http.Header{"Origin": {"null"}, "Access-Control-Request-Method": {"PUT"}})
	if pre.code != 204 || pre.header.Get("Access-Control-Allow-Methods") != "PUT" {
		t.Fatalf("preflight: %d %v", pre.code, pre.header)
	}

	// The opaque page cannot drive the runtime, even with the person's session cookie along.
	withSession := http.Header{"Origin": {"null"}, "Cookie": {session}, "Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"cors"}}
	for _, path := range []string{"/agents/api/state", "/api/state", "/agents/api/preview/open"} {
		if a := do("GET", base+path, "", withSession); a.code != 403 {
			t.Errorf("%s from the preview: %d", path, a.code)
		}
	}

	// Without the key nothing opens: another key, the bare slot.
	wrong := base + "/agents/__pv/1-" + strings.Repeat("0", 32) + "/users/7"
	if a := do("GET", wrong, "", nil); !strings.Contains(a.body, "hl-preview-reload") || strings.Contains(a.body, "dev page") {
		t.Fatalf("a wrong key: %d %s", a.code, a.body)
	}
	if a := do("GET", base+"/agents/__pv/1/users/7", "", nil); a.code != 404 {
		t.Fatalf("no key: %d", a.code)
	}

	// The page reloaded past its prefix: the pane is asked to open the path again.
	back := do("GET", base+"/users/7?x=1", "", http.Header{"Sec-Fetch-Dest": {"iframe"}, "Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}})
	if !strings.Contains(back.body, `"hl-preview-reload"`) || !strings.Contains(back.body, `"/users/7?x=1"`) {
		t.Fatalf("reload: %d %s", back.code, back.body)
	}

	// localStorage of the page lives with the runtime, per person and target.
	if a := do("POST", o.Origin+"/__tk/storage", `{"theme":"dark"}`, opaque); a.code != 204 {
		t.Fatalf("storage write: %d %s", a.code, a.body)
	}
	reopened := do("POST", base+"/api/preview/open", body, page)
	var o2 struct {
		Origin string `json:"origin"`
	}
	_ = json.Unmarshal([]byte(reopened.body), &o2)
	if o2.Origin == o.Origin {
		t.Fatal("every open has a key of its own")
	}
	if a := do("GET", o2.Origin+"/__tk/storage", "", opaque); a.body != `{"theme":"dark"}` {
		t.Fatalf("storage after a new open: %s", a.body)
	}
}
