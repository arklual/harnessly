package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Files browses the folder of a v2 conversation with the Legacy editor's reading (internal/files): paths are relative to
// the folder, what leaves it by name or by a link is refused, a file is UTF-8 text up to 1 MB, links and special files
// are not listed. A save is whole (a temporary file renamed over) and names the revision it read.
type v2Files struct {
	record func(conv string) (wire.Conversation, bool)
	// touched is whether a turn of the conversation wrote, edited or read the file at an absolute path: such a file
	// opens from its card in the feed although it lies outside the conversation's folder (a worktree beside it).
	touched func(ctx context.Context, conv, path string) (bool, error)
	// delivered is the sha256 of a file the agent handed over in the conversation (deliver_file), which may lie in /tmp:
	// that one file opens to read, as long as its bytes are the ones handed over. nil for none.
	delivered func(ctx context.Context, conv, path string) (string, bool, error)
	// access is the viewer's places and secrets rule: what may be downloaded besides the conversation's folder.
	access *files.Access
}

var _ wire.FilesSource = (*v2Files)(nil)

func (f *v2Files) root(conv string) (string, error) {
	rec, ok := f.record(conv)
	if !ok {
		return "", convo.ErrNotFound
	}
	if rec.Cwd == "" {
		return "", &wire.FileError{Code: wire.RefuseNotFound, Message: "The conversation has no folder."}
	}
	return rec.Cwd, nil
}

// fileError is the editor's refusal in the wire's words.
func fileError(e *files.EditorError) error {
	code := wire.RefuseInternal
	switch e.Status {
	case http.StatusForbidden:
		code = wire.RefuseForbidden
	case http.StatusNotFound:
		code = wire.RefuseNotFound
	case http.StatusUnsupportedMediaType:
		code = wire.RefuseUnsupported
	case http.StatusConflict:
		code = wire.RefuseConflict
	}
	return &wire.FileError{Code: code, Message: e.Text}
}

func (f *v2Files) Dir(_ context.Context, conv, dir string) (wire.DirListing, error) {
	root, err := f.root(conv)
	if err != nil {
		return wire.DirListing{}, err
	}
	// «Машина» in «Файлы» (the old «Диск»): "/" is the workspace's places, an absolute folder in them is listed
	// with absolute paths. What looks like a secret is not shown; nothing outside the places is.
	if strings.HasPrefix(dir, "/") {
		return f.machineDir(dir)
	}
	obj, ee := files.ListProject(root, dir)
	if ee != nil {
		return wire.DirListing{}, fileError(ee)
	}
	out := wire.DirListing{Path: dir, Entries: []wire.FileEntry{}}
	for _, field := range obj {
		switch field.K {
		case "truncated":
			out.Truncated, _ = field.V.(bool)
		case "entries":
			list, _ := field.V.([]files.Object)
			for _, item := range list {
				var e wire.FileEntry
				for _, kv := range item {
					s, _ := kv.V.(string)
					switch kv.K {
					case "name":
						e.Name = s
					case "path":
						e.Path = s
					case "kind":
						e.Kind = wire.FileKind(s)
					}
				}
				out.Entries = append(out.Entries, e)
			}
		}
	}
	markGit(out.Entries, filepath.Join(root, dir))
	return out, nil
}

// markGit puts git's marks on the entries of a folder: new, or changed and not committed.
func markGit(entries []wire.FileEntry, dir string) {
	marks := files.GitMarks(dir)
	for i := range entries {
		entries[i].Git = wire.FileGit(marks[entries[i].Name])
	}
}

// machineDir lists a folder of the machine for «Машина»: "/" is the list of the workspace's places.
func (f *v2Files) machineDir(dir string) (wire.DirListing, error) {
	refused := &wire.FileError{Code: wire.RefuseForbidden, Message: "outside the workspace"}
	if f.access == nil || filepath.Clean(dir) != dir {
		return wire.DirListing{}, refused
	}
	out := wire.DirListing{Path: dir, Entries: []wire.FileEntry{}}
	if dir == "/" {
		for _, r := range f.access.Roots {
			if st, err := os.Stat(r); err == nil && st.IsDir() {
				out.Entries = append(out.Entries, wire.FileEntry{Name: r, Path: r, Kind: wire.FileKind("directory")})
			}
		}
		return out, nil
	}
	real, err := files.Realpath(dir)
	if err != nil || !f.access.Allowed(real) || f.access.Secret(real) {
		return wire.DirListing{}, refused
	}
	obj, ee := files.ListProject(dir, "")
	if ee != nil {
		return wire.DirListing{}, fileError(ee)
	}
	out.Root = rootOf(f.access.Roots, dir)
	if out.Root == "" {
		out.Root = rootOf(f.access.Roots, real)
	}
	for _, field := range obj {
		switch field.K {
		case "truncated":
			out.Truncated, _ = field.V.(bool)
		case "entries":
			list, _ := field.V.([]files.Object)
			for _, item := range list {
				var e wire.FileEntry
				for _, kv := range item {
					s, _ := kv.V.(string)
					switch kv.K {
					case "name":
						e.Name = s
					case "kind":
						e.Kind = wire.FileKind(s)
					}
				}
				e.Path = filepath.Join(dir, e.Name)
				if f.access.Secret(e.Path) {
					continue
				}
				out.Entries = append(out.Entries, e)
			}
		}
	}
	markGit(out.Entries, dir)
	return out, nil
}

