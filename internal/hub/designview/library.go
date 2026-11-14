package designview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/safego"
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

// A project's item is built on its own, not in the request that asked for it: the page gives the hub a
// couple of seconds and leaves, and a build that ended with the request would start from nothing every time (a cold hub
// never answered the library at all). A build outlives its request, later requests wait for the same one, and the
// projects and their files are read several at a time.
const (
	// listHeads is how many projects' newest revisions one request asks the store for at a time.
	listHeads = 8
	// listBuildsAtOnce is how many projects' items the hub builds at a time, for all requests.
	listBuildsAtOnce = 6
	// listBlobs is how many files of one project are read from the store at a time.
	listBlobs = 4
	// listBuildTime is the longest one project's item is built.
	listBuildTime = 2 * time.Minute
)

// listBuild is one project's item being built at a revision.
type listBuild struct {
	done chan struct{}
	item designaccess.LibraryItem
	err  error
}

// Library lists the projects named of the space's library that the store has files of, newest first. A project the
// store could not be read for is left out (and logged): the rest of the library is still the person's to open.
func (v *View) Library(ctx context.Context, spaceID string, ids []string) ([]designaccess.LibraryItem, error) {
	if strings.ContainsAny(spaceID, "/\\.") || spaceID == "" {
		return nil, ErrMissing
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	asked := make([]string, 0, len(ids))
	for _, id := range ids {
		if !design.IsLibraryID(id) || seen[id] {
			continue
		}
		seen[id] = true
		asked = append(asked, id)
	}
	got := make([]*designaccess.LibraryItem, len(asked))
	gate := make(chan struct{}, listHeads)
	var wg sync.WaitGroup
	for i, id := range asked {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, ok, err := v.listed(ctx, room, gate, spaceID, id)
			if err != nil {
				if ctx.Err() == nil {
					v.o.Log.Warn("design view: a project left out of the library, the store did not give it", "space", spaceID, "project", id, "error", err)
				}
				return
			}
			if ok {
				got[i] = &item
			}
		}()
	}
	wg.Wait()
	// The caller has left: what was being built goes on being built for the next one.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]designaccess.LibraryItem, 0, len(asked))
	for _, item := range got {
		if item != nil {
			out = append(out, *item)
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

// listed is one project's item at its newest revision; false when the store has no file of it. ctx bounds the wait,
// not the build.
func (v *View) listed(ctx context.Context, room Room, gate chan struct{}, spaceID, id string) (designaccess.LibraryItem, bool, error) {
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return designaccess.LibraryItem{}, false, ctx.Err()
	}
	es, err := room.HistoryUnder(ctx, id+"/", 1)
	<-gate
	if err != nil || len(es) == 0 {
		return designaccess.LibraryItem{}, false, err
	}
	head := es[0].Rev
	key := spaceID + "/" + id
	v.mu.Lock()
	kept, ok := v.listing[key]
	if ok && kept.rev == head {
		v.mu.Unlock()
		return kept.item, kept.item.ID != "", nil
	}
	bkey := fmt.Sprintf("%s@%d", key, head)
	b := v.listBuilds[bkey]
	if b == nil {
		b = &listBuild{done: make(chan struct{})}
		if v.listBuilds == nil {
			v.listBuilds = map[string]*listBuild{}
		}
		v.listBuilds[bkey] = b
		safego.Go("designview list", func() { v.buildListed(b, spaceID, id, key, bkey, head) })
	}
	v.mu.Unlock()
	select {
	case <-b.done:
	case <-ctx.Done():
		return designaccess.LibraryItem{}, false, ctx.Err()
	}
	if b.err != nil {
		return designaccess.LibraryItem{}, false, b.err
	}
	return b.item, b.item.ID != "", nil
}

// buildListed reads the project's item at head from the store and keeps it; it does not end with any request.
func (v *View) buildListed(b *listBuild, spaceID, id, key, bkey string, head int64) {
	defer close(b.done)
	defer func() {
		v.mu.Lock()
		delete(v.listBuilds, bkey)
		v.mu.Unlock()
	}()
	v.listGate <- struct{}{}
	defer func() { <-v.listGate }()
	ctx, cancel := context.WithTimeout(context.Background(), listBuildTime)
	defer cancel()
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		b.err = err
		return
	}
	item, err := v.keptListed(ctx, spaceID, id, head, func() (designaccess.LibraryItem, error) {
		snapshot, err := room.Snapshot(ctx, id+"/", head)
		if err != nil {
			return designaccess.LibraryItem{}, err
		}
		item, err := v.listProject(ctx, room, spaceID, id, snapshot.Entries)
		item.Rev = head
		return item, err
	})
	if err != nil {
		b.err = err
		return
	}
	v.mu.Lock()
	if v.listing == nil {
		v.listing = map[string]listedProject{}
	}
	// A newer revision's item, built meanwhile, is not replaced by this one.
	if kept, ok := v.listing[key]; !ok || kept.rev <= head {
		v.listing[key] = listedProject{rev: head, item: item}
	}
	v.mu.Unlock()
	b.item = item
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
	type read struct{ sha, file string }
	var reads []read
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
			reads = append(reads, read{sha: e.Sha, file: file})
			continue
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			return designaccess.LibraryItem{}, err
		}
	}
	// The files the listing reads come from the store several at a time; the first failure ends the rest.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg      sync.WaitGroup
		failMu  sync.Mutex
		failure error
	)
	gate := make(chan struct{}, listBlobs)
	for _, rd := range reads {
		if ctx.Err() != nil {
			break
		}
		gate <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-gate }()
			err := func() error {
				blob, err := v.blob(ctx, room, spaceID, rd.sha)
				if err != nil {
					return err
				}
				if err := os.Link(blob, rd.file); err == nil {
					return nil
				}
				return copyFile(blob, rd.file)
			}()
			if err != nil {
				failMu.Lock()
				if failure == nil {
					failure = err
				}
				failMu.Unlock()
				cancel()
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		return designaccess.LibraryItem{}, failure
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

// keptListed is the project's item at head as the hub's database keeps it (design_derived): worked out once
// by build from the revision's files and given from there by every replica and after a restart, instead of reading the
// project's files again. Without the database, build each time.
func (v *View) keptListed(ctx context.Context, spaceID, id string, head int64, build func() (designaccess.LibraryItem, error)) (designaccess.LibraryItem, error) {
	d, code := v.derivedStore()
	if d == nil {
		return build()
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("list\x00v1\x00%s\x00%s\x00%s\x00%d", code, spaceID, id, head)))
	key := hex.EncodeToString(sum[:])
	if a, ok := d.Lookup(ctx, key); ok {
		if rc, err := d.Open(ctx, spaceID, a.Sha); err == nil {
			body, err := io.ReadAll(io.LimitReader(rc, maxDerived))
			_ = rc.Close()
			var item designaccess.LibraryItem
			if err == nil && json.Unmarshal(body, &item) == nil {
				return item, nil
			}
		}
		// The store lost the blob or it does not read: worked out again below.
	}
	item, err := build()
	if err != nil {
		return item, err
	}
	body, err := json.Marshal(item)
	if err != nil {
		return item, nil
	}
	bs := sha256.Sum256(body)
	a := DerivedAnswer{Key: key, SpaceID: spaceID, Project: id, Rev: head, Sha: hex.EncodeToString(bs[:]), Size: int64(len(body)),
		Type: "application/json", Headers: map[string]string{}}
	if err := d.Keep(context.WithoutCancel(ctx), a, body); err != nil {
		v.o.Log.Warn("design view: a library item was not kept", "space", spaceID, "project", id, "rev", head, "error", err)
	}
	return item, nil
}

