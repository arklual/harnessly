// Package secretstest has a Vault-compatible store in memory for the tests of the safe's servers.
package secretstest

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// MemKV is a secrets.KV in memory: paths as keys, List answering like a KV mount (folders end in "/").
type MemKV struct {
	mu   sync.Mutex
	Data map[string]map[string]string
	// Down makes every call refused as unavailable.
	Down bool
}

// New is an empty store.
func New() *MemKV { return &MemKV{Data: map[string]map[string]string{}} }

var _ secrets.KV = (*MemKV)(nil)

func down() error {
	return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: errors.New("down")}
}

func (m *MemKV) Read(_ context.Context, p string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Down {
		return nil, down()
	}
	d, ok := m.Data[p]
	if !ok {
		return nil, &secrets.Refusal{Reason: secrets.ReasonNotFound, Err: errors.New("no such path")}
	}
	out := map[string]string{}
	for k, v := range d {
		out[k] = v
	}
	return out, nil
}

func (m *MemKV) Write(_ context.Context, p string, d map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Down {
		return down()
	}
	m.Data[p] = d
	return nil
}

func (m *MemKV) Delete(_ context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Down {
		return down()
	}
	delete(m.Data, p)
	return nil
}

func (m *MemKV) List(_ context.Context, p string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Down {
		return nil, down()
	}
	seen := map[string]bool{}
	for k := range m.Data {
		if rest, ok := strings.CutPrefix(k, p+"/"); ok {
			if i := strings.Index(rest, "/"); i >= 0 {
				seen[rest[:i+1]] = true
			} else {
				seen[rest] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil, &secrets.Refusal{Reason: secrets.ReasonNotFound, Err: errors.New("no such folder")}
	}
	out := []string{}
	for k := range seen {
		out = append(out, k)
	}
	return out, nil
}
