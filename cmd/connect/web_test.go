package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowPairs(t *testing.T) {
	t.Setenv("HARNESSLY_CONNECT_CONFIG", filepath.Join(t.TempDir(), "device.json"))
	rt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Code, Name string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/connect/v1/register" || req.Code != "hx_good" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"код неверный или истёк: возьмите новый"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"d-1","name":"`+req.Name+`"}`)
	}))
	defer rt.Close()
	a := &app{log: testLogger(t)}
	auto := false
	w, err := startWindow(a)
	if err != nil {
		t.Fatal(err)
	}
	w.autostart = func(on bool) error { auto = on; return nil }
	w.isOn = func() bool { return auto }
	post := func(v url.Values) string {
		resp, err := http.PostForm(w.url, v)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if body := post(url.Values{"do": {"pair"}, "url": {rt.URL}, "code": {"hx_bad"}, "name": {"Mac"}}); !strings.Contains(body, "код неверный") {
		t.Fatalf("bad code page: %s", body)
	}
	body := post(url.Values{"do": {"pair"}, "url": {rt.URL}, "code": {"hx_good"}, "name": {"Mac"}})
	if !strings.Contains(body, "Mac →") || !auto {
		t.Fatalf("after pairing: auto=%v %s", auto, body)
	}
	if _, err := os.Stat(os.Getenv("HARNESSLY_CONNECT_CONFIG")); err != nil {
		t.Fatal("config not written")
	}
	a.stop()
	// Another site's page cannot press the buttons.
	req, _ := http.NewRequest(http.MethodPost, w.url, strings.NewReader("do=forget"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site post: %s", resp.Status)
	}
}

func testLogger(t *testing.T) *log.Logger { return log.New(io.Discard, "", 0) }
