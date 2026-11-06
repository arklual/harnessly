package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A hub link serves the design access operations: the snapshot and the guest token go to the runtime, and the
// runtime's design events come back to a hub that subscribed to them.
func TestHubServesDesignAccess(t *testing.T) {
	seen := make(chan string, 4)
	r := startHubAgent(t, rigOptions{runtimeRoute: func(w http.ResponseWriter, req *http.Request) bool {
		if !strings.Contains(req.URL.Path, "/design/") {
			return false
		}
		raw, _ := io.ReadAll(req.Body)
		seen <- req.URL.Path + " " + string(raw)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(req.URL.Path, "/design/guest-token") {
			_, _ = w.Write([]byte(`{"token":"a.b.c","header":"x-harnessly-design-token","expiresAt":1}`))
			return true
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
		return true
	}})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if !contains(hello.Features, hubproto.FeatureDesignAccess) {
		t.Fatalf("features %v", hello.Features)
	}
	snap := map[string]any{"enforce": true, "at": "2026-11-05T10:00:00Z", "projects": map[string]any{},
		"people": map[string]any{"b@example.com": map[string]any{"projects": map[string]any{}, "team": map[string]any{"p1": "none"}}}}
	if res := c.mustCall(hubproto.OpDesignAccess, snap); string(res) != `{"ok":true}` {
		t.Fatalf("access: %s", res)
	}
	got := <-seen
	if !strings.HasSuffix(strings.Fields(got)[0], "/api/platform/design/access") || !strings.Contains(got, `"b@example.com"`) || strings.Contains(got, `"op"`) {
		t.Fatalf("relayed snapshot: %s", got)
	}
	tok := decode[hubproto.DesignPublicToken](t, c.mustCall(hubproto.OpDesignPublicToken, map[string]any{"project": "p1"}))
	if tok.Token != "a.b.c" || tok.Header == "" {
		t.Fatalf("token %+v", tok)
	}
	if got := <-seen; !strings.HasSuffix(strings.Fields(got)[0], "/api/platform/design/guest-token") || !strings.Contains(got, `"project":"p1"`) {
		t.Fatalf("relayed token request: %s", got)
	}

	// A design event reaches the hub only after it asked for it by name.
	r.agent.localToken = "local-secret"
	if rec := post(r.agent, "/design/event", "local-secret", `{"kind":"design.created","project":"p1","person":"a@example.com"}`); rec.Code != 202 {
		t.Fatalf("event: %d", rec.Code)
	}
	time.Sleep(150 * time.Millisecond)
	if ev := designEvents(c); len(ev) != 0 {
		t.Fatalf("an event went to a hub that did not ask: %v", ev)
	}
	c.mustCall("link.subscribe", map[string]any{"events": []string{hubproto.EventDesign}})
	if rec := post(r.agent, "/design/event", "local-secret", `{"kind":"design.created","project":"p1","person":"a@example.com"}`); rec.Code != 202 {
		t.Fatalf("event: %d", rec.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(designEvents(c)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	ev := designEvents(c)
	if len(ev) != 1 || ev[0] != (hubproto.DesignEvent{Kind: "design.created", Project: "p1", Person: "a@example.com"}) {
		t.Fatalf("events %+v", ev)
	}
}

func designEvents(c *hubConn) []hubproto.DesignEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []hubproto.DesignEvent
	for _, ev := range c.events {
		if string(ev["event"]) != `"`+hubproto.EventDesign+`"` {
			continue
		}
		var d hubproto.DesignEvent
		_ = json.Unmarshal(ev["design"], &d)
		out = append(out, d)
	}
	return out
}
