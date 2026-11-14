// Package designview is the hub's own read of a design project: a revision of the project from the store,
// put into a folder of the hub from the store's contents (the blobs by sha256, kept once and linked into every
// revision that has them), and answered by the same code that answers a guest on the runtime (design.API). Nobody's
// machine takes part: a guest link, live or pinned, opens while the author's machine is off, and opening it costs
// that machine nothing.
package designview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Room is a space's design collection as the hub reads it.
type Room interface {
	HistoryUnder(ctx context.Context, prefix string, limit int) ([]filestore.Entry, error)
	Snapshot(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error)
	OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error)
}

// Rooms opens a space's design collection.
type Rooms func(ctx context.Context, spaceID string) (Room, error)

// Options builds a View.
type Options struct {
	Rooms Rooms
	// Covers opens a space's covers collection; nil: no covers.
	Covers Rooms
	// Dir holds the revisions put into folders and the contents they share (<data>/design-view).
	Dir string
	// GuestApp is the built web page: the guest page of a link is served from it.
	GuestApp fs.FS
	Log      *slog.Logger
	// Keep is how many revisions stay in folders (default 48); MaxBytes caps one revision (default 512 MiB).
	Keep     int
	MaxBytes int64
	// Poll is how often a waiting reader looks for a new revision (default 2 s).
	Poll time.Duration
	// Live is how many projects keep their folder and library between revisions (default 12; negative: none, every
	// revision is laid out on its own as before HRN-1004).
	Live int
	// BusyWait is how long a new revision waits for slow readers of a live folder (default 1.5 s) before reads go by the
	// revision's own folder.
	BusyWait time.Duration
}

// View answers the reads of design projects from the store.
type View struct {
	o Options

	mu    sync.Mutex
	snaps map[string]*snap
	order []string // oldest first
	edits map[string]*workspace
	lives map[string]*live
	// liveOrder is the keys of lives, least recently used first.
	liveOrder []string
	// listing is the library's items by space and project (library.go).
	listing map[string]listedProject
	// derived keeps the answers about a revision in the store (derived.go); build names the code that works them out.
	derived Derived
	build   string
	// listBuilds is the items being built, by space, project and revision.
	listBuilds map[string]*listBuild
	// listGate bounds the items built at once.
	listGate chan struct{}
}

type snap struct {
	ready chan struct{}
	err   error
	api   *design.API
	base  string
}

const (
	defaultKeep  = 48
	defaultBytes = 512 << 20
	maxFiles     = 20000
	defaultLive  = 12
)

