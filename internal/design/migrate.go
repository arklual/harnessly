package design

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Migration of a library to meta/: every project without it gets meta/project.json and a page per board, built from
// exactly what the listing shows today (artifact.json's list with the other board files, else canvas.json, else the
// files), in the same order, with the same titles and sizes. Board files and assets are never touched; the only files
// written are meta/ and the two built from it (project/canvas.json, artifact.json). A page's id is derived from the
// project and the board path, so the same library migrated twice, or on two machines that share it, gets the same
// ids and the same bytes. A project that has meta/ already only gets the built files brought up to date.

// MigratedPage is a page a migration makes.
type MigratedPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	File  string `json:"file"`
}

// ProjectMigration is what a migration did, or would do, to one project.
type ProjectMigration struct {
	ID string `json:"id"`
	// Status: "migrate" (a dry run would migrate it), "migrated", "current" (meta/ was there), "skipped" (no
	// project/ folder) or "error".
	Status string         `json:"status"`
	Pages  []MigratedPage `json:"pages"`
	// Writes are the files written (or that would be), relative to the library.
	Writes []string `json:"writes"`
	Error  string   `json:"error,omitempty"`
}

const migrationSeed = "harnessly-design-page\x00"

// migratedPageID is the id a migration gives a board: the time of the project's creation plus its position (so the
// ids sort in the list's order), the entropy a hash of the project and the board path.
func migratedPageID(ref, path string, base int64, at int) string {
	sum := sha256.Sum256([]byte(migrationSeed + ref + "\x00" + path))
	var e [10]byte
	copy(e[:], sum[:10])
	return ULID(base+int64(at), e)
}

func parseCreated(s string) int64 {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil && t.UnixMilli() > 0 {
			return t.UnixMilli()
		}
	}
	return 0
}

// createdOf is when a project without meta/ was made, as its files say: artifact.json's importedAt, else canvas.json's
// createdOnFiles.at; "" when neither says.
func (l *Library) createdOf(ref string) string {
	if o, ok := readJSON(filepath.Join(l.ArtifactDir(ref), ArtifactFile)).(*jsvalue.Object); ok {
		if s, ok := CleanText(get(o, "importedAt"), 40); ok && parseCreated(s) > 0 {
			return s
		}
	}
	if o, ok := readJSON(filepath.Join(l.ProjectDir(ref), canvasJSONName)).(*jsvalue.Object); ok {
		if s, ok := CleanText(get(o.Object("createdOnFiles"), "at"), 40); ok && parseCreated(s) > 0 {
			return s
		}
	}
	return ""
}

// MigrationModel is the model a project without meta/ would get.
func (l *Library) MigrationModel(ref string) *ProjectModel {
	project := l.ProjectDirOf(ref)
	if project == "" || !isRealDir(project) {
		return nil
	}
	boards, title, source := l.legacyListing(ref, project)
	expand := map[string]bool{}
	if o, ok := readJSON(filepath.Join(project, canvasJSONName)).(*jsvalue.Object); ok {
		if bs := o.Object("boards"); bs != nil {
			for _, k := range bs.Keys() {
				if e := bs.Object(k); e != nil && e.String("expand") == "fill" {
					expand[k] = true
				}
			}
		}
	}
	created := l.createdOf(ref)
	base := parseCreated(created)
	m := &ProjectModel{Meta: ProjectMeta{Title: title, Source: source, CreatedAt: created, Release: LegacyRelease, Kits: []any{}, CanvasKey: l.canvasKey()}, Pages: []Page{}}
	seen := map[string]bool{}
	for i, b := range boards {
		if seen[b.Path] {
			continue
		}
		seen[b.Path] = true
		order := float64(len(m.Pages) + 1)
		p := Page{ID: migratedPageID(ref, b.Path, base, i), Title: b.Title, File: b.Path, W: b.W, H: b.H, Order: &order}
		if expand[b.Path] {
			fill := "fill"
			p.Expand = &fill
		}
		m.Pages = append(m.Pages, p)
	}
	return m
}

func (l *Library) rel(p string) string {
	if r, err := filepath.Rel(l.Root(), p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// MigrateProject migrates one project (see the top of the file); dryRun writes nothing.
func (l *Library) MigrateProject(ref string, dryRun bool) ProjectMigration {
	out := ProjectMigration{ID: ref, Pages: []MigratedPage{}, Writes: []string{}}
	fail := func(err error) ProjectMigration {
		out.Status, out.Error = "error", err.Error()
		return out
	}
	if !IsLibraryID(ref) || !isRealDir(l.ProjectDir(ref)) {
		out.Status = "skipped"
		return out
	}
	mu := l.lockOf("boards\x00" + ref)
	mu.Lock()
	defer mu.Unlock()
	m := l.ReadModel(ref)
	if m != nil {
		out.Status = "current"
	} else {
		if _, err := os.Lstat(l.MetaDir(ref)); err == nil {
			return fail(&PageError{Msg: l.rel(l.MetaDir(ref)) + " is there without " + MetaProjectFile + ": look at it by hand", Status: 409})
		}
		m = l.MigrationModel(ref)
		if m == nil {
			out.Status = "skipped"
			return out
		}
		out.Status = "migrate"
		for _, p := range m.Pages {
			out.Pages = append(out.Pages, MigratedPage{ID: p.ID, Title: p.Title, File: p.File})
			out.Writes = append(out.Writes, l.rel(filepath.Join(l.pagesDir(ref), p.ID+".json")))
		}
		out.Writes = append(out.Writes, l.rel(filepath.Join(l.MetaDir(ref), MetaProjectFile)))
		if !dryRun {
			// The pages first and project.json last: until it is there the project is read as before, and a run that
			// stopped half way writes the same pages again.
			for _, p := range m.Pages {
				if _, err := l.writePage(ref, p); err != nil {
					return fail(err)
				}
			}
			if _, err := l.writeProjectMeta(ref, m.Meta); err != nil {
				return fail(err)
			}
			out.Status = "migrated"
			l.listing.invalidatePath(ref)
		}
	}
	for _, w := range l.derivedFiles(ref, m) {
		if !w.Changed {
			continue
		}
		out.Writes = append(out.Writes, l.rel(w.Path))
		if !dryRun {
			if _, err := writeIfChanged(w.Path, w.data); err != nil {
				return fail(err)
			}
		}
	}
	if !dryRun {
		l.listing.invalidatePath(ref)
	}
	return out
}

// Migrate migrates every project of the library, in the order of their folder names.
func (l *Library) Migrate(dryRun bool) []ProjectMigration {
	entries, _ := os.ReadDir(l.Root())
	var refs []string
	for _, e := range entries {
		if e.IsDir() && IsLibraryID(e.Name()) {
			refs = append(refs, e.Name())
		}
	}
	sort.Strings(refs)
	out := []ProjectMigration{}
	for _, ref := range refs {
		out = append(out, l.MigrateProject(ref, dryRun))
	}
	return out
}
