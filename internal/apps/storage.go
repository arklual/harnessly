package apps

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/appstore"
)

// The storage of an app's views (hl.storage.<scope>): key-value areas, one file each (internal/appstore). Install,
// user and space are one area each; chat and doc are one area per chat or document, named by the owner the page gives.
// On a machine of its own they are all under <data>/apps/storage/<app id>/<scope>/. Under a hub only the
// install's area is there: user, space, chat and doc are in the space's apps collection of the hub's store, the same on
// every machine of the space and there with the machines off, the user's area named by the person
// (storage/<app>/user/<person>.json, which only the person's machines see). Removing an install leaves the data, so a
// reinstall finds it (the 30-day purge of the spec is not built yet).

// Scope is an area of the storage.
type Scope = appstore.Scope

const (
	ScopeInstall = appstore.ScopeInstall
	ScopeUser    = appstore.ScopeUser
	ScopeChat    = appstore.ScopeChat
	ScopeSpace   = appstore.ScopeSpace
	ScopeDoc     = appstore.ScopeDoc
)

// scopeLimit and scopeTotal are the limits of an area and of all the areas of a scope of an app (appstore).
var (
	scopeLimit = appstore.Limit
	scopeTotal = appstore.Total
)

var (
	// ErrQuota: the write would take the area past its limit.
	ErrQuota = appstore.ErrQuota
	// ErrStorageArgs: a scope, owner or key that is not one.
	ErrStorageArgs = appstore.ErrArgs
	// ErrConflict: a write that named the version it read (ifVersion) found the key moved on. The error is a
	// *ConflictError, which carries the version the key has now.
	ErrConflict = appstore.ErrConflict
)

// ConflictError is the refusal of a write whose ifVersion is not the key's version; Current is the version it has.
type ConflictError = appstore.ConflictError

// Change is a key of an area that was written or deleted; it carries no value.
type Change struct {
	App   string
	Scope Scope
	// Owner is the chat or document of an area per owner; empty for the other scopes.
	Owner string
	Key   string
}

// Storage is the machine's app storage.
type Storage struct {
	Root string
	mu   sync.Mutex

	centerMu sync.Mutex
	center   *Center
	// seen is the version of every key of the center's areas as this machine last knew them: a change another machine
	// made is told to the views (OnChange) key by key.
	seen map[string]map[string]int64

	subMu sync.Mutex
	subs  map[int]func(Change)
	next  int
}

// OnChange calls fn for every key written or deleted from now on, whoever wrote it (a view, an agent's host tool, the
// install's settings form). fn runs on the writer's goroutine after the write is on disk and must not block. It
// returns the function that stops the calls.
func (s *Storage) OnChange(fn func(Change)) (cancel func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.subs == nil {
		s.subs = map[int]func(Change){}
	}
	id := s.next
	s.next++
	s.subs[id] = fn
	return func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		delete(s.subs, id)
	}
}

