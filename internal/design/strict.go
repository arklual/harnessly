package design

// The strict check of a project on a kit: designkit/strict.go holds the rules; this file says which files
// of a project it reads and when it is on.
//
// It is on when the person asks (`design check --strict`), when the project says so (meta/project.json
// "strict": true) or when the kit does (its record's "strict": true). It reads, under project/:
//
//	pages/<name>.tsx            page modules: the kit's components and local components only
//	any other .tsx / .jsx       the project's local components: their own HTML is allowed, their values are tokens
//	*.dc.html written by hand   boards: <x-import> of the kit, styles from tokens (a board built from a page module is
//	                            checked through its source)
//
// Fixtures (*.fixtures.*) are data and are not read.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// StrictReport is what the strict check found in a project.
type StrictReport struct {
	Project string `json:"project"`
	Kit     string `json:"kit"`
	Version string `json:"version"`
	// On says why strict mode applies to the project without the flag: "project", "kit" or "" (asked for).
	On       string                    `json:"on,omitempty"`
	Checked  []string                  `json:"checked"`
	Findings []designkit.StrictFinding `json:"findings"`
}

const strictMaxFile = 4 << 20

// StrictOn says whether the project is checked in strict mode by its own setting ("project") or its kit's ("kit").
func (l *Library) StrictOn(project string) string {
	if o, ok := readSmallJSON(filepath.Join(l.MetaDir(project), MetaProjectFile)); ok {
		if on, _ := get(o, "strict").(bool); on {
			return "project"
		}
	}
	if c, err := l.strictCanvas(project); err == nil && c.Strict {
		return "kit"
	}
	return ""
}

// strictCanvas is the kit the project is pinned to, as assembled with its bases, for the theme the project mounts.
func (l *Library) strictCanvas(project string) (*designkit.Canvas, error) {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return nil, errors.New("the project is not on a kit (meta/project.json has no kit): strict mode checks a project against its kit")
	}
	version := pin.Version
	if pin.Draft {
		version = designkit.DraftVersion
	}
	theme := ""
	for _, t := range designkit.Themes(l.KitsDir(), pin.ID, version) {
		for _, m := range pin.Mounts {
			if t.ID == m || t.Folder == "themes/"+m {
				theme = t.ID
			}
		}
		if theme != "" {
			break
		}
	}
	c, err := designkit.ReadCanvas(l.KitsDir(), pin.ID, version, theme)
	if err != nil {
		return nil, errors.New("the kit " + pin.ID + " " + version + " the project is pinned to cannot be read here: " + err.Error())
	}
	return c, nil
}

// CheckStrict runs the strict check on a project; only, when set, names the pages (by name) and boards (by file,
// with or without .dc.html) to read.
func (l *Library) CheckStrict(project string, only []string) (*StrictReport, error) {
	if !IsLibraryID(project) {
		return nil, errors.New("unknown project " + project)
	}
	dir := l.ProjectDirOf(project)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, errors.New("no project " + project + " in the library")
	}
	c, err := l.strictCanvas(project)
	if err != nil {
		return nil, err
	}
	k := designkit.NewStrictKit(c)
	rep := &StrictReport{Project: project, Kit: c.Kit, Version: c.Version, On: l.StrictOn(project), Checked: []string{}, Findings: []designkit.StrictFinding{}}
	want := func(rel string) bool {
		if len(only) == 0 {
			return true
		}
		base := strings.TrimSuffix(strings.TrimSuffix(rel, ".dc.html"), ".tsx")
		for _, o := range only {
			o = strings.TrimSuffix(strings.TrimSuffix(o, ".dc.html"), ".tsx")
			if o == base || pagesDirname+"/"+o == base {
				return true
			}
		}
		return false
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	_ = filepath.WalkDir(realDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(realDir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (rel == "ds" || rel == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	local := func(from string) bool {
		from = strings.TrimPrefix(from, "./")
		if from == "" || strings.HasPrefix(from, "/") || strings.Contains(from, "://") {
			return false
		}
		clean := filepath.ToSlash(filepath.Clean(from))
		if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "ds/") {
			return false
		}
		_, err := os.Stat(filepath.Join(realDir, filepath.FromSlash(clean)))
		return err == nil
	}
	for _, rel := range files {
		ext := filepath.Ext(rel)
		switch {
		case strings.Contains(filepath.Base(rel), pageFixtureEx+"."):
			continue
		case strings.HasSuffix(rel, ".dc.html"):
			if _, built := l.BoardIsBuilt(project, rel); built || !want(rel) {
				continue
			}
			src, ok := readStrictFile(filepath.Join(realDir, rel))
			if !ok {
				continue
			}
			rep.Checked = append(rep.Checked, rel)
			rep.Findings = append(rep.Findings, k.CheckBoard(rel, src, local)...)
		case ext == ".tsx" || ext == ".jsx":
			isPage := strings.HasPrefix(rel, pagesDirname+"/") && strings.Count(rel, "/") == 1 && ext == pageSrcExt
			if len(only) > 0 && (!isPage || !want(rel)) {
				continue
			}
			src, ok := readStrictFile(filepath.Join(realDir, rel))
			if !ok {
				continue
			}
			rep.Checked = append(rep.Checked, rel)
			rep.Findings = append(rep.Findings, k.CheckModule(rel, src, !isPage)...)
		}
	}
	return rep, nil
}

func readStrictFile(p string) (string, bool) {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > strictMaxFile {
		return "", false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(raw), true
}
