package design

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Importing an export of the design tool (a zip, or a project folder) into the library: unpacked aside, attachments no
// board refers to dropped, anything an agent could take for its own instructions removed, artifact.json written, and
// the whole folder renamed into place, so a failed import leaves nothing half-done.

// ImportError is a refusal worded for the person; Conflict when the id is taken (the caller may pick another).
type ImportError struct {
	Msg      string
	Conflict bool
}

func (e *ImportError) Error() string { return e.Msg }

func importErr(msg string) error { return &ImportError{Msg: msg} }

func importConflict(id string) error {
	return &ImportError{Msg: "Макет «" + id + "» уже есть", Conflict: true}
}

var (
	attachmentDirs = []string{"uploads", "screenshots"}
	textExt        = map[string]bool{".html": true, ".htm": true, ".js": true, ".jsx": true, ".css": true, ".json": true}
	translit       = map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
		'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}
	zipSuffix     = regexp.MustCompile(`(?i)\.zip$`)
	nonSlug       = regexp.MustCompile(`[^a-z0-9]+`)
	trailingDash  = regexp.MustCompile(`-+$`)
	unzipTotalRe  = regexp.MustCompile(`^(\d+)\s+(\d+)\s+files?$`)
	agentFileName = regexp.MustCompile(`(?i)^(claude|agents?|gemini|copilot)[\w.-]*\.mdx?$`)
)

// SlugifyArtifactID is an id from a file or project name: transliterated, lower case, dashes; -2, -3… when taken.
func SlugifyArtifactID(name string, taken map[string]bool) string {
	var b strings.Builder
	for _, c := range zipSuffix.ReplaceAllString(jsLower(norm.NFC.String(name)), "") {
		if t, ok := translit[c]; ok {
			b.WriteString(t)
		} else {
			b.WriteRune(c)
		}
	}
	s := edgeDashes.ReplaceAllString(nonSlug.ReplaceAllString(b.String(), "-"), "")
	if len(s) > 48 {
		s = s[:48]
	}
	s = trailingDash.ReplaceAllString(s, "")
	if s == "" {
		s = "design"
	}
	if !taken[s] {
		return s
	}
	for n := 2; ; n++ {
		if c := s + "-" + strconv.Itoa(n); !taken[c] {
			return c
		}
	}
}

// jsLower is toLowerCase where it differs from Go's: İ lowers to i with a combining dot.
func jsLower(s string) string { return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307")) }

// ParseUnzipListing is the total unpacked bytes and entries from `unzip -l`'s last line; ok false when it is not a
// listing unzip could make.
func ParseUnzipListing(listing string) (bytes, entries int64, ok bool) {
	lines := strings.Split(jscompat.Trim(listing), "\n")
	last := jscompat.Trim(lines[len(lines)-1])
	m := unzipTotalRe.FindStringSubmatch(last)
	if m == nil {
		return 0, 0, false
	}
	b, _ := strconv.ParseFloat(m[1], 64)
	e, _ := strconv.ParseFloat(m[2], 64)
	return int64(b), int64(e), true
}

func listArchive(archive string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("unzip", "-l", archive)
	cmd.Stdout = &limitWriter{w: &out, left: 64 * 1024 * 1024}
	err := cmd.Run()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", importErr("На этой машине нет unzip: поставьте пакет unzip")
		}
		return "", nil
	}
	if lw := cmd.Stdout.(*limitWriter); lw.over {
		return "", nil
	}
	return out.String(), nil
}

// limitWriter stands for node's maxBuffer: past it the command counts as failed.
type limitWriter struct {
	w    io.Writer
	left int
	over bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		l.over = true
		return 0, errors.New("maxBuffer exceeded")
	}
	l.left -= len(p)
	return l.w.Write(p)
}

func unpack(archive, into string) error {
	stdout, err := listArchive(archive)
	if err != nil {
		return err
	}
	b, e, ok := ParseUnzipListing(stdout)
	if !ok {
		return importErr("Это не zip-архив проекта")
	}
	if b > LimitUnpackedBytes || e > LimitEntries {
		return importErr("Архив слишком большой: больше гигабайта или двадцати тысяч файлов")
	}
	// unzip skips entries with .. or an absolute path itself; a link it restores is dropped below with the agent files.
	cmd := exec.Command("unzip", "-q", "-o", archive, "-d", into)
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, left: 16 * 1024 * 1024}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		// Exit code 1 is unzip's «warnings, the rest unpacked».
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return importErr("Архив не распаковался: он повреждён или зашифрован")
		}
	}
	return nil
}

