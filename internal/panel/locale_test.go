package panel

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishRefusals(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if _, err := targets.Parse(obj("kind", "exec")); err != "Unknown command: open, show, hide, status" {
		t.Errorf("unknown kind: %q", err)
	}
	if _, err := targets.Parse("status"); err != "A command object is required" {
		t.Errorf("not an object: %q", err)
	}
	if _, err := targets.Parse(obj("kind", "open", "tab", "nope")); !strings.HasPrefix(err, "open: unknown tab, available: ") {
		t.Errorf("unknown tab: %q", err)
	}
	if _, err := targets.Parse(obj("kind", "open", "tab", "overview", "target", "x")); err != "open overview: this tab needs no target" {
		t.Errorf("no target: %q", err)
	}
	for tab, want := range map[string]string{
		"browser":   "open browser: opens a port of this machine (4400, localhost:4400/path) or a feature stand https://<slug>.preview.example.org; the agent does not open other external addresses",
		"artifacts": "open artifacts: a link to a document or a board of a.example.org is required (https://a.example.org/a/… or /b/…)",
		"gitlab":    "open gitlab: a link to a GitLab merge request or commit (https://gitlab.example.org/…), or to a GitHub pull request, commit or issue (https://github.com/…) is required",
	} {
		if _, err := targets.Parse(open(tab, "not a target")); err != want {
			t.Errorf("%s hint: %q", tab, err)
		}
	}
	if cyrillic.MatchString(targets.hint("files", path)) {
		t.Error("the English path hint has Russian text")
	}
}

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)
