package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The channel exists only with the flag at "1", and names a socket only for a Codex whose app-server it knows: every
// other machine starts its Codex chats as ever.
func TestCodexInputIsOffUnlessFlaggedAndSupported(t *testing.T) {
	for _, flag := range []string{"", "0", "true", "on"} {
		if c := newCodexInput(t.TempDir(), flag); c != nil || c.input() != nil {
			t.Fatalf("flag %q turned the channel on", flag)
		}
	}
	if codexInputOf(nil) != nil || codexInputOf(&core{}) != nil || codexInputOf(&core{agents: &coreAgents{}}) != nil {
		t.Fatal("a core without the channel gives the conversations none")
	}
	data, bin := t.TempDir(), t.TempDir()
	c := newCodexInput(data, "1")
	if c == nil || c.input() == nil {
		t.Fatal("the flag did not turn the channel on")
	}
	t.Setenv("PATH", bin)
	if c.socket("tab-a") != "" {
		t.Fatal("a machine without codex has no socket")
	}
	version := "codex-cli 0.146.0"
	c.version = func(string) (string, error) { return version, nil }
	old := filepath.Join(bin, "codex")
	if err := os.WriteFile(old, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if c.socket("tab-a") != "" {
		t.Fatal("a Codex older than the protocol has no socket")
	}
	// Another binary at the path: its version is read anew.
	version = "codex-cli 0.147.0"
	if err := os.WriteFile(old, []byte("#!/bin/sh\n# newer\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := c.socket("tab-a"), filepath.Join(data, "codex-input", "tab-a.sock"); got != want {
		t.Fatalf("socket %q, want %q", got, want)
	}
}
