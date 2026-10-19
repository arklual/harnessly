package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/collate"
)

// Realpath resolves every link of an absolute path. The result is text as the old runtime held it (invalid UTF-8
// replaced), so what follows works on the same name it did.
func Realpath(p string) (string, error) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if errnoOf(err) == 0 {
			// filepath gives up on a chain of links with a plain error; the system says ELOOP.
			err = syscall.ELOOP
		}
		return "", &os.PathError{Op: "realpath", Path: p, Err: errnoOrSelf(err)}
	}
	return DecodeUTF8([]byte(real)), nil
}

func errnoOrSelf(err error) error {
	if e := errnoOf(err); e != 0 {
		return e
	}
	return err
}

func errnoOf(err error) syscall.Errno {
	var e syscall.Errno
	if errors.As(err, &e) {
		return e
	}
	return 0
}

// within: target is root or inside it (both resolved, absolute).
func within(root, target string) bool {
	return target == root || root == "/" || strings.HasPrefix(target, root+"/")
}

const (
	editorMaxBytes   = 1_000_000
	editorMaxEntries = 2_000
)

// EditorError is a refusal of the editor routes: a status and the text shown to the person.
type EditorError struct {
	Status int
	Text   string
}

func (e *EditorError) Error() string { return e.Text }

var (
	errNotInProject   = &EditorError{403, "Можно открывать только файлы текущего проекта."}
	errOutside        = &EditorError{403, "Путь находится за пределами проекта."}
	errLinkOutside    = &EditorError{403, "Ссылка ведёт за пределы проекта."}
	errTooLarge       = &EditorError{415, "Редактор поддерживает файлы до 1 МБ."}
	errNotText        = &EditorError{415, "Этот файл не является текстовым."}
	errNotUTF8        = &EditorError{415, "Редактор поддерживает только текст в UTF-8."}
	errNotRegular     = &EditorError{415, "Можно редактировать только обычные текстовые файлы."}
	errGone           = &EditorError{404, "Файл или папка больше не существуют."}
	errNoAccess       = &EditorError{403, "Нет доступа к этому файлу."}
	errCannotReadFile = &EditorError{500, "Не удалось прочитать или сохранить файл."}
)

// editorFail maps a failure to the editor's refusal.
func editorFail(err error) *EditorError {
	var ee *EditorError
	if errors.As(err, &ee) {
		return ee
	}
	switch errnoOf(err) {
	case syscall.ENOENT, syscall.ENOTDIR:
		return errGone
	case syscall.EACCES, syscall.EPERM, syscall.ELOOP:
		return errNoAccess
	}
	return errCannotReadFile
}

// resolveInProject is a path of the project (relative to root) resolved: refused when it is absolute, leaves the
// project by name, or leads out of it through a link.
func resolveInProject(root, relative string) (realRoot, target string, err error) {
	if strings.Contains(relative, "\x00") || strings.HasPrefix(relative, "/") {
		return "", "", errNotInProject
	}
	if realRoot, err = Realpath(root); err != nil {
		return "", "", err
	}
	lexical := filepath.Join(realRoot, relative)
	if !within(realRoot, lexical) {
		return "", "", errOutside
	}
	if target, err = Realpath(lexical); err != nil {
		return "", "", err
	}
	if !within(realRoot, target) {
		return "", "", errLinkOutside
	}
	return realRoot, target, nil
}

// ListProject is one folder of a tab's project for the editor: folders first, then files, by name; links are left
// out; at most 2000 entries are looked at.
func ListProject(root, relative string) (Object, *EditorError) {
	realRoot, target, err := resolveInProject(root, relative)
	if err != nil {
		return nil, editorFail(err)
	}
	fd, err := syscall.Open(target, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, editorFail(&os.PathError{Op: "opendir", Path: target, Err: err})
	}
	dir := os.NewFile(uintptr(fd), target)
	defer dir.Close()
	type entry struct {
		name, path string
		folder     bool
	}
	var entries []entry
	count, truncated := 0, false
	for !truncated {
		batch, err := dir.ReadDir(64)
		for _, item := range batch {
			if count++; count > editorMaxEntries {
				truncated = true
				break
			}
			if !item.Type().IsRegular() && !item.IsDir() {
				continue
			}
			name := DecodeUTF8([]byte(item.Name()))
			entries = append(entries, entry{name: name, path: path.Join(relative, name), folder: item.IsDir()})
		}
		if err == io.EOF || len(batch) == 0 {
			break
		}
		if err != nil {
			return nil, editorFail(err)
		}
	}
	sortStable(entries, func(c *collate.Collator, a, b entry) int {
		if a.folder != b.folder {
			if a.folder {
				return -1
			}
			return 1
		}
		return c.CompareString(a.name, b.name)
	})
	list := make([]Object, len(entries))
	for i, e := range entries {
		kind := "file"
		if e.folder {
			kind = "directory"
		}
		list[i] = Object{{"name", e.name}, {"path", e.path}, {"kind", kind}}
	}
	return Object{{"path", relative}, {"name", BaseName(realRoot)}, {"entries", list}, {"truncated", truncated}}, nil
}

