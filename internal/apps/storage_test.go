package apps

import (
	"github.com/ly-solutions/harnessly/internal/appstore"

	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newStorage(t *testing.T) *Storage { return &Storage{Root: filepath.Join(t.TempDir(), "storage")} }

func TestStorageVersionsCount(t *testing.T) {
	s := newStorage(t)
	if _, v, ok, err := s.GetVersioned("a.b", ScopeUser, "", "k"); err != nil || ok || v != 0 {
		t.Fatalf("never written: %d %v %v", v, ok, err)
	}
	for want := int64(1); want <= 3; want++ {
		v, err := s.SetIf("a.b", ScopeUser, "", "k", json.RawMessage(`1`), nil)
		if err != nil || v != want {
			t.Fatalf("write %d: %d %v", want, v, err)
		}
	}
	// Another key counts on its own.
	if v, _ := s.SetIf("a.b", ScopeUser, "", "other", json.RawMessage(`1`), nil); v != 1 {
		t.Fatalf("other key: %d", v)
	}
	// A delete is a change: the version goes on, and a key made anew never repeats a version a reader holds.
	if err := s.Delete("a.b", ScopeUser, "", "k"); err != nil {
		t.Fatal(err)
	}
	if _, v, ok, _ := s.GetVersioned("a.b", ScopeUser, "", "k"); ok || v != 4 {
		t.Fatalf("deleted: %d %v", v, ok)
	}
	if v, _ := s.SetIf("a.b", ScopeUser, "", "k", json.RawMessage(`2`), nil); v != 5 {
		t.Fatalf("again: %d", v)
	}
	// The version record is no key of the area.
	keys, _ := s.Keys("a.b", ScopeUser, "")
	if strings.Join(keys, ",") != "k,other" {
		t.Fatalf("keys: %v", keys)
	}
	if err := s.Set("a.b", ScopeUser, "", appstore.VersionsKey, json.RawMessage(`1`)); !errors.Is(err, ErrStorageArgs) {
		t.Fatalf("reserved key: %v", err)
	}
}

func TestStorageOldDataIsVersionZero(t *testing.T) {
	s := newStorage(t)
	file := filepath.Join(s.Root, "a.b", "user", "area.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"old":{"n":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	v, version, ok, err := s.GetVersioned("a.b", ScopeUser, "", "old")
	if err != nil || !ok || version != 0 || string(v) != `{"n":1}` {
		t.Fatalf("old: %s %d %v %v", v, version, ok, err)
	}
	if got, _ := s.SetIf("a.b", ScopeUser, "", "old", json.RawMessage(`2`), ptr(0)); got != 1 {
		t.Fatalf("first write over old data: %d", got)
	}
}

func ptr(n int64) *int64 { return &n }

func TestStorageIfVersionConflict(t *testing.T) {
	s := newStorage(t)
	if _, err := s.SetIf("a.b", ScopeChat, "c1", "k", json.RawMessage(`"first"`), ptr(0)); err != nil {
		t.Fatalf("create when absent: %v", err)
	}
	// Creating again as if absent finds the key there.
	_, err := s.SetIf("a.b", ScopeChat, "c1", "k", json.RawMessage(`"second"`), ptr(0))
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Current != 1 || !errors.Is(err, ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	if v, _, _ := s.Get("a.b", ScopeChat, "c1", "k"); string(v) != `"first"` {
		t.Fatalf("overwritten: %s", v)
	}
	if v, err := s.SetIf("a.b", ScopeChat, "c1", "k", json.RawMessage(`"second"`), ptr(1)); err != nil || v != 2 {
		t.Fatalf("current: %d %v", v, err)
	}
}

func TestStorageOnlyOneOfTwoRacingWritesWins(t *testing.T) {
	s := newStorage(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.SetIf("a.b", ScopeUser, "", "k", json.RawMessage(`1`), ptr(0)); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d writers won the same version", won)
	}
}

func TestStorageQuotaIsItsOwnError(t *testing.T) {
	s := newStorage(t)
	big := json.RawMessage(`"` + strings.Repeat("x", 300<<10) + `"`)
	if _, err := s.SetIf("a.b", ScopeChat, "c1", "k", big, nil); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota: %v", err)
	}
	// Refused whole: neither the value nor the version moved.
	if _, v, ok, _ := s.GetVersioned("a.b", ScopeChat, "c1", "k"); ok || v != 0 {
		t.Fatalf("after quota: %d %v", v, ok)
	}
}

func TestStorageTellsChangesWithoutValues(t *testing.T) {
	s := newStorage(t)
	var got []Change
	stop := s.OnChange(func(c Change) { got = append(got, c) })
	_ = s.Set("a.b", ScopeUser, "", "k", json.RawMessage(`"secret"`))
	_ = s.SetMany("a.b", ScopeDoc, "d1", map[string]json.RawMessage{"y": json.RawMessage(`1`), "x": json.RawMessage(`2`)})
	_ = s.Delete("a.b", ScopeUser, "", "k")
	_ = s.Delete("a.b", ScopeUser, "", "k") // nothing there: nothing changed
	_, _ = s.SetIf("a.b", ScopeUser, "", "k", json.RawMessage(`1`), ptr(99))
	stop()
	_ = s.Set("a.b", ScopeUser, "", "late", json.RawMessage(`1`))
	want := []Change{
		{"a.b", ScopeUser, "", "k"},
		{"a.b", ScopeDoc, "d1", "x"},
		{"a.b", ScopeDoc, "d1", "y"},
		{"a.b", ScopeUser, "", "k"},
	}
	if len(got) != len(want) {
		t.Fatalf("changes: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("change %d: %+v", i, got[i])
		}
	}
}