// place is the folder and the name a file is read and saved under: the conversation's folder and the path in it, or,
// for an absolute path a turn of the conversation touched or one in the workspace's places, the file's own folder and its name.
func (f *v2Files) place(ctx context.Context, conv, file string) (root, rel string, err error) {
	root, err = f.root(conv)
	if err != nil || !strings.HasPrefix(file, "/") || f.touched == nil || filepath.Clean(file) != file {
		return root, file, err
	}
	ok, err := f.touched(ctx, conv, file)
	if err != nil {
		return root, file, err
	}
	// Any file of the workspace's places opens too, as the viewer opens it: a file the person was sent to
	// outside the chat's folder reads here instead of in the old viewer. Secrets stay closed.
	if !ok && f.access != nil {
		if real, rerr := files.Realpath(file); rerr == nil && f.access.Allowed(real) && !f.access.Secret(real) {
			ok = true
		}
	}
	if !ok {
		return root, file, nil
	}
	return filepath.Dir(file), filepath.Base(file), nil
}

func (f *v2Files) File(ctx context.Context, conv, file string) (wire.FileContent, error) {
	root, rel, err := f.place(ctx, conv, file)
	if err != nil {
		return wire.FileContent{}, err
	}
	if strings.HasPrefix(rel, "/") && f.delivered != nil {
		sum, ok, err := f.delivered(ctx, conv, file)
		if err != nil {
			return wire.FileContent{}, err
		}
		if ok {
			obj, ee := files.ReadDeliveredFile(file, sum)
			if ee != nil {
				return wire.FileContent{}, fileError(ee)
			}
			return fileContent(file, obj), nil
		}
	}
	obj, ee := files.ReadProjectFile(root, rel)
	if ee != nil {
		return wire.FileContent{}, fileError(ee)
	}
	return fileContent(file, obj), nil
}

// Abs is the absolute, resolved path of a file as File would open it: what a document made of the file is bound to.
func (f *v2Files) Abs(ctx context.Context, conv, file string) (string, error) {
	root, rel, err := f.place(ctx, conv, file)
	if err != nil {
		return "", err
	}
	abs, ee := files.ProjectFilePath(root, rel)
	if ee != nil {
		return "", fileError(ee)
	}
	return abs, nil
}

// Save writes the text over the revision the page read; a file that changed since is a conflict.
func (f *v2Files) Save(ctx context.Context, conv, file, content, revision string) (wire.FileContent, error) {
	root, rel, err := f.place(ctx, conv, file)
	if err != nil {
		return wire.FileContent{}, err
	}
	obj, ee := files.SaveProjectFile(root, rel, content, revision)
	if ee != nil {
		return wire.FileContent{}, fileError(ee)
	}
	return fileContent(file, obj), nil
}

func fileContent(file string, obj files.Object) wire.FileContent {
	out := wire.FileContent{Path: file}
	for _, field := range obj {
		switch field.K {
		case "content":
			out.Content, _ = field.V.(string)
		case "revision":
			out.Revision, _ = field.V.(string)
		case "bytes":
			out.Bytes, _ = field.V.(int)
		}
	}
	return out
}

