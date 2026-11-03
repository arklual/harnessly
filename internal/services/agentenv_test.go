package services

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgentEnvGivesTheTrackerTokenOnly(t *testing.T) {
	t.Setenv("YOUTRACK_TOKEN", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".config/agentenv-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "youtrack-token"), []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := []string{".config/agentenv-test"}

	if got := New(Config{Home: home, SecretDirs: secrets}).AgentEnv(); got != nil {
		t.Fatalf("no tracker, no token: %q", got)
	}
	s := New(Config{Home: home, SecretDirs: secrets, TrackerURL: "https://tracker.example.com"})
	if got := s.AgentEnv(); !reflect.DeepEqual(got, []string{"YOUTRACK_TOKEN=from-file"}) {
		t.Fatalf("%q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "youtrack-token"), []byte("rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentEnv(); !reflect.DeepEqual(got, []string{"YOUTRACK_TOKEN=rotated"}) {
		t.Fatalf("a rotated token is taken at the next launch: %q", got)
	}
	if err := os.Remove(filepath.Join(dir, "youtrack-token")); err != nil {
		t.Fatal(err)
	}
	if got := New(Config{Home: home, SecretDirs: secrets, TrackerURL: "https://tracker.example.com"}).AgentEnv(); got != nil {
		t.Fatalf("no token, nothing: %q", got)
	}
}
