package apps

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

// The storage of an app's views (hl.storage.<scope>): key-value areas under <data>/apps/storage/<app id>/<scope>/.
// Install, user and space are one area each (a machine has one person; a team's space lives where its runtime does);
// chat and doc are one area per chat or document, named by the owner the page gives. Removing an install leaves the
// folder, so a reinstall finds its data (the 30-day purge of the spec is not built yet).

// Scope is an area of the storage.
type Scope string

const (
	ScopeInstall Scope = "install"
	ScopeUser    Scope = "user"
	ScopeChat    Scope = "chat"
	ScopeSpace   Scope = "space"
	ScopeDoc     Scope = "doc"
)

// scopeLimit is the most one area may hold, in bytes of its JSON file.
var scopeLimit = map[Scope]int{
	ScopeInstall: 5 << 20,
	ScopeUser:    1 << 20,
	ScopeChat:    256 << 10,
	ScopeSpace:   10 << 20,
	ScopeDoc:     100 << 10,
}

// scopeTotal is the most all the areas of one scope of one app may hold together, so many chats or documents cannot
// add up past what the scope is meant to cost.
var scopeTotal = map[Scope]int64{
	ScopeInstall: 5 << 20,
	ScopeUser:    1 << 20,
	ScopeChat:    20 << 20,
	ScopeSpace:   10 << 20,
	ScopeDoc:     20 << 20,
}

// perOwner are the scopes whose area belongs to a chat or a document.
func (s Scope) perOwner() bool { return s == ScopeChat || s == ScopeDoc }

const maxKeyBytes = 256

var (
	// ErrQuota: the write would take the area past its limit.
	ErrQuota = errors.New("apps: the storage area is full")
	// ErrStorageArgs: a scope, owner or key that is not one.
	ErrStorageArgs = errors.New("apps: bad storage arguments")
	// ErrConflict: a write that named the version it read (ifVersion) found the key moved on. The error is a
	// *ConflictError, which carries the version the key has now.
	ErrConflict = errors.New("apps: the key changed since it was read")

	ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// ConflictError is the refusal of a write whose ifVersion is not the key's version; Current is the version it has.
type ConflictError struct{ Current int64 }

func (e *ConflictError) Error() string        { return ErrConflict.Error() }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// versionsKey is where an area's file keeps the version of each key, next to the values. A key's version is a counter
// that only grows: a write adds one, a delete adds one too and keeps the number, so a key made anew never repeats a
// version a reader holds. A key with no version (data written before versions) is at 0. The name is not a key a
// caller can use.
const versionsKey = "__hl_versions"

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

func (s *Storage) file(app string, scope Scope, owner string) (string, error) {
	if !idRe.MatchString(app) {
		return "", ErrStorageArgs
	}
	if _, ok := scopeLimit[scope]; !ok {
		return "", ErrStorageArgs
	}
	name := "area"
	if scope.perOwner() {
		if !ownerRe.MatchString(owner) {
			return "", ErrStorageArgs
		}
		name = owner
	}
	return filepath.Join(s.Root, app, string(scope), name+".json"), nil
}

func checkKey(key string) error {
	if key == "" || len(key) > maxKeyBytes || key == versionsKey {
		return ErrStorageArgs
	}
	return nil
}

// area is an area as its file holds it: the values and the version of each key.
type area struct {
	values   map[string]json.RawMessage
	versions map[string]int64
}

func (s *Storage) read(file string) (*area, error) {
	a := &area{values: map[string]json.RawMessage{}, versions: map[string]int64{}}
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	for k, v := range all {
		if k == versionsKey {
			// A damaged versions record is the same as none: every key is back at 0.
			_ = json.Unmarshal(v, &a.versions)
			if a.versions == nil {
				a.versions = map[string]int64{}
			}
			continue
		}
		a.values[k] = v
	}
	return a, nil
}

func (a *area) marshal() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(a.values)+1)
	for k, v := range a.values {
		out[k] = v
	}
	if len(a.versions) > 0 {
		raw, err := json.Marshal(a.versions)
		if err != nil {
			return nil, err
		}
		out[versionsKey] = raw
	}
	return json.Marshal(out)
}

