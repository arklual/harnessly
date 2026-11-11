package design

// `harnessly design migrate --to tsx`: the boards of a project become page modules, by rules, and each
// one only when the page draws what the board drew, pixel for pixel, in the project's own board engine.
// `--to board` takes them back: the board file as it was, byte for byte.
//
// What a migration writes for the board <name>.dc.html:
//
//	project/pages/<name>.tsx, <name>.fixtures.ts, <name>.board.html   the page (see boardtsx.go)
//	project/pages/board-engine/index.ts                             once per project
//	project/<name>.dc.html                                          the board built from the page
//	meta/tsx-migration/<name>.dc.html                               the board as it was, for --to board
//	meta/tsx-migration/<name>.json                                  what was written, to see a later change
//
// A board the rules do not cover, or whose page draws differently, stays as it is and is named in the report.

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed boardengine.ts
var boardEngineSource string

const tsxMigrationDir = "tsx-migration"

// BoardMigration is what happened to one board.
type BoardMigration struct {
	Board string `json:"board"`
	// Status: migrated, would-migrate (dry run), kept (not translatable), differs (the page draws differently),
	// unstable (the board itself draws differently each time), already (built from a page), rolled-back, changed
	// (the page was edited after the migration; --force rolls back anyway), error.
	Status string   `json:"status"`
	Reason string   `json:"reason,omitempty"`
	Files  []string `json:"files,omitempty"`
	// Pixels differ between the board and its page, when Status is differs.
	Pixels int `json:"pixels,omitempty"`
}

// TSXMigration is the report of a migration of one project, either way.
type TSXMigration struct {
	Project string           `json:"project"`
	To      string           `json:"to"`
	DryRun  bool             `json:"dryRun,omitempty"`
	Boards  []BoardMigration `json:"boards"`
	Done    int              `json:"done"`
	Kept    int              `json:"kept"`
}

// TSXMigrateOptions picks the boards and how they are proven.
type TSXMigrateOptions struct {
	Only   []string // board files or names; all the project's boards when empty
	DryRun bool
	Force  bool // --to board: roll back a page edited after the migration
	// Compare draws a board and its page and says how many pixels differ (-1: the board does not draw the same way
	// twice). Nil: Chrome, with the project's engine.
	Compare BoardComparer
}

// BoardComparer draws the board file as it is and as the given bytes, in the project's folder.
type BoardComparer interface {
	Compare(boardFile string, original, built []byte) (pixels int, err error)
	Close()
}

type tsxRecord struct {
	V        int               `json:"v"`
	Board    string            `json:"board"`
	Original string            `json:"original"` // sha256 of the board as it was
	Files    map[string]string `json:"files"`    // project-relative file -> sha256 as written
	At       string            `json:"at"`
}

func tsxSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (l *Library) tsxMigrationDir(project string) string {
	return filepath.Join(l.MetaDir(project), tsxMigrationDir)
}

// boardFilesOf lists the project's board files (top level of project/), sorted.
func (l *Library) boardFilesOf(project string) []string {
	entries, err := os.ReadDir(l.ProjectDirOf(project))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".dc.html") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func pickBoards(all, only []string) ([]string, error) {
	if len(only) == 0 {
		return all, nil
	}
	have := map[string]bool{}
	for _, b := range all {
		have[b] = true
	}
	var out []string
	for _, o := range only {
		f := strings.TrimPrefix(o, "./")
		if !strings.HasSuffix(f, ".dc.html") {
			f += ".dc.html"
		}
		if !have[f] {
			return nil, fmt.Errorf("the project has no board %s", f)
		}
		out = append(out, f)
	}
	return out, nil
}

