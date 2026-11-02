package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scaffold(t *testing.T, id, template string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := Scaffold(dir, id, "", template); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScaffoldPassesValidate(t *testing.T) {
	for _, tpl := range Templates {
		dir := scaffold(t, "acme.demo-app", tpl)
		pkg, ps := LoadPackage(dir)
		if pkg == nil || len(ps) != 0 {
			t.Fatalf("%s: %v", tpl, ps)
		}
		if pkg.Manifest.Name != "Demo app" {
			t.Fatalf("%s: name %q", tpl, pkg.Manifest.Name)
		}
	}
	if err := Scaffold(t.TempDir(), "Bad Id", "", ""); err == nil {
		t.Fatal("want a bad id refused")
	}
	dir := scaffold(t, "acme.demo", "chat")
	if err := Scaffold(dir, "acme.demo", "", "chat"); err == nil {
		t.Fatal("want a folder that is not empty refused")
	}
}

func TestPackageFiles(t *testing.T) {
	dir := scaffold(t, "acme.demo", "chat")
	if err := os.Remove(filepath.Join(dir, "skills/how-to/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"local":{"command":"node","args":["x.js"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "leak")); err != nil {
		t.Fatal(err)
	}
	_, ps := LoadPackage(dir)
	for _, path := range []string{"agent.skills[0]", ".mcp.json:local", "leak"} {
		if problemAt(ps, path) == nil {
			t.Errorf("want a problem at %s, got %v", path, ps)
		}
	}
}

func TestPackageRefusesEscape(t *testing.T) {
	p := &Package{Dir: t.TempDir()}
	for _, rel := range []string{"../x", "/etc/passwd", "a/../../x"} {
		if _, err := p.inside(rel); err == nil {
			t.Errorf("%s: want refused", rel)
		}
	}
}

func TestRegistryInstallCopyAndRemove(t *testing.T) {
	data := t.TempDir()
	r := &Registry{Root: filepath.Join(data, "apps")}
	src := scaffold(t, "acme.demo", "chat")

	got, ps, err := r.Install(src, InstallOptions{HostVersion: "0.17.0"})
	if err != nil || len(ps) != 0 {
		t.Fatalf("install: %v %v", err, ps)
	}
	want := filepath.Join(data, "apps", "packages", "acme.demo", "0.1.0")
	if got.Dir != want || got.Dev {
		t.Fatalf("installed at %s dev=%v", got.Dir, got.Dev)
	}
	if _, err := os.Stat(filepath.Join(want, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("the copy has no plugin manifest")
	}

	// An edit of the author's folder does not reach the copy.
	if err := os.WriteFile(filepath.Join(src, "instructions.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(want, "instructions.md")); strings.Contains(string(raw), "changed") {
		t.Fatal("the copy follows the author's folder")
	}

	// A new version replaces the old one and drops its copy.
	manifest := filepath.Join(src, ManifestPath)
	raw, _ := os.ReadFile(manifest)
	if err := os.WriteFile(manifest, []byte(strings.Replace(string(raw), `"0.1.0"`, `"0.2.0"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Install(src, InstallOptions{HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	list, _ := r.List()
	if len(list) != 1 || list[0].Version != "0.2.0" || list[0].InstalledAt != got.InstalledAt {
		t.Fatalf("list after update: %+v", list)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("the old version's copy is left")
	}

	// Reopened from disk, the registry knows the app.
	again := &Registry{Root: r.Root}
	if a, err := again.Get("acme.demo"); err != nil || a.Manifest == nil || a.Manifest.Name != "Demo" {
		t.Fatalf("reread: %+v %v", a, err)
	}

	if err := r.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("acme.demo"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("after remove: %v", err)
	}
	if err := r.Remove("acme.demo"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("remove twice: %v", err)
	}
}

func TestRegistryDevInstall(t *testing.T) {
	r := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	src := scaffold(t, "acme.demo", "pane-chat")
	got, _, err := r.Install(src, InstallOptions{Dev: true, HostVersion: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(src)
	if got.Dir != abs || !got.Dev {
		t.Fatalf("dev install at %s", got.Dir)
	}
	if err := r.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, ManifestPath)); err != nil {
		t.Fatal("remove of a dev install deleted the author's folder")
	}
}

func TestRegistryRefuses(t *testing.T) {
	r := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
	src := scaffold(t, "acme.demo", "chat")
	if _, _, err := r.Install(src, InstallOptions{HostVersion: "0.16.31"}); !errors.Is(err, ErrHostVersion) {
		t.Fatalf("old host: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, ManifestPath), []byte(`{"id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ps, err := r.Install(src, InstallOptions{HostVersion: "0.17.0"})
	if !errors.Is(err, ErrInvalid) || len(ps.Errors()) == 0 {
		t.Fatalf("broken package: %v %v", err, ps)
	}
	if list, _ := r.List(); len(list) != 0 {
		t.Fatal("a refused install is listed")
	}
}

// The example apps of docs/apps pass the strict check.
func TestExampleApps(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("..", "..", "docs", "apps", "*", ManifestPath))
	if len(dirs) == 0 {
		t.Fatal("no example apps")
	}
	for _, m := range dirs {
		dir := filepath.Dir(filepath.Dir(m))
		pkg, ps := LoadPackage(dir)
		if pkg == nil || len(ps) != 0 {
			t.Errorf("%s: %v", dir, ps)
		}
	}
	// Cashback carries a view: its pane entry loads, and it speaks through classic scripts (a module script cannot carry
	// the slot's cookie from the view's opaque origin).
	pkg, ps := LoadPackage(filepath.Join("..", "..", "docs", "apps", "cashback"))
	if pkg == nil || len(ps) != 0 || len(pkg.Manifest.Views) != 2 || !pkg.Manifest.HasTarget(TargetApp) {
		t.Fatalf("cashback: %v", ps)
	}
	page, err := pkg.File("ui/index.html")
	if err != nil || !strings.Contains(string(page), `src="/__hl/app-sdk.js"`) || strings.Contains(string(page), `type="module"`) {
		t.Fatalf("cashback page: %v", err)
	}
}

// A generated view loads the SDK as a classic script and ships its own script file.
func TestScaffoldViewIsClassic(t *testing.T) {
	dir := scaffold(t, "acme.demo", "pane-chat")
	page, err := os.ReadFile(filepath.Join(dir, "ui", "index.html"))
	if err != nil || !strings.Contains(string(page), `<script src="/__hl/app-sdk.js"></script>`) || strings.Contains(string(page), "module") && strings.Contains(string(page), `type="module"`) {
		t.Fatalf("page: %v %s", err, page)
	}
	if _, err := os.Stat(filepath.Join(dir, "ui", "main.js")); err != nil {
		t.Fatal(err)
	}
}
