package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkTranscript(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	old := filepath.Join(cfg, "projects", "-srv-proj", "abc-1.jsonl")
	_ = os.MkdirAll(filepath.Dir(old), 0o700)
	_ = os.WriteFile(old, []byte("{}\n"), 0o600)
	if got := claudeProject("/home/me/my.proj"); got != "-home-me-my-proj" {
		t.Fatal(got)
	}
	if err := linkTranscript([]string{"--settings", "x", "--resume", "abc-1", "go"}, "/home/me/proj"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cfg, "projects", "-home-me-proj", "abc-1.jsonl")
	if target, err := os.Readlink(link); err != nil || target != old {
		t.Fatalf("%q %v", target, err)
	}
	// Again (a second resume there) and with no resume: nothing changes, nothing fails.
	if err := linkTranscript([]string{"--resume", "abc-1"}, "/home/me/proj"); err != nil {
		t.Fatal(err)
	}
	if err := linkTranscript([]string{"-p", "hi"}, "/home/me/other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(cfg, "projects", "-home-me-other")); err == nil {
		t.Fatal("made a project without a resume")
	}
}

func TestTrustFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	_ = os.WriteFile(filepath.Join(cfg, ".claude.json"), []byte(`{"projects":{}}`), 0o600)
	if err := trustFolder("/home/me/proj"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg, ".claude.json"))
	if !strings.Contains(string(raw), `"/home/me/proj"`) || !strings.Contains(string(raw), `"hasTrustDialogAccepted": true`) {
		t.Fatalf("%s", raw)
	}
}
