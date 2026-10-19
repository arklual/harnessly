package vault

import (
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/jsvalue"
)

const origin = "https://stand-1.preview.example.org"

type rig struct {
	mu        sync.Mutex
	published []*jsvalue.Object
	clock     int64
	b         *Bridge
}

func newRig(subscribers int) *rig {
	r := &rig{}
	r.b = NewBridge(func(e *jsvalue.Object) { r.mu.Lock(); r.published = append(r.published, e); r.mu.Unlock() }, func() int { return subscribers })
	r.b.Now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return time.UnixMilli(r.clock) }
	r.b.Timeout = 50 * time.Millisecond
	return r
}

func (r *rig) waitPublished(t *testing.T, n int) *jsvalue.Object {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.published) >= n {
			e := r.published[n-1]
			r.mu.Unlock()
			return e
		}
		r.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("not published")
	return nil
}

// Ported from the previous runtime's vault bridge tests.
func TestBridge(t *testing.T) {
	r := newRig(1)
	r.b.Timeout = 2 * time.Second
	done := make(chan Outcome)
	go func() { done <- r.b.Dispatch("tab-1", origin) }()
	e := r.waitPublished(t, 1)
	id := e.String("id")
	if string(jsvalue.Stringify(e, "")) != `{"type":"vault-request","id":"`+id+`","tabId":"tab-1","origin":"`+origin+`"}` || len(id) != 36 {
		t.Fatal(string(jsvalue.Stringify(e, "")))
	}
	if !r.b.Answer(id, Result{"logged-in", "Вошёл"}) || r.b.Answer(id, Result{"denied", "поздно"}) {
		t.Fatal("first answer only")
	}
	if o := <-done; o.Kind != "answered" || o.Result.Status != "logged-in" {
		t.Fatal(o)
	}
	if o := newRig(0).b.Dispatch("tab-1", origin); o.Kind != "no-ui" {
		t.Fatal(o)
	}
	if o := newRig(1).b.Dispatch("tab-1", origin); o.Kind != "timeout" {
		t.Fatal(o)
	}
}

func TestOnePerTabAndRate(t *testing.T) {
	r := newRig(1)
	r.b.Timeout = 2 * time.Second
	done := make(chan Outcome)
	go func() { done <- r.b.Dispatch("tab-1", origin) }()
	e := r.waitPublished(t, 1)
	if o := r.b.Dispatch("tab-1", origin); o.Kind != "busy" {
		t.Fatal(o)
	}
	r.b.Answer(e.String("id"), Result{"denied", "нет"})
	<-done
	for i := 2; i <= 6; i++ {
		r.mu.Lock()
		r.clock += 1000
		r.mu.Unlock()
		go func() { done <- r.b.Dispatch("tab-1", origin) }()
		e := r.waitPublished(t, i)
		r.b.Answer(e.String("id"), Result{"denied", "нет"})
		<-done
	}
	r.b.Timeout = 30 * time.Millisecond
	if o := r.b.Dispatch("tab-1", origin); o.Kind != "rate-limited" {
		t.Fatal(o)
	}
	if o := r.b.Dispatch("tab-2", origin); o.Kind != "timeout" {
		t.Fatal(o)
	}
	r.mu.Lock()
	r.clock += 10 * 60_000
	r.mu.Unlock()
	if o := r.b.Dispatch("tab-1", origin); o.Kind != "timeout" {
		t.Fatal(o)
	}
}

func TestOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://Example.ORG": "https://example.org", "https://example.org/": "https://example.org", "https://example.org:443": "https://example.org",
		"https://example.org:8443": "https://example.org:8443", "HTTPS://a.b": "https://a.b", "https://[::1]:9000": "https://[::1]:9000",
	} {
		if got := Origin(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	for _, in := range []any{"http://example.org", "https://example.org/path", "https://example.org?x", "https://u@example.org", "https://example.org.",
		"https://example.org:0", "https://exa mple.org", "https://example.org#x", "https://a\\b", "", float64(1), nil, "https://" + strings.Repeat("a", 260)} {
		if got := Origin(in); got != "" {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestParseResultAndReply(t *testing.T) {
	r := ParseResult(jsvalue.NewObject("status", "logged-in", "message", " Вошёл\n\tв  \u001b[0m "+strings.Repeat("x", 400)))
	if r == nil || r.Status != "logged-in" || !strings.HasPrefix(r.Message, "Вошёл в ␛[0m") || len([]rune(r.Message)) > 300 {
		t.Fatal(r)
	}
	for _, v := range []any{nil, jsvalue.NewObject("status", "hacked", "message", "x"), jsvalue.NewObject("status", "denied")} {
		if ParseResult(v) != nil {
			t.Errorf("%v", v)
		}
	}
	for kind, want := range map[string]int{"timeout": 200, "no-ui": 503, "busy": 409, "rate-limited": 429} {
		if status, _ := CLIReply(Outcome{Kind: kind}); status != want {
			t.Errorf("%s: %d", kind, status)
		}
	}
}
