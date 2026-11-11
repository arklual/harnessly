package designview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// live is a project kept between its revisions: one folder and one library per (space, project), brought up
// to a new revision by the files that changed, not laid out again with a library of cold caches. Only a read that asks
// for the head uses it; a read of one version (a pinned link) has its own folder per revision (snapshot).
type live struct {
	key   string
	ready chan struct{}
	err   error
	base  string

	// mu is held for reading while an answer is written from the folder and for writing while the folder changes.
	mu    sync.RWMutex
	dead  bool
	rev   int64
	api   *design.API
	files map[string]string // path under the project -> sha, at rev

	// adv lets one revision at a time be brought up (the contents are fetched outside mu, so readers go on meanwhile).
	adv sync.Mutex
}

// liveAt is the project's live folder at revision at, locked for reading (the caller unlocks); nil when the read is to go
// the other way (a revision older than the folder's, a project that cannot be laid out, or ctx ended).
func (v *View) liveAt(ctx context.Context, spaceID, project string, at int64) *live {
	if !design.IsLibraryID(project) || at <= 0 || strings.ContainsAny(spaceID, "/\\.") {
		return nil
	}
	for try := 0; try < 3; try++ {
		l := v.liveOf(spaceID, project, at)
		select {
		case <-l.ready:
		case <-ctx.Done():
			return nil
		}
		if l.err != nil {
			return nil
		}
		l.mu.RLock()
		switch {
		case l.dead:
		case l.rev == at:
			return l
		case l.rev > at:
			l.mu.RUnlock()
			return nil
		}
		l.mu.RUnlock()
		if err := v.advance(ctx, l, spaceID, project, at); err != nil {
			if errors.Is(err, errBusy) {
				// A slow reader holds the folder: this read goes by the revision's own folder, the live one catches up later.
				return nil
			}
			v.o.Log.Warn("design view: a project not brought up to a revision", "space", spaceID, "project", project, "rev", at, "error", err)
			return nil
		}
	}
	return nil
}

// liveOf is the project's live entry, started when there is none; the least recently used past Live are let go.
func (v *View) liveOf(spaceID, project string, at int64) *live {
	key := spaceID + "/" + project
	v.mu.Lock()
	l := v.lives[key]
	if l == nil {
		l = &live{key: key, ready: make(chan struct{}), base: filepath.Join(v.o.Dir, "live", spaceID, project)}
		v.lives[key] = l
		safego.Go("designview fillLive", func() { v.fillLive(l, spaceID, project, at) })
	}
	for i, k := range v.liveOrder {
		if k == key {
			v.liveOrder = append(v.liveOrder[:i], v.liveOrder[i+1:]...)
			break
		}
	}
	v.liveOrder = append(v.liveOrder, key)
	var out []*live
	for len(v.liveOrder) > v.o.Live {
		old := v.liveOrder[0]
		v.liveOrder = v.liveOrder[1:]
		if o := v.lives[old]; o != nil {
			delete(v.lives, old)
			out = append(out, o)
		}
	}
	v.mu.Unlock()
	for _, o := range out {
		safego.Go("designview dropLive", func() { v.dropLive(o) })
	}
	return l
}

// errBusy is a live folder that readers did not let go of in time to be changed.
var errBusy = errors.New("designview: the live folder is held by readers")

const (
	// busyWait is how long a revision waits for the readers of the folder to let go before the read goes another way.
	busyWait  = 1500 * time.Millisecond
	busyRetry = 10 * time.Millisecond
)

// lockWithin takes the write lock if the readers let go within wait. TryLock does not queue: a writer that waits
// never holds back the readers that come.
func (l *live) lockWithin(wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for !l.mu.TryLock() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(busyRetry)
	}
	return true
}

// dropLive takes the folder away once nobody reads from it: it is out of the map already, so no new reader finds it, and
// a reader that is slow is waited for in the background without holding back anyone.
func (v *View) dropLive(l *live) {
	<-l.ready
	for !l.mu.TryLock() {
		time.Sleep(50 * time.Millisecond)
	}
	l.dead = true
	if l.api != nil {
		l.api.L.StopWatching()
	}
	_ = os.RemoveAll(l.base)
	l.mu.Unlock()
	if parent := filepath.Dir(l.base); parent != filepath.Join(v.o.Dir, "live") {
		_ = os.Remove(parent)
	}
	v.pruneBlobs()
}