// ServeDownload answers GET .../download?path=…: the paths are the folder's (relative) or absolute ones; a file is
// handed over when it lies in the conversation's folder or in the workspace's places (the viewer's roots), or a turn
// of the conversation touched or delivered it, and is no secret. One file goes whole or in ranges, a folder or several
// paths as one zip.
func (f *v2Files) ServeDownload(w http.ResponseWriter, r *http.Request, conv string) {
	fail := func(status int, text string) { files.WriteJSON(w, r, status, files.Error(text)) }
	root, err := f.root(conv)
	if err != nil {
		fail(http.StatusNotFound, locale.Pick("No such conversation", "Такого чата нет"))
		return
	}
	realRoot, _ := files.Realpath(root)
	q := r.URL.Query()
	asked := q["path"]
	if f.access == nil || len(asked) == 0 || len(asked) > 1000 {
		fail(http.StatusBadRequest, locale.Pick("Nothing to download", "Нечего скачать"))
		return
	}
	allowIn := func(real string) bool {
		return realRoot != "" && (real == realRoot || strings.HasPrefix(real, realRoot+"/")) || f.access.Allowed(real)
	}
	items := make([]files.DownloadItem, 0, len(asked))
	for _, p := range asked {
		abs := p
		if !strings.HasPrefix(p, "/") {
			abs = filepath.Join(root, p)
		}
		allow := allowIn
		// A file a turn wrote or delivered outside the places opens from its card: that one file, not its folder.
		if strings.HasPrefix(p, "/") && f.touchedOrDelivered(r.Context(), conv, abs) {
			allow = func(real string) bool { return allowIn(real) || real == mustReal(abs) }
		}
		it, refused := f.access.ResolveDownload(abs, allow)
		if refused != nil {
			fail(refused.Status, refused.Text)
			return
		}
		items = append(items, it)
	}
	if len(items) == 1 && !items[0].Dir {
		files.ServeFileDownload(w, r, items[0].Real)
		return
	}
	f.access.ServeZipDownload(w, r, items, files.ZipName(items, q.Get("name")), allowIn)
}

func mustReal(p string) string {
	real, err := files.Realpath(p)
	if err != nil {
		return "\x00"
	}
	return real
}

func (f *v2Files) touchedOrDelivered(ctx context.Context, conv, abs string) bool {
	if filepath.Clean(abs) != abs {
		return false
	}
	if f.touched != nil {
		if ok, err := f.touched(ctx, conv, abs); err == nil && ok {
			return true
		}
	}
	if f.delivered != nil {
		if _, ok, err := f.delivered(ctx, conv, abs); err == nil && ok {
			return true
		}
	}
	return false
}

// ServeGitSide answers GET .../file-git?path=…[&ref=HEAD]: the file as the revision has it, for the editor's diff
// The file is placed as File places it (the folder's, or an absolute one it may open); the other side is the
// editor's own text, unsaved edits too.
func (f *v2Files) ServeGitSide(w http.ResponseWriter, r *http.Request, conv string) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, text string) { files.WriteJSON(w, r, status, files.Error(text)) }
	q := r.URL.Query()
	ref, ok := files.Ref(q["ref"], "HEAD")
	if !ok || ref == "WORKTREE" || len(q["path"]) != 1 || q.Get("path") == "" {
		fail(http.StatusBadRequest, locale.Pick("A file and a revision are required", "Нужны файл и ревизия"))
		return
	}
	root, rel, err := f.place(r.Context(), conv, q.Get("path"))
	if err != nil {
		fail(http.StatusNotFound, locale.Pick("No such conversation", "Такого чата нет"))
		return
	}
	abs, ee := files.ProjectFilePath(root, rel)
	if ee != nil {
		fail(ee.Status, ee.Text)
		return
	}
	if f.access != nil && f.access.Secret(abs) {
		fail(http.StatusForbidden, locale.Pick("This file cannot be opened from the workspace", "Этот файл нельзя открыть из рабочего пространства"))
		return
	}
	side, inRepo, tracked := files.GitBlob(abs, ref)
	files.WriteJSON(w, r, http.StatusOK, files.Object{
		{K: "ref", V: ref}, {K: "inRepo", V: inRepo}, {K: "tracked", V: tracked},
		{K: "content", V: side.Content}, {K: "truncated", V: side.Truncated}, {K: "binary", V: side.Binary},
	})
}

// searchTime is how long «Найти в файлах» may look before it answers with what it found.
const searchTime = 5 * time.Second

