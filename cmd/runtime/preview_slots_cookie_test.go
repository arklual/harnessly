package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

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