func (v *View) fillLive(l *live, spaceID, project string, at int64) {
	defer close(l.ready)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = os.RemoveAll(l.base)
	files, err := v.extract(ctx, spaceID, project, at, l.base)
	if err != nil {
		l.err = err
		v.o.Log.Warn("design view: a revision not read from the store", "space", spaceID, "project", project, "rev", at, "error", err)
		v.forget(l)
		_ = os.RemoveAll(l.base)
		return
	}
	lib := design.New(design.Config{
		LibraryDir: l.base,
		DataDir:    filepath.Join(v.o.Dir, "data"),
		Passive:    true,
		ShareProbe: func(string) design.ShareFSReport { return design.ShareFSReport{OK: true, At: time.Now()} },
	})
	l.api = &design.API{L: lib, GuestApp: v.o.GuestApp, PlatformAPI: "/api/platform"}
	l.files, l.rev = files, at
}

func (v *View) forget(l *live) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.lives[l.key] == l {
		delete(v.lives, l.key)
		for i, k := range v.liveOrder {
			if k == l.key {
				v.liveOrder = append(v.liveOrder[:i], v.liveOrder[i+1:]...)
				break
			}
		}
	}
}

// advance brings the live folder up to revision at: the contents that changed are fetched while readers go on, then put
// in under the write lock, which is a few renames. A failure half way leaves the folder as neither revision: it is let
// go and the next read lays it out again.
func (v *View) advance(ctx context.Context, l *live, spaceID, project string, at int64) error {
	l.adv.Lock()
	defer l.adv.Unlock()
	if l.rev >= at {
		return nil
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return err
	}
	snapshot, err := room.Snapshot(ctx, project+"/", at)
	if err != nil {
		return err
	}
	if len(snapshot.Entries) == 0 {
		return ErrMissing
	}
	if len(snapshot.Entries) > maxFiles {
		return fmt.Errorf("designview: %d files in one project", len(snapshot.Entries))
	}
	next := map[string]string{}
	blobs := map[string]string{} // path -> kept file, for the ones that changed
	var total int64
	for _, e := range snapshot.Entries {
		rel, ok := strings.CutPrefix(e.Path, project+"/")
		if !ok || rel == "" || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") || !filestore.ValidSha(e.Sha) {
			continue
		}
		if total += e.Size; total > v.o.MaxBytes {
			return fmt.Errorf("designview: the project is over %d bytes", v.o.MaxBytes)
		}
		next[rel] = e.Sha
		if l.files[rel] == e.Sha {
			continue
		}
		file, err := v.blob(ctx, room, spaceID, e.Sha)
		if err != nil {
			return err
		}
		blobs[rel] = file
	}
	if !l.lockWithin(v.busyWait()) {
		return errBusy
	}
	defer l.mu.Unlock()
	if l.dead {
		return context.Canceled
	}
	dir := filepath.Join(l.base, project)
	fail := func(err error) error {
		l.dead = true
		v.forget(l)
		safego.Go("designview advance", func() {
			l.api.L.StopWatching()
			_ = os.RemoveAll(l.base)
		})
		return err
	}
	for rel, blob := range blobs {
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fail(err)
		}
		tmp := dst + ".live-new"
		_ = os.Remove(tmp)
		if err := os.Link(blob, tmp); err != nil {
			if err := copyFile(blob, tmp); err != nil {
				return fail(err)
			}
		}
		if err := os.Rename(tmp, dst); err != nil {
			return fail(err)
		}
	}
	for rel := range l.files {
		if _, kept := next[rel]; !kept {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
				return fail(err)
			}
		}
	}
	l.files, l.rev = next, at
	l.api.L.Refresh()
	return nil
}

func (v *View) busyWait() time.Duration {
	if v.o.BusyWait > 0 {
		return v.o.BusyWait
	}
	return busyWait
}
