package designkit

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// scaffold is the kit `harnessly design kit init` starts from: every file a kit needs, a provider (Root) with light
// and dark modes, a plain component (Button) and a compound one (Card with its parts, drawn by elements in its sample).
// It passes Check with no problem at all (TestInitPassesTheCheck): an agent starts from a valid kit and keeps it valid.
//
//go:embed scaffold
var scaffold embed.FS

// InitOptions name the new kit.
type InitOptions struct {
	ID        string // lower case, digits, -
	Namespace string // the window global and the boards' prefix
	Title     string
	Source    string // library or draft
}

// Init writes a new kit into dir, which must be missing or empty.
func Init(dir string, o InitOptions) ([]string, error) {
	if !ValidID(o.ID) {
		return nil, fmt.Errorf("id %q: lower case letters, digits and -", o.ID)
	}
	if !namespaceRe.MatchString(o.Namespace) {
		return nil, fmt.Errorf("namespace %q is not a JavaScript name", o.Namespace)
	}
	if o.Source != "library" && o.Source != "draft" {
		return nil, fmt.Errorf("source %q: library or draft", o.Source)
	}
	if o.Title == "" {
		o.Title = o.Namespace
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	fill := strings.NewReplacer("__ID__", o.ID, "__NS__", o.Namespace, "__TITLE__", o.Title, "__SOURCE__", o.Source)
	var wrote []string
	err := fs.WalkDir(scaffold, "scaffold", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := scaffold.ReadFile(p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "scaffold/")
		file := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		wrote = append(wrote, path.Clean(rel))
		return os.WriteFile(file, []byte(fill.Replace(string(raw))), 0o644)
	})
	return wrote, err
}
