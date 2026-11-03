package hlcli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/docs"
)

// docsPage is a page of the guide, listed by the docs package (the same list the runtime's page shows).
type docsPage = docs.Page

// docsPages lists the guide in English, in reading order.
func docsPages(guide fs.FS) ([]docsPage, error) { return docs.Pages(guide, docs.LocaleEN) }

// appDocs prints the list of pages or one page, or writes the guide and the example packages into a folder, where they
// can be searched and copied. It needs no runtime, no network and no token, so it works inside the wall of a chat with apps.
func (c *cli) appDocs(args []string) error {
	return c.appDocsFrom(docs.Guide, args)
}

func (c *cli) appDocsFrom(guide fs.FS, args []string) error {
	pages, err := docsPages(guide)
	if err != nil {
		return fail("the guide: %v", err)
	}
	if len(args) == 0 {
		c.out("The guide to writing harnessly apps and plugins, built into this binary.\n\n")
		for _, p := range pages {
			c.out("  %-14s %s\n", p.Name, p.Title)
		}
		c.out("\nharnessly app docs PAGE         print a page\n")
		c.out("harnessly app docs export DIR   write the pages and the example packages into DIR, to search and copy\n")
		return nil
	}
	if args[0] == "export" {
		if len(args) != 2 {
			return usageErr("app docs export needs a folder: harnessly app docs export ./harnessly-docs")
		}
		return c.appDocsExport(guide, pages, c.appDir(args[1]))
	}
	if len(args) > 1 {
		return usageErr("app docs takes one page")
	}
	for _, p := range pages {
		if p.Name == args[0] {
			raw, err := fs.ReadFile(guide, p.File)
			if err != nil {
				return fail("the guide: %v", err)
			}
			c.out("%s", raw)
			return nil
		}
	}
	names := make([]string, 0, len(pages))
	for _, p := range pages {
		names = append(names, p.Name)
	}
	return usageErr("no page %q; the pages: %s", args[0], strings.Join(names, ", "))
}

// appDocsExport refuses a folder that has something in it: the export writes over nothing of the person's.
func (c *cli) appDocsExport(guide fs.FS, pages []docsPage, dir string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fail("%s is not empty", dir)
	}
	write := func(to, from string) error {
		raw, err := fs.ReadFile(guide, from)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, filepath.FromSlash(to))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o644)
	}
	for _, p := range pages {
		if err := write(p.File, p.File); err != nil {
			return fail("%v", err)
		}
	}
	for _, root := range []string{"apps", "ru"} {
		err := fs.WalkDir(guide, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			return write(p, p)
		})
		if err != nil {
			return fail("%v", err)
		}
	}
	c.out("wrote the guide (build-apps/, apps.md, ru/) and the example packages (apps/) to %s\n", dir)
	return nil
}