// New makes a View; nil without a store.
func New(o Options) *View {
	if o.Rooms == nil {
		return nil
	}
	if o.Keep <= 0 {
		o.Keep = defaultKeep
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = defaultBytes
	}
	if o.Poll <= 0 {
		o.Poll = 2 * time.Second
	}
	if o.Live == 0 {
		o.Live = defaultLive
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	// The folders of a previous run are not this run's: they go, and with them the contents nothing links.
	_ = os.RemoveAll(filepath.Join(o.Dir, "rev"))
	_ = os.RemoveAll(filepath.Join(o.Dir, "edit"))
	_ = os.RemoveAll(filepath.Join(o.Dir, "live"))
	_ = os.RemoveAll(filepath.Join(o.Dir, "list"))
	v := &View{o: o, snaps: map[string]*snap{}, lives: map[string]*live{}, listGate: make(chan struct{}, listBuildsAtOnce)}
	safego.Go("designview pruneBlobs", func() { v.pruneBlobs() })
	return v
}

// Head is the newest revision under the project's folder: 0 when the store has nothing of it (a project not moved yet).
func (v *View) Head(ctx context.Context, spaceID, project string) (int64, error) {
	if !design.IsLibraryID(project) {
		return 0, nil
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	es, err := room.HistoryUnder(ctx, project+"/", 1)
	if err != nil || len(es) == 0 {
		return 0, err
	}
	return es[0].Rev, nil
}

// Wait returns the project's head once it is past since, or the head as it is when ctx ends first.
func (v *View) Wait(ctx context.Context, spaceID, project string, since int64) (int64, error) {
	t := time.NewTicker(v.o.Poll)
	defer t.Stop()
	for {
		head, err := v.Head(ctx, spaceID, project)
		if err != nil || head > since {
			return head, err
		}
		select {
		case <-ctx.Done():
			return head, nil
		case <-t.C:
		}
	}
}

// WaitLibrary returns the newest revision of the space's library (every project and the shelves) once it is past
// since, or as it is when ctx ends first: the page's long poll instead of reading the library on a timer.
func (v *View) WaitLibrary(ctx context.Context, spaceID string, since int64) (int64, error) {
	if strings.ContainsAny(spaceID, "/\\.") || spaceID == "" {
		return 0, ErrMissing
	}
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	t := time.NewTicker(v.o.Poll)
	defer t.Stop()
	var head int64
	for {
		es, err := room.HistoryUnder(ctx, "", 1)
		if err != nil {
			if ctx.Err() != nil {
				return head, nil
			}
			return 0, err
		}
		if len(es) > 0 {
			head = es[0].Rev
		}
		if head > since {
			return head, nil
		}
		select {
		case <-ctx.Done():
			return head, nil
		case <-t.C:
		}
	}
}

// Serve answers a public read of the project as it was at revision at (design.API.ServeSnapshot): segs are the path
// under the project (none for its board list), pages limits the boards (nil: all), pinned says the version on the
// answer as a pinned read does.
func (v *View) Serve(w http.ResponseWriter, r *http.Request, spaceID, project string, at int64, segs, pages []string, pinned bool) error {
	// an answer about a revision is kept in the store once worked out, and given from there by every replica.
	if done, err := v.serveDerived(w, r, spaceID, project, at, segs, pages, pinned, func(w http.ResponseWriter, r *http.Request) error {
		return v.serveFolder(w, r, spaceID, project, at, segs, pages, pinned)
	}); done {
		return err
	}
	return v.serveFolder(w, r, spaceID, project, at, segs, pages, pinned)
}

// serveFolder answers from the revision put into a folder of this hub, by the runtime's own code.
func (v *View) serveFolder(w http.ResponseWriter, r *http.Request, spaceID, project string, at int64, segs, pages []string, pinned bool) error {
	// The head of a project (a read that does not name its version) is answered from the project's own folder and
	// library, which a new revision only brings up to date; a read of one version is answered from that version's.
	if !pinned && v.o.Live > 0 {
		if l := v.liveAt(r.Context(), spaceID, project, at); l != nil {
			defer l.mu.RUnlock()
			l.api.ServeSnapshot(w, r, project, segs, "", pages)
			return nil
		}
	}
	s, err := v.snapshot(r.Context(), spaceID, project, at)
	if err != nil {
		return err
	}
	rev := ""
	if pinned {
		rev = design.StoreRevID(at)
	}
	s.api.ServeSnapshot(w, r, project, segs, rev, pages)
	return nil
}

// ErrMissing is a revision with nothing of the project.
var ErrMissing = errors.New("designview: the project has no files at that revision")

func (v *View) snapshot(ctx context.Context, spaceID, project string, at int64) (*snap, error) {
	if !design.IsLibraryID(project) || at <= 0 || strings.ContainsAny(spaceID, "/\\.") {
		return nil, ErrMissing
	}
	key := fmt.Sprintf("%s/%d/%s", spaceID, at, project)
	v.mu.Lock()
	s := v.snaps[key]
	if s == nil {
		s = &snap{ready: make(chan struct{}), base: filepath.Join(v.o.Dir, "rev", spaceID, fmt.Sprint(at))}
		v.snaps[key] = s
		v.order = append(v.order, key)
		safego.Go("designview fill", func() { v.fill(s, spaceID, project, at) })
		v.evictLocked()
	}
	v.mu.Unlock()
	select {
	case <-s.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.err != nil {
		v.mu.Lock()
		if v.snaps[key] == s {
			delete(v.snaps, key)
		}
		v.mu.Unlock()
		return nil, s.err
	}
	return s, nil
}

// fill puts the revision into its folder: beside it first, moved into place when whole.
func (v *View) fill(s *snap, spaceID, project string, at int64) {
	defer close(s.ready)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	target := filepath.Join(s.base, project)
	if _, err := os.Stat(target); err != nil {
		if _, s.err = v.extract(ctx, spaceID, project, at, s.base); s.err != nil {
			v.o.Log.Warn("design view: a revision not read from the store", "space", spaceID, "project", project, "rev", at, "error", s.err)
			return
		}
	}
	lib := design.New(design.Config{
		LibraryDir: s.base,
		DataDir:    filepath.Join(v.o.Dir, "data"),
		Passive:    true,
		// The folder is the hub's own, written from the store's contents only: no one else's links or files in it.
		ShareProbe: func(string) design.ShareFSReport { return design.ShareFSReport{OK: true, At: time.Now()} },
	})
	s.api = &design.API{L: lib, GuestApp: v.o.GuestApp, PlatformAPI: "/api/platform"}
}

func (v *View) extract(ctx context.Context, spaceID, project string, at int64, base string) (map[string]string, error) {
	room, err := v.o.Rooms(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	snapshot, err := room.Snapshot(ctx, project+"/", at)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Entries) == 0 {
		return nil, ErrMissing
	}
	if len(snapshot.Entries) > maxFiles {
		return nil, fmt.Errorf("designview: %d files in one project", len(snapshot.Entries))
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(base, ".fill-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	pruneMu.RLock()
	defer pruneMu.RUnlock()
	var total int64
	files := map[string]string{}
	shas := make([]string, 0, len(snapshot.Entries))
	for _, e := range snapshot.Entries {
		rel, ok := strings.CutPrefix(e.Path, project+"/")
		if !ok || rel == "" || path.Clean(rel) != rel || strings.HasPrefix(rel, "../") || !filestore.ValidSha(e.Sha) {
			continue
		}
		if total += e.Size; total > v.o.MaxBytes {
			return nil, fmt.Errorf("designview: the project is over %d bytes", v.o.MaxBytes)
		}
		files[rel] = e.Sha
		shas = append(shas, e.Sha)
	}
	// a hub that has just started has none of the contents, and reading them one after another was most of
	// the seconds a guest waited for the first page; they are read several at a time.
	kept, err := v.blobs(ctx, room, spaceID, shas)
	if err != nil {
		return nil, err
	}
	for rel, sha := range files {
		blob := kept[sha]
		file := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, err
		}
		// A link to the kept contents: one copy on disk for every revision that has it, never written through.
		if err := os.Link(blob, file); err != nil {
			if err := copyFile(blob, file); err != nil {
				return nil, err
			}
		}
	}
	if err := os.Rename(tmp, filepath.Join(base, project)); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return files, nil
}

// fillBlobs is how many blobs of one revision are read from the store at a time.
const fillBlobs = 8

// blobs is the kept files of the blobs named, by sha, read from the store several at a time; the first failure ends
// the rest.
func (v *View) blobs(ctx context.Context, room Room, spaceID string, shas []string) (map[string]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		failure error
	)
	out := make(map[string]string, len(shas))
	gate := make(chan struct{}, fillBlobs)
	asked := map[string]bool{}
	for _, sha := range shas {
		if asked[sha] {
			continue
		}
		asked[sha] = true
		if ctx.Err() != nil {
			break
		}
		gate <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-gate }()
			file, err := v.blob(ctx, room, spaceID, sha)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if failure == nil {
					failure = err
				}
				cancel()
				return
			}
			out[sha] = file
		}()
	}
	wg.Wait()
	if failure != nil {
		return nil, failure
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// blob is the kept file of a blob, read from the store once.
func (v *View) blob(ctx context.Context, room Room, spaceID, sha string) (string, error) {
	dir := filepath.Join(v.o.Dir, "blobs", spaceID, sha[:2])
	file := filepath.Join(dir, sha)
	if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() {
		return file, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	rc, err := room.OpenBlob(ctx, sha)
	if err != nil {
		return "", fmt.Errorf("blob %s: %w", sha, err)
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(dir, ".blob-")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := newHasher()
	if _, err := io.Copy(io.MultiWriter(tmp, h), rc); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o400); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if h.hex() != sha {
		return "", fmt.Errorf("blob %s: the contents hash otherwise", sha)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return "", err
	}
	// the runtime's code names a file's version by its time and size (design.FileVersion), and that version
	// is the ETag a guest holds. The kept file's time is made of its contents, so every replica of the hub, and the same
	// replica after a restart, gives the same file the same ETag.
	at := blobTime(sha)
	_ = os.Chtimes(file, at, at)
	return file, nil
}

// blobTime is the time a kept blob's file carries: a second made of the first bytes of its sha256.
func blobTime(sha string) time.Time {
	n, err := strconv.ParseUint(sha[:8], 16, 32)
	if err != nil {
		return time.Unix(1_600_000_000, 0)
	}
	return time.Unix(1_600_000_000+int64(n%100_000_000), 0)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// evictLocked drops the oldest revisions past Keep: their folders, and the kept contents no revision links any more.
func (v *View) evictLocked() {
	for len(v.order) > v.o.Keep {
		old := v.order[0]
		v.order = v.order[1:]
		s := v.snaps[old]
		delete(v.snaps, old)
		if s == nil {
			continue
		}
		parts := strings.SplitN(old, "/", 3)
		project := parts[2]
		safego.Go("designview prune", func() {
			<-s.ready
			_ = os.RemoveAll(filepath.Join(s.base, project))
			if entries, err := os.ReadDir(s.base); err == nil && len(entries) == 0 {
				_ = os.Remove(s.base)
			}
			v.pruneBlobs()
		})
	}
}

// pruneMu keeps pruneBlobs off the kept contents while a fill links them: a blob fetched for a fill has
// a link count of one until the fill links it, and pruning in between would take it away.
var pruneMu sync.RWMutex

// pruneBlobs removes the kept contents that no revision folder links (a link count of one).
func (v *View) pruneBlobs() {
	pruneMu.Lock()
	defer pruneMu.Unlock()
	root := filepath.Join(v.o.Dir, "blobs")
	var lone []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if st, err := os.Stat(p); err == nil && linkCount(st) == 1 {
			lone = append(lone, p)
		}
		return nil
	})
	sort.Strings(lone)
	for _, p := range lone {
		_ = os.Remove(p)
	}
}

// Warm lays the project out at revision at ahead of a read: a hub that has just started has nothing of the
// store on its disk, and the first guest of a link waited for the whole project to be read. It is the read's own way
// in, without an answer: the live folder when the hub keeps them, the revision's folder otherwise.
func (v *View) Warm(ctx context.Context, spaceID, project string, at int64) error {
	if v.o.Live > 0 {
		if l := v.liveAt(ctx, spaceID, project, at); l != nil {
			l.mu.RUnlock()
			return nil
		}
	}
	_, err := v.snapshot(ctx, spaceID, project, at)
	return err
}
