// Package evals provides the same scenario catalog to an installed CLI as to a checkout.
package evals

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/design/ownengine"
)

//go:embed all:agent
var catalog embed.FS

// Extract materializes a private catalog for file-based setup and fixtures.
// embed.FS omits symlinks and file modes, so restore the board engine and launchers.
func Extract(dir string) error {
	src, err := fs.Sub(catalog, "agent")
	if err != nil {
		return err
	}
	if err := os.CopyFS(dir, src); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures/shop/project/support.js"), []byte(ownengine.Script), 0600); err != nil {
		return err
	}
	for _, name := range []string{"blender-lab/mock/blender", "blender-lab/mock/blender_wrap.sh", "blender-lab/mock/install.sh", "orch-long/scripts/crunch.sh", "orch-stop/check.sh"} {
		if err := os.Chmod(filepath.Join(dir, "fixtures", name), 0700); err != nil {
			return err
		}
	}
	return nil
}
