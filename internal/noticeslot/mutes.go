package noticeslot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Mutes are the sources the person turned off from the plate's menu («Не показывать от …»), kept in a file so they
// stay off after a restart. Path "" keeps them in memory only (tests).
type Mutes struct {
	Path string

	mu     sync.Mutex
	loaded bool
	set    map[string]Source
}

func (m *Mutes) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	m.set = map[string]Source{}
	if m.Path == "" {
		return
	}
	raw, err := os.ReadFile(m.Path)
	if err != nil {
		return
	}
	var list []Source
	if json.Unmarshal(raw, &list) != nil {
		return
	}
	for _, s := range list {
		m.set[s.Key()] = s
	}
}

// Has tells a source that is off.
func (m *Mutes) Has(src Source) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	_, ok := m.set[src.Key()]
	return ok
}

// List is the sources that are off, by name.
func (m *Mutes) List() []Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	out := make([]Source, 0, len(m.set))
	for _, s := range m.set {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || out[i].Name == out[j].Name && out[i].Key() < out[j].Key()
	})
	return out
}

// Set turns a source off (on=false) or back on.
func (m *Mutes) Set(src Source, off bool) error {
	if src.Kind != SourceApp && src.Kind != SourceAgent || src.ID == "" {
		return ErrArgs
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if off {
		m.set[src.Key()] = src
	} else {
		delete(m.set, src.Key())
	}
	if m.Path == "" {
		return nil
	}
	list := make([]Source, 0, len(m.set))
	for _, s := range m.set {
		list = append(list, s)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return errors.Join(os.Rename(tmp, m.Path))
}
