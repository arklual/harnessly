package agents_test

import (
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestLaunchRefusalsInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if _, err := agents.FormatStartEnv([]agents.EnvVar{{Key: "PATH", Value: "x"}}, "APP_"); err == nil || err.Error() != "Invalid environment variable PATH" {
		t.Errorf("env: %v", err)
	}
	if _, err := agents.ResolvePromptFiles("codex", []string{"/a.md"}, t.TempDir(), "~/.data"); err == nil || err.Error() != "Only Claude supports instruction files" {
		t.Errorf("files: %v", err)
	}
}
