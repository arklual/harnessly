package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseCompare(t *testing.T) {
	for _, s := range []string{"", "dev", "1.2", "1.2.x", "1.2.3-", "01.2.3", "-1.2.3", "1.2.3.4"} {
		if _, ok := Parse(s); ok {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"0.15.46", "v0.15.46", 0},
		{"0.15.47", "0.15.46", 1},
		{"0.16.0", "0.15.99", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0+build5", "1.0.0", 0},
	}
	for _, c := range cases {
		a, ok1 := Parse(c.a)
		b, ok2 := Parse(c.b)
		if !ok1 || !ok2 {
			t.Fatalf("parse %q %q", c.a, c.b)
		}
		if got := Compare(a, b); got != c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(b, a); got != -c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func serve(t *testing.T, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

const rel = `{"tag_name":"v0.16.0","html_url":"https://github.com/o/r/releases/tag/v0.16.0","published_at":"2026-10-01T10:00:00Z"}`

func TestCheckNewer(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, rel, &hits)
	c := &Checker{Current: "0.15.46", URL: s.URL}
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := c.Status()
	want := Status{Current: "0.15.46", Latest: "0.16.0", URL: "https://github.com/o/r/releases/tag/v0.16.0",
		PublishedAt: "2026-10-01T10:00:00Z", UpdateAvailable: true}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	// The second request carries the ETag and a 304 keeps the answer.
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Status() != want || hits.Load() != 2 {
		t.Fatalf("after 304: %+v, %d hits", c.Status(), hits.Load())
	}
}

func TestCheckNotNewer(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, rel, &hits)
	c := &Checker{Current: "0.16.0", URL: s.URL}
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Status().UpdateAvailable {
		t.Fatal("same version is not an update")
	}
}

func TestCheckErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"500":        func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"rate limit": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) },
		"garbage":    func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"bad tag":    func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"tag_name":"nightly"}`)) },
		"prerelease": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"v9.0.0","prerelease":true}`))
		},
	} {
		s := httptest.NewServer(h)
		c := &Checker{Current: "0.15.0", URL: s.URL}
		if err := c.Check(context.Background()); err == nil {
			t.Errorf("%s: no error", name)
		}
		if c.Status().UpdateAvailable {
			t.Errorf("%s: update reported", name)
		}
		s.Close()
	}
}

func TestDevNeverAsks(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, rel, &hits)
	for _, v := range []string{"dev", "", "abc"} {
		c := &Checker{Current: v, URL: s.URL}
		c.Run(context.Background()) // returns at once
		if Comparable(v) || c.Check(context.Background()) == nil {
			t.Errorf("%q compared", v)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests", hits.Load())
	}
}

func TestRunChecksAndStops(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, rel, &hits)
	c := &Checker{Current: "0.15.0", URL: s.URL, Interval: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
	if hits.Load() < 2 || !c.Status().UpdateAvailable {
		t.Fatalf("hits %d, %+v", hits.Load(), c.Status())
	}
}