// Get is the value under a key; ok is false when there is none.
func (s *Storage) Get(app string, scope Scope, owner, key string) (value json.RawMessage, ok bool, err error) {
	value, _, ok, err = s.GetVersioned(app, scope, owner, key)
	return value, ok, err
}

// GetVersioned is Get with the key's version (0 for data kept before versions, or a key never written).
func (s *Storage) GetVersioned(app string, scope Scope, owner, key string) (value json.RawMessage, version int64, ok bool, err error) {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return nil, 0, false, err
	}
	if err := checkKey(key); err != nil {
		return nil, 0, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.read(file)
	if err != nil {
		return nil, 0, false, err
	}
	value, ok = a.values[key]
	return value, a.versions[key], ok, nil
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
	file, err := s.file(app, scope, owner)
	if err != nil {
		return 0, err
	}
	if err := checkKey(key); err != nil {
		return 0, err
	}
	if !json.Valid(value) {
		return 0, ErrStorageArgs
	}
	version, err := func() (int64, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		a, err := s.read(file)
		if err != nil {
			return 0, err
		}
		if ifVersion != nil && *ifVersion != a.versions[key] {
			return 0, &ConflictError{Current: a.versions[key]}
		}
		a.values[key] = value
		a.versions[key]++
		return a.versions[key], s.write(app, scope, file, a)
	}()
	if err != nil {
		return 0, err
	}
	s.emit(Change{App: app, Scope: scope, Owner: owner, Key: key})
	return version, nil
}

// write saves an area if it stays inside the limits of its scope.
func (s *Storage) write(app string, scope Scope, file string, a *area) error {
	raw, err := a.marshal()
	if err != nil {
		return err
	}
	if len(raw) > scopeLimit[scope] {
		return ErrQuota
	}
	// The scope's other areas, and this one as it was, are on disk: the write adds the difference.
	var before int64
	if info, err := os.Stat(file); err == nil {
		before = info.Size()
	}
	if s.scopeBytes(app, scope)-before+int64(len(raw)) > scopeTotal[scope] {
		return ErrQuota
	}
	return writeFileAtomic(file, raw)
}

// SetMany stores several keys of one area in a single write: all of them or, when the area would pass its limit, none.
func (s *Storage) SetMany(app string, scope Scope, owner string, values map[string]json.RawMessage) error {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return err
	}
	for k, v := range values {
		if err := checkKey(k); err != nil {
			return err
		}
		if !json.Valid(v) {
			return ErrStorageArgs
		}
	}
	err = func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		a, err := s.read(file)
		if err != nil {
			return err
		}
		for k, v := range values {
			a.values[k] = v
			a.versions[k]++
		}
		return s.write(app, scope, file, a)
	}()
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

// scopeBytes is what the areas of a scope of an app weigh on disk.
func (s *Storage) scopeBytes(app string, scope Scope) int64 {
	var total int64
	_ = filepath.WalkDir(filepath.Join(s.Root, app, string(scope)), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Delete removes a key; a key that is not there is not an error. The key's version goes up by one and is kept.
func (s *Storage) Delete(app string, scope Scope, owner, key string) error {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return err
	}
	if err := checkKey(key); err != nil {
		return err
	}
	changed, err := func() (bool, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		a, err := s.read(file)
		if err != nil {
			return false, err
		}
		if _, ok := a.values[key]; !ok {
			return false, nil
		}
		delete(a.values, key)
		a.versions[key]++
		raw, err := a.marshal()
		if err != nil {
			return false, err
		}
		return true, writeFileAtomic(file, raw)
	}()
	if err != nil || !changed {
		return err
	}
	s.emit(Change{App: app, Scope: scope, Owner: owner, Key: key})
	return nil
}

// Keys lists an area's keys, sorted.
func (s *Storage) Keys(app string, scope Scope, owner string) ([]string, error) {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.read(file)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(a.values))
	for k := range a.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// ValidOwner says whether a text can name the chat or document an area belongs to.
func ValidOwner(owner string) bool { return ownerRe.MatchString(owner) }