// hoistSingleFolder: a project zipped together with its folder has the boards one level down; that folder becomes the
// project.
func hoistSingleFolder(project string) error {
	all, err := os.ReadDir(project)
	if err != nil {
		return err
	}
	var entries []os.DirEntry
	for _, e := range all {
		if e.Name() != "__MACOSX" {
			entries = append(entries, e)
		}
	}
	for _, e := range entries {
		if e.Type().IsRegular() && IsBoardFile(e.Name()) {
			return nil
		}
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}
	inner := filepath.Join(project, ".hoist-"+randomHex(4))
	if err := os.Rename(filepath.Join(project, entries[0].Name()), inner); err != nil {
		return err
	}
	names, err := readNames(inner)
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := os.Rename(filepath.Join(inner, n), filepath.Join(project, n)); err != nil {
			return err
		}
	}
	return os.Remove(inner)
}

func walkText(dir string, skip map[string]bool, visit func(file string) (bool, error)) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if !skip[full] {
				found, err := walkText(full, skip, visit)
				if err != nil || found {
					return found, err
				}
			}
		} else if e.Type().IsRegular() && textExt[strings.ToLower(extname(e.Name()))] {
			found, err := visit(full)
			if err != nil || found {
				return found, err
			}
		}
	}
	return false, nil
}

func pruneAttachments(project string) ([]string, error) {
	dropped := []string{}
	skip := map[string]bool{}
	for _, d := range attachmentDirs {
		skip[filepath.Join(project, d)] = true
	}
	for _, name := range attachmentDirs {
		dir := filepath.Join(project, name)
		if !exists(dir) {
			continue
		}
		referenced, err := walkText(project, skip, func(file string) (bool, error) {
			b, err := os.ReadFile(file)
			if err != nil {
				return false, err
			}
			return strings.Contains(jscompat.DecodeUTF8(b), name+"/"), nil
		})
		if err != nil {
			return nil, err
		}
		if !referenced {
			if err := os.RemoveAll(dir); err != nil {
				return nil, err
			}
			dropped = append(dropped, name)
		}
	}
	if err := removeFileForce(filepath.Join(project, ".thumbnail")); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(filepath.Join(project, "__MACOSX")); err != nil {
		return nil, err
	}
	return dropped, nil
}

// removeFileForce is fs.rm(file, { force: true }): a missing file is fine, a folder is an error.
func removeFileForce(p string) error {
	st, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	if st.IsDir() {
		return errors.New("Path is a directory: " + p)
	}
	return os.Remove(p)
}

// IsAgentEntry is what an agent could take as its own instructions or settings when it works in the folder: every
// hidden entry, every link and the instruction files read by name.
func IsAgentEntry(e os.DirEntry) bool {
	return strings.HasPrefix(e.Name(), ".") || e.Type()&os.ModeSymlink != 0 || (e.Type().IsRegular() && agentFileName.MatchString(e.Name()))
}

func dropAgentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dropped []string
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if IsAgentEntry(e) {
			if err := os.RemoveAll(full); err != nil {
				return nil, err
			}
			dropped = append(dropped, full)
		} else if e.IsDir() {
			more, err := dropAgentFiles(full)
			if err != nil {
				return nil, err
			}
			dropped = append(dropped, more...)
		}
	}
	return dropped, nil
}

type boardSource struct{ file, source string }

