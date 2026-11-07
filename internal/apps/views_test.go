package apps

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// installedPaneApp installs a pane-chat app into a fresh registry and puts files beside its view.
func installedPaneApp(t *testing.T, network *Network) (*Registry, string) {
	t.Helper()
	dir := scaffold(t, "acme.demo", "pane-chat")
	for rel, body := range map[string]string{"ui/app.js": "export {}", "secret.txt": "no", "instructions.md": "agent only"} {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if network != nil {
		raw, _ := os.ReadFile(filepath.Join(dir, ManifestPath))
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		m.Network = network
		raw, _ = json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	if _, _, err := reg.Install(dir, InstallOptions{HostVersion: FirstVersion}); err != nil {
		t.Fatal(err)
	}
	return reg, dir
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8000"
	h.ServeHTTP(rec, req)
	return rec
}

// tokened is a view server with a session of the demo app and the address prefix of its files.
func tokened(t *testing.T, reg *Registry) (*ViewServer, string) {
	t.Helper()
	s := NewViewServer(reg)
	sess, err := s.Sessions.Issue("acme.demo", "main", "pane", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, "/" + sess.Token
}

func TestViewServerServesOnlyViewFiles(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s, tok := tokened(t, reg)
	h := s.Handler()

	for path, want := range map[string]int{
		tok + "/acme.demo/ui/index.html":        200,
		tok + "/acme.demo/ui/app.js":            200,
		tok + "/acme.demo/secret.txt":           404,
		tok + "/acme.demo/instructions.md":      404,
		tok + "/acme.demo/harnessly/app.json":   404,
		tok + "/acme.demo/ui/../secret.txt":     404,
		tok + "/acme.demo/ui/%2e%2e/secret.txt": 404,
		tok + "/acme.demo/ui/missing.html":      404,
		tok + "/acme.other/ui/index.html":       404,
		tok + "/acme.demo":                      404,
		"/acme.demo/ui/index.html":              404,
		"/nosuchtoken/acme.demo/ui/index.html":  404,
		"/":                                     404,
		SDKPath:                                 200,
		SDKModulePath:                           200,
		"/__hl/app-sdk.d.ts":                    200,
		ShimPath:                                200,
	} {
		if got := get(t, h, path).Code; got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}

	rec := get(t, h, tok+"/acme.demo/ui/index.html")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type %q", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("want nosniff")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("csp %q", csp)
	}
	// The classic script has no exports and publishes window.harnessly; the module keeps its exports.
	classic, _ := io.ReadAll(get(t, h, SDKPath).Body)
	if strings.Contains(string(classic), "export ") || !strings.Contains(string(classic), "globalThis.harnessly") {
		t.Error("the classic sdk is not a classic script")
	}
	if body, _ := io.ReadAll(get(t, h, SDKModulePath).Body); !strings.Contains(string(body), "export async function connect") {
		t.Error("the module sdk is not served")
	}

	post := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodPost, tok+"/acme.demo/ui/index.html", nil)
	preq.Host = "127.0.0.1:8000"
	h.ServeHTTP(post, preq)
	if post.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", post.Code)
	}
}