// ServeSearch answers GET .../find?q=<text>[&path=<folder>]: the lines of the text files under the folder
// that hold the text. The folder is the chat's folder or one in it, or an absolute folder of the workspace's places (as
// «Машина» lists it); the paths come back as the files tab opens them: relative to the chat's folder, or absolute.
func (f *v2Files) ServeSearch(w http.ResponseWriter, r *http.Request, conv string) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, text string) { files.WriteJSON(w, r, status, files.Error(text)) }
	q := r.URL.Query()
	query, dir := q.Get("q"), q.Get("path")
	if strings.TrimSpace(query) == "" || len(query) > files.SearchMaxQuery || len(q["q"]) != 1 || len(q["path"]) > 1 {
		fail(http.StatusBadRequest, locale.Pick("Text to find is required", "Нужен текст для поиска"))
		return
	}
	root, err := f.root(conv)
	if err != nil {
		fail(http.StatusNotFound, locale.Pick("No such conversation", "Такого чата нет"))
		return
	}
	refused := func() {
		fail(http.StatusForbidden, locale.Pick("This folder cannot be searched", "В этой папке искать нельзя"))
	}
	var base, prefix string
	switch {
	case strings.HasPrefix(dir, "/"):
		real, rerr := files.Realpath(dir)
		if f.access == nil || filepath.Clean(dir) != dir || rerr != nil || !f.access.Allowed(real) || f.access.Secret(real) {
			refused()
			return
		}
		base, prefix = real, dir
	default:
		realRoot, rerr := files.Realpath(root)
		if rerr != nil {
			fail(http.StatusNotFound, locale.Pick("Folder not found", "Папка не найдена"))
			return
		}
		real, rerr := files.Realpath(filepath.Join(realRoot, dir))
		if rerr != nil || (real != realRoot && !strings.HasPrefix(real, realRoot+"/")) {
			refused()
			return
		}
		base, prefix = real, strings.Trim(filepath.ToSlash(filepath.Clean("/"+dir)), "/")
	}
	if st, serr := os.Stat(base); serr != nil || !st.IsDir() {
		fail(http.StatusBadRequest, locale.Pick("This is not a folder", "Это не папка"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTime)
	defer cancel()
	var secret func(string) bool
	if f.access != nil {
		secret = f.access.Secret
	}
	hits, truncated := files.SearchText(ctx, base, query, secret)
	out := make([]files.Object, 0, len(hits))
	for _, h := range hits {
		path := h.Path
		if prefix != "" {
			path = strings.TrimSuffix(prefix, "/") + "/" + h.Path
		}
		out = append(out, files.Object{{K: "path", V: path}, {K: "line", V: h.Line}, {K: "col", V: h.Col}, {K: "text", V: h.Text}})
	}
	files.WriteJSON(w, r, http.StatusOK, files.Object{{K: "hits", V: out}, {K: "truncated", V: truncated}})
}

// entryOp is a change «Файлы» asks for: a new empty file in a folder, a new name, or the trash.
type entryOp struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Name string `json:"name"`
}

// ServeEntryOp answers POST .../file-op {op, path, name}: "newFile" makes an empty file `name` in the folder
// `path`, "rename" names the entry `path` `name` in its folder, "trash" moves the entry `path` to the person's trash.
// Paths are as the files tab has them (relative to the chat's folder, or absolute in the workspace's places), and so
// is the answer's path. The chat's folder, a place itself and what looks like credentials are not changed.
func (f *v2Files) ServeEntryOp(w http.ResponseWriter, r *http.Request, conv string) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, text string) { files.WriteJSON(w, r, status, files.Error(text)) }
	var op entryOp
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&op); err != nil {
		fail(http.StatusBadRequest, locale.Pick("What to change is required", "Нужно, что менять"))
		return
	}
	root, err := f.root(conv)
	if err != nil {
		fail(http.StatusNotFound, locale.Pick("No such conversation", "Такого чата нет"))
		return
	}
	refused := func() {
		fail(http.StatusForbidden, locale.Pick("This cannot be changed from the workspace", "Это нельзя менять из рабочего пространства"))
	}
	// The entry by its folder resolved and its own name as it is: a link is changed itself, never what it leads to.
	abs, shown, ok := f.entryOf(root, op.Path, op.Op == "newFile")
	if !ok {
		refused()
		return
	}
	// What is renamed or moved away keeps the chat's folder, the places and secret folders where they are: not
	// they, not a folder they lie in, not a secret-looking file under another name.
	if op.Op != "newFile" && (f.holdsGuarded(abs, root) || files.SecretName(filepath.Base(abs)) ||
		(f.access != nil && f.access.HoldsSecret(abs))) {
		refused()
		return
	}
	switch op.Op {
	case "newFile":
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			fail(http.StatusBadRequest, locale.Pick("This is not a folder", "Это не папка"))
			return
		}
		if f.secret(filepath.Join(abs, op.Name)) {
			refused()
			return
		}
		_, err = files.NewEmptyFile(abs, op.Name)
		shown = joinShown(shown, op.Name)
	case "rename":
		if f.secret(filepath.Join(filepath.Dir(abs), op.Name)) {
			refused()
			return
		}
		_, err = files.RenameEntry(abs, op.Name)
		shown = joinShown(dirShown(shown), op.Name)
	case "trash":
		trash, terr := files.TrashDir()
		if terr != nil {
			fail(http.StatusInternalServerError, locale.Pick("The trash is not found", "Корзина не найдена"))
			return
		}
		err = files.TrashEntry(abs, trash, time.Now())
	default:
		fail(http.StatusBadRequest, locale.Pick("Unknown change", "Непонятно, что менять"))
		return
	}
	// The answer names no path: the system's own errors are said by their kind.
	var said *files.EntryError
	switch {
	case errors.Is(err, files.ErrEntryExists):
		fail(http.StatusConflict, err.Error())
	case errors.Is(err, os.ErrNotExist):
		fail(http.StatusNotFound, locale.Pick("It is not there any more", "Этого уже нет"))
	case errors.As(err, &said):
		fail(http.StatusBadRequest, said.Error())
	case errors.Is(err, os.ErrPermission):
		fail(http.StatusForbidden, locale.Pick("No rights to change this", "Нет прав это менять"))
	case err != nil:
		fail(http.StatusInternalServerError, locale.Pick("This did not work", "Не получилось"))
	default:
		files.WriteJSON(w, r, http.StatusOK, files.Object{{K: "path", V: shown}})
	}
}

