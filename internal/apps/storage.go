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

	ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// Storage is the machine's app storage.
type Storage struct {
	Root string
	mu   sync.Mutex
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
	if key == "" || len(key) > maxKeyBytes {
		return ErrStorageArgs
	}
	return nil
}

func (s *Storage) read(file string) (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var area map[string]json.RawMessage
	if err := json.Unmarshal(raw, &area); err != nil {
		return nil, err
	}
	if area == nil {
		area = map[string]json.RawMessage{}
	}
	return area, nil
}

// Get is the value under a key; ok is false when there is none.
func (s *Storage) Get(app string, scope Scope, owner, key string) (value json.RawMessage, ok bool, err error) {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return nil, false, err
	}
	if err := checkKey(key); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	area, err := s.read(file)
	if err != nil {
		return nil, false, err
	}
	value, ok = area[key]
	return value, ok, nil
}

// Set stores a value; a write that takes the area past its limit is refused whole.
func (s *Storage) Set(app string, scope Scope, owner, key string, value json.RawMessage) error {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return err
	}
	if err := checkKey(key); err != nil {
		return err
	}
	if !json.Valid(value) {
		return ErrStorageArgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	area, err := s.read(file)
	if err != nil {
		return err
	}
	area[key] = value
	raw, err := json.Marshal(area)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	area, err := s.read(file)
	if err != nil {
		return err
	}
	for k, v := range values {
		area[k] = v
	}
	raw, err := json.Marshal(area)
	if err != nil {
		return err
	}
	if len(raw) > scopeLimit[scope] {
		return ErrQuota
	}
	var before int64
	if info, err := os.Stat(file); err == nil {
		before = info.Size()
	}
	if s.scopeBytes(app, scope)-before+int64(len(raw)) > scopeTotal[scope] {
		return ErrQuota
	}
	return writeFileAtomic(file, raw)
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

// Delete removes a key; a key that is not there is not an error.
func (s *Storage) Delete(app string, scope Scope, owner, key string) error {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return err
	}
	if err := checkKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	area, err := s.read(file)
	if err != nil {
		return err
	}
	if _, ok := area[key]; !ok {
		return nil
	}
	delete(area, key)
	raw, err := json.Marshal(area)
	if err != nil {
		return err
	}
	return writeFileAtomic(file, raw)
}

// Keys lists an area's keys, sorted.
func (s *Storage) Keys(app string, scope Scope, owner string) ([]string, error) {
	file, err := s.file(app, scope, owner)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	area, err := s.read(file)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(area))
	for k := range area {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// ValidOwner says whether a text can name the chat or document an area belongs to.
func ValidOwner(owner string) bool { return ownerRe.MatchString(owner) }