// ReadProjectFile is a text file of a tab's project for the editor, with the revision a save must name.
func ReadProjectFile(root, relative string) (Object, *EditorError) {
	realRoot, target, err := resolveInProject(root, relative)
	if err != nil {
		return nil, editorFail(err)
	}
	f, err := openNoFollow(target, syscall.O_RDONLY)
	if err != nil {
		return nil, editorFail(err)
	}
	defer f.Close()
	data, ee := readBounded(f)
	if ee != nil {
		return nil, ee
	}
	return Object{{"path", relative}, {"content", string(data)}, {"revision", revisionOf(realRoot, target, data)}, {"bytes", len(data)}}, nil
}

func openNoFollow(target string, mode int) (*os.File, error) {
	fd, err := syscall.Open(target, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: target, Err: err}
	}
	return os.NewFile(uintptr(fd), target), nil
}

// readBounded is an open file's text: a regular file of at most 1 MB, without NUL bytes, in UTF-8.
func readBounded(f *os.File) ([]byte, *EditorError) {
	st, err := f.Stat()
	if err != nil {
		return nil, editorFail(err)
	}
	if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if st.Size() > editorMaxBytes {
		return nil, errTooLarge
	}
	buf := make([]byte, editorMaxBytes+1)
	n := 0
	for n < len(buf) {
		k, err := f.ReadAt(buf[n:], int64(n))
		n += k
		if err == io.EOF || k == 0 && err == nil {
			break
		}
		if err != nil {
			return nil, editorFail(err)
		}
	}
	data := buf[:n]
	if ee := checkText(data); ee != nil {
		return nil, ee
	}
	return data, nil
}

func checkText(data []byte) *EditorError {
	switch {
	case len(data) > editorMaxBytes:
		return errTooLarge
	case bytes.IndexByte(data, 0) >= 0:
		return errNotText
	case !utf8.Valid(data):
		return errNotUTF8
	}
	return nil
}

func revisionOf(realRoot, target string, data []byte) string {
	h := sha256.New()
	h.Write([]byte(realRoot))
	h.Write([]byte{0})
	h.Write([]byte(target))
	h.Write([]byte{0})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

var (
	errChanged     = &EditorError{409, "Файл изменился после открытия. Скопируйте свои правки и загрузите актуальную версию."}
	errUnsupported = &EditorError{415, "Текст содержит неподдерживаемые символы."}
	writeLocks     = keyedLocks{held: map[string]*keyedLock{}}
)

// keyedLocks serialize saves of one file, so two pages cannot both save over the same revision; a file's lock is
// dropped once nobody holds or waits for it.
type keyedLocks struct {
	mu   sync.Mutex
	held map[string]*keyedLock
}

type keyedLock struct {
	sync.Mutex
	users int
}

func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	l := k.held[key]
	if l == nil {
		l = &keyedLock{}
		k.held[key] = l
	}
	l.users++
	k.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		k.mu.Lock()
		if l.users--; l.users == 0 {
			delete(k.held, key)
		}
		k.mu.Unlock()
	}
}

// WriteProjectFile saves a file of a tab's project over the revision the page opened, through the descriptor it
// checked: no truncation on open, no final link followed. loneSurrogate says the text held a lone UTF-16 surrogate,
// which the previous runtime could not write back as it came.
func WriteProjectFile(root, relative, content, revision string, loneSurrogate bool) (Object, *EditorError) {
	realRoot, target, err := resolveInProject(root, relative)
	if err != nil {
		return nil, editorFail(err)
	}
	data := []byte(content)
	if ee := checkText(data); ee != nil {
		return nil, ee
	}
	if loneSurrogate {
		return nil, errUnsupported
	}
	defer writeLocks.lock(target)()
	f, err := openNoFollow(target, syscall.O_RDWR)
	if err != nil {
		return nil, editorFail(err)
	}
	defer f.Close()
	current, ee := readBounded(f)
	if ee != nil {
		return nil, ee
	}
	if revisionOf(realRoot, target, current) != revision {
		return nil, errChanged
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return nil, editorFail(err)
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		return nil, editorFail(err)
	}
	if err := f.Sync(); err != nil {
		return nil, editorFail(err)
	}
	return Object{{"path", relative}, {"content", content}, {"revision", revisionOf(realRoot, target, data)}, {"bytes", len(data)}}, nil
}
