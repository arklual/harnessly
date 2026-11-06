package hlcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func docsLibCLI(t *testing.T, stdin string) (*cli, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	env := map[string]string{"HARNESSLY_DOCS_DIR": filepath.Join(dir, "docs")}
	c := &cli{env: Env{Getenv: func(k string) string { return env[k] }, Home: dir, Cwd: dir, Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &out}}
	return c, &out, dir
}

func TestDocsAddThenListSearchRead(t *testing.T) {
	c, out, dir := docsLibCLI(t, "Restart with kubectl rollout restart deploy/api.\n")
	pic := filepath.Join(dir, "shot.png")
	_ = os.WriteFile(pic, []byte("\x89PNG"), 0o644)
	if err := c.run([]string{"docs", "add", "runbooks/restart-api", "--title", "Restart the API", "--section-title", "Runbooks", "--picture", pic}); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	page := filepath.Join(dir, "docs", "runbooks", "restart-api.md")
	raw, err := os.ReadFile(page)
	if err != nil || !strings.HasPrefix(string(raw), "# Restart the API\n\nRestart with") {
		t.Fatalf("page: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "runbooks", "img", "shot.png")); err != nil {
		t.Errorf("picture not copied: %v", err)
	}
	if m, _ := os.ReadFile(filepath.Join(dir, "docs", "runbooks", "section.json")); !strings.Contains(string(m), `"Runbooks"`) {
		t.Errorf("manifest: %s", m)
	}

	out.Reset()
	if err := c.run([]string{"docs"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "runbooks/restart-api") || !strings.Contains(got, "start/overview") {
		t.Errorf("list: %s", got)
	}
	out.Reset()
	if err := c.run([]string{"docs", "search", "rollout", "restart"}); err != nil || !strings.Contains(out.String(), "runbooks/restart-api") {
		t.Errorf("search: %v %s", err, out)
	}
	out.Reset()
	if err := c.run([]string{"docs", "read", "restart-api"}); err != nil || !strings.Contains(out.String(), "kubectl rollout") {
		t.Errorf("read: %v %s", err, out)
	}

	// No silent overwrite; --replace writes over.
	c.env.Stdin = strings.NewReader("# Restart the API\n\nv2\n")
	if err := c.run([]string{"docs", "add", "runbooks/restart-api"}); err == nil {
		t.Error("overwrote without --replace")
	}
	c.env.Stdin = strings.NewReader("# Restart the API\n\nv2\n")
	if err := c.run([]string{"docs", "add", "runbooks/restart-api", "--replace"}); err != nil {
		t.Error(err)
	}
}

func TestDocsAddRefusesBadNamesAndUntitledPages(t *testing.T) {
	for _, args := range [][]string{
		{"docs", "add", "../etc/passwd"},
		{"docs", "add", "Runbooks/Deploy"},
		{"docs", "add", "runbooks"},
		{"docs", "add", "runbooks/x", "--audience", "everyone"},
	} {
		c, _, _ := docsLibCLI(t, "# T\n")
		if err := c.run(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	c, _, _ := docsLibCLI(t, "no title here\n")
	if err := c.run([]string{"docs", "add", "runbooks/x"}); err == nil {
		t.Error("a page without a title was written")
	}
}

func TestDocsAddToBuiltinSectionKeepsItsTitle(t *testing.T) {
	c, _, dir := docsLibCLI(t, "# Our VPN\n\nConnect first.\n")
	if err := c.run([]string{"docs", "add", "start/vpn"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "start", "section.json")); err == nil {
		t.Error("a manifest was written over a built-in section")
	}
}
