package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestScrub(t *testing.T) {
	cases := map[string]string{
		"fetch https://hub.example.com/api/x?token=abc failed": "fetch <url> failed",
		"open /home/ann/project/main.go: no such file":         "open ~/project/main.go: no such file",
		`C:\Users\ann\app\x.ts: bad`:                           `~\app\x.ts: bad`,
		"user ann@example.org denied":                          "user <email> denied",
		"dial 10.0.0.12:8080: refused":                         "dial <ip>: refused",
		"chat 3f2a9c1e-1b2c-4d5e-8f90-a1b2c3d4e5f6 gone":       "chat <id> gone",
		// Split, so secret scanners do not take the fixture for a key.
		"key sk_" + "live_abcdefghijklmnopqrstuvwxyz0123 rejected": "key <id> rejected",
		"took 15342 ms   at\n line 7":                              "took <n> ms at line 7",
	}
	for in, want := range cases {
		if got := Scrub(in); got != want {
			t.Errorf("Scrub(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Scrub(strings.Repeat("ab ", 400)); len([]rune(got)) != MessageMax+1 {
		t.Errorf("not cut: %d runes", len([]rune(got)))
	}
}

// The same error goes once an hour; past PerHour distinct errors nothing more goes until the hour turns; off sends none.
func TestReporterBounds(t *testing.T) {
	on := true
	q := New(Config{URL: "http://127.0.0.1:1/batch/", Delay: time.Hour, Allow: func() bool { return on }})
	now := time.Unix(1_800_000_000, 0)
	r := &Reporter{Queue: q, PerHour: 2, Now: func() time.Time { return now }}
	if !r.Error("web", "exception", "boom at /home/ann/x.js", nil) {
		t.Fatal("first error not queued")
	}
	if r.Error("web", "exception", "boom at /home/bob/x.js", nil) {
		t.Fatal("the same error (after scrubbing) queued twice in an hour")
	}
	if !r.Error("runtime", "http", "upstream 502", jsvalue.NewObject("status", 502)) {
		t.Fatal("second distinct error not queued")
	}
	if r.Error("runtime", "http", "upstream 503", nil) {
		t.Fatal("past PerHour")
	}
	now = now.Add(time.Hour)
	if !r.Error("web", "exception", "boom at /home/ann/x.js", nil) {
		t.Fatal("the hour turned, the error should go again")
	}
	on = false
	if r.Error("web", "exception", "another", nil) {
		t.Fatal("queued while turned off")
	}
	q.mu.Lock()
	n := len(q.queue)
	first := q.queue[0]
	q.mu.Unlock()
	if n != 3 {
		t.Fatalf("queued %d events, want 3", n)
	}
	body := string(Body("k", "id", nil, []Event{first}))
	for _, want := range []string{`"event":"$exception"`, `"type":"web.exception"`, `"value":"boom at ~/x.js"`, `"$exception_fingerprint"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
}
