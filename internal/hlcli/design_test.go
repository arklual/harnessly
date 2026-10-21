package hlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
)

func designLibrary(t *testing.T) string {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	root := t.TempDir()
	for file, content := range map[string]string{
		"site/artifact.json":         `{"v":1,"title":"Site","boards":[{"title":"Home","path":"Home.dc.html"}]}`,
		"site/project/Home.dc.html":  "<main>home</main>",
		"site/project/Other.dc.html": "<main>other</main>",
	} {
		p := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDesignMigrate(t *testing.T) {
	root := designLibrary(t)
	r := runWith(t, t.TempDir(), nil, "", "design", "migrate", "--dry-run", "--library", root)
	if r.code != 0 || !strings.Contains(r.out, "dry run") || !strings.Contains(r.out, "site\tmigrate\t2 pages") {
		t.Fatalf("dry run: %d %s %s", r.code, r.out, r.errs)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "meta")); err == nil {
		t.Fatal("a dry run wrote meta/")
	}
	r = runWith(t, t.TempDir(), map[string]string{"RUNTIME_DESIGN_LIBRARY_DIR": root}, "", "--json", "design", "migrate")
	var out struct {
		Projects []design.ProjectMigration `json:"projects"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.out), &out) != nil || len(out.Projects) != 1 || out.Projects[0].Status != "migrated" {
		t.Fatalf("migrate: %d %s %s", r.code, r.out, r.errs)
	}
	r = runWith(t, t.TempDir(), nil, "", "design", "migrate", "--library", root, "site")
	if r.code != 0 || !strings.Contains(r.out, "site\tcurrent\t0 pages\t0 files") {
		t.Fatalf("again: %d %s", r.code, r.out)
	}
	if r := runWith(t, t.TempDir(), nil, "", "design", "migrate"); r.code != 2 {
		t.Fatalf("no library: %d %s", r.code, r.errs)
	}
}

func TestDesignPageCommandsThroughTheRuntime(t *testing.T) {
	root := designLibrary(t)
	l := design.New(design.Config{LibraryDir: root, Texts: design.Texts{CLI: "harnessly"}})
	t.Cleanup(l.StopWatching)
	l.Migrate(false)
	api := &design.API{L: l, CLIToken: func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "tok" }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/design/pages" {
			w.WriteHeader(404)
			return
		}
		code, v, h := api.CLIPages(r).Split()
		for k, vs := range h {
			w.Header()[k] = vs
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	run := func(args ...string) result { return runWith(t, t.TempDir(), env, "", args...) }

	r := run("design", "page", "create", "site", "--title", "Pricing", "--route", "/pricing")
	if r.code != 0 || !strings.Contains(r.out, `"Pricing": Pricing.dc.html`) {
		t.Fatalf("create: %d %s %s", r.code, r.out, r.errs)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "project", "Pricing.dc.html")); err != nil {
		t.Fatal("no board file")
	}
	r = run("design", "page", "list", "site")
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	if r.code != 0 || len(lines) != 4 || !strings.Contains(lines[1], "\tHome\tHome.dc.html") || !strings.Contains(lines[3], "\tPricing\tPricing.dc.html") {
		t.Fatalf("list: %d %q", r.code, r.out)
	}
	id := strings.Split(lines[3], "\t")[0]
	if r := run("design", "page", "rename", "site", "Pricing", "--title", "Plans", "--file", "Plans.dc.html"); r.code != 0 || !strings.Contains(r.out, "Page "+id+` is now "Plans": Plans.dc.html`) {
		t.Fatalf("rename: %d %s %s", r.code, r.out, r.errs)
	}
	if r := run("design", "page", "remove", "site", id); r.code != 0 || !strings.Contains(r.out, "Removed page "+id) {
		t.Fatalf("remove: %d %s %s", r.code, r.out, r.errs)
	}
	if r := run("design", "page", "remove", "site", id); r.code != 1 || !strings.Contains(r.errs, "No such page") {
		t.Fatalf("remove again: %d %s", r.code, r.errs)
	}
	if r := run("--json", "design", "page", "list", "site"); r.code != 0 || !strings.Contains(r.out, `"release": "legacy"`) {
		t.Fatalf("json: %d %s", r.code, r.out)
	}
	if r := run("design", "page", "create", "site"); r.code != 2 {
		t.Fatalf("no title: %d", r.code)
	}
}

func TestDesignGuardHook(t *testing.T) {
	root := designLibrary(t)
	canvas := filepath.Join(root, "site", "project", "canvas.json")
	in, _ := json.Marshal(map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": canvas}})
	var out bytes.Buffer
	code := Run([]string{"hook", "design-guard"}, Env{Getenv: func(string) string { return "" }, Stdin: bytes.NewReader(in), Stdout: &out})
	if code != 0 || !strings.Contains(out.String(), `"permissionDecision":"deny"`) || !strings.Contains(out.String(), "design page") {
		t.Fatalf("deny: %d %s", code, out.String())
	}
	in, _ = json.Marshal(map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, "site", "project", "Home.dc.html")}})
	out.Reset()
	if code := Run([]string{"hook", "design-guard"}, Env{Getenv: func(string) string { return "" }, Stdin: bytes.NewReader(in), Stdout: &out}); code != 0 || out.Len() != 0 {
		t.Fatalf("allow: %d %s", code, out.String())
	}
}
