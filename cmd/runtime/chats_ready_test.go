package main

import "testing"

func TestChatsStateFollowsTheStart(t *testing.T) {
	t.Setenv("RUNTIME_V2", "")
	c := &core{}
	if got := c.chatsState(); got != "" {
		t.Fatalf("a runtime without conversations reports %q", got)
	}
	c.v2d = &deps{}
	if got := c.chatsState(); got != chatsStarting {
		t.Fatalf("before the conversations are loaded: %q", got)
	}
	v := &v2Live{}
	c.v2.Store(v)
	if got := c.chatsState(); got != chatsStarting {
		t.Fatalf("stored but not loaded: %q", got)
	}
	v.ready.Store(true)
	if got := c.chatsState(); got != chatsRestoring {
		t.Fatalf("loaded, the take-over runs: %q", got)
	}
	v.settled.Store(true)
	if got := c.chatsState(); got != chatsRestoring {
		t.Fatalf("taken over, the agents are not resumed yet: %q", got)
	}
	c.started.Store(true)
	if got := c.chatsState(); got != chatsReady {
		t.Fatalf("started: %q", got)
	}
	t.Setenv("RUNTIME_V2", "off")
	if got := c.chatsState(); got != "" {
		t.Fatalf("v2 off reports %q", got)
	}
}