func readBoardSources(project string) ([]boardSource, error) {
	entries, err := os.ReadDir(project)
	if err != nil {
		return nil, err
	}
	var out []boardSource
	for _, e := range entries {
		if e.Type().IsRegular() && IsBoardFile(e.Name()) {
			b, err := os.ReadFile(filepath.Join(project, e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, boardSource{e.Name(), jscompat.DecodeUTF8(b)})
		}
	}
	return out, nil
}

const overviewBoard = "Все страницы"

// DetectBoards: wrapper boards become their pages, the rest stay as they are; the overview board goes first, the rest
// by name.
func DetectBoards(sources []boardSource) []Board {
	boards := make([]Board, 0, len(sources))
	for _, s := range sources {
		title := BoardTitleOf(s.file)
		if b := InnerPageBoard(title, s.source); b != nil {
			boards = append(boards, *b)
		} else {
			boards = append(boards, Board{Title: title, Path: s.file})
		}
	}
	sort.SliceStable(boards, func(i, j int) bool {
		fi, fj := boards[i].Title == overviewBoard, boards[j].Title == overviewBoard
		if fi != fj {
			return fi
		}
		return compareRuScripts(boards[i].Title, boards[j].Title) < 0
	})
	return boards
}

// ImportOptions name an import: Source is a zip or a project folder; SourceName is recorded as where it came from.
type ImportOptions struct {
	Source, ID string
	Title      *string
	SourceName *string
	Force      bool
}

// ImportResult is the project put into the library.
type ImportResult struct {
	ID, Title, Dir string
	Boards         int
	Dropped        []string
}

func boardsJSON(boards []Board) []any {
	out := make([]any, 0, len(boards))
	for _, b := range boards {
		out = append(out, jsvalue.NewObject("title", b.Title, "path", b.Path, "w", intOrNull(b.W), "h", intOrNull(b.H)))
	}
	return out
}

func intOrNull(p *int) any {
	if p == nil {
		return nil
	}
	return float64(*p)
}

func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// ImportProject puts an export of the design tool into the library under id.
func (l *Library) ImportProject(o ImportOptions) (ImportResult, error) {
	defer l.listing.invalidateAll()
	if !IsLibraryID(o.ID) {
		return ImportResult{}, importErr("Имя макета: латинские буквы в нижнем регистре, цифры и дефисы")
	}
	target := l.ArtifactDir(o.ID)
	if exists(target) && !o.Force {
		return ImportResult{}, importConflict(o.ID)
	}
	root := l.Root()
	if err := os.MkdirAll(root, 0o777); err != nil {
		return ImportResult{}, err
	}
	stage := filepath.Join(root, ".importing-"+o.ID+"-"+randomHex(4))
	project := filepath.Join(stage, ProjectDirname)
	defer os.RemoveAll(stage)
	if err := os.MkdirAll(project, 0o777); err != nil {
		return ImportResult{}, err
	}
	st, err := os.Stat(o.Source)
	if err != nil {
		return ImportResult{}, importErr("Файл проекта не найден")
	}
	if st.IsDir() {
		err = copyTree(o.Source, project)
	} else {
		err = unpack(o.Source, project)
	}
	if err != nil {
		return ImportResult{}, err
	}
	if err := hoistSingleFolder(project); err != nil {
		return ImportResult{}, err
	}
	if _, err := dropAgentFiles(stage); err != nil {
		return ImportResult{}, err
	}
	dropped, err := pruneAttachments(project)
	if err != nil {
		return ImportResult{}, err
	}
	sources, err := readBoardSources(project)
	if err != nil {
		return ImportResult{}, err
	}
	boards := DetectBoards(sources)
	if len(boards) == 0 {
		return ImportResult{}, importErr("В проекте нет досок *.dc.html: это точно экспорт Claude Design?")
	}
	shown := ""
	if o.Title != nil {
		shown = jscompat.Slice(jscompat.Trim(*o.Title), LimitTitle)
	}
	if shown == "" {
		shown = o.ID
	}
	source := nodeBasename(o.Source)
	if o.SourceName != nil {
		source = *o.SourceName
	}
	meta := jsvalue.NewObject("v", 1, "title", shown, "source", source, "importedAt", isoNow(), "boards", boardsJSON(boards))
	if err := os.WriteFile(filepath.Join(stage, ArtifactFile), append(jsvalue.Stringify(meta, "  "), '\n'), 0o666); err != nil {
		return ImportResult{}, err
	}
	if o.Force && exists(target) {
		trash := filepath.Join(root, ".trash")
		if err := os.MkdirAll(trash, 0o777); err != nil {
			return ImportResult{}, err
		}
		if err := os.Rename(target, filepath.Join(trash, o.ID+"-"+strconv.FormatInt(time.Now().UnixMilli(), 10))); err != nil {
			return ImportResult{}, err
		}
	}
	if !o.Force && exists(target) {
		return ImportResult{}, importConflict(o.ID)
	}
	if err := os.Rename(stage, target); err != nil {
		if errors.Is(err, os.ErrExist) || isNotEmpty(err) {
			return ImportResult{}, importConflict(o.ID)
		}
		return ImportResult{}, err
	}
	return ImportResult{ID: o.ID, Title: shown, Dir: target, Boards: len(boards), Dropped: dropped}, nil
}

// copyTree is fs.cp(source, target, { recursive: true }) for a project folder: folders, files and links as links.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o777)
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(to, b, info.Mode().Perm())
		}
		return nil
	})
}

// TakenArtifactIDs are the ids already in the library.
func (l *Library) TakenArtifactIDs() map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(l.Root())
	for _, e := range entries {
		if e.IsDir() && IsLibraryID(e.Name()) {
			out[e.Name()] = true
		}
	}
	return out
}

// compareRuScripts is localeCompare(b, 'ru', { numeric: true }) with ICU's Russian script order, which the collator
// here lacks: Cyrillic letters before Latin ones. Latin base letters are moved to a script that sorts after Cyrillic
// (Armenian, in alphabet order with its own cases), so letters of each script keep their order among themselves and
// marks stay secondary differences.
func compareRuScripts(a, b string) int { return compareRu(a, b) }
