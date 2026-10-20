package limits

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishCodexbarFailures(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	for stdout, want := range map[string]string{
		"nonsense":                           "codexbar: the answer is not JSON",
		`[{"provider":"codex"}]`:             "codexbar: the answer has no Claude",
		`[{"provider":"claude"}]`:            "codexbar: the answer has no usage",
		`[{"provider":"claude","usage":{}}]`: "codexbar: no 5 h and 7 d windows",
		`[{"provider":"claude","error":{}}]`: "codexbar: no description",
	} {
		if _, failure := ParseCodexbar([]byte(stdout), 1); failure != want {
			t.Errorf("%s: %q, want %q", stdout, failure, want)
		}
	}
	if _, err := runCodexbarCLI(context.Background(), filepath.Join(t.TempDir(), "missing"), nil); err == nil || err.Error() != "did not start (ENOENT)" {
		t.Errorf("missing binary: %v", err)
	}
}
