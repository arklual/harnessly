package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The hub's environment plugins load once the agent wrote one: the hub machine's, the observe link's under <data>/hub,
// and the observe link's own folder when it is elsewhere; looked at on every launch.
func TestHubEnvPluginDirs(t *testing.T) {
	data := t.TempDir()
	other := t.TempDir()
	dirs := hubEnvPluginDirs(data, filepath.Join(other, "machine.json"))
	if got := dirs(); len(got) != 0 {
		t.Fatalf("no plugin yet: %v", got)
	}
	plugin := func(link string) string {
		d := filepath.Join(link, "environment", "claude-plugin")
		if err := os.MkdirAll(filepath.Join(d, ".claude-plugin"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".claude-plugin", "plugin.json"), []byte(`{"name":"hub-environment"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return d
	}
	want := []string{plugin(data), plugin(filepath.Join(data, "hub")), plugin(other)}
	if got := dirs(); !slices.Equal(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
	// The default observe folder is not listed twice.
	if got := hubEnvPluginDirs(data, filepath.Join(data, "hub", "machine.json"))(); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}
