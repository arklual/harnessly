package hlcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityPrepareDoesNotInstall(t *testing.T) {
	parent := t.TempDir()
	first, err := PrepareAntigravityPlugin(parent, "/opt/harnessly")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareAntigravityPlugin(parent, "/opt/harnessly")
	if err != nil || first == second {
		t.Fatal("preparation overwrote existing package")
	}
	for _, name := range []string{"plugin.json", "hooks.json", "mcp_config.json"} {
		raw, err := os.ReadFile(filepath.Join(first, name))
		if err != nil || !json.Valid(raw) {
			t.Fatalf("invalid plugin asset %s", name)
		}
		if strings.Contains(string(raw), "Bearer") || strings.Contains(string(raw), "cli-token") {
			t.Fatal("credential-bearing plugin config")
		}
	}
	var manifest struct {
		Name string `json:"name"`
	}
	raw, _ := os.ReadFile(filepath.Join(first, "plugin.json"))
	_ = json.Unmarshal(raw, &manifest)
	if manifest.Name != "harnessly-native" {
		t.Fatal("unexpected plugin namespace")
	}
	if _, err := PrepareAntigravityPlugin(parent, "relative-harnessly"); err == nil {
		t.Fatal("relative executable accepted")
	}
}
