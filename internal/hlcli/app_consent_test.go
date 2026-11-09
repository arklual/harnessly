package hlcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// the CLI says what an app waits for and where the person agrees; no command grants consent.
func TestAppEnableSaysConsentWaits(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "hs-chat", "app", "enable", "example.waits")
	if r.code == 0 || !strings.Contains(r.errs, "waits for the person's consent to ai, secrets:token") || !strings.Contains(r.errs, "«Приложения» → example.waits") {
		t.Fatalf("enable of a waiting app: %d %q %q", r.code, r.out, r.errs)
	}
}

func TestAppDevSaysWhereToConsent(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "", "app", "dev", t.TempDir())
	if r.code != 0 || !strings.Contains(r.out, "«Разрешить» in this chat") || !strings.Contains(r.out, "«Приложения» → example.app → «Посмотреть доступы»") || strings.Contains(r.out, "app allow") {
		t.Fatalf("dev: %d %q %q", r.code, r.out, r.errs)
	}
}

func TestAppLogs(t *testing.T) {
	f := newFake(t)
	r := f.run(t, "", "app", "logs", "example.app", "--limit", "5")
	if r.code != 0 || !strings.Contains(r.out, `error  "main" "TypeError: x is undefined"`) || !strings.Contains(r.out, "the app's words") {
		t.Fatalf("logs: %d %q %q", r.code, r.out, r.errs)
	}
	if c := f.last(); c.Path != "/api/cli/apps/logs" || !strings.Contains(c.Query, "limit=5") {
		t.Fatalf("asked %+v", c)
	}
	if r := f.run(t, "", "app", "logs", "example.quiet"); r.code != 0 || !strings.Contains(r.out, "no errors") {
		t.Fatalf("no lines: %d %q", r.code, r.out)
	}
}

func TestAppHelpNamesExplain(t *testing.T) {
	r := runWith(t, t.TempDir(), map[string]string{}, "", "app", "--help")
	for _, want := range []string{"explain", "logs", "/m/<machine>/apps/<id>", "no command does"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("app --help lacks %q: %q", want, r.out)
		}
	}
	r = runWith(t, t.TempDir(), map[string]string{}, "", "--help")
	for _, want := range []string{"harnessly app explain ID", "harnessly app logs ID"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
}

func TestValidateVerdicts(t *testing.T) {
	warn := apps.Problems{{Path: "x", Message: "w", Warning: true}}
	if got := validateVerdict(warn, true, false); got != "1 warning(s) treated as errors by --strict: the package is not ready" {
		t.Errorf("strict warning: %q", got)
	}
	mixed := apps.Problems{{Message: "e"}, {Message: "w", Warning: true}, {Message: "w2", Warning: true}}
	if got := validateVerdict(mixed, true, false); got != "1 error(s), 2 warning(s) treated as errors by --strict: the package is not ready" {
		t.Errorf("strict mixed: %q", got)
	}
	if got := validateVerdict(mixed, false, false); got != "1 error(s): the package is not ready" {
		t.Errorf("plain mixed: %q", got)
	}
	if got := validateVerdict(nil, false, true); got != "1 error(s): the package is not ready" {
		t.Errorf("no package: %q", got)
	}
	if got := validateOK(warn); got != "ok, 1 warning(s)" {
		t.Errorf("ok with a warning: %q", got)
	}
	if got := validateOK(nil); got != "ok" {
		t.Errorf("plain ok: %q", got)
	}
}

// The whole command: a package with a warning passes plainly with the count, fails --strict naming the warning.
func TestAppValidateWarningsEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(dir, "example.warned", "", "chat"); err != nil {
		t.Fatal(err)
	}
	// A colour of its own in a stylesheet is a design warning (design_check.go).
	if err := os.WriteFile(filepath.Join(dir, "extra.css"), []byte("a { color: #ff0000; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runWith(t, t.TempDir(), map[string]string{}, "", "app", "validate", "--strict", dir)
	if r.code == 0 || !strings.Contains(r.out, "warning(s) treated as errors by --strict") {
		t.Fatalf("strict: %q", r.out)
	}
	r = runWith(t, t.TempDir(), map[string]string{}, "", "app", "validate", dir)
	if r.code != 0 || !strings.HasPrefix(lastLine(r.out), "ok, ") || !strings.Contains(lastLine(r.out), "warning(s): example.warned") {
		t.Fatalf("plain: %d %q", r.code, r.out)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
