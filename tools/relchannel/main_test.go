package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPromoteWritesThePointerOnlyAfterTheWindow(t *testing.T) {
	dir := t.TempDir()
	rel := write(t, dir, "release.json", `{"tag_name":"v0.16.180","name":"v0.16.180","html_url":"https://github.com/o/r/releases/tag/v0.16.180",
		"published_at":"2026-11-03T12:00:00Z","draft":false,"prerelease":false,"assets":[]}`)
	out := filepath.Join(dir, "stable.json")
	var stdout, stderr bytes.Buffer

	// Six days out: refused, nothing written. A pointer that is not there yet is the first promotion.
	args := []string{"promote", "-release", rel, "-current", filepath.Join(dir, "current.json"), "-out", out}
	if code := run(append(args, "-now", "2026-11-09T12:00:00Z"), &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	if _, err := os.Stat(out); err == nil || !strings.Contains(stderr.String(), "the window is 168h") {
		t.Fatalf("a refused promotion wrote the pointer or gave no reason: %s", stderr.String())
	}

	stderr.Reset()
	if code := run(append(args, "-now", "2026-11-10T12:00:00Z"), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema": 1`, `"channel": "stable"`, `"version": "0.16.180"`, `"released_at": "2026-11-03T12:00:00Z"`,
		`"promoted_at": "2026-11-10T12:00:00Z"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("no %s in %s", want, body)
		}
	}
	if !strings.Contains(stdout.String(), "stable: none -> 0.16.180") {
		t.Fatalf("stdout %q", stdout.String())
	}

	// The pointer in place is newer: refused unless asked.
	older := write(t, dir, "older.json", `{"tag_name":"v0.16.150","published_at":"2026-10-01T12:00:00Z"}`)
	back := filepath.Join(dir, "back.json")
	args = []string{"promote", "-release", older, "-current", out, "-out", back, "-now", "2026-11-10T12:00:00Z"}
	if code := run(args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "older than the stable 0.16.180") {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	if code := run(append(args, "-allow-older"), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
}

func TestPromoteRefusesAWithdrawnReleaseAndBadInput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "stable.json")
	var stdout, stderr bytes.Buffer
	rel := write(t, dir, "release.json", `{"tag_name":"v0.16.180","name":"v0.16.180 [withdrawn]","published_at":"2026-10-01T12:00:00Z"}`)
	if code := run([]string{"promote", "-release", rel, "-out", out, "-now", "2026-11-10T12:00:00Z"}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "withdrawn") {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	broken := write(t, dir, "current.json", `{"version":"0.16.100","files":{}}`)
	good := write(t, dir, "good.json", `{"tag_name":"v0.16.180","published_at":"2026-10-01T12:00:00Z"}`)
	if code := run([]string{"promote", "-release", good, "-current", broken, "-out", out}, &stdout, &stderr); code != 1 {
		t.Fatalf("a pointer in place that cannot be read must stop the promotion: exit %d", code)
	}
	for _, args := range [][]string{{}, {"promote"}, {"promote", "-release", good}, {"promote", "-release", good, "-out", out, "-window", "0"}} {
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a pointer was written")
	}
}