// holdsGuarded is whether the entry at abs (its own path, not followed) is the chat's folder or a place, or holds one:
// each is compared by its path as written and as resolved.
func (f *v2Files) holdsGuarded(abs, root string) bool {
	guarded := []string{filepath.Clean(root)}
	if real, err := files.Realpath(root); err == nil {
		guarded = append(guarded, real)
	}
	if f.access != nil {
		for _, place := range f.access.Roots {
			guarded = append(guarded, place)
			if real, err := files.Realpath(place); err == nil {
				guarded = append(guarded, real)
			}
		}
	}
	for _, g := range guarded {
		if g == abs || strings.HasPrefix(g, abs+"/") || abs == "/" {
			return true
		}
	}
	return false
}

func (f *v2Files) secret(abs string) bool { return f.access != nil && f.access.Secret(abs) }

// entryOf is the entry a change names: its absolute path (its folder resolved, its name kept) and the path as the
// files tab has it. A folder may be the chat's folder itself only where it is the place of a new file (`folder`).
func (f *v2Files) entryOf(root, path string, folder bool) (abs, shown string, ok bool) {
	if strings.Contains(path, "\x00") {
		return "", "", false
	}
	if strings.HasPrefix(path, "/") {
		if f.access == nil || filepath.Clean(path) != path || f.access.Secret(path) {
			return "", "", false
		}
		parent, err := files.Realpath(filepath.Dir(path))
		if err != nil {
			return "", "", false
		}
		abs = filepath.Join(parent, filepath.Base(path))
		if !f.access.Allowed(abs) || f.access.Secret(abs) {
			return "", "", false
		}
		for _, place := range f.access.Roots {
			if abs == place || path == place {
				if !folder {
					return "", "", false
				}
			}
		}
		if folder {
			real, rerr := files.Realpath(path)
			if rerr != nil || !f.access.Allowed(real) {
				return "", "", false
			}
			abs = real
		}
		return abs, path, true
	}
	realRoot, err := files.Realpath(root)
	if err != nil {
		return "", "", false
	}
	rel := strings.Trim(filepath.ToSlash(filepath.Clean("/"+path)), "/")
	if rel == "" || rel == "." {
		if !folder {
			return "", "", false
		}
		return realRoot, "", true
	}
	parent, err := files.Realpath(filepath.Join(realRoot, filepath.Dir(rel)))
	if err != nil || (parent != realRoot && !strings.HasPrefix(parent, realRoot+"/")) {
		return "", "", false
	}
	abs = filepath.Join(parent, filepath.Base(rel))
	if f.secret(abs) {
		return "", "", false
	}
	if folder {
		real, rerr := files.Realpath(abs)
		if rerr != nil || (real != realRoot && !strings.HasPrefix(real, realRoot+"/")) {
			return "", "", false
		}
		abs = real
	}
	return abs, rel, true
}

func dirShown(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return ""
}

func joinShown(dir, name string) string {
	switch dir {
	case "":
		return name
	case "/":
		return "/" + name
	}
	return dir + "/" + name
}

// rootOf is the longest of the places that holds dir; "" when none does.
func rootOf(roots []string, dir string) string {
	best := ""
	for _, r := range roots {
		if (dir == r || strings.HasPrefix(dir, strings.TrimSuffix(r, "/")+"/")) && len(r) > len(best) {
			best = r
		}
	}
	return best
}
