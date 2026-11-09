package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/capabilities"
)

func capsOf(t *testing.T, env map[string]string) map[string]capabilities.Capability {
	t.Helper()
	d := runtimeCapabilities(func(k string) string { return env[k] })
	if d.Server != "runtime" {
		t.Fatalf("server = %q", d.Server)
	}
	out := map[string]capabilities.Capability{}
	for _, c := range d.Capabilities {
		out[c.ID] = c
	}
	return out
}

func TestRuntimeCapabilitiesOnABareRuntime(t *testing.T) {
	c := capsOf(t, nil)
	for _, id := range []string{"gitlab", "tracker", "link-meta", "docs", "chats", "hub-link", "disk"} {
		if c[id].Status != capabilities.NeedsConfig || c[id].Configure == "" || c[id].Reason == "" {
			t.Errorf("%s = %+v: a service nobody named needs config and says which", id, c[id])
		}
	}
	for _, id := range []string{"efficiency", "vault", "history", "voice-cloud"} {
		if c[id].Status != capabilities.Off || c[id].Reason == "" {
			t.Errorf("%s = %+v: a gateway part is off with a reason", id, c[id])
		}
	}
}

func TestRuntimeCapabilitiesFollowTheSettings(t *testing.T) {
	c := capsOf(t, map[string]string{
		"RUNTIME_WEB_FEATURES": "vault,chats", "RUNTIME_GITLAB_HOSTS": "gitlab.example.com",
		"RUNTIME_TRACKER_URL": "https://tracker.example.com", "RUNTIME_DOCS_HOST": "docs.example.com",
	})
	for _, id := range []string{"vault", "chats", "gitlab", "tracker", "docs", "link-meta"} {
		if c[id].Status != capabilities.On {
			t.Errorf("%s = %+v, want on", id, c[id])
		}
	}
	if c["efficiency"].Status != capabilities.Off {
		t.Errorf("efficiency = %+v", c["efficiency"])
	}
}

func TestCapabilitiesRoute(t *testing.T) {
	routes := capabilityRoutes(deps{})
	for _, p := range []string{"/api/capabilities", "/agents/api/capabilities"} {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		if !routes[0].Match(r) {
			t.Fatalf("%s is not matched", p)
		}
		status, v, _ := routes[0].Compute(r)
		if doc, ok := v.(capabilities.Document); status != 200 || !ok || len(doc.Capabilities) == 0 {
			t.Errorf("%s → %d %+v", p, status, v)
		}
	}
	if status, _, _ := routes[0].Compute(httptest.NewRequest(http.MethodPost, "/api/capabilities", nil)); status != 405 {
		t.Errorf("POST = %d", status)
	}
}