func (s *Storage) emit(changes ...Change) {
	s.subMu.Lock()
	fns := make([]func(Change), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.subMu.Unlock()
	for _, c := range changes {
		for _, fn := range fns {
			fn(c)
		}
	}
}

var (
	storagesMu sync.Mutex
	storages   = map[string]*Storage{}
)

// OpenStorage is the process's one Storage of a data folder.
func OpenStorage(dataDir string) *Storage {
	root := filepath.Join(dataDir, "apps", "storage")
	storagesMu.Lock()
	defer storagesMu.Unlock()
	if s := storages[root]; s != nil {
		return s
	}
	s := &Storage{Root: root}
	storages[root] = s
	return s
}

// Center is the hub's store as the storage keeps its areas there: Files in the space's apps collection, Person the
// name of the person's user area ("" keeps the user area on the machine, as with a hub that names nobody).
type Center struct {
	Files  appstore.Files
	Person string
}

// UseCenter keeps the user, space, chat and doc areas in the hub's store from now on; nil puts them back on the
// machine. The runtime sets it when it links to a hub's store (cmd/runtime).
func (s *Storage) UseCenter(c *Center) {
	s.centerMu.Lock()
	defer s.centerMu.Unlock()
	s.center = c
	s.seen = map[string]map[string]int64{}
}

func (s *Storage) centerOf() *Center {
	s.centerMu.Lock()
	defer s.centerMu.Unlock()
	return s.center
}

// centerTimeout bounds one call of a view to the hub's store.
const centerTimeout = 15 * time.Second

// where is the files and the path of an area, and the unlock of what a read and its write hold.
func (s *Storage) where(app string, scope Scope, owner string) (appstore.Files, string, func(), error) {
	name := "area"
	if scope.PerOwner() {
		if !appstore.ValidOwner(owner) {
			return nil, "", nil, ErrStorageArgs
		}
		name = owner
	}
	c := s.centerOf()
	personal := scope == ScopeUser || scope == ScopeChat
	if c != nil && scope != ScopeInstall && (!personal || c.Person != "") {
		switch scope {
		case ScopeUser:
			name = c.Person
		case ScopeChat:
			name = c.Person + "." + owner
		}
		rel, err := appstore.Path(app, scope, name)
		// The store's revisions keep two machines' writes apart; a lock of this process would only queue the views.
		return c.Files, rel, func() {}, err
	}
	rel, err := appstore.Path(app, scope, name)
	if err != nil {
		return nil, "", nil, err
	}
	s.mu.Lock()
	return appstore.Folder(s.Root), rel, s.mu.Unlock, nil
}

func (s *Storage) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), centerTimeout)
}

// Get is the value under a key; ok is false when there is none.
func (s *Storage) Get(app string, scope Scope, owner, key string) (value json.RawMessage, ok bool, err error) {
	value, _, ok, err = s.GetVersioned(app, scope, owner, key)
	return value, ok, err
}

// GetVersioned is Get with the key's version (0 for data kept before versions, or a key never written).
func (s *Storage) GetVersioned(app string, scope Scope, owner, key string) (value json.RawMessage, version int64, ok bool, err error) {
	if err := appstore.CheckKey(key); err != nil {
		return nil, 0, false, err
	}
	files, rel, unlock, err := s.where(app, scope, owner)
	if err != nil {
		return nil, 0, false, err
	}
	defer unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	a, err := appstore.Read(ctx, files, rel)
	if err != nil {
		return nil, 0, false, err
	}
	value, ok = a.Values[key]
	return value, a.Versions[key], ok, nil
}

// Set stores a value; a write that takes the area past its limit is refused whole.
func (s *Storage) Set(app string, scope Scope, owner, key string, value json.RawMessage) error {
	_, err := s.SetIf(app, scope, owner, key, value, nil)
	return err
}