// Cover is a project's cover picture as the store has it, at most limit bytes; nil when there is none.
func (v *View) Cover(ctx context.Context, spaceID, project string, limit int64) ([]byte, error) {
	if v.o.Covers == nil || strings.ContainsAny(spaceID, "/\\.") || !(filestore.Covers{}).Synced(filestore.CoverPath(project)) {
		return nil, nil
	}
	room, err := v.o.Covers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	es, err := room.HistoryUnder(ctx, "", 1)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	snapshot, err := room.Snapshot(ctx, "", es[0].Rev)
	if err != nil {
		return nil, err
	}
	for _, e := range snapshot.Entries {
		if e.Path != filestore.CoverPath(project) || e.Size > limit || !filestore.ValidSha(e.Sha) {
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

// CoverVersions are the projects of a space that have a cover in the store, by project id, each with the cover's sha.
func (v *View) CoverVersions(ctx context.Context, spaceID string) (map[string]string, error) {
	if v.o.Covers == nil || strings.ContainsAny(spaceID, "/\\.") {
		return nil, nil
	}
	room, err := v.o.Covers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	es, err := room.HistoryUnder(ctx, "", 1)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	snapshot, err := room.Snapshot(ctx, "", es[0].Rev)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range snapshot.Entries {
		if id, ok := filestore.CoverID(e.Path); ok && filestore.ValidSha(e.Sha) {
			out[id] = e.Sha
		}
	}
	return out, nil
}
