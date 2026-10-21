package design

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationOfALibraryCopy proves a migration on a copy of a real library before it is run on the library itself:
// DESIGN_MIGRATION_COPY=/tmp/x/lib-copy go test ./internal/design -run TestMigrationOfALibraryCopy -v
// The copy is migrated in place. A dry run must write nothing at all; the run must leave every file but meta/,
// artifact.json and project/canvas.json byte for byte as it was, the library must list the same projects and boards
// after it, and a second run must write nothing.
func TestMigrationOfALibraryCopy(t *testing.T) {
	dir := os.Getenv("DESIGN_MIGRATION_COPY")
	if dir == "" {
		t.Skip("DESIGN_MIGRATION_COPY names no library copy")
	}
	all := func() map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			sum := sha256.Sum256(b)
			rel, _ := filepath.Rel(dir, p)
			out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
			return nil
		})
		return out
	}
	allowed := func(rel string) bool {
		parts := strings.Split(rel, "/")
		return len(parts) >= 2 && IsLibraryID(parts[0]) && (parts[1] == MetaDirname || (len(parts) == 2 && parts[1] == ArtifactFile) ||
			(len(parts) == 3 && parts[1] == ProjectDirname && parts[2] == canvasJSONName))
	}
	l := New(Config{LibraryDir: dir, CanvasKey: os.Getenv("RUNTIME_DESIGN_CANVAS_KEY"), Texts: Texts{CLI: "harnessly"}})
	t.Cleanup(l.StopWatching)
	before := all()
	shown := listing(l)
	boards := 0
	for _, b := range shown {
		boards += len(b)
	}
	t.Logf("before: %d files, %d projects, %d boards listed", len(before), len(shown), boards)

	dry := l.Migrate(true)
	if after := all(); !jsonEqual(before, after) {
		t.Fatal("the dry run wrote something")
	}
	report := l.Migrate(false)
	if len(dry) != len(report) {
		t.Fatalf("the dry run saw %d projects, the run %d", len(dry), len(report))
	}
	for i := range dry {
		if !jsonEqual(dry[i].Pages, report[i].Pages) || !jsonEqual(dry[i].Writes, report[i].Writes) {
			t.Errorf("%s: the dry run promised %v %v, the run did %v %v", dry[i].ID, dry[i].Pages, dry[i].Writes, report[i].Pages, report[i].Writes)
		}
	}
	pages, writes := 0, 0
	for _, p := range report {
		pages += len(p.Pages)
		writes += len(p.Writes)
		t.Logf("%s: %s, %d pages, %d files written %s", p.ID, p.Status, len(p.Pages), len(p.Writes), p.Error)
		if p.Status == "error" {
			t.Errorf("%s: %s", p.ID, p.Error)
		}
	}
	after := all()
	changed, added := 0, 0
	for rel, sum := range after {
		old, had := before[rel]
		switch {
		case !had && !allowed(rel):
			t.Errorf("a new file outside meta/: %s", rel)
		case !had:
			added++
		case old != sum && !allowed(rel):
			t.Errorf("changed: %s", rel)
		case old != sum:
			changed++
		}
	}
	for rel := range before {
		if _, ok := after[rel]; !ok {
			t.Errorf("gone: %s", rel)
		}
	}
	unchanged := 0
	for rel, sum := range before {
		if !allowed(rel) && after[rel] == sum {
			unchanged++
		}
	}
	t.Logf("after: %d pages made, %d files written (%d new, %d rewritten), %d other files byte for byte the same", pages, writes, added, changed, unchanged)
	l.listing.invalidateAll()
	if got := listing(l); !jsonEqual(got, shown) {
		t.Fatalf("the listing changed:\n%v\nwant\n%v", got, shown)
	}
	for _, a := range l.ListArtifacts() {
		for _, b := range a.Boards {
			if b.ID == "" {
				t.Errorf("%s: board %s has no page id", a.ID, b.Path)
			}
		}
	}
	again := all()
	for _, p := range l.Migrate(false) {
		if len(p.Writes) != 0 {
			t.Errorf("second run wrote %s: %v", p.ID, p.Writes)
		}
	}
	if !jsonEqual(again, all()) {
		t.Fatal("the second run changed files")
	}
	t.Logf("listing identical after the migration; second run wrote nothing")
}
