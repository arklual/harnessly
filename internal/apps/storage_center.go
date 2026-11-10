package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/appstore"
	"github.com/ly-solutions/harnessly/internal/filestore"
)

// HRN-898. Under a hub the user, space, chat and doc areas are in the space's apps collection of the hub's store. When
// the runtime links to it, the areas it kept on its disk go there first (the move), then the views read and write the
// store, and every machine of the space hears of the others' writes through the store's changes.
//
// The move puts every key of a local area that the store's area has never had into it, on top of the store's
// revision: a space's area two machines kept apart before comes out as one, and a key both have keeps the store's
// value. Nothing on the disk is deleted (the machine goes back to it if the runtime is rolled back). A journal beside
// the areas (MovedFile) says per area what was moved, so a restart goes on from there and an area moved before is
// moved again only if its file changed since.

// CenterClient is the store's API the apps speak under a hub (filesync.Client).
type CenterClient interface {
	appstore.Client
	Changes(ctx context.Context, since int64, wait time.Duration) (filestore.Changes, error)
	Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error)
}

// HubLink is what the apps keep in the hub's store: the storage's areas, and the team's apps when Registry is set.
type HubLink struct {
	Client CenterClient
	// Person names the person's user and chat areas ("" keeps them on the machine).
	Person   string
	Registry *Registry
	// Host is the runtime's version, which a team's package is checked against.
	Host string
	Log  *slog.Logger
	Now  func() time.Time
}

// MovedFile is the move's journal, under the storage's root.
const MovedFile = ".moved-to-hub.json"

// Moved is the journal's record of one local area.
type Moved struct {
	// To is the area's path in the store, under storage/.
	To string `json:"to"`
	// Sha is the local file's sha256 when it was moved.
	Sha string `json:"sha"`
	// Status: "moved" (the keys the store lacked are there now), "same" (the store had them all), "skipped" (a user
	// or chat area with no person to name it: it stays on the machine), "failed".
	Status string `json:"status"`
	Keys   int    `json:"keys,omitempty"`
	Error  string `json:"error,omitempty"`
	At     string `json:"at"`
}

// centerRetry is how long the storage waits before it asks a store that did not answer again.
var centerRetry = 30 * time.Second

// RunCenter keeps the apps in the hub's store until ctx ends: it moves the local areas, switches the storage to the
// store, writes this machine's team there and writes it again whenever it changes here, and follows the store's
// changes (the areas' keys to the views, the team to the registry). It returns when ctx ends; the storage stays on
// the store.
func (s *Storage) RunCenter(ctx context.Context, l HubLink) {
	log := l.Log
	if log == nil {
		log = slog.Default()
	}
	if l.Now == nil {
		l.Now = time.Now
	}
	c, person := l.Client, l.Person
	files := appstore.ClientFiles(c, "app-storage")
	for {
		moved, failed, err := s.MoveToCenter(ctx, files, person)
		if err == nil && failed == 0 {
			if moved > 0 {
				log.Info("apps: the storage's areas moved to the hub's store", "areas", moved)
			}
			break
		}
		log.Warn("apps: the storage's areas not all moved to the hub's store, it goes on on the machine", "failed", failed, "error", err)
		if !wait(ctx, centerRetry) {
			return
		}
	}
	s.UseCenter(&Center{Files: files, Person: person})
	if l.Registry != nil {
		publish := make(chan struct{}, 1)
		l.Registry.OnTeamChange(func() {
			select {
			case publish <- struct{}{}:
			default:
			}
		})
		publish <- struct{}{}
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-publish:
				}
				for ctx.Err() == nil {
					err := l.Registry.PublishTeam(ctx, c)
					if err == nil {
						break
					}
					log.Warn("apps: the team not written to the hub's store", "error", err)
					if !wait(ctx, centerRetry) {
						return
					}
				}
			}
		}()
	}
	if l.Registry != nil && person != "" {
		go NewGuests(l.Registry, s).keepInCenter(ctx, c, person, func(err error) {
			log.Warn("apps: the guest links not all put into the hub's store", "error", err)
		})
	}
	s.follow(ctx, l, files, log)
}

