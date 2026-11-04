package main

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

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
