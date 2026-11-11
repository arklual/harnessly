package designview

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
)

// The library of a space as the store has it, for the page while the person's machine is off or slow: each
// project's title, boards, kind and time of its newest revision, read with the runtime's own code from a folder that
// holds only the files a listing reads (the model, artifact.json, canvas.json, the boards' pages); every other file is
// there empty, so the code that asks whether a board's file exists finds it. A project's item is kept until the project
// gets a newer revision.

var _ designaccess.Librarian = (*View)(nil)

// listedMax caps a file the listing lays out with its contents; a larger one is laid out empty.
const listedMax = 512 << 10

type listedProject struct {
	rev  int64
	item designaccess.LibraryItem
}

// Library lists the projects named of the space's library that the store has files of, newest first.
func (v *View) Library(ctx context.Context, spaceID string, ids []string) ([]designaccess.LibraryItem, error) {
	if strings.ContainsAny(spaceID, "/\\.") || spaceID == "" {
		return nil, ErrMissing
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]designaccess.LibraryItem, 0, len(ids))
	for _, id := range ids {
		if !design.IsLibraryID(id) || seen[id] {
			continue
		}
		seen[id] = true
		item, ok, err := v.listed(ctx, room, spaceID, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// listed is one project's item at its newest revision; false when the store has no file of it.
func (v *View) listed(ctx context.Context, room Room, spaceID, id string) (designaccess.LibraryItem, bool, error) {
	es, err := room.HistoryUnder(ctx, id+"/", 1)
	if err != nil || len(es) == 0 {
		return designaccess.LibraryItem{}, false, err
	}
	head := es[0].Rev
	key := spaceID + "/" + id
	v.mu.Lock()
	kept, ok := v.listing[key]
	v.mu.Unlock()
	if ok && kept.rev == head {
		return kept.item, kept.item.ID != "", nil
	}
	snapshot, err := room.Snapshot(ctx, id+"/", head)
	if err != nil {
		return designaccess.LibraryItem{}, false, err
	}
	item, err := v.listProject(ctx, room, spaceID, id, snapshot.Entries)
	if err != nil {
		return designaccess.LibraryItem{}, false, err
	}
	item.Rev = head
	v.mu.Lock()
	if v.listing == nil {
		v.listing = map[string]listedProject{}
	}
	v.listing[key] = listedProject{rev: head, item: item}
	v.mu.Unlock()
	return item, item.ID != "", nil
}

// listedFile: the listing reads this file's contents (the rest it only finds).
func listedFile(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".json", ".html", ".htm":
		return true
	}
	return false
}

func (v *View) listProject(ctx context.Context, room Room, spaceID, id string, entries []filestore.Entry) (designaccess.LibraryItem, error) {
	if len(entries) == 0 || len(entries) > maxFiles {
		return designaccess.LibraryItem{}, nil
	}
	if err := os.MkdirAll(filepath.Join(v.o.Dir, "list"), 0o700); err != nil {
		return designaccess.LibraryItem{}, err
	}
	root, err := os.MkdirTemp(filepath.Join(v.o.Dir, "list"), "lib-")
	if err != nil {
		return designaccess.LibraryItem{}, err
	}
	defer os.RemoveAll(root)
	pruneMu.RLock()
	defer pruneMu.RUnlock()
	var newest int64
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Path, id+"/")
		if !ok || rel == "" || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") {
			continue
		}
		newest = max(newest, e.At)
		file := filepath.Join(root, id, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return designaccess.LibraryItem{}, err
		}
		if listedFile(rel) && e.Size <= listedMax && filestore.ValidSha(e.Sha) {
			blob, err := v.blob(ctx, room, spaceID, e.Sha)
			if err != nil {
				return designaccess.LibraryItem{}, err
			}
			if err := os.Link(blob, file); err == nil {
				continue
			}
			if err := copyFile(blob, file); err != nil {
				return designaccess.LibraryItem{}, err
			}
			continue
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			return designaccess.LibraryItem{}, err
		}
	}
	lib := design.New(design.Config{
		LibraryDir: root,
		DataDir:    filepath.Join(v.o.Dir, "data"),
		Passive:    true,
		ShareProbe: func(string) design.ShareFSReport { return design.ShareFSReport{OK: true, At: time.Now()} },
	})
	a := lib.ReadArtifact(id)
	if a == nil {
		return designaccess.LibraryItem{}, nil
	}
	item := designaccess.LibraryItem{ID: id, Title: a.Title, UpdatedAt: newest, Kind: "site", Boards: []designaccess.LibraryBoard{}}
	switch {
	case lib.IsVideo(id):
		item.Kind = "video"
	case lib.IsDeck(id):
		item.Kind = "slides"
	}
	for _, b := range a.Boards {
		title := b.Title
		if title == "" {
			title = b.Path
		}
		if b.Path != "" {
			item.Boards = append(item.Boards, designaccess.LibraryBoard{Title: title, Path: b.Path})
		}
	}
	return item, nil
}

// ReadFile is a file of the space's library at its newest revision, at most limit bytes; nil when there is none.
func (v *View) ReadFile(ctx context.Context, spaceID, rel string, limit int64) ([]byte, error) {
	dir, _ := path.Split(rel)
	if dir == "" || strings.ContainsAny(spaceID, "/\\.") {
		return nil, nil
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	es, err := room.HistoryUnder(ctx, dir, 1)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	snapshot, err := room.Snapshot(ctx, dir, es[0].Rev)
	if err != nil {
		return nil, err
	}
	for _, e := range snapshot.Entries {
		if e.Path != rel || e.Size > limit || !filestore.ValidSha(e.Sha) {
			continue
		}
		rc, err := room.OpenBlob(ctx, e.Sha)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, limit))
	}
	return nil, nil
}
