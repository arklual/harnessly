package design

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestTSXMigrationOfALibraryCopy proves `migrate --to tsx` and `--to board` on a copy of a real library:
//
//	DESIGN_TSX_MIGRATION_COPY=/data/lib-copy [DESIGN_TSX_MIGRATION_PROJECTS=a,b] go test ./internal/design -run TestTSXMigrationOfALibraryCopy -v -timeout 6h
//
// Each board is turned into a page only when the page, built and drawn by the project's engine in Chrome, gives the
// board's picture pixel for pixel; the others are named with the reason. A dry run writes nothing. The run writes only
// the boards it migrated, their pages and meta/tsx-migration. A rollback gives every file back byte for byte.
// The dry run is made on the first project (DESIGN_TSX_MIGRATION_DRY=0: none).
func TestTSXMigrationOfALibraryCopy(t *testing.T) {
	dir := os.Getenv("DESIGN_TSX_MIGRATION_COPY")
	if dir == "" {
		t.Skip("DESIGN_TSX_MIGRATION_COPY names no library copy")
	}
	all := func(project string) map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(filepath.Join(dir, project), func(p string, d fs.DirEntry, err error) error {
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
	l := New(Config{LibraryDir: dir, Texts: Texts{CLI: "harnessly"}})
	t.Cleanup(func() { l.StopWatching(); l.StopPages() })
	var projects []string
	if only := os.Getenv("DESIGN_TSX_MIGRATION_PROJECTS"); only != "" {
		projects = strings.Split(only, ",")
	} else {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() && IsLibraryID(e.Name()) {
				if st, err := os.Stat(filepath.Join(dir, e.Name(), ProjectDirname)); err == nil && st.IsDir() {
					projects = append(projects, e.Name())
				}
			}
		}
	}
	sort.Strings(projects)
	jobs, _ := strconv.Atoi(os.Getenv("DESIGN_TSX_MIGRATION_JOBS"))
	if jobs < 1 {
		jobs = 1
	}
	var mu sync.Mutex
	reasons := map[string]int{}
	total, migrated := 0, 0
	one := func(i int, p string) {
		before := all(p)
		if i == 0 && os.Getenv("DESIGN_TSX_MIGRATION_DRY") != "0" {
			if _, err := l.MigrateToTSX(p, TSXMigrateOptions{DryRun: true}); err != nil {
				t.Errorf("%s dry run: %v", p, err)
				return
			}
			if after := all(p); !jsonEqual(before, after) {
				t.Errorf("%s: the dry run wrote something", p)
				return
			}
		}
		rep, err := l.MigrateToTSX(p, TSXMigrateOptions{})
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return
		}
		mu.Lock()
		for _, b := range rep.Boards {
			total++
			if b.Status == "migrated" {
				migrated++
				continue
			}
			key := b.Status + ": " + b.Reason
			if b.Status == "differs" {
				key = "differs"
			}
			reasons[key]++
			t.Logf("%s/%s: %s %s", p, b.Board, b.Status, b.Reason)
		}
		mu.Unlock()
		t.Logf("%s: %d of %d boards migrated", p, rep.Done, len(rep.Boards))
		// Only the migrated boards, their pages and meta/tsx-migration changed.
		after := all(p)
		okFile := func(rel string) bool {
			parts := strings.SplitN(rel, "/", 3)
			if len(parts) < 3 || parts[0] != p {
				return false
			}
			if parts[1] == MetaDirname && strings.HasPrefix(parts[2], tsxMigrationDir+"/") {
				return true
			}
			if parts[1] != ProjectDirname {
				return false
			}
			if strings.HasPrefix(parts[2], pagesDirname+"/") {
				return true
			}
			for _, b := range rep.Boards {
				if b.Status == "migrated" && parts[2] == b.Board {
					return true
				}
			}
			return false
		}
		for rel, sum := range after {
			if old, had := before[rel]; (!had || old != sum) && !okFile(rel) {
				t.Errorf("written outside the migration: %s", rel)
			}
		}
		// Back to the boards: every file as it was.
		back, err := l.MigrateToBoard(p, TSXMigrateOptions{})
		if err != nil {
			t.Errorf("%s rollback: %v", p, err)
			return
		}
		if back.Done != rep.Done {
			t.Errorf("%s: rolled back %d of %d", p, back.Done, rep.Done)
		}
		got := all(p)
		for rel, sum := range got {
			if before[rel] != sum {
				t.Errorf("%s: after the rollback %s is not as it was", p, rel)
			}
		}
		for rel := range before {
			if _, ok := got[rel]; !ok {
				t.Errorf("%s: after the rollback %s is gone", p, rel)
			}
		}
	}
	work := make(chan int)
	var wg sync.WaitGroup
	for j := 0; j < jobs; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				one(i, projects[i])
			}
		}()
	}
	for i := range projects {
		work <- i
	}
	close(work)
	wg.Wait()
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	for _, k := range keys {
		t.Logf("%4d  %s", reasons[k], k)
	}
	t.Logf("%d of %d boards migrated and rolled back byte for byte", migrated, total)
}