func TestViewServerRefusesLinkOut(t *testing.T) {
	reg, dir := installedPaneApp(t, nil)
	// A dev install serves the author's folder: a link there must not lead the view out of it.
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("beyond"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, _ := reg.Get("acme.demo")
	if err := os.Symlink(outside, filepath.Join(app.Dir, "ui", "leak.txt")); err != nil {
		t.Fatal(err)
	}
	_ = dir
	s, tok := tokened(t, reg)
	if got := get(t, s.Handler(), tok+"/acme.demo/ui/leak.txt").Code; got != 404 {
		t.Errorf("a link out of the package: %d", got)
	}
}

func TestViewPagesCarryTheSessionNonce(t *testing.T) {
	reg, dir := installedPaneApp(t, nil)
	s := NewViewServer(reg)
	one, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	two, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	if one.Nonce == "" || one.Nonce == two.Nonce {
		t.Fatalf("nonces %q %q", one.Nonce, two.Nonce)
	}
	page := func(sess Session) string {
		rec := get(t, s.Handler(), "/"+sess.Token+"/acme.demo/ui/index.html")
		if rec.Code != 200 || rec.Header().Get("Content-Length") != strconv.Itoa(rec.Body.Len()) {
			t.Fatalf("page: %d length %s of %d", rec.Code, rec.Header().Get("Content-Length"), rec.Body.Len())
		}
		return rec.Body.String()
	}
	// The nonce's meta is followed at once by the shim's script tag, before any other script of the page; the shim and
	// the SDK go by a path relative to the page (ui/index.html), under the session's own path.
	meta := func(n string) string {
		return `<meta name="` + NonceMeta + `" content="` + n + `"><script src="../../__hl/bridge-shim.js"></script>`
	}
	if body := page(one); !strings.Contains(body, `<script src="../../__hl/app-sdk.js">`) || strings.Contains(body, `"/__hl/`) {
		t.Errorf("the SDK is not relative to the page: %s", body)
	}
	for _, name := range []string{"app-sdk.js", "bridge-shim.js", "app-sdk.mjs", "hl-ui.css", "lib/echarts.js"} {
		if rec := get(t, s.Handler(), "/"+one.Token+"/__hl/"+name); rec.Code != 200 {
			t.Errorf("the SDK's %s under the session: %d", name, rec.Code)
		}
	}
	if rec := get(t, s.Handler(), "/not-a-session/__hl/app-sdk.js"); rec.Code != 404 {
		t.Errorf("the SDK under a dead session: %d", rec.Code)
	}
	if body := page(one); !strings.Contains(body, meta(one.Nonce)) || strings.Contains(body, two.Nonce) {
		t.Errorf("the page does not carry its own session's nonce: %s", body)
	}
	if body := page(two); !strings.Contains(body, meta(two.Nonce)) {
		t.Errorf("the other session's page: %s", body)
	}
	// Scripts and other files are served as they are.
	if rec := get(t, s.Handler(), "/"+one.Token+"/acme.demo/ui/app.js"); strings.Contains(rec.Body.String(), "hl-nonce") {
		t.Error("a script got the nonce")
	}
	meta = func(n string) string {
		return `<meta name="` + NonceMeta + `" content="` + n + `"><script src="` + ShimPath + `"></script>`
	}
	for in, want := range map[string]string{
		"<!doctype html><html><head><title>x</title></head></html>": "<head>" + meta("N") + "<title>",
		"<HEAD lang=x><p>":            "<HEAD lang=x>" + meta("N") + "<p>",
		"<!doctype html><html><body>": "<html><head>" + meta("N") + "</head><body>",
		"<!doctype html><p>":          "<!doctype html>" + meta("N") + "<p>",
		"<p>":                         meta("N") + "<p>",
	} {
		if got := string(injectNonce([]byte(in), "N")); !strings.Contains(got, want) {
			t.Errorf("%q gave %q, want %q in it", in, got, want)
		}
	}
	_ = dir
}

func TestViewSessionRenew(t *testing.T) {
	s := NewViewSessions()
	now := time.Now()
	s.Now = func() time.Time { return now }
	sess, _ := s.Issue("acme.demo", "main", "pane", "chat-1")
	now = now.Add(SessionIdle - time.Minute)
	left, ok := s.Renew(sess.Token)
	if !ok || left != SessionIdle {
		t.Fatalf("a live session: %v %v", left, ok)
	}
	got, _ := s.Get(sess.Token)
	if got.App != "acme.demo" || got.View != "main" || got.Target != "pane" || got.Conversation != "chat-1" || got.Nonce != sess.Nonce {
		t.Errorf("renewing changed the session: %+v", got)
	}
	// Renewed again and again, it still ends at its longest time.
	for i := 0; i < 11; i++ {
		now = now.Add(SessionIdle - time.Minute)
		if _, ok := s.Renew(sess.Token); !ok {
			t.Fatalf("renew %d refused", i)
		}
	}
	if left, ok := s.Renew(sess.Token); ok && left > SessionMax {
		t.Errorf("left %v", left)
	}
	now = now.Add(SessionMax)
	if _, ok := s.Renew(sess.Token); ok {
		t.Error("a session past its longest time was renewed")
	}
	idle, _ := s.Issue("acme.demo", "main", "pane", "")
	now = now.Add(SessionIdle + time.Second)
	if _, ok := s.Renew(idle.Token); ok {
		t.Error("an expired session was renewed")
	}
	if _, ok := s.Renew("nosuch"); ok {
		t.Error("an unknown token was renewed")
	}
}

func TestViewCSP(t *testing.T) {
	m := &Manifest{Network: &Network{
		Connect:   []string{"https://api.acme.test", "bad source; script-src *"},
		Resources: []string{"https://cdn.acme.test"},
		Frames:    []string{"https://docs.acme.test"},
	}}
	csp := m.ViewCSP()
	for _, want := range []string{
		"sandbox allow-scripts allow-forms allow-popups", "default-src 'none'", "script-src 'self'", "connect-src https://api.acme.test;",
		"img-src 'self' data: blob: https://cdn.acme.test", "frame-src https://docs.acme.test", "base-uri 'none'", "form-action 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("want %q in %q", want, csp)
		}
	}
	if strings.Contains(csp, "script-src *") || strings.Contains(csp, "bad source") {
		t.Errorf("a source with a directive got in: %q", csp)
	}
	if def := (&Manifest{}).ViewCSP(); !strings.Contains(def, "connect-src 'none'") || !strings.Contains(def, "frame-src 'none'") {
		t.Errorf("default %q", def)
	}
}