// MigrateToTSX turns the project's boards into page modules (see the top of the file).
func (l *Library) MigrateToTSX(project string, opt TSXMigrateOptions) (*TSXMigration, error) {
	dir := l.ProjectDirOf(project)
	if !IsLibraryID(project) || dir == "" {
		return nil, fmt.Errorf("no project %s", project)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no project %s", project)
	}
	boards, err := pickBoards(l.boardFilesOf(project), opt.Only)
	if err != nil {
		return nil, err
	}
	rep := &TSXMigration{Project: project, To: "tsx", DryRun: opt.DryRun, Boards: []BoardMigration{}}
	cmp := opt.Compare
	if cmp == nil && len(boards) > 0 {
		c, err := l.newChromeComparer(dir)
		if err != nil {
			return nil, err
		}
		cmp = c
		defer c.Close()
	}
	pagesDir := filepath.Join(dir, pagesDirname)
	engineFile := filepath.Join(pagesDir, boardEngineDir, "index.ts")
	engineMade := false
	for _, board := range boards {
		bm := l.migrateBoard(project, dir, board, opt, cmp, &engineMade)
		if bm.Status == "migrated" || bm.Status == "would-migrate" {
			rep.Done++
		} else if bm.Status != "already" {
			rep.Kept++
		}
		rep.Boards = append(rep.Boards, bm)
	}
	// The engine helper stays only for the pages that use it.
	if engineMade && (opt.DryRun || rep.Done == 0) {
		_ = os.Remove(engineFile)
		_ = os.Remove(filepath.Dir(engineFile))
		_ = os.Remove(pagesDir) // only when empty
	}
	return rep, nil
}

