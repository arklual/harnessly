package hlcli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/designkit"
)

// harnessly design pack export|import: Sinopia projects together with the UI kits they are pinned to, as one file
// Both work on the library and the kits folder themselves, they do not need the runtime to be up.

const packHelp = `harnessly design pack export <project…> [--all] [--kit ID[@VERSION]…] --out FILE [--accept-restricted] [--library DIR] [--kits DIR] [--json]
harnessly design pack import <file> [--dry-run] [--library DIR] [--kits DIR] [--json]

One file (tar.gz) with projects and the kits they are pinned to, to carry to another harnessly (a closed network).

  export    packs the named projects (or every project with --all), and every kit version they are pinned to with the
            kits those need. --kit adds a kit on its own (the newest version without @VERSION). The pack starts with
            sinopia-pack.json: the format version, the projects, the kits, every file with its size and sha256.
            Service folders, files that look like credentials and an agent's instructions stay out. A kit with
            license-restricted files (a corporate font) is packed only after --accept-restricted; the files are named.
  import    checks the manifest against the bytes, then adds what is missing. --dry-run only reports: which projects
            and kits would be added, which are there already, which conflict. A kit version that is installed with
            other content is never overwritten: that refuses the whole import and writes nothing. A project keeps its
            page ids and its kit pin; a project id that is taken gets a new id (the report says which). The same pack
            again changes nothing. A pack from a newer format is refused with a message. Nothing goes out to the network.

The library is --library, else $RUNTIME_DESIGN_LIBRARY_DIR, else design-artifacts in $RUNTIME_WORKSPACE_DIR; the kits
folder is --kits, else $RUNTIME_DESIGN_KITS_DIR, else design-kits in the data folder.
`

func (c *cli) designPack(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		c.out("%s", packHelp)
		return nil
	}
	sub := args[0]
	var pos, kits []string
	var library, kitsDir, out string
	var all, dry, accept bool
	for i := 1; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", usageErr("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "all":
			all = true
		case "dry-run":
			dry = true
		case "accept-restricted":
			accept = true
		case "json":
			c.json = true
		case "library":
			library, err = value()
		case "kits", "dir":
			kitsDir, err = value()
		case "out", "o":
			out, err = value()
		case "kit":
			var k string
			if k, err = value(); err == nil {
				kits = append(kits, k)
			}
		default:
			err = usageErr("unknown option %s", a)
		}
		if err != nil {
			return err
		}
	}
	lib, err := c.libraryDir(library)
	if err != nil {
		return err
	}
	if kitsDir == "" {
		kitsDir = strings.TrimSpace(c.env.Getenv("RUNTIME_DESIGN_KITS_DIR"))
	}
	if kitsDir == "" {
		kitsDir = designkit.DefaultDir(c.dataDir())
	}
	if !filepath.IsAbs(kitsDir) {
		kitsDir = filepath.Join(c.env.Cwd, kitsDir)
	}
	l := design.New(design.Config{LibraryDir: lib, KitsDir: kitsDir, Texts: design.Texts{CLI: design.GuardCLI()}})
	switch sub {
	case "export":
		return c.packExport(l, pos, kits, out, all, accept)
	case "import":
		if len(pos) != 1 {
			return usageErr("design pack import takes one file: harnessly design pack import ./portable.tar.gz [--dry-run]")
		}
		return c.packImport(l, pos[0], dry)
	}
	return usageErr("unknown design pack command %q (export, import)", sub)
}

func (c *cli) packExport(l *design.Library, projects, kits []string, out string, all, accept bool) error {
	if out == "" {
		return usageErr("design pack export needs --out FILE")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(c.env.Cwd, out)
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".pack-*")
	if err != nil {
		return fail("%v", err)
	}
	defer os.Remove(tmp.Name())
	m, err := l.PackExport(context.Background(), design.PackExportOptions{Projects: projects, All: all, Kits: kits, AcceptRestricted: accept}, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	var restricted *design.PackRestrictedError
	if errors.As(err, &restricted) {
		c.out("These kit files have a restricted license:\n")
		for _, r := range restricted.Restricted {
			c.out("  %s: %s\n", r.Path, r.Reason)
		}
		return fail("%v: run again with --accept-restricted", err)
	}
	if err != nil {
		return fail("%v", err)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return fail("%v", err)
	}
	if c.json {
		raw, _ := json.MarshalIndent(m, "", "  ")
		c.out("%s\n", raw)
		return nil
	}
	c.out("%s: %d projects, %d kits, %d files, sha256 %s\n", out, len(m.Projects), len(m.Kits), len(m.Files), m.SHA256)
	for _, p := range m.Projects {
		kit := ""
		if p.Kit != nil {
			kit = "  kit " + p.Kit.ID + " " + p.Kit.Version
		}
		c.out("  project %s: %d files%s\n", p.ID, p.Files, kit)
	}
	for _, k := range m.Kits {
		c.out("  kit %s %s\n", k.ID, k.Version)
	}
	for _, w := range m.Warnings {
		c.out("  warning: %s\n", w)
	}
	return nil
}

func (c *cli) packImport(l *design.Library, file string, dry bool) error {
	if !filepath.IsAbs(file) {
		file = filepath.Join(c.env.Cwd, file)
	}
	f, err := os.Open(file)
	if err != nil {
		return fail("%v", err)
	}
	defer f.Close()
	res, err := l.PackImport(f, design.PackImportOptions{DryRun: dry})
	if c.json {
		raw, _ := json.MarshalIndent(res, "", "  ")
		c.out("%s\n", raw)
	} else if res.Manifest != nil {
		mode := ""
		if dry {
			mode = " (dry run: nothing written)"
		}
		c.out("Pack of %s%s\n", res.Manifest.ExportedAt, mode)
		for _, k := range res.Kits {
			c.out("  kit %s %s: %s\n", k.ID, k.Version, k.Status)
			if k.Diff != nil {
				c.out("    differs: %d added, %d changed, %d removed files\n", len(k.Diff.Added), len(k.Diff.Changed), len(k.Diff.Removed))
			}
		}
		for _, p := range res.Projects {
			as := ""
			if p.As != p.ID {
				as = " as " + p.As
			}
			c.out("  project %s%s: %s, %d files\n", p.ID, as, p.Status, p.Files)
		}
		for _, d := range res.Dropped {
			c.out("  not taken: %s\n", d)
		}
		for _, w := range res.Warnings {
			c.out("  warning: %s\n", w)
		}
	}
	if err != nil {
		return fail("%v", err)
	}
	return nil
}