func TestViewPath(t *testing.T) {
	m := &Manifest{ID: "acme.demo", Views: []View{
		{ID: "main", Entry: "ui/index.html"}, {ID: "ext", Entry: "https://app.acme.test/"},
	}}
	if p, err := m.ViewPath("main"); err != nil || p != "/acme.demo/ui/index.html" {
		t.Errorf("%q %v", p, err)
	}
	for _, id := range []string{"ext", "nope"} {
		if _, err := m.ViewPath(id); !errors.Is(err, ErrViewNotFound) {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestViewServerListens(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s := NewViewServer(reg)
	defer s.Close()
	port, err := s.EnsureServer()
	if err != nil || port == 0 {
		t.Fatalf("%d %v", port, err)
	}
	if again, _ := s.EnsureServer(); again != port {
		t.Errorf("a second start moved the port: %d, %d", port, again)
	}
	sess, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/" + sess.Token + "/acme.demo/ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestPaneRef(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	for in, want := range map[string]string{
		"acme.demo":       "acme.demo/main",
		"acme.demo/main":  "acme.demo/main",
		"acme.demo/other": "",
		"acme.nobody":     "",
		"acme.demo/../x":  "",
		"":                "",
	} {
		if got := reg.PaneRef(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// A view's origin is opaque whatever the frame around it says: the CSP sandbox has no allow-same-origin.
func TestViewsHaveAnOpaqueOrigin(t *testing.T) {
	for _, m := range []*Manifest{{}, {Network: &Network{Connect: []string{"https://a.test"}}}} {
		csp := m.ViewCSP()
		if !strings.HasPrefix(csp, "sandbox allow-scripts allow-forms allow-popups;") || strings.Contains(csp, "allow-same-origin") {
			t.Errorf("csp %q", csp)
		}
	}
}

func TestViewServerChecksTheHost(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s, tok := tokened(t, reg)
	defer s.Close()
	port, err := s.EnsureServer()
	if err != nil {
		t.Fatal(err)
	}
	do := func(host string) int {
		req := httptest.NewRequest(http.MethodGet, tok+"/acme.demo/ui/index.html", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	p := strconv.Itoa(port)
	for host, want := range map[string]int{
		"127.0.0.1:" + p: 200, "localhost:" + p: 200, "[::1]:" + p: 200,
		"evil.example:" + p: 403, "127.0.0.1:1": 403, "127.0.0.1": 403, "10.0.0.5:" + p: 403, "localhost.evil.example:" + p: 403,
	} {
		if got := do(host); got != want {
			t.Errorf("Host %s: %d, want %d", host, got, want)
		}
	}
}

// One app's token opens that app's files only, and a token that ran out opens nothing.
func TestViewSessions(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s := NewViewServer(reg)
	other, _ := s.Sessions.Issue("acme.other", "main", "pane", "")
	if got := get(t, s.Handler(), "/"+other.Token+"/acme.demo/ui/index.html").Code; got != 404 {
		t.Errorf("another app's token: %d", got)
	}
	now := time.Now()
	s.Sessions.Now = func() time.Time { return now }
	sess, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	path := "/" + sess.Token + "/acme.demo/ui/index.html"
	now = now.Add(SessionIdle - time.Minute)
	if got := get(t, s.Handler(), path).Code; got != 200 {
		t.Errorf("a use inside the idle time: %d", got)
	}
	now = now.Add(SessionIdle - time.Minute) // renewed by the use
	if got := get(t, s.Handler(), path).Code; got != 200 {
		t.Errorf("a renewed session: %d", got)
	}
	now = now.Add(SessionIdle + time.Minute)
	if got := get(t, s.Handler(), path).Code; got != 404 {
		t.Errorf("an idle session: %d", got)
	}
	long, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	for i := 0; i < 14; i++ {
		now = now.Add(SessionIdle - time.Minute)
		s.Sessions.Get(long.Token)
	}
	if _, ok := s.Sessions.Get(long.Token); ok {
		t.Error("a session lives past its longest time")
	}
	a, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	b, _ := s.Sessions.Issue("acme.demo", "main", "pane", "")
	if a.Token == b.Token || len(a.Token) < 30 {
		t.Errorf("tokens %q %q", a.Token, b.Token)
	}
}

func TestFrameURL(t *testing.T) {
	m := &Manifest{Network: &Network{Frames: []string{"app.acme.test", "*.cdn.acme.test"}}}
	for entry, ok := range map[string]bool{
		"https://app.acme.test/view":      true,
		"https://x.cdn.acme.test/":        true,
		"https://evil.test/":              false,
		"https://app.acme.test@evil.test": false,
		"https://user:pw@app.acme.test/":  false,
		"http://app.acme.test/":           false,
		"javascript:alert(1)":             false,
		"data:text/html,<script>":         false,
		"https:///x":                      false,
	} {
		if _, got := m.FrameURL(entry); got != ok {
			t.Errorf("%s: %v, want %v", entry, got, ok)
		}
	}
	if _, ok := (&Manifest{}).FrameURL("https://app.acme.test/"); ok {
		t.Error("no network.frames: nothing may be framed")
	}
}

// ab.c-d and ab-c.d are one plugin to the agent (mcp__plugin_ab-c-d_…): the second is refused.
func TestInstallRefusesAPluginNameTwice(t *testing.T) {
	reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	for _, c := range []struct {
		id string
		ok bool
	}{{"ab.c-d", true}, {"ab-c.d", false}, {"ab.c-d", true}, {"acme.other", true}} {
		dir := scaffold(t, c.id, "chat")
		_, _, err := reg.Install(dir, InstallOptions{HostVersion: FirstVersion})
		if (err == nil) != c.ok || (!c.ok && !errors.Is(err, ErrPluginName)) {
			t.Errorf("%s: %v", c.id, err)
		}
	}
}
