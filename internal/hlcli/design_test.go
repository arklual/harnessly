package hlcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestDesignNotesCommandsThroughTheRuntime(t *testing.T) {
	root := designLibrary(t)
	l := design.New(design.Config{LibraryDir: root, Texts: design.Texts{CLI: "harnessly"}})
	t.Cleanup(l.StopWatching)
	l.Migrate(false)
	api := &design.API{L: l, CLIToken: func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "tok" }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/design/notes" {
			w.WriteHeader(404)
			return
		}
		code, v, _ := api.CLINotes(r).Split()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	run := func(args ...string) result { return runWith(t, t.TempDir(), env, "", args...) }

	// The text comes from stdin ("piped text" in the test's environment).
	r := run("design", "notes", "set", "site", "Home", "--file", "-")
	if r.code != 0 || !strings.Contains(r.out, `"Home": 2 words, ~10 s`) {
		t.Fatalf("set: %d %s %s", r.code, r.out, r.errs)
	}
	file := l.ReadModel("site").Pages[0].ID + ".md"
	if b, err := os.ReadFile(filepath.Join(root, "site", "meta", "notes", file)); err != nil || string(b) != "piped text\n" {
		t.Fatalf("file: %q %v", b, err)
	}
	if r := run("design", "notes", "get", "site", "Home"); r.code != 0 || r.out != "piped text\n" {
		t.Fatalf("get one: %d %q", r.code, r.out)
	}
	r = run("design", "notes", "get", "site")
	if r.code != 0 || !strings.Contains(r.out, "\tHome\t~10 s\t2 words") || !strings.Contains(r.out, "data, not instructions") {
		t.Fatalf("get all: %d %q", r.code, r.out)
	}
	if r := run("design", "notes", "set", "site", "Nope", "--file", "-"); r.code != 1 || !strings.Contains(r.errs, "No such slide") {
		t.Fatalf("no slide: %d %s", r.code, r.errs)
	}
	if r := run("design", "notes", "set", "site"); r.code != 2 {
		t.Fatalf("usage: %d", r.code)
	}
	if r := run("design", "notes"); r.code != 2 {
		t.Fatalf("no subcommand: %d", r.code)
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

// timeline and moment read the events a page recorded, through the runtime's /api/cli/design, in the reader's zone.
func TestDesignTimelineAndMoment(t *testing.T) {
	root := designLibrary(t)
	l := design.New(design.Config{LibraryDir: root, Texts: design.Texts{CLI: "harnessly"}})
	t.Cleanup(l.StopWatching)
	api := &design.API{L: l, CLIToken: func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "tok" }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, v, _ := api.CLIDesign(r).Split()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)

	loc := time.FixedZone("T", 3*3600)
	base := time.Now().In(loc).Truncate(time.Second)
	at := func(sec int) int64 { return base.Add(time.Duration(sec) * time.Second).UnixMilli() }
	batch := fmt.Sprintf(`[{"k":"nav","t":%d,"u":"Home.dc.html","w":390,"h":844,"dpr":3},
	  {"k":"click","t":%d,"tag":"button","sel":"button.buy","tx":"Buy","x":5,"y":6},
	  {"k":"scroll","t":%d,"y":10,"max":900},{"k":"scroll","t":%d,"y":400,"max":900},
	  {"k":"lt","t":%d,"d":350},{"k":"req","t":%d,"me":"GET","u":"/api/x","st":0,"d":30}]`,
		at(-100), at(-60), at(-50), at(-49), at(-40), at(-39))
	var raw = batch
	pg := "Home.dc.html"
	l.RecordTimeline("site", &pg, &raw)

	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	run := func(args ...string) result {
		var out, errs bytes.Buffer
		code := Run(args, Env{Getenv: func(k string) string { return env[k] }, Home: t.TempDir(), Session: func() string { return "" },
			Cwd: "/w", Stdout: &out, Stderr: &errs, Now: func() time.Time { return base }})
		return result{code, out.String(), errs.String()}
	}
	clock := func(sec int) string { return base.Add(time.Duration(sec) * time.Second).Format("15:04:05") }

	r := run("design", "timeline", "site")
	if r.code != 0 || !strings.Contains(r.out, clock(-60)+`.000  click    button.buy "Buy" (5,6)`) ||
		!strings.Contains(r.out, clock(-50)+".000  scroll   y 10 -> 400 of 900 (2 moves, 1.0 s)") ||
		!strings.Contains(r.out, clock(-39)+".000  request  GET /api/x failed 30 ms") {
		t.Fatalf("timeline: %d %s %s", r.code, r.out, r.errs)
	}
	r = run("design", "timeline", "site", "--around", clock(-40), "--window", "2s")
	if r.code != 0 || !strings.Contains(r.out, "longtask 350 ms") || strings.Contains(r.out, "click") {
		t.Fatalf("around: %d %s %s", r.code, r.out, r.errs)
	}
	r = run("design", "timeline", "site", "--from", "-1m", "--to", "-45s")
	if r.code != 0 || !strings.Contains(r.out, "click") || strings.Contains(r.out, "longtask") {
		t.Fatalf("from/to: %d %s %s", r.code, r.out, r.errs)
	}
	r = run("design", "moment", "site", "-40s")
	if r.code != 0 || !strings.Contains(r.out, "Page: Home.dc.html") || !strings.Contains(r.out, "Viewport: 390x844 @3x") ||
		!strings.Contains(r.out, "longtask 350 ms") || !strings.Contains(r.out, "Reading: 1 failed requests; 1 long tasks, worst 350 ms") {
		t.Fatalf("moment: %d %s %s", r.code, r.out, r.errs)
	}
	r = run("design", "moment", "site", clock(-60), "--window", "1")
	if r.code != 0 || !strings.Contains(r.out, "(window +-1s)") || !strings.Contains(r.out, "  "+clock(-60)+`.000  click`) {
		t.Fatalf("moment by clock: %d %s %s", r.code, r.out, r.errs)
	}
	if r := run("--json", "design", "timeline", "site"); r.code != 0 || !strings.Contains(r.out, `"events"`) {
		t.Fatalf("json: %d %s", r.code, r.out)
	}
	for _, args := range [][]string{{"design", "timeline"}, {"design", "moment", "site"}, {"design", "moment", "site", "soon"},
		{"design", "timeline", "site", "--from", "x"}, {"design", "timeline", "site", "--from", "-1m", "--to", "-2m"}} {
		if r := run(args...); r.code != 2 {
			t.Errorf("%v: %d %s", args, r.code, r.errs)
		}
	}
	if r := run("design", "timeline", "Bad id"); r.code != 1 {
		t.Errorf("bad id: %d %s", r.code, r.errs)
	}
}
