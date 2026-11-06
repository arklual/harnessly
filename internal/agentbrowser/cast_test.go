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

func TestScreencastRetainsPageEventsDuringPersonControl(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	p := newPage(Tab{ID: "page", Conversation: "chat", Control: "person"})
	p.binding.session = "session"
	i.pages[p.ID] = p
	listener := &agentListener{conv: "chat", tab: p.ID, events: make(chan AgentEvent, 3)}
	i.agentListeners = map[*agentListener]bool{listener: true}
	i.event("Runtime.consoleAPICalled", "session", []byte(`{}`))
	i.event("Network.requestWillBeSent", "session", []byte(`{}`))
	i.event("Page.screencastFrame", "session", []byte(`{"data":"YQ==","sessionId":1,
 "metadata":{"deviceWidth":800,"deviceHeight":600}}`))
	if len(listener.events) != 1 {
		t.Fatalf("expected only the Page event, got %d events", len(listener.events))
	}
	if event := <-listener.events; event.Method != "Page.screencastFrame" {
		t.Fatalf("Page event was filtered: %s", event.Method)
	}
	if p.last == nil || string(p.last.JPEG) != "a" || len(i.ackQueue) != 1 {
		t.Fatal("person control discarded the frame or its acknowledgment")
	}
}
