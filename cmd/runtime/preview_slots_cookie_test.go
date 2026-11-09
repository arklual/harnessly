package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

func TestLoopbackOrigin(t *testing.T) {
	for origin, want := range map[string]bool{
		"http://127.0.0.1:18734": true,
		"http://localhost:3000":  true,
		"http://[::1]:8080":      true,
		"http://127.0.0.2:80":    true,
		"https://127.0.0.1:443":  false,
		"http://10.0.0.1:80":     false,
		"http://example.com":     false,
		"not a url":              false,
	} {
		if got := loopbackOrigin(origin); got != want {
			t.Errorf("%s: %v", origin, got)
		}
	}
}

// signedAt is a slot cookie signed as at iat: the newest of several a browser holds is the session.
func signedAt(secret []byte, port int, iat int64) string {
	claims := map[string]any{"sub": "user", "slot": 1, "origin": "http://127.0.0.1:18734", "app": "http://127.0.0.1:18733",
		"target": map[string]any{"kind": "port", "port": port}, "aud": previewSlotAudience, "iat": iat, "exp": iat + 3600}
	raw, _ := json.Marshal(claims)
	b64 := base64.RawURLEncoding
	signing := b64.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + b64.EncodeToString(raw)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + b64.EncodeToString(mac.Sum(nil))
}

func TestSlotSessionNewestCookie(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	s := &previewSlots{ports: []int{18734}}
	now := time.Now().Unix()
	old, fresh := signedAt(secret, 41353, now-60), signedAt(secret, 42213, now)
	for _, order := range [][2]string{{old, fresh}, {fresh, old}} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:18734/x", nil)
		// A plain cookie left from before and a secure one: whichever comes first, the newer one wins.
		r.Header.Set("Cookie", s.slotCookie(1, false)+"="+order[0]+"; "+s.slotCookie(1, true)+"="+order[1])
		sess := s.session(r, 1, secret)
		if sess == nil || sess.target.Port != 42213 {
			t.Fatalf("the newest cookie is the session: %+v", sess)
		}
	}
}

func TestShowsAppViews(t *testing.T) {
	t.Cleanup(func() { appViewPort.Store(0) })
	sess := &previewSession{target: devtools.Target{Kind: "port", Port: 42213}}
	if sess.showsAppViews() {
		t.Fatal("no view server yet: a slot shows no app views")
	}
	appViewPort.Store(42213)
	if !sess.showsAppViews() {
		t.Fatal("the slot on the view server's port shows app views")
	}
	if (&previewSession{target: devtools.Target{Kind: "port", Port: 3000}}).showsAppViews() {
		t.Fatal("a dev server is not the view server")
	}
}

func TestAppViewAssetGoesWithoutTheSlotCookie(t *testing.T) {
	sessions := apps.NewViewSessions()
	live, err := sessions.Issue("acme.check", "main", "app", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { appViewPort.Store(0); appViewSessions.Store(nil) })
	appViewPort.Store(42213)
	appViewSessions.Store(sessions)
	req := func(method, path, dest string) *http.Request {
		r := httptest.NewRequest(method, "https://slot.example"+path, nil)
		if dest != "" {
			r.Header.Set("Sec-Fetch-Dest", dest)
		}
		return r
	}
	for _, c := range []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"the SDK", req("GET", apps.SDKPath, "script"), true},
		{"the bridge shim", req("GET", apps.ShimPath, "script"), true},
		{"a file of a live view", req("GET", "/"+live.Token+"/acme.check/ui/app.js", "script"), true},
		{"a file of no view", req("GET", "/not-a-token/acme.check/ui/app.js", "script"), false},
		{"the view's page itself", req("GET", "/"+live.Token+"/acme.check/ui/index.html", "iframe"), false},
		{"the view's page, posing as a script", req("GET", "/"+live.Token+"/acme.check/ui/index.html", "script"), false},
		{"a path with no extension", req("GET", "/"+live.Token+"/acme.check/ui", "script"), false},
		{"a style of a live view", req("GET", "/"+live.Token+"/acme.check/ui/app.CSS", "style"), true},
		{"a navigation", req("GET", apps.SDKPath, "document"), false},
		{"no fetch metadata", req("GET", apps.SDKPath, ""), false},
		{"a write", req("POST", "/"+live.Token+"/acme.check/ui/app.js", "script"), false},
		{"a dev server's file", req("GET", "/src/main.ts", "script"), false},
	} {
		if got := appViewAsset(c.r); got != c.want {
			t.Errorf("%s: appViewAsset = %v, want %v", c.name, got, c.want)
		}
	}
	appViewPort.Store(0)
	if appViewAsset(req("GET", apps.SDKPath, "script")) {
		t.Error("no view server yet: nothing goes without the cookie")
	}
	appViewPort.Store(42213)
	if sess := appViewSession("https://slot.example"); !sess.showsAppViews() || sess.origin != "https://slot.example" {
		t.Errorf("the session goes to the view server under the slot's origin: %+v", sess)
	}
}

func TestAppViewEnterKeepsTheSlot(t *testing.T) {
	t.Cleanup(func() { appViewPort.Store(0) })
	secret := []byte("0123456789abcdef0123456789abcdef")
	s := &previewSlots{ports: []int{18734}, spent: map[string]int64{}}
	enter := func(port int, jti string) http.Header {
		t.Helper()
		ticket := agent.SignPayload(secret, previewTicketAudience, map[string]any{"sub": "user", "slot": 1,
			"origin": "http://127.0.0.1:18734", "app": "http://127.0.0.1:18733", "jti": jti, "path": "/",
			"target": map[string]any{"kind": "port", "port": port}}, time.Minute)
		w := httptest.NewRecorder()
		s.enter(w, httptest.NewRequest("GET", "http://127.0.0.1:18734/__tk/enter?t="+ticket, nil), 1, secret, "")
		if w.Code != http.StatusFound {
			t.Fatalf("enter %d: %d %s", port, w.Code, w.Body)
		}
		return w.Header()
	}
	appViewPort.Store(42213)
	// A dev server's first open wipes what the slot showed before; an app's view has nothing there to wipe.
	if enter(3000, "a").Get("Clear-Site-Data") == "" {
		t.Fatal("a dev server's enter without the wipe")
	}
	if h := enter(42213, "b"); h.Get("Clear-Site-Data") != "" {
		t.Fatalf("an app view's enter wipes the slot: %v", h)
	}
}

func TestSlotOriginOKLetsAViewAssetSayNull(t *testing.T) {
	sess := &previewSession{app: "https://app.example", target: devtools.Target{Kind: "port", Port: 3000}}
	for _, c := range []struct {
		name, from string
		asset, ok  bool
	}{
		{"no Origin", "", false, true},
		{"the slot itself", "https://slot.example", false, true},
		{"the app", "https://app.example", false, true},
		{"opaque origin, a view's file", "null", true, true},
		{"opaque origin, anything else", "null", false, false},
		{"another site, a view's file", "https://evil.example", true, false},
	} {
		if got := slotOriginOK(c.from, "https://slot.example", sess, c.asset); got != c.ok {
			t.Errorf("%s: %v, want %v", c.name, got, c.ok)
		}
	}
}
