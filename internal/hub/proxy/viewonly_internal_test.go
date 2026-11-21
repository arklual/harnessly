package proxy

import "testing"

func TestRuntimePath(t *testing.T) {
	for in, want := range map[string]string{
		"/m/abc/api/timeline":        "/api/timeline",
		"/m/abc/agents/api/timeline": "/api/timeline",
		"/m/abc/agentsx":             "/agentsx",
		"/api/status":                "/api/status",
	} {
		if got := runtimePath(in, "abc"); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestViewMessage(t *testing.T) {
	allow := viewSockets["/api/status"]
	for msg, want := range map[string]bool{
		`{"type":"ping"}`:                    true,
		`{"type":"status:request-sync"}`:     true,
		`{"type":"status:tab-dismissed"}`:    false,
		`{"type":"status:ack-notification"}`: false,
		`[]`:                                 false,
		``:                                   false,
	} {
		if got := viewMessage([]byte(msg), allow); got != want {
			t.Errorf("%s: %v", msg, got)
		}
	}
	if viewMessage([]byte(`{"type":"timeline:resume"}`), viewSockets["/api/timeline"]) {
		t.Error("timeline:resume starts the agent: a task, not watching")
	}
}