// follow tells the views of the store's changes to the areas (quietly for what was there when it started) and applies
// the team the store has.
func (s *Storage) follow(ctx context.Context, l HubLink, files appstore.Files, log *slog.Logger) {
	c := l.Client
	since, quiet := int64(0), true
	for ctx.Err() == nil {
		hold := 25 * time.Second
		if quiet {
			hold = 0
		}
		ch, err := c.Changes(ctx, since, hold)
		if err != nil {
			if ctx.Err() == nil {
				log.Debug("apps: the store's changes not read", "error", err)
			}
			if !wait(ctx, 5*time.Second) {
				return
			}
			continue
		}
		for _, e := range ch.Entries {
			if e.Path == TeamStateFile && l.Registry != nil {
				problems, err := l.Registry.ApplyStoredTeam(ctx, c, e, l.Host, l.Now())
				if err != nil || len(problems) > 0 {
					log.Warn("apps: the team from the hub's store not applied whole", "problems", problems, "error", err)
				}
				continue
			}
			rel, ok := strings.CutPrefix(e.Path, appstore.StorePrefix)
			if !ok {
				continue
			}
			var raw []byte
			if !e.Deleted {
				if raw, _, err = files.Read(ctx, rel); err != nil {
					continue
				}
			}
			s.NoticeCenter(rel, raw, quiet)
		}
		since = max(since, ch.Rev)
		if !ch.More {
			quiet = false
		}
	}
}

// MoveToCenter moves the local user, space, chat and doc areas into files (see above) and answers how many it moved
// now and how many failed; an area the journal has as moved with the same file is not read again.
func (s *Storage) MoveToCenter(ctx context.Context, files appstore.Files, person string) (moved, failed int, err error) {
	journal := map[string]Moved{}
	raw, err := os.ReadFile(filepath.Join(s.Root, MovedFile))
	switch {
	case err == nil:
		_ = json.Unmarshal(raw, &journal)
	case !errors.Is(err, fs.ErrNotExist):
		return 0, 0, err
	}
	locals, err := s.localAreas()
	if err != nil {
		return 0, 0, err
	}
	for _, rel := range locals {
		if ctx.Err() != nil {
			return moved, failed, ctx.Err()
		}
		data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(rel)))
		if err != nil {
			return moved, failed, err
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if j, ok := journal[rel]; ok && j.Sha == sha && j.Status != "failed" {
			continue
		}
		rec := s.moveArea(ctx, files, rel, data, person)
		rec.Sha, rec.At = sha, time.Now().UTC().Format(time.RFC3339)
		journal[rel] = rec
		switch rec.Status {
		case "failed":
			failed++
		case "moved":
			moved++
		}
	}
	out, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return moved, failed, err
	}
	if err := appstore.WriteFileAtomic(filepath.Join(s.Root, MovedFile), out); err != nil {
		return moved, failed, err
	}
	return moved, failed, nil
}

// localAreas are the paths of the local areas that belong in the store, sorted.
func (s *Storage) localAreas() ([]string, error) {
	var out []string
	err := filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == s.Root {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(s.Root, p)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || !strings.HasSuffix(parts[2], ".json") || strings.HasPrefix(parts[2], ".") {
			return nil
		}
		scope := Scope(parts[1])
		if scope == ScopeInstall || !scope.Known() || !appstore.ValidApp(parts[0]) {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func (s *Storage) moveArea(ctx context.Context, files appstore.Files, rel string, data []byte, person string) Moved {
	parts := strings.Split(rel, "/")
	app, scope, name := parts[0], Scope(parts[1]), strings.TrimSuffix(parts[2], ".json")
	switch {
	case (scope == ScopeUser || scope == ScopeChat) && person == "":
		return Moved{Status: "skipped"}
	case scope == ScopeUser:
		name = person
	case scope == ScopeChat:
		name = person + "." + name
	}
	to, err := appstore.Path(app, scope, name)
	if err != nil {
		return Moved{Status: "failed", Error: err.Error()}
	}
	local, err := appstore.Parse(data)
	if err != nil {
		return Moved{To: to, Status: "failed", Error: "the local area is not JSON: " + err.Error()}
	}
	added := 0
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	_, err = appstore.Update(cctx, files, scope, to, false, func(a *appstore.Area) (bool, error) {
		added = 0
		for k, v := range local.Values {
			if _, has := a.Values[k]; has || a.Versions[k] != 0 {
				continue
			}
			a.Values[k] = v
			a.Versions[k] = max(local.Versions[k], 1)
			added++
		}
		return added > 0, nil
	})
	if err != nil {
		return Moved{To: to, Status: "failed", Error: err.Error()}
	}
	// Checked against the store as it is now: every local key is there, with the local value or the store's own.
	now, err := appstore.Read(cctx, files, to)
	if err != nil {
		return Moved{To: to, Status: "failed", Error: err.Error()}
	}
	for k := range local.Values {
		if _, ok := now.Values[k]; !ok && now.Versions[k] == 0 {
			return Moved{To: to, Status: "failed", Error: "the key " + k + " is not in the store after the move"}
		}
	}
	if added == 0 {
		return Moved{To: to, Status: "same"}
	}
	return Moved{To: to, Status: "moved", Keys: added}
}

func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
