package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func moodboardRegistry(t *testing.T) (*Registry, KindInfo, SinopiaKind, *Storage) {
	t.Helper()
	r := newRegistry(t)
	abs, _ := filepath.Abs(filepath.Join("..", "..", "docs", "apps", "moodboard"))
	if _, _, err := r.Install(abs, InstallOptions{HostVersion: "99.0.0", Dev: true}); err != nil {
		t.Fatal(err)
	}
	info, k, err := r.Kind("acme.moodboard")
	if err != nil {
		t.Fatal(err)
	}
	return r, info, k, OpenStorage(t.TempDir())
}

func docValue(t *testing.T, st *Storage, key string) string {
	t.Helper()
	v, ok, err := st.Get("acme.moodboard", ScopeDoc, "doc1", key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return "<none>"
	}
	return string(v)
}

func TestOpenDocumentMigrates(t *testing.T) {
	r, info, k, st := moodboardRegistry(t)
	set := func(key, v string) {
		if err := st.Set("acme.moodboard", ScopeDoc, "doc1", key, json.RawMessage(v)); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a document with no recorded version is left as it is", func(t *testing.T) {
		set("board", `{"notes":[]}`)
		if got := r.OpenDocument(st, "doc1", info, k); got.ReadOnly || got.Migrated || docValue(t, st, "board") != `{"notes":[]}` {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("an old document is migrated, its old copy kept, and it can be reverted", func(t *testing.T) {
		set("board", `{"notes":[{"text":"a"}]}`)
		set(SchemaKey, `1`)
		got := r.OpenDocument(st, "doc1", info, k)
		if got.ReadOnly || !got.Migrated || got.From != 1 || got.Version != 2 {
			t.Fatalf("%+v", got)
		}
		if v := docValue(t, st, "board"); v != `{"cards":[{"text":"a"}]}` {
			t.Fatalf("migrated: %s", v)
		}
		if v := docValue(t, st, "board@v1"); v != `{"notes":[{"text":"a"}]}` || docValue(t, st, SchemaKey) != "2" {
			t.Fatalf("old copy %s, version %s", v, docValue(t, st, SchemaKey))
		}
		// Opened again, it is current and nothing runs.
		if again := r.OpenDocument(st, "doc1", info, k); again.Migrated || again.ReadOnly {
			t.Fatalf("again: %+v", again)
		}
		if err := r.RevertMigration(st, "doc1", info, k, 1); err != nil {
			t.Fatal(err)
		}
		if v := docValue(t, st, "board"); v != `{"notes":[{"text":"a"}]}` || docValue(t, st, SchemaKey) != "1" || docValue(t, st, "board@v1") != "<none>" {
			t.Fatalf("reverted: %s %s", v, docValue(t, st, SchemaKey))
		}
		if err := r.RevertMigration(st, "doc1", info, k, 1); err == nil {
			t.Fatal("a copy that is gone was reverted to")
		}
	})

	t.Run("a document that does not fit is read-only and unchanged", func(t *testing.T) {
		set("board", `["not an object"]`)
		set(SchemaKey, `1`)
		_ = st.Delete("acme.moodboard", ScopeDoc, "doc1", "board@v1")
		got := r.OpenDocument(st, "doc1", info, k)
		if !got.ReadOnly || got.Migrated || got.Reason == "" || got.Code != ReadOnlyFailed {
			t.Fatalf("%+v", got)
		}
		if docValue(t, st, "board") != `["not an object"]` || docValue(t, st, SchemaKey) != "1" || docValue(t, st, "board@v1") != "<none>" {
			t.Fatal("a failed migration changed the document")
		}
	})

	t.Run("a document of a newer version is read-only", func(t *testing.T) {
		set(SchemaKey, `3`)
		if got := r.OpenDocument(st, "doc1", info, k); !got.ReadOnly || !strings.Contains(got.Reason, "3") || got.Code != ReadOnlyNewer {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("no room for the old copy means no migration", func(t *testing.T) {
		big := `{"notes":[{"text":"` + strings.Repeat("x", 60<<10) + `"}]}`
		set("board", big)
		set(SchemaKey, `1`)
		got := r.OpenDocument(st, "doc1", info, k)
		if !got.ReadOnly || got.Code != ReadOnlyNoRoom || got.Migrated || docValue(t, st, "board") != big || docValue(t, st, SchemaKey) != "1" {
			t.Fatalf("half migrated: %+v", got)
		}
	})

	t.Run("a garbled version is read-only", func(t *testing.T) {
		set(SchemaKey, `"two"`)
		if got := r.OpenDocument(st, "doc1", info, k); !got.ReadOnly {
			t.Fatalf("%+v", got)
		}
	})
}

func TestKindDataKeyIsChecked(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "apps", "moodboard", "harnessly", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ to, path string }{
		"none with migrations": {``, "sinopia.kinds[0].dataKey"},
		"an @ key":             {`"dataKey": "@schema",`, "sinopia.kinds[0].dataKey"},
	} {
		text := strings.Replace(string(raw), `"dataKey": "board",`, c.to, 1)
		m, ps := ParseManifest([]byte(text))
		if m == nil {
			t.Fatalf("%s: %v", name, ps)
		}
		found := false
		for _, p := range ps.Errors() {
			found = found || strings.Contains(p.Path, c.path)
		}
		if !found {
			t.Errorf("%s: %v", name, ps)
		}
	}
}