// SetIf stores a value and answers the key's new version. With ifVersion the write is made only if the key is at that
// version (0 is a key never written or kept before versions): otherwise it is refused with a *ConflictError and
// nothing changes.
func (s *Storage) SetIf(app string, scope Scope, owner, key string, value json.RawMessage, ifVersion *int64) (int64, error) {
	if err := appstore.CheckKey(key); err != nil {
		return 0, err
	}
	if !json.Valid(value) {
		return 0, ErrStorageArgs
	}
	var version int64
	err := s.update(app, scope, owner, true, func(a *appstore.Area) (bool, error) {
		if ifVersion != nil && *ifVersion != a.Versions[key] {
			return false, &ConflictError{Current: a.Versions[key]}
		}
		a.Values[key] = value
		a.Versions[key]++
		version = a.Versions[key]
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	s.emit(Change{App: app, Scope: scope, Owner: owner, Key: key})
	return version, nil
}

// update changes an area where it is kept, and remembers what a center's area is now.
func (s *Storage) update(app string, scope Scope, owner string, quota bool, change func(*appstore.Area) (bool, error)) error {
	files, rel, unlock, err := s.where(app, scope, owner)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	a, err := appstore.Update(ctx, files, scope, rel, quota, change)
	if err != nil {
		return err
	}
	if _, local := files.(appstore.Folder); !local {
		s.remember(rel, a)
	}
	return nil
}

// SetMany stores several keys of one area in a single write: all of them or, when the area would pass its limit, none.
func (s *Storage) SetMany(app string, scope Scope, owner string, values map[string]json.RawMessage) error {
	for k, v := range values {
		if err := appstore.CheckKey(k); err != nil {
			return err
		}
		if !json.Valid(v) {
			return ErrStorageArgs
		}
	}
	err := s.update(app, scope, owner, true, func(a *appstore.Area) (bool, error) {
		for k, v := range values {
			a.Values[k] = v
			a.Versions[k]++
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changes := make([]Change, len(keys))
	for i, k := range keys {
		changes[i] = Change{App: app, Scope: scope, Owner: owner, Key: k}
	}
	s.emit(changes...)
	return nil
}

// Delete removes a key; a key that is not there is not an error. The key's version goes up by one and is kept.
func (s *Storage) Delete(app string, scope Scope, owner, key string) error {
	if err := appstore.CheckKey(key); err != nil {
		return err
	}
	changed := false
	err := s.update(app, scope, owner, false, func(a *appstore.Area) (bool, error) {
		if _, ok := a.Values[key]; !ok {
			return false, nil
		}
		delete(a.Values, key)
		a.Versions[key]++
		changed = true
		return true, nil
	})
	if err != nil || !changed {
		return err
	}
	s.emit(Change{App: app, Scope: scope, Owner: owner, Key: key})
	return nil
}

// Keys lists an area's keys, sorted.
func (s *Storage) Keys(app string, scope Scope, owner string) ([]string, error) {
	files, rel, unlock, err := s.where(app, scope, owner)
	if err != nil {
		return nil, err
	}
	defer unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	a, err := appstore.Read(ctx, files, rel)
	if err != nil {
		return nil, err
	}
	return a.Keys(), nil
}

// ValidOwner says whether a text can name the chat or document an area belongs to.
func ValidOwner(owner string) bool { return appstore.ValidOwner(owner) }

func (s *Storage) remember(rel string, a *appstore.Area) {
	s.centerMu.Lock()
	defer s.centerMu.Unlock()
	if s.seen == nil {
		s.seen = map[string]map[string]int64{}
	}
	v := make(map[string]int64, len(a.Versions))
	for k, n := range a.Versions {
		v[k] = n
	}
	s.seen[rel] = v
}

// NoticeCenter is an area of the center as the store has it now (rel under storage/; nil raw for one deleted),
// whoever wrote it: the views hear of every key whose version moved since this machine last knew the area. quiet only
// learns the area, for the catch-up when the storage first links.
func (s *Storage) NoticeCenter(rel string, raw []byte, quiet bool) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".json") {
		return
	}
	app, scope, name := parts[0], Scope(parts[1]), strings.TrimSuffix(parts[2], ".json")
	if _, err := appstore.Path(app, scope, name); err != nil || scope == ScopeInstall {
		return
	}
	a, err := appstore.Parse(raw)
	if err != nil {
		return
	}
	s.centerMu.Lock()
	c := s.center
	owner := ""
	switch {
	case c == nil, scope == ScopeUser && name != c.Person:
		s.centerMu.Unlock()
		return
	case scope == ScopeChat:
		var mine bool
		owner, mine = strings.CutPrefix(name, c.Person+".")
		if !mine || c.Person == "" {
			s.centerMu.Unlock()
			return
		}
	case scope == ScopeDoc:
		owner = name
	}
	if s.seen == nil {
		s.seen = map[string]map[string]int64{}
	}
	before := s.seen[rel]
	now := make(map[string]int64, len(a.Versions))
	for k, n := range a.Versions {
		now[k] = n
	}
	s.seen[rel] = now
	s.centerMu.Unlock()
	if quiet {
		return
	}
	var keys []string
	for k, n := range now {
		if before[k] != n {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	changes := make([]Change, len(keys))
	for i, k := range keys {
		changes[i] = Change{App: app, Scope: scope, Owner: owner, Key: k}
	}
	s.emit(changes...)
}
