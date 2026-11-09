package designkit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBump(t *testing.T) {
	cases := map[[2]string]string{
		{"0.5.0", ""}:           "0.6.0",
		{"0.5.3", "patch"}:      "0.5.4",
		{"0.5.3", "minor"}:      "0.6.0",
		{"0.5.3", "major"}:      "1.0.0",
		{"1.2.3-rc.1", "patch"}: "1.2.4",
	}
	for in, want := range cases {
		got, err := Bump(in[0], in[1])
		if err != nil || got != want {
			t.Fatalf("Bump(%q, %q) = %q, %v; want %q", in[0], in[1], got, err, want)
		}
	}
	if _, err := Bump("0.5.0", "huge"); err == nil {
		t.Fatal("an unknown part must be refused")
	}
}

func TestMountOfThemesAndWholeKit(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"themes/light/fonts.css": "a", "themes/dark/fonts.css": "b", "kit.json": "{}"})
	if got := MountOf(root, "acme", "light"); got != filepath.Join(root, "themes", "light") {
		t.Fatalf("theme mount: %q", got)
	}
	if MountOf(root, "acme", "../x") != "" || MountOf(root, "acme", "other") != "" {
		t.Fatal("a name that is not a theme must not mount")
	}
	if got := Mounts(root, "acme"); len(got) != 2 {
		t.Fatalf("mounts: %v", got)
	}
	flat := t.TempDir()
	writeTree(t, flat, map[string]string{"tokens.css": "a"})
	if MountOf(flat, "acme", "acme") != flat || Mounts(flat, "acme")[0] != "acme" {
		t.Fatal("a kit without themes mounts whole under its id")
	}
}

func TestPublishDraftMakesAnImmutableVersionWithItsChanges(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "acme", "0.5.0")
	writeTree(t, base, map[string]string{"kit.json": "{\n \"id\": \"acme\",\n \"version\": \"0.5.0\"\n}\n", "themes/light/a.css": "a", "themes/light/old.css": "o"})
	agedTree(t, base)
	draft := t.TempDir()
	if _, _, err := CopyTree(base, draft, nil); err != nil {
		t.Fatal(err)
	}
	writeTree(t, draft, map[string]string{"themes/light/a.css": "A", "themes/light/new.css": "n", "support.js": "engine"})
	if err := os.Remove(filepath.Join(draft, "themes/light/old.css")); err != nil {
		t.Fatal(err)
	}
	skip := []string{"support.js"}
	changes, err := Changes(base, draft, skip)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{{"themes/light/a.css", "changed"}, {"themes/light/new.css", "added"}, {"themes/light/old.css", "removed"}}
	if len(changes) != len(want) {
		t.Fatalf("changes: %v", changes)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("changes: %v", changes)
		}
	}
	next, _ := Bump("0.5.0", "")
	p, err := Publish(dir, "acme", next, draft, skip, PublishOptions{Notes: "Кнопка ярче", Changes: changes, From: "0.5.0"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version.Version != "0.6.0" || p.ChangedCount != 3 || p.Notes != "Кнопка ярче" {
		t.Fatalf("published: %+v", p)
	}
	root := Root(dir, "acme", "0.6.0")
	if root == "" {
		t.Fatal("the new version is not installed")
	}
	if _, err := os.Stat(filepath.Join(root, "support.js")); err == nil {
		t.Fatal("a skipped file must not be published")
	}
	var stamped struct{ ID, Version string }
	if b, _ := os.ReadFile(filepath.Join(root, "kit.json")); json.Unmarshal(b, &stamped) != nil || stamped.Version != "0.6.0" || stamped.ID != "acme" {
		t.Fatalf("kit.json is not stamped: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(base, "themes/light/a.css")); string(b) != "a" {
		t.Fatal("the base version changed")
	}
	if got, ok := ReadPublished(dir, "acme", "0.6.0"); !ok || got.Notes != "Кнопка ярче" || got.From != "0.5.0" || len(got.Changed) != 3 {
		t.Fatalf("record: %+v", got)
	}
	if v, ok := Latest(dir, "acme"); !ok || v.Version != "0.6.0" {
		t.Fatalf("latest: %+v", v)
	}
	if _, err := Publish(dir, "acme", "0.6.0", draft, skip, PublishOptions{}); !errors.Is(err, ErrExists) {
		t.Fatalf("a published version must never be replaced: %v", err)
	}
}

func TestDraftRecord(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadDraft(dir, "acme"); ok {
		t.Fatal("no draft yet")
	}
	if err := WriteDraft(dir, "acme", Draft{Project: "acme-draft", Base: "0.5.0", Skip: []string{"support.js"}}); err != nil {
		t.Fatal(err)
	}
	d, ok := ReadDraft(dir, "acme")
	if !ok || d.Project != "acme-draft" || d.Base != "0.5.0" {
		t.Fatalf("draft: %+v", d)
	}
	if err := ClearDraft(dir, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadDraft(dir, "acme"); ok {
		t.Fatal("the draft is cleared")
	}
	if len(List(dir)) != 0 {
		t.Fatal("a draft record is not a version")
	}
}

func TestANewDraftTakesTheAtlasesOfItsVersionAndKeepsTheFilesTimes(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "acme", "1.0.0")
	writeTree(t, base, map[string]string{"kit.json": `{"id":"acme","version":"1.0.0"}`, "themes/light/a.css": "a"})
	writeTree(t, filepath.Join(dir, "acme", ".1.0.0.prep", "light"), map[string]string{"prep.json": `{"schema":1}`})
	writeTree(t, filepath.Join(dir, "acme", ".draft.prep", "light"), map[string]string{"old.png": "x"})
	if err := SeedDraftPrep(dir, "acme", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acme", ".draft.prep", "light", "prep.json")); err != nil {
		t.Fatal("the draft has not the version's atlases")
	}
	if _, err := os.Stat(filepath.Join(dir, "acme", ".draft.prep", "light", "old.png")); err == nil {
		t.Fatal("an older draft's atlas stays")
	}
	draft := t.TempDir()
	if _, _, err := CopyTree(base, draft, nil); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(filepath.Join(base, "themes/light/a.css"))
	b, _ := os.Stat(filepath.Join(draft, "themes/light/a.css"))
	if !a.ModTime().Equal(b.ModTime()) {
		t.Fatal("the copy does not keep the file's time")
	}
	if changes, err := Changes(base, draft, nil); err != nil || len(changes) != 0 {
		t.Fatalf("an untouched copy has changes: %v %v", changes, err)
	}
}

// agedTree dates a version's files an hour back, as an installed version is older than any edit of its draft (a file
// written in the same tick as its source would keep its time).
func agedTree(t *testing.T, root string) {
	t.Helper()
	then := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		return os.Chtimes(p, then, then)
	})
	if err != nil {
		t.Fatal(err)
	}
}
