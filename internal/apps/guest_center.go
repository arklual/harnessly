package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// A guest link opens with the machine that made it off: under a hub the machine puts each of its live links
// (never the token: the hash, as it keeps it) into the space's apps collection, guests/<person>/<hash>.json, with the
// version and sum of the app's package, whose archive goes to bundles/<app>/<sum>.tgz. The hub serves the link at
// <hub>/apps/guest/<token>/ from there (internal/hub/appsview): the page, the view's files out of the archive, and the
// link's own area of the storage, which is in the store already. A link that ended or was revoked leaves the store.

// StoredGuest is a link as the store keeps it.
type StoredGuest struct {
	Link GuestLink `json:"link"`
	// Version and Sum are the app's package the link shows; its archive is BundlePath(app, Sum).
	Version string `json:"version"`
	Sum     string `json:"sum"`
}

// GuestsDir is the folder of the links in the apps collection.
const GuestsDir = "guests/"

// GuestPath is where a person's link is in the apps collection.
func GuestPath(person, hash string) string { return GuestsDir + person + "/" + hash + ".json" }

// BundlePath is where an app's package is in the apps collection, by the sum of its archive.
func BundlePath(app, sum string) string { return "bundles/" + app + "/" + sum + ".tgz" }

// bundleOf is the archive of an installed package, packed once per folder and version in a process.
type bundleOf struct {
	mu    sync.Mutex
	packs map[string]packed
}

type packed struct {
	raw []byte
	sum string
}

func (b *bundleOf) get(app Installed) (packed, error) {
	key := app.Dir + "\x00" + app.Version
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.packs[key]; ok {
		return p, nil
	}
	raw, sum, err := Pack(app.Dir)
	if err != nil {
		return packed{}, err
	}
	if b.packs == nil {
		b.packs = map[string]packed{}
	}
	p := packed{raw: raw, sum: sum}
	b.packs[key] = p
	return p, nil
}

// PublishGuests makes the store's links of person this machine's live ones: each with its app's package, and the
// links that ended taken out.
func (g *Guests) PublishGuests(ctx context.Context, c CenterClient, person string, bundles *bundleOf) error {
	if person == "" {
		return nil
	}
	if bundles == nil {
		bundles = &bundleOf{}
	}
	live, err := g.List("")
	if err != nil {
		return err
	}
	snap, err := c.Snapshot(ctx, GuestsDir+person+"/", int64(1)<<62)
	if err != nil {
		return err
	}
	there := map[string]filestore.Entry{}
	for _, e := range snap.Entries {
		there[e.Path] = e
	}
	var errs []error
	for _, l := range live {
		app, err := g.reg.Get(l.App)
		if err != nil {
			continue
		}
		p, err := bundles.get(app)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", l.App, err))
			continue
		}
		bundle := BundlePath(l.App, p.sum)
		if e, err := c.Current(ctx, bundle); err != nil {
			errs = append(errs, err)
			continue
		} else if e == nil || e.Deleted {
			if _, err := c.Put(ctx, bundle, p.raw, "apps-guest"); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", bundle, err))
				continue
			}
		}
		raw, err := json.Marshal(StoredGuest{Link: l, Version: app.Version, Sum: p.sum})
		if err != nil {
			return err
		}
		path := GuestPath(person, l.Hash)
		delete(there, path)
		if e, err := c.Current(ctx, path); err == nil && e != nil && !e.Deleted && e.Sha == ArchiveSum(raw) {
			continue
		}
		if _, err := c.Put(ctx, path, raw, "apps-guest"); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
	}
	for path, e := range there {
		if _, err := c.Commit(ctx, filestore.CommitRequest{Path: path, Base: e.Rev, Deleted: true, Author: "apps-guest"}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

// ReadStoredGuest reads a link of the store and checks it is the one asked for.
func ReadStoredGuest(raw []byte, hash string) (StoredGuest, error) {
	var sg StoredGuest
	if err := json.Unmarshal(raw, &sg); err != nil {
		return sg, err
	}
	if sg.Link.Hash != hash || !idRe.MatchString(sg.Link.App) || len(sg.Sum) != 64 {
		return StoredGuest{}, ErrGuestGone
	}
	return sg, nil
}

// UnpackBundle puts an app's archive (checked against its sum) into dir/<sum> once, and answers the app as installed
// there, for a link of the store to show.
func UnpackBundle(raw []byte, sum, app, version, dir string) (Installed, error) {
	if ArchiveSum(raw) != sum {
		return Installed{}, fmt.Errorf("%w: not the sum the link names", ErrArchive)
	}
	dst := filepath.Join(dir, sum)
	if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(ManifestPath))); err != nil {
		tmp, err := os.MkdirTemp(dir, ".unpack-*")
		if err != nil {
			return Installed{}, err
		}
		defer os.RemoveAll(tmp)
		if err := Unpack(raw, tmp); err != nil {
			return Installed{}, err
		}
		if err := os.Rename(tmp, dst); err != nil && !errors.Is(err, os.ErrExist) {
			if _, statErr := os.Stat(filepath.Join(dst, filepath.FromSlash(ManifestPath))); statErr != nil {
				return Installed{}, err
			}
		}
	}
	pkg, ps := CachedPackage(dst)
	if pkg == nil || len(ps.Errors()) > 0 {
		return Installed{}, fmt.Errorf("%w: the package does not check: %v", ErrArchive, ps.Errors())
	}
	if pkg.Manifest.ID != app || pkg.Manifest.Version != version || pkg.Manifest.Trust == TrustTrusted {
		return Installed{}, fmt.Errorf("%w: not the app and version the link names", ErrArchive)
	}
	return Installed{ID: app, Version: version, Dir: dst, Manifest: pkg.Manifest}, nil
}

// guestsRefresh is how often a machine puts its links into the store again (a link made or revoked is in it by then).
var guestsRefresh = 15 * time.Second

func (g *Guests) keepInCenter(ctx context.Context, c CenterClient, person string, logf func(error)) {
	bundles := &bundleOf{}
	stamp := ""
	for ctx.Err() == nil {
		if now := g.stamp(); now != stamp {
			if err := g.PublishGuests(ctx, c, person, bundles); err != nil {
				logf(err)
			} else {
				stamp = now
			}
		}
		if !wait(ctx, guestsRefresh) {
			return
		}
	}
}

// stamp changes when the links' file does, or a link ended since.
func (g *Guests) stamp() string {
	info, err := os.Stat(g.file())
	if err != nil {
		return "none"
	}
	live, _ := g.List("")
	ids := make([]string, len(live))
	for i, l := range live {
		ids[i] = l.ID
	}
	return fmt.Sprintf("%d/%d/%s", info.Size(), info.ModTime().UnixNano(), strings.Join(ids, ","))
}
