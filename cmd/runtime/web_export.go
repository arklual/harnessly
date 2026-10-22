package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/web"
)

// webExport writes the page this runtime would serve into dir: its files as they are and index.html with the base, the
// API prefixes and the page settings of this environment filled in. A proxy in front of the runtimes can then serve the
// page itself, the same build as the runtime, while the machine behind it is down. `runtime web export <dir>`.
func webExport(dir string) error {
	files := web.Dist()
	if src := os.Getenv("RUNTIME_WEB_DIR"); src != "" {
		files = os.DirFS(src)
	}
	if files == nil {
		return errors.New("this runtime carries no page (build with -tags webui or set RUNTIME_WEB_DIR)")
	}
	c := newWebConfig(files)
	page, err := c.page()
	if err != nil {
		return fmt.Errorf("index.html: %w", err)
	}
	err = fs.WalkDir(files, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out := filepath.Join(dir, filepath.FromSlash(name))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if name == "index.html" {
			return os.WriteFile(out, page, 0o644)
		}
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		return os.WriteFile(out, raw, 0o644)
	})
	return err
}

// subcommand runs `runtime web export <dir>`; false when the arguments name no subcommand and the runtime should serve
// (arguments it never took before are left alone as they were).
func subcommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "web" {
		return false, nil
	}
	if len(args) == 3 && args[1] == "export" {
		return true, webExport(args[2])
	}
	return true, errors.New("usage: runtime web export <dir>")
}
