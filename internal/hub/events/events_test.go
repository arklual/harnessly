package events

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublishReachesTheRightStreams(t *testing.T) {
	b := NewBroker()
	owner := b.Subscribe("s1", "u1", true)
	member := b.Subscribe("s1", "u2", false)
	elsewhere := b.Subscribe("s2", "u1", true)

	b.PublishJSON("s1", "machine", map[string]string{"id": "m1"}, false)
	b.PublishJSON("s1", "job", map[string]string{"id": "j1"}, true)

	read := func(s *Sub) []string {
		var out []string
		for {
			select {
			case e := <-s.ch:
				out = append(out, e.Name+" "+string(e.Data))
			default:
				return out
			}
		}
	}
	if got := read(owner); len(got) != 2 || got[0] != `machine {"id":"m1"}` || got[1] != `job {"id":"j1"}` {
		t.Errorf("owner: %q", got)
	}
	if got := read(member); len(got) != 1 || !strings.HasPrefix(got[0], "machine") {
		t.Errorf("member must not see owner events: %q", got)
	}
	if got := read(elsewhere); len(got) != 0 {
		t.Errorf("another space: %q", got)
	}
}

func TestSlowStreamIsDroppedNotWaitedFor(t *testing.T) {
	b := NewBroker()
	slow := b.Subscribe("s1", "u1", true)
	fast := b.Subscribe("s1", "u2", true)
	done := make(chan struct{})
	go func() {
		for i := 0; i < queue*2; i++ {
			b.PublishJSON("s1", "machine", i, false)
			select {
			case <-fast.ch:
			default:
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a slow stream")
	}
	select {
	case <-slow.done:
	default:
		t.Error("the slow stream was not dropped")
	}
	if b.Count("s1") != 1 {
		t.Errorf("%d streams left", b.Count("s1"))
	}
}

func TestKickAndClose(t *testing.T) {
	b := NewBroker()
	a := b.Subscribe("s1", "u1", false)
	c := b.Subscribe("s1", "u2", false)
	b.Kick("s1", "u1")
	select {
	case <-a.done:
	default:
		t.Error("kicked stream is open")
	}
	select {
	case <-c.done:
		t.Error("another user was kicked")
	default:
	}
	b.Close()
	select {
	case <-c.done:
	default:
		t.Error("Close left a stream open")
	}
	if b.Subscribe("s1", "u3", false) != nil {
		t.Error("subscribed after Close")
	}
}

func TestServeWritesSSE(t *testing.T) {
	old := PingEvery
	PingEvery = 30 * time.Millisecond
	defer func() { PingEvery = old }()

	b := NewBroker()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.Serve(w, r, b.Subscribe("s1", "u1", true))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("Cache-Control") == "" {
		t.Errorf("headers: %v", res.Header)
	}
	lines := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	expect := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case l := <-lines:
				if l == want {
					return
				}
			case <-deadline:
				t.Fatalf("never saw %q", want)
			}
		}
	}
	expect(": ok")
	expect(": ping")
	for b.Count("s1") == 0 {
		time.Sleep(time.Millisecond)
	}
	b.PublishJSON("s1", "members", map[string]any{}, false)
	expect("event: members")
	expect("data: {}")
	cancel()
	deadline := time.After(3 * time.Second)
	for b.Count("s1") != 0 {
		select {
		case <-deadline:
			t.Fatal("stream not released after the client left")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
