package agenteval

import (
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// A scenario's kit fixture is installed in the stand the way a kit is installed on a machine.
func TestInstallKitFixture(t *testing.T) {
	dir := t.TempDir()
	if err := installKit(filepath.Join("..", "..", "evals", "agent", "fixtures", "kits", "acme-ui"), dir); err != nil {
		t.Fatal(err)
	}
	if designkit.Resolve(dir, "acme-ui", "0.1.0", []string{"design-system.json"}) == "" {
		t.Fatal("the kit is not installed")
	}
}
