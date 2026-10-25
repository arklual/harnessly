package main

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/files"
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
// for an absolute path a turn of the conversation touched, the file's own folder and its name.
func (f *v2Files) place(ctx context.Context, conv, file string) (root, rel string, err error) {
	root, err = f.root(conv)
	if err != nil || !strings.HasPrefix(file, "/") || f.touched == nil || filepath.Clean(file) != file {
		return root, file, err
	}
	ok, err := f.touched(ctx, conv, file)
	if err != nil || !ok {
		return root, file, err
	}
	return filepath.Dir(file), filepath.Base(file), nil
}

func (f *v2Files) File(ctx context.Context, conv, file string) (wire.FileContent, error) {
	root, rel, err := f.place(ctx, conv, file)
	if err != nil {
		return wire.FileContent{}, err
	}
	obj, ee := files.ReadProjectFile(root, rel)
	if ee != nil {
		return wire.FileContent{}, fileError(ee)
	}
	return fileContent(file, obj), nil
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
