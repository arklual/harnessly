package agentbrowser

import (
	"context"
	"testing"
	"time"
)

func TestScreencastFlushesFinalFrame(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	p := newPage(Tab{ID: "page"})
	p.binding.session = "session"
	v := &Viewer{frames: make(chan Frame, 1)}
	p.viewers[v] = true
	p.delivered = time.Now()
	i.pages[p.ID] = p
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); i.acknowledge(ctx) }()
	defer func() { cancel(); <-done }()
	i.event("Page.screencastFrame", "session", []byte(`{"data":"YQ==","sessionId":1,
 "metadata":{"deviceWidth":800,"deviceHeight":600}}`))
	i.event("Page.screencastFrame", "session", []byte(`{"data":"Yg==","sessionId":2,
 "metadata":{"deviceWidth":800,"deviceHeight":600}}`))
	select {
	case frame := <-v.Frames():
		if string(frame.JPEG) != "b" {
			t.Fatalf("not the latest frame: %q", frame.JPEG)
		}
	case <-time.After(time.Second):
		t.Fatal("final static frame was discarded by throttling")
	}
	t.Log("latest static frame delivered after throttling interval without another Chrome event")
}
