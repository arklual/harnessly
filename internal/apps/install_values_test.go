package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardsPackage is a package that names the installation's boards service: its MCP server and the host its views frame.
func boardsPackage(t *testing.T) string {
	t.Helper()
	dir := versioned(t, "acme.boards", "1.0.0")
	path := filepath.Join(dir, ManifestPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["network"] = map[string]any{"frames": []string{"${install:boardsUrl}"}}
	raw, _ = json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{"mcpServers": {"boards": {"type": "http", "url": "${install:boardsUrl}/mcp", "auth": {"type": "platform"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFillInstall(t *testing.T) {
	got, missing := FillInstall(`{"url": "${install:boardsUrl}/mcp", "x": "${install:artifactsUrl}", "y": "${secret:k}"}`,
		map[string]string{"boardsUrl": "https://boards.example"})
	if got != `{"url": "https://boards.example/mcp", "x": "${install:artifactsUrl}", "y": "${secret:k}"}` || len(missing) != 1 || missing[0] != "artifactsUrl" {
		t.Fatalf("%s %v", got, missing)
	}
	if got, _ := FillInstall(`"${install:a}"`, map[string]string{"a": `https://x.example/"q"`}); got != `"https://x.example/\"q\""` {
		t.Fatalf("a value goes in as the inside of a JSON string: %s", got)
	}
	if !installAt("${install:boardsUrl}", true) || installAt("${install:boardsUrl}/x", true) || !installAt("${install:boardsUrl}/x", false) ||
		installAt("https://a/${install:boardsUrl}", false) {
		t.Fatal("installAt")
	}
}

// A package may name the installation's address; checked as written, it is valid.
func TestAPackageMayNameTheInstallationsAddress(t *testing.T) {
	pkg, ps := LoadPackage(boardsPackage(t))
	if pkg == nil || len(ps.Errors()) > 0 {
		t.Fatalf("problems: %s", ps.Error())
	}
}

// The environment installs the app with the hub's address put in; without one the app is installed but not there for
// the person, and comes into sight once the hub has the address.
func TestEnvironmentAppWithAndWithoutTheInstallationsAddress(t *testing.T) {
	r := newRegistry(t)
	dir := boardsPackage(t)
	opt := EnvInstallOptions{Link: "own", ItemID: "item-boards", HostVersion: TemplateVersion, Now: teamNow}

	got, changed, err := r.InstallForEnv(dir, opt)
	if err != nil || !changed || len(got.Missing) != 1 || got.Missing[0] != "boardsUrl" {
		t.Fatalf("without an address: %v %v %+v", err, changed, got.Missing)
	}
	if !r.EnvHasItem("own", "item-boards") {
		t.Fatal("the item's app is not recorded")
	}
	if list, err := r.List(); err != nil || len(list) != 0 {
		t.Fatalf("a not configured app is listed: %v %+v", err, list)
	}
	if all, err := r.ListAll(); err != nil || len(all) != 0 {
		t.Fatalf("a not configured app is in ListAll: %v %+v", err, all)
	}
	if _, err := r.Get("acme.boards"); err == nil {
		t.Fatal("a not configured app is found")
	}

	opt.Install = map[string]string{"boardsUrl": "https://boards.example"}
	got, changed, err = r.InstallForEnv(dir, opt)
	if err != nil || !changed || len(got.Missing) != 0 {
		t.Fatalf("with an address: %v %v %+v", err, changed, got.Missing)
	}
	if got.Manifest.Network == nil || len(got.Manifest.Network.Frames) != 1 || got.Manifest.Network.Frames[0] != "https://boards.example" {
		t.Fatalf("frames: %+v", got.Manifest.Network)
	}
	raw, err := os.ReadFile(filepath.Join(got.Dir, ".mcp.json"))
	if err != nil || !strings.Contains(string(raw), `"https://boards.example/mcp"`) {
		t.Fatalf(".mcp.json of the copy: %s %v", raw, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json")); !strings.Contains(string(b), "${install:boardsUrl}") {
		t.Fatal("the source package was changed")
	}
	list, err := r.List()
	if err != nil || len(list) != 1 || list[0].ID != "acme.boards" {
		t.Fatalf("a configured app is not listed: %v %+v", err, list)
	}
	if _, changed, _ := r.InstallForEnv(dir, opt); changed {
		t.Fatal("the same address installed it again")
	}
	// Another address installs it anew.
	opt.Install = map[string]string{"boardsUrl": "https://boards2.example"}
	if got, changed, err := r.InstallForEnv(dir, opt); err != nil || !changed || got.Manifest.Network.Frames[0] != "https://boards2.example" {
		t.Fatalf("a new address: %v %v", err, changed)
	}
	// An address the package cannot use (frames are https) is refused, not installed half-way.
	opt.Install = map[string]string{"boardsUrl": "http://boards.internal"}
	if _, _, err := r.InstallForEnv(dir, opt); err == nil {
		t.Fatal("an http address was installed")
	}
}
