package design

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Managing the boards of a design from the panel: rename, reorder, duplicate, remove, and the design's own title.
// The list lives in artifact.json; a design whose boards were worked out from its files gets the list written the
// first time it is changed.

// BoardEditError is a refusal worded for the person.
type BoardEditError struct{ Msg string }

func (e *BoardEditError) Error() string { return e.Msg }

func editErr(msg string) error { return &BoardEditError{Msg: msg} }

var BoardActions = map[string]bool{"rename": true, "move": true, "duplicate": true, "remove": true, "title": true}

// BoardEdit is one change; Title and Delta are nil when the request had none of the right type.
type BoardEdit struct {
	Action string
	Path   *string
	Title  *string
	Delta  *float64
}

var (
	boardTitleControls = regexp.MustCompile("[\x00-\x1f\x7f]+")
)

func cleanBoardTitle(v *string) (string, error) {
	t := ""
	if v != nil {
		t = jscompat.Trim(spaceRun.ReplaceAllString(boardTitleControls.ReplaceAllString(*v, " "), " "))
	}
	if t == "" {
		return "", editErr("Название не может быть пустым")
	}
	return jscompat.Slice(t, LimitTitle), nil
}

func (l *Library) readMeta(id string) *jsvalue.Object {
	if o, ok := readJSON(filepath.Join(l.ArtifactDir(id), ArtifactFile)).(*jsvalue.Object); ok {
		return o
	}
	return jsvalue.NewObject()
}

func boardsValue(boards []Board) []any {
	out := make([]any, 0, len(boards))
	for _, b := range boards {
		var w, h any
		if b.W != nil {
			w = float64(*b.W)
		}
		if b.H != nil {
			h = float64(*b.H)
		}
		out = append(out, jsvalue.NewObject("title", b.Title, "path", b.Path, "w", w, "h", h))
	}
	return out
}

func (l *Library) writeMeta(id string, meta *jsvalue.Object) error {
	doc := jsvalue.NewObject("v", 1.0)
	for _, k := range meta.Keys() {
		v, _ := meta.Get(k)
		doc.Set(k, v)
	}
	file := filepath.Join(l.ArtifactDir(id), ArtifactFile)
	temp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(temp, append(jsvalue.Stringify(doc, "  "), '\n'), 0o666); err != nil {
		return err
	}
	return os.Rename(temp, file)
}

func (l *Library) copyName(dir, file string) (string, error) {
	stem := strings.TrimSuffix(file, dcSuffix)
	for n := 1; n < 100; n++ {
		name := stem + " копия"
		if n > 1 {
			name += " " + strconv.Itoa(n)
		}
		name += dcSuffix
		if !exists(filepath.Join(dir, name)) {
			return name, nil
		}
	}
	return "", editErr("Слишком много копий этой доски")
}

// boardFile is the board's own file inside the project, through realpath: a link to a file elsewhere is never copied
// or moved.
func (l *Library) boardFile(id, file string) (string, error) {
	real := l.ResolveProjectFile(id, []string{file})
	if real != "" {
		if st, err := os.Lstat(filepath.Join(l.ProjectDir(id), file)); err == nil && st.Mode().IsRegular() {
			return real, nil
		}
	}
	return "", editErr("Файл доски не найден в проекте")
}

func copyExclusive(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return err
}

func (l *Library) trashFile(id, file string) error {
	source, err := l.boardFile(id, file)
	if err != nil {
		return err
	}
	trash := filepath.Join(l.Root(), ".trash")
	if err := os.MkdirAll(trash, 0o777); err != nil {
		return err
	}
	return os.Rename(source, filepath.Join(trash, id+"-"+strconv.FormatInt(nowMs(), 10)+"-"+file))
}

// EditBoards applies one change and returns the boards as they are now. Two edits of one design queue.
func (l *Library) EditBoards(id string, e BoardEdit) ([]Board, error) {
	m := l.lockOf("boards\x00" + id)
	m.Lock()
	defer m.Unlock()
	// What this edit wrote is read back at once, not when the watcher hears of it.
	defer l.listing.invalidatePath(id)
	if !IsLibraryID(id) {
		return nil, editErr("Доски можно менять только у макетов библиотеки")
	}
	a := l.ReadArtifact(id)
	if a == nil {
		return nil, editErr("Макета больше нет")
	}
	meta := l.readMeta(id)
	boards := append([]Board{}, a.Boards...)
	if e.Action == "title" {
		t, err := cleanBoardTitle(e.Title)
		if err != nil {
			return nil, err
		}
		meta.Set("title", t)
		meta.Set("boards", boardsValue(boards))
		return boards, l.writeMeta(id, meta)
	}
	at := -1
	for i, b := range boards {
		if e.Path != nil && b.Path == *e.Path {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, editErr("Такой доски нет")
	}
	board := boards[at]
	switch e.Action {
	case "rename":
		t, err := cleanBoardTitle(e.Title)
		if err != nil {
			return nil, err
		}
		boards[at].Title = t
	case "move":
		d := 0.0
		if e.Delta != nil {
			d = *e.Delta
		}
		sign := 0
		if d > 0 {
			sign = 1
		} else if d < 0 {
			sign = -1
		}
		to := int(math.Min(float64(len(boards)-1), math.Max(0, float64(at+sign))))
		moved := boards[at]
		boards = append(boards[:at], boards[at+1:]...)
		boards = append(boards[:to], append([]Board{moved}, boards[to:]...)...)
	case "duplicate":
		if !IsBoardFile(board.Path) {
			return nil, editErr("Копировать можно доску-файл; экран админки собран из общего бандла")
		}
		name, err := l.copyName(l.ProjectDir(id), board.Path)
		if err != nil {
			return nil, err
		}
		src, err := l.boardFile(id, board.Path)
		if err != nil {
			return nil, err
		}
		if err := copyExclusive(src, filepath.Join(l.ProjectDir(id), name)); err != nil {
			return nil, err
		}
		dup := Board{Title: board.Title + " копия", Path: name, W: board.W, H: board.H}
		boards = append(boards[:at+1], append([]Board{dup}, boards[at+1:]...)...)
	case "remove":
		if len(boards) == 1 {
			return nil, editErr("Последнюю доску не удалить: удалите макет целиком")
		}
		boards = append(boards[:at], boards[at+1:]...)
		still := false
		for _, b := range boards {
			still = still || b.Path == board.Path
		}
		if IsBoardFile(board.Path) && !still {
			if err := l.trashFile(id, board.Path); err != nil {
				return nil, err
			}
		}
	default:
		return nil, editErr("Неизвестное действие")
	}
	meta.Set("boards", boardsValue(boards))
	return boards, l.writeMeta(id, meta)
}

// IsBoardEditError tells a refusal from a failure.
func IsBoardEditError(err error) bool {
	var e *BoardEditError
	return errors.As(err, &e)
}
