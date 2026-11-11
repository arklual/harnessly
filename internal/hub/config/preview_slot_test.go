package config

import "testing"

func TestPreviewSlotHostTemplate(t *testing.T) {
	c := &Config{Host: "hub.example.org:8443", AgentHost: "agent.example.org", PreviewSlotDomain: "slots.example.net"}
	h := c.PreviewSlotHost("pabcdefghijkl", 3)
	if h != "pabcdefghijkl-3.slots.example.net:8443" {
		t.Fatalf("host: %q", h)
	}
	if handle, n, ok := c.PreviewSlotFromHost(h); !ok || handle != "pabcdefghijkl" || n != 3 {
		t.Fatalf("round trip: %q %d %v", handle, n, ok)
	}
	for _, bad := range []string{
		"pabcdefghijkl-7.slots.example.net", // slot out of range
		"pabcdefghijkl-0.slots.example.net",
		"abcdefghijklm-1.slots.example.net", // no p
		"pabcdefghijkl-1.x.slots.example.net",
		"pabcdefghijkl-1.slots.example.org",
		"pabcdefghijkl.slots.example.net",
		"hub.example.org", "agent.example.org", "slots.example.net", "",
	} {
		if _, _, ok := c.PreviewSlotFromHost(bad); ok {
			t.Errorf("%q read as a slot host", bad)
		}
	}
	if (&Config{}).PreviewSlotHost("pabcdefghijkl", 1) != "" {
		t.Error("without a domain there is no host")
	}
}