func (l *Library) migrateBoard(project, dir, board string, opt TSXMigrateOptions, cmp BoardComparer, engineMade *bool) BoardMigration {
	bm := BoardMigration{Board: board}
	name := strings.TrimSuffix(board, ".dc.html")
	if _, built := l.BoardIsBuilt(project, board); built {
		bm.Status = "already"
		return bm
	}
	keep := func(format string, a ...any) BoardMigration {
		bm.Status, bm.Reason = "kept", fmt.Sprintf(format, a...)
		return bm
	}
	if !pageNameRe.MatchString(name) {
		return keep("the name %q cannot be a page's name (letters, digits, spaces and - _ . , · : ( ) « » — –)", name)
	}
	boardPath := filepath.Join(dir, board)
	original, err := os.ReadFile(boardPath)
	if err != nil {
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	pagesDir := filepath.Join(dir, pagesDirname)
	files := map[string]string{
		pagesDirname + "/" + name + pageSrcExt:                 "",
		pagesDirname + "/" + name + pageFixtureEx + ".ts":      "",
		pagesDirname + "/" + name + pageFrameExt:               "",
		pagesDirname + "/" + name + pageFixtureEx + pageSrcExt: "",
	}
	for f := range files {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
			return keep("%s already exists", f)
		}
	}
	if _, err := os.Stat(filepath.Join(l.tsxMigrationDir(project), board)); err == nil {
		return keep("meta/%s/%s already exists: an earlier migration of this board was not rolled back", tsxMigrationDir, board)
	}
	if why := plainDirs(dir, pagesDirname, pagesDirname+"/"+boardEngineDir); why != "" {
		return keep("%s", why)
	}
	if why := plainDirs(l.ArtifactDir(project), MetaDirname, MetaDirname+"/"+tsxMigrationDir); why != "" {
		return keep("%s", why)
	}
	if err := os.MkdirAll(filepath.Join(pagesDir, boardEngineDir), 0o755); err != nil {
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	engineFile := filepath.Join(pagesDir, boardEngineDir, "index.ts")
	if _, err := os.Lstat(engineFile); err != nil {
		if err := writeFileAtomic(engineFile, []byte(boardEngineSource)); err != nil {
			bm.Status, bm.Reason = "error", err.Error()
			return bm
		}
		*engineMade = true
	}
	var written []string
	clean := func() {
		for _, f := range written {
			_ = os.Remove(filepath.Join(dir, filepath.FromSlash(f)))
		}
		written = nil
	}
	write := func(m *BoardModule) error {
		clean()
		out := map[string]string{
			pagesDirname + "/" + name + pageSrcExt:   m.Page,
			pagesDirname + "/" + name + pageFrameExt: m.Frame,
		}
		if m.HasProps {
			out[pagesDirname+"/"+name+pageFixtureEx+".ts"] = m.Fixtures
		}
		keys := make([]string, 0, len(out))
		for k := range out {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, f := range keys {
			if err := writeFileAtomic(filepath.Join(dir, filepath.FromSlash(f)), []byte(out[f])); err != nil {
				return err
			}
			written = append(written, f)
		}
		return nil
	}
	var built []byte
	pixels := 0
	for _, tidy := range []bool{true, false} {
		mod, err := TranslateBoard(name, string(original), tidy)
		if err != nil {
			clean()
			var nt *NotTranslatable
			if errors.As(err, &nt) {
				return keep("%s", nt.Reason)
			}
			bm.Status, bm.Reason = "error", err.Error()
			return bm
		}
		if err := write(mod); err != nil {
			clean()
			bm.Status, bm.Reason = "error", err.Error()
			return bm
		}
		b, _, err := l.buildPageBoard(project, name)
		if err != nil {
			clean()
			return keep("the page does not build: %s", firstLine(err.Error()))
		}
		built = b
		pixels, err = cmp.Compare(board, original, built)
		if err != nil {
			clean()
			bm.Status, bm.Reason = "error", "drawing the board: "+err.Error()
			return bm
		}
		if pixels == 0 || pixels < 0 {
			break
		}
	}
	switch {
	case pixels < 0:
		clean()
		bm.Status, bm.Reason = "unstable", "the board itself draws differently from one load to the next: there is nothing to compare its page with"
		return bm
	case pixels > 0:
		clean()
		bm.Status, bm.Pixels = "differs", pixels
		bm.Reason = fmt.Sprintf("the page draws %d pixels differently from the board", pixels)
		return bm
	}
	bm.Files = append([]string{}, written...)
	if opt.DryRun {
		clean()
		bm.Status = "would-migrate"
		return bm
	}
	rec := tsxRecord{V: 1, Board: board, Original: tsxSum(original), Files: map[string]string{}, At: time.Now().UTC().Format(time.RFC3339)}
	for _, f := range written {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			clean()
			bm.Status, bm.Reason = "error", err.Error()
			return bm
		}
		rec.Files[f] = tsxSum(b)
	}
	rec.Files[board] = tsxSum(built)
	recBytes, _ := json.MarshalIndent(rec, "", "  ")
	mdir := l.tsxMigrationDir(project)
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		clean()
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	if err := writeFileAtomic(filepath.Join(mdir, board), original); err != nil {
		clean()
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	if err := writeFileAtomic(filepath.Join(mdir, name+".json"), append(recBytes, '\n')); err != nil {
		_ = os.Remove(filepath.Join(mdir, board))
		clean()
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	if err := writeFileAtomic(boardPath, built); err != nil {
		_ = os.Remove(filepath.Join(mdir, board))
		_ = os.Remove(filepath.Join(mdir, name+".json"))
		clean()
		bm.Status, bm.Reason = "error", err.Error()
		return bm
	}
	bm.Status = "migrated"
	return bm
}

// MigrateToBoard rolls a migration back: each migrated board as it was, byte for byte, its page files removed.
func (l *Library) MigrateToBoard(project string, opt TSXMigrateOptions) (*TSXMigration, error) {
	dir := l.ProjectDirOf(project)
	if !IsLibraryID(project) || dir == "" {
		return nil, fmt.Errorf("no project %s", project)
	}
	mdir := l.tsxMigrationDir(project)
	entries, _ := os.ReadDir(mdir)
	var all []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dc.html") {
			all = append(all, e.Name())
		}
	}
	sort.Strings(all)
	boards, err := pickBoards(all, opt.Only)
	if err != nil {
		return nil, fmt.Errorf("%v (not migrated)", err)
	}
	rep := &TSXMigration{Project: project, To: "board", DryRun: opt.DryRun, Boards: []BoardMigration{}}
	for _, board := range boards {
		bm := BoardMigration{Board: board}
		name := strings.TrimSuffix(board, ".dc.html")
		original, err := os.ReadFile(filepath.Join(mdir, board))
		recRaw, rerr := os.ReadFile(filepath.Join(mdir, name+".json"))
		var rec tsxRecord
		if err != nil || rerr != nil || json.Unmarshal(recRaw, &rec) != nil || tsxSum(original) != rec.Original {
			bm.Status, bm.Reason = "error", "the saved board or its record in meta/"+tsxMigrationDir+" is missing or damaged"
			rep.Kept++
			rep.Boards = append(rep.Boards, bm)
			continue
		}
		// The record names only the files a migration of this board writes: anything else is not removed.
		expected := map[string]bool{board: true, pagesDirname + "/" + name + pageSrcExt: true,
			pagesDirname + "/" + name + pageFixtureEx + ".ts": true, pagesDirname + "/" + name + pageFrameExt: true}
		var changed []string
		files := make([]string, 0, len(rec.Files))
		foreign := ""
		for f := range rec.Files {
			if !expected[f] {
				foreign = f
			}
			files = append(files, f)
		}
		why := plainDirs(dir, pagesDirname)
		if foreign != "" {
			why = "the record in meta/" + tsxMigrationDir + " names a file a migration does not write: " + foreign
		}
		if why != "" {
			bm.Status, bm.Reason = "error", why
			rep.Kept++
			rep.Boards = append(rep.Boards, bm)
			continue
		}
		sort.Strings(files)
		for _, f := range files {
			if f == board {
				continue // rebuilt from the page: its edits are the page's
			}
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
			if err != nil || tsxSum(b) != rec.Files[f] {
				changed = append(changed, f)
			}
		}
		if len(changed) > 0 && !opt.Force {
			bm.Status = "changed"
			bm.Reason = strings.Join(changed, ", ") + " changed since the migration: rolling back loses that change (--force rolls back anyway)"
			rep.Kept++
			rep.Boards = append(rep.Boards, bm)
			continue
		}
		bm.Files = files
		if opt.DryRun {
			bm.Status = "would-roll-back"
			rep.Done++
			rep.Boards = append(rep.Boards, bm)
			continue
		}
		if err := writeFileAtomic(filepath.Join(dir, board), original); err != nil {
			bm.Status, bm.Reason = "error", err.Error()
			rep.Kept++
			rep.Boards = append(rep.Boards, bm)
			continue
		}
		for _, f := range files {
			if f != board {
				_ = os.Remove(filepath.Join(dir, filepath.FromSlash(f)))
			}
		}
		_ = os.Remove(filepath.Join(mdir, board))
		_ = os.Remove(filepath.Join(mdir, name+".json"))
		bm.Status = "rolled-back"
		rep.Done++
		rep.Boards = append(rep.Boards, bm)
	}
	if !opt.DryRun {
		_ = os.Remove(mdir) // only when empty
		l.dropBoardEngineIfUnused(dir)
	}
	return rep, nil
}

// plainDirs says why a migration must not write under these folders of base: one of them is a symbolic link (or not
// a folder). "" when each is a plain folder or not there yet.
func plainDirs(base string, rels ...string) string {
	for _, rel := range rels {
		st, err := os.Lstat(filepath.Join(base, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return rel + " is a symbolic link or not a folder: nothing is written through it"
		}
	}
	return ""
}

// dropBoardEngineIfUnused removes pages/board-engine when no page imports it any more and it is as written.
func (l *Library) dropBoardEngineIfUnused(dir string) {
	pagesDir := filepath.Join(dir, pagesDirname)
	engineFile := filepath.Join(pagesDir, boardEngineDir, "index.ts")
	b, err := os.ReadFile(engineFile)
	if err != nil || string(b) != boardEngineSource {
		return
	}
	entries, _ := os.ReadDir(pagesDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), pageSrcExt) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(pagesDir, e.Name()))
		if err == nil && strings.Contains(string(src), "\"./"+boardEngineDir+"\"") {
			return
		}
	}
	_ = os.Remove(engineFile)
	_ = os.Remove(filepath.Dir(engineFile))
	_ = os.Remove(pagesDir)
}
