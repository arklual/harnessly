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

// An observe link keeps the designs' access only when the machine's operator says so (AGENT_HUB_DESIGN=on). It then
// relays the snapshot and the guest token, hears the design events, and opens tunnels that reach the guest read of a
// design and nothing else; the rest stays closed and the cluster's tunnels keep the runtime's own port.
func TestHubObserveLinkServesDesignsWhenOptedIn(t *testing.T) {
	t.Setenv(designOptIn, "on")
	seen := make(chan string, 4)
	r := startHubAgent(t, rigOptions{observe: true, runtimeRoute: func(w http.ResponseWriter, req *http.Request) bool {
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
	if !contains(hello.Features, hubproto.FeatureDesignAccess) || contains(hello.Features, "tunnel") || contains(hello.Features, hubproto.FeatureDrain) {
		t.Fatalf("features %v", hello.Features)
	}
	if hello.Policy.RuntimeSession || hello.Policy.Terminal || hello.Policy.Source != observeSource {
		t.Fatalf("observe policy %+v", hello.Policy)
	}
	c.mustCall(hubproto.OpDesignAccess, map[string]any{"enforce": true, "projects": map[string]any{}})
	if got := <-seen; strings.Fields(got)[0] != "/agents/api/platform/design/access" {
		t.Fatalf("relayed snapshot: %s", got)
	}
	c.mustCall(hubproto.OpDesignPublicToken, map[string]any{"project": "p1"})
	if got := <-seen; strings.Fields(got)[0] != "/agents/api/platform/design/guest-token" {
		t.Fatalf("relayed token request: %s", got)
	}
	// What the opt-in does not cover stays closed.
	for op, params := range map[string]map[string]any{
		"runtime.session": {"subject": "a@example.com"},
		"key.deliver":     {"provider": "claude", "envelope": seal(t, r.machine, "key:claude", "sk-x")},
		"budget.set":      {"version": 1, "mode": "stop", "entries": []any{}},
	} {
		if _, e := c.call(op, params); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%s: %q", op, e)
		}
	}

	tid := strings.Repeat("ab", 16)
	c.mustCall("tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": tid, "target": "runtime"}})
	req := func(method, path, extra string) string {
		return method + " " + path + " HTTP/1.1\r\nHost: hub.example.com\r\nConnection: keep-alive\r\nContent-Length: 0\r\n" + extra + "\r\n"
	}
	base := "/m/" + r.machine.MachineID
	got := httpOverTunnel(t, c, tid, req("GET", base+"/api/other/design-public/p1/boards/index.html", "x-harnessly-design-token: a.b.c\r\nAuthorization: Bearer x\r\n"))
	if !strings.HasPrefix(got, "HTTP/1.1 200") || !strings.Contains(got, `"path":"/agents/api/platform/design-public/p1/boards/index.html"`) {
		t.Fatalf("guest read: %q", got)
	}
	if h := r.runtime.last.Load(); h == nil || h.Header.Get("x-harnessly-design-token") != "a.b.c" || h.Header.Get("Authorization") != "" {
		t.Errorf("the runtime saw %v", h)
	}
	for i, bad := range []string{
		req("GET", base+"/api/status", ""),
		req("GET", base+"/api/platform/design/access", ""),
		req("POST", base+"/api/platform/design-public/p1", ""),
		req("GET", base+"/api/platform/design-public/p1/%2e%2e/%2e%2e/files", ""),
		req("GET", base+"/api/platform/design-public/p1/a%2Fb", ""),
		req("GET", base+"/api/platform/design-public/p1?x=1", ""),
		req("GET", base+"/api/platform/design-public/p1", "Cookie: session-token=x\r\n"),
		req("GET", "/m/someoneelse/api/platform/design-public/p1", ""),
	} {
		tn := strings.Repeat(string("0123456789"[i]), 32)
		c.mustCall("tunnel.open", map[string]any{"spec": map[string]string{"tunnelId": tn, "target": "runtime"}})
		if got := httpOverTunnel(t, c, tn, bad); !strings.HasPrefix(got, "HTTP/1.1 403") || !strings.Contains(got, "machine_policy") {
			t.Errorf("%q: %q", strings.SplitN(bad, "\r\n", 2)[0], got)
		}
	}
	if got := r.agent.tunnels.runtimePort; got != r.runtime.port {
		t.Errorf("the cluster's tunnels now go to port %d, not the runtime's %d", got, r.runtime.port)
	}

	// The design events reach the hub that asked for them.
	r.agent.localToken = "local-secret"
	c.mustCall("link.subscribe", map[string]any{"events": []string{hubproto.EventDesign}})
	if rec := post(r.agent, "/design/event", "local-secret", `{"kind":"design.created","project":"p1"}`); rec.Code != 202 {
		t.Fatalf("event: %d", rec.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(designEvents(c)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ev := designEvents(c); len(ev) != 1 || ev[0].Project != "p1" {
		t.Fatalf("events %+v", ev)
	}
}
