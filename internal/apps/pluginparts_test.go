package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginPartsRefusedBelowTrusted(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"hooks", "hooks/hooks.json", `{"hooks":{}}`},
		{"agents", "agents/x.md", "x"},
		{"styles", "output-styles/x.md", "x"},
		{"case", "Hooks/hooks.json", `{}`},
		{"file", "settings.json", `{}`},
		{"manifest", ".claude-plugin/plugin.json", `{"name":"acme-demo","hooks":"./h.json"}`},
	} {
		dir := scaffold(t, "acme.demo", "chat")
		full := filepath.Join(dir, tc.path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, ps := LoadPackage(dir)
		var found bool
		for _, p := range ps.Errors() {
			if strings.Contains(p.String(), "may not carry") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want a refusal, got %v", tc.name, ps)
		}
	}
	dir := scaffold(t, "acme.demo", "chat")
	_ = os.MkdirAll(filepath.Join(dir, "hooks"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "hooks/hooks.json"), []byte(`{}`), 0o644)
	if _, _, err := (&Registry{Root: filepath.Join(t.TempDir(), "apps")}).Install(dir, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err == nil {
		t.Error("install must refuse hooks/")
	} else if !strings.Contains(err.Error(), "hooks/") && !errors.Is(err, ErrInvalid) {
		t.Errorf("error does not name the folder: %v", err)
	}
}

func TestPluginPartsListedForTrusted(t *testing.T) {
	dir := scaffold(t, "acme.demo", "chat")
	m := filepath.Join(dir, ManifestPath)
	raw, _ := os.ReadFile(m)
	raw = []byte(strings.Replace(string(raw), `"trust": "declarative"`, `"trust": "trusted"`, 1))
	if err := os.WriteFile(m, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "hooks"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "hooks/hooks.json"), []byte(`{}`), 0o644)
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		t.Fatalf("trusted may carry hooks: %v", ps)
	}
	if len(pkg.PluginParts) != 1 || pkg.PluginParts[0] != "hooks/" {
		t.Errorf("parts: %v", pkg.PluginParts)
	}
}

func TestFieldsTheHostDoesNotRunAreRefused(t *testing.T) {
	base := func(extra string) string {
		return `{"id":"acme.x","name":"X","description":"d","version":"1.0.0","harnessly":">=0.17","trust":"declarative","targets":["chat"],` + extra + `"permissions":[],"agent":{"instructions":"i.md"}}`
	}
	for _, tc := range []struct{ name, extra, path string }{
		{"icon", `"icon":"x.svg",`, "icon"},
		{"storage", `"storage":{},`, "storage"},
		{"backend", `"backend":{"url":"https://x.y"},`, "backend"},
		{"template", `"activation":{"on":["manual"],"template":"t"},`, "activation.template"},
		{"view", `"activation":{"on":["view"]},`, "activation.on[0]"},
	} {
		_, ps := ParseManifest([]byte(base(tc.extra)))
		if p := problemAt(ps, tc.path); p == nil || !strings.Contains(p.Message, "not supported yet") {
			t.Errorf("%s: %v", tc.name, ps)
		}
	}
	// The top-level `when` is refused with the way to a condition: rules by the document's status.
	if _, ps := ParseManifest([]byte(base(`"when":"a",`))); problemAt(ps, "when") == nil || !strings.Contains(problemAt(ps, "when").Message, "agent.tools.when") {
		t.Errorf("when: %v", ps)
	}
	for _, perm := range []string{"users.read", "sinopia.export"} {
		_, ps := ParseManifest([]byte(strings.Replace(base(""), `"permissions":[]`, `"permissions":["`+perm+`"]`, 1)))
		if p := problemAt(ps, "permissions[0]"); p == nil || !strings.Contains(p.Message, "not supported yet") {
			t.Errorf("%s: %v", perm, ps)
		}
	}
	_, ps := ParseManifest([]byte(strings.Replace(base(""), `"instructions":"i.md"`, `"instructions":"i.md","flow":"f.json"`, 1)))
	if p := problemAt(ps, "agent.flow"); p == nil {
		t.Errorf("flow: %v", ps)
	}
}
