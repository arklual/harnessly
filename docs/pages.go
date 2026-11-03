package docs

import (
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Locales are the languages the guide is written in; English is the source and the fallback of any page.
const (
	LocaleEN = "en"
	LocaleRU = "ru"
)

// Page is a page of the guide: its name (on the command line, in the address of the page), its file in the embedded docs,
// its title and its headings.
type Page struct {
	Name     string
	File     string
	Title    string
	Headings []string
}

// Pages lists the guide in reading order (its files are numbered), and last the reference. The English files make the
// list; for another locale a page is that locale's file when it has one, else the English one.
func Pages(guide fs.FS, locale string) ([]Page, error) {
	files, err := fs.Glob(guide, "build-apps/*.md")
	if err != nil {
		return nil, err
	}
	// The overview first, then the numbered pages, then the reference.
	sort.Slice(files, func(i, j int) bool {
		a, b := path.Base(files[i]), path.Base(files[j])
		if (a == "README.md") != (b == "README.md") {
			return a == "README.md"
		}
		return a < b
	})
	files = append(files, "apps.md")
	pages := make([]Page, 0, len(files))
	for _, file := range files {
		raw, chosen, err := readLocalized(guide, locale, file)
		if err != nil {
			return nil, err
		}
		title, headings := outline(string(raw))
		pages = append(pages, Page{Name: PageName(file), File: chosen, Title: title, Headings: headings})
	}
	return pages, nil
}

// readLocalized reads the file of the locale, else the English one; it says which it read.
func readLocalized(guide fs.FS, locale, file string) ([]byte, string, error) {
	if locale != "" && locale != LocaleEN && !strings.ContainsAny(locale, "/\\.") {
		local := locale + "/" + file
		if raw, err := fs.ReadFile(guide, local); err == nil {
			return raw, local, nil
		}
	}
	raw, err := fs.ReadFile(guide, file)
	return raw, file, err
}

// PageName drops the folder, the reading-order number and the extension: build-apps/03-views.md is views.
func PageName(file string) string {
	name := strings.TrimSuffix(path.Base(file), ".md")
	switch {
	case file == "apps.md":
		return "reference"
	case name == "README":
		return "overview"
	}
	if num, rest, ok := strings.Cut(name, "-"); ok && strings.Trim(num, "0123456789") == "" {
		return rest
	}
	return name
}

// outline is the title (the first level-one heading) and the headings below it, the fenced code left out.
func outline(text string) (string, []string) {
	title := ""
	var headings []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if t, ok := strings.CutPrefix(line, "# "); ok {
			if title == "" {
				title = strings.TrimSpace(t)
			}
			continue
		}
		if t, ok := strings.CutPrefix(line, "## "); ok {
			headings = append(headings, strings.TrimSpace(t))
		} else if t, ok := strings.CutPrefix(line, "### "); ok {
			headings = append(headings, strings.TrimSpace(t))
		}
	}
	return title, headings
}

// Read is the Markdown of the page called name in the locale; false when there is no such page. The name is matched
// against the listed pages, so nothing the caller sends reaches the file system.
func Read(guide fs.FS, locale, name string) ([]byte, bool, error) {
	pages, err := Pages(guide, locale)
	if err != nil {
		return nil, false, err
	}
	for _, p := range pages {
		if p.Name == name {
			raw, err := fs.ReadFile(guide, p.File)
			return raw, err == nil, err
		}
	}
	return nil, false, nil
}
