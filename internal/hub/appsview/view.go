// Package appsview is the hub's own serving of a harnessly app's guest link (HRN-898): the page, the view's files and
// the link's area of the storage, all from the space's apps collection of the hub's store, so a link opens with the
// machine that made it off. The machine puts its live links there (guests/<person>/<hash>.json, never the token) and
// the app's package (bundles/<app>/<sum>.tgz); the hub finds a link by the token's hash, checks the archive against its
// sum, unpacks it into a folder of its own and answers with the runtime's own guest code (apps.Guests). What a guest
// may do is what it may on the machine: the view, its theme and the link's storage, nothing of the machine.
//
// The hub's API does not import this package (the apps import the agent, whose tests import the API); the hub's main
// sets api.AppsView to Hub.
package appsview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/appstore"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

// Rooms is the hub's store as the view reads it (storehub.Store).
type Rooms interface {
	AppsRoom(ctx context.Context, spaceID string) (*filestore.Store, error)
	AppsSpaces(ctx context.Context) ([]string, error)
}

// Options configure the view.
type Options struct {
	Rooms Rooms
	// Dir holds the unpacked packages (<data>/apps-view).
	Dir string
	Log *slog.Logger
	// Rescan is the least time between two looks through every space for a link the hub does not know yet.
	Rescan time.Duration
}

// View serves the guest links of every space.
type View struct {
	o Options

	mu      sync.Mutex
	index   map[string]place // the token's hash → where the link is
	scanned time.Time
	spaces  map[string]*space
	apps    map[string]apps.Installed // by the archive's sum
}

type place struct{ space, path string }

type space struct {
	guests *apps.Guests
}

// maxGuestFile bounds a link's file.
const maxGuestFile = 64 << 10

// New is the view; nil without rooms.
func New(o Options) *View {
	if o.Rooms == nil {
		return nil
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Rescan <= 0 {
		o.Rescan = 30 * time.Second
	}
	_ = os.RemoveAll(filepath.Join(o.Dir, "bundles"))
	return &View{o: o, index: map[string]place{}, spaces: map[string]*space{}, apps: map[string]apps.Installed{}}
}

// Hub is the view over the hub's store, as the hub's API takes it (api.AppsView).
func Hub(st *storehub.Store, dataDir string, log *slog.Logger) storehub.AppsViewer {
	v := New(Options{Rooms: st, Dir: filepath.Join(dataDir, "apps-view"), Log: log})
	if v == nil {
		return nil
	}
	return v
}

// ServeHTTP answers /apps/guest/<token>/...
func (v *View) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, apps.GuestPrefix), "/")
	hash := apps.GuestHash(token)
	at, ok := v.find(r.Context(), hash)
	if !ok || token == "" || len(token) > 128 {
		gone(w)
		return
	}
	sp, err := v.spaceOf(r.Context(), at.space)
	if err != nil {
		v.o.Log.Warn("apps view: a space's store not opened", "error", err)
		http.Error(w, "failed", http.StatusServiceUnavailable)
		return
	}
	sp.guests.ServeHTTP(w, r)
}

func gone(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, "This link has ended.")
}

// find is where the link of a hash is: from the index, or after a look through every space (no more often than
// Rescan), for a link made since.
func (v *View) find(ctx context.Context, hash string) (place, bool) {
	v.mu.Lock()
	at, ok := v.index[hash]
	stale := time.Since(v.scanned) >= v.o.Rescan
	v.mu.Unlock()
	if ok || !stale {
		return at, ok
	}
	index := map[string]place{}
	spaces, err := v.o.Rooms.AppsSpaces(ctx)
	if err != nil {
		v.o.Log.Warn("apps view: the spaces not listed", "error", err)
		return place{}, false
	}
	for _, sp := range spaces {
		room, err := v.o.Rooms.AppsRoom(ctx, sp)
		if err != nil {
			continue
		}
		snap, err := room.Snapshot(ctx, apps.GuestsDir, int64(1)<<62)
		if err != nil {
			continue
		}
		for _, e := range snap.Entries {
			name := e.Path[strings.LastIndex(e.Path, "/")+1:]
			if h, ok := strings.CutSuffix(name, ".json"); ok {
				index[h] = place{space: sp, path: e.Path}
			}
		}
	}
	v.mu.Lock()
	v.index, v.scanned = index, time.Now()
	at, ok = index[hash]
	v.mu.Unlock()
	return at, ok
}

func (v *View) spaceOf(ctx context.Context, spaceID string) (*space, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if sp := v.spaces[spaceID]; sp != nil {
		return sp, nil
	}
	room, err := v.o.Rooms.AppsRoom(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	storage := &apps.Storage{Root: filepath.Join(v.o.Dir, "storage", spaceID)}
	// The link's area is a document's area (guest-<id>), in the store: the hub writes it there under its own name.
	storage.UseCenter(&apps.Center{Files: appstore.RoomFiles(room, "hub", "guest")})
	sp := &space{}
	sp.guests = apps.NewGuestsFrom(func(hash string) (apps.GuestLink, apps.Installed, error) {
		return v.lookup(context.Background(), room, spaceID, hash)
	}, storage)
	v.spaces[spaceID] = sp
	return sp, nil
}

// lookup reads the link of a hash in a space's room and the app it shows, unpacked.
func (v *View) lookup(ctx context.Context, room *filestore.Store, spaceID, hash string) (apps.GuestLink, apps.Installed, error) {
	v.mu.Lock()
	at, ok := v.index[hash]
	v.mu.Unlock()
	if !ok || at.space != spaceID {
		return apps.GuestLink{}, apps.Installed{}, apps.ErrGuestGone
	}
	raw, err := readFile(ctx, room, at.path, maxGuestFile)
	if errors.Is(err, os.ErrNotExist) {
		return apps.GuestLink{}, apps.Installed{}, apps.ErrGuestGone
	}
	if err != nil {
		return apps.GuestLink{}, apps.Installed{}, err
	}
	sg, err := apps.ReadStoredGuest(raw, hash)
	if err != nil {
		return apps.GuestLink{}, apps.Installed{}, apps.ErrGuestGone
	}
	v.mu.Lock()
	app, ok := v.apps[sg.Sum]
	v.mu.Unlock()
	if ok && app.ID == sg.Link.App && app.Version == sg.Version {
		return sg.Link, app, nil
	}
	archive, err := readFile(ctx, room, apps.BundlePath(sg.Link.App, sg.Sum), apps.MaxArchiveBytes)
	if errors.Is(err, os.ErrNotExist) {
		return apps.GuestLink{}, apps.Installed{}, apps.ErrGuestGone
	}
	if err != nil {
		return apps.GuestLink{}, apps.Installed{}, err
	}
	dir := filepath.Join(v.o.Dir, "bundles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apps.GuestLink{}, apps.Installed{}, err
	}
	app, err = apps.UnpackBundle(archive, sg.Sum, sg.Link.App, sg.Version, dir)
	if err != nil {
		v.o.Log.Warn("apps view: a link's package refused", "app", sg.Link.App, "error", err)
		return apps.GuestLink{}, apps.Installed{}, apps.ErrGuestGone
	}
	v.mu.Lock()
	v.apps[sg.Sum] = app
	v.mu.Unlock()
	return sg.Link, app, nil
}

// readFile is a file of the room as it is now, at most max bytes; os.ErrNotExist when there is none.
func readFile(ctx context.Context, room *filestore.Store, path string, max int64) ([]byte, error) {
	e, err := room.Current(ctx, path)
	if err != nil {
		return nil, err
	}
	if e == nil || e.Deleted {
		return nil, os.ErrNotExist
	}
	if e.Size > max {
		return nil, errors.New("apps view: " + path + " is too large")
	}
	rc, err := room.OpenBlob(ctx, e.Sha)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var b bytes.Buffer
	if _, err := io.Copy(&b, io.LimitReader(rc, max+1)); err != nil {
		return nil, err
	}
	if int64(b.Len()) > max {
		return nil, errors.New("apps view: " + path + " is too large")
	}
	return b.Bytes(), nil
}
