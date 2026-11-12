package memsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fakeHub keeps notes the way the hub does (internal/hub/store PutMemoryNote): a write from a stale base is a conflict.
type fakeHub struct {
	notes map[string]hubproto.MemoryNote
	down  bool
}

func newHub() *fakeHub { return &fakeHub{notes: map[string]hubproto.MemoryNote{}} }

type machine struct {
	hub  *fakeHub
	name string
}

func (m machine) Pull(_ context.Context, projects []string) ([]hubproto.MemoryNote, error) {
	if m.hub.down {
		return nil, errors.New("offline")
	}
	want := map[string]bool{}
	for _, p := range projects {
		want[p] = true
	}
	out := []hubproto.MemoryNote{}
	for _, n := range m.hub.notes {
		if want[n.Project] {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m machine) Put(_ context.Context, p hubproto.MemoryPut) (hubproto.MemoryPutResult, error) {
	if m.hub.down {
		return hubproto.MemoryPutResult{}, errors.New("offline")
	}
	n := hubproto.MemoryNote{Project: p.Project, Name: p.Name, Body: p.Body, Version: hubproto.MemoryVersion(p.Body), Machine: m.name}
	if p.Delete {
		n.Body, n.Version, n.Deleted = "", hubproto.MemoryTombstone, true
	}
	key := p.Project + "/" + p.Name
	cur, ok := m.hub.notes[key]
	if ok && !(cur.Version == n.Version && cur.Deleted == n.Deleted) && cur.Version != p.Base {
		c := cur
		return hubproto.MemoryPutResult{Conflict: &c}, nil
	}
	m.hub.notes[key] = n
	return hubproto.MemoryPutResult{Note: &n}, nil
}

const project = "git:example.com/acme/app"

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(raw)
}

func TestNoteReachesAnotherMachineAndConflictsKeepBoth(t *testing.T) {
	hub := newHub()
	a := &Syncer{Client: machine{hub, "a"}}
	b := &Syncer{Client: machine{hub, "b"}}
	da, db := filepath.Join(t.TempDir(), "memory"), filepath.Join(t.TempDir(), "memory")
	ctx := context.Background()

	write(t, da, "deploy.md", "synthetic note\n")
	write(t, da, "MEMORY.md", "- [Deploy](deploy.md)\n")
	if rep := a.Sync(ctx, []Project{{project, da}}); len(rep.Pushed) != 2 || len(rep.Errors) != 0 {
		t.Fatalf("a: %+v", rep)
	}
	// A fresh machine of the same person, a folder that does not exist yet.
	if rep := b.Sync(ctx, []Project{{project, db}}); len(rep.Pulled) != 2 {
		t.Fatalf("b: %+v", rep)
	}
	if got := read(t, db, "deploy.md"); got != "synthetic note\n" {
		t.Fatalf("b reads %q", got)
	}

	// B edits, A syncs: A gets it. A second sync changes nothing.
	write(t, db, "deploy.md", "edited on b\n")
	b.Sync(ctx, []Project{{project, db}})
	a.Sync(ctx, []Project{{project, da}})
	if got := read(t, da, "deploy.md"); got != "edited on b\n" {
		t.Fatalf("a reads %q", got)
	}
	if rep := a.Sync(ctx, []Project{{project, da}}); len(rep.Pulled)+len(rep.Pushed)+len(rep.Conflicts) != 0 {
		t.Fatalf("an idle sync did something: %+v", rep)
	}

	// Both edit the same note: both versions survive.
	write(t, da, "deploy.md", "from a\n")
	write(t, db, "deploy.md", "from b\n")
	a.Sync(ctx, []Project{{project, da}})
	rep := b.Sync(ctx, []Project{{project, db}})
	if len(rep.Conflicts) != 1 || rep.Conflicts[0].Machine != "a" {
		t.Fatalf("conflict: %+v", rep)
	}
	if got := read(t, db, "deploy.md"); got != "from b\n" {
		t.Fatalf("the local note was overwritten: %q", got)
	}
	if got := read(t, db, rep.Conflicts[0].Copy); got != "from a\n" {
		t.Fatalf("the other version: %q", got)
	}
	// Resolved by keeping b's: the next sync pushes it over a's (the copy stays local), and a gets it.
	if rep := b.Sync(ctx, []Project{{project, db}}); len(rep.Pushed) != 1 || len(hub.notes) != 2 {
		t.Fatalf("after the merge: %+v %v", rep, hub.notes)
	}
	a.Sync(ctx, []Project{{project, da}})
	if got := read(t, da, "deploy.md"); got != "from b\n" {
		t.Fatalf("after the merge a reads %q", got)
	}
}

func TestDeleteTravelsAndOfflineKeepsNotes(t *testing.T) {
	hub := newHub()
	a := &Syncer{Client: machine{hub, "a"}}
	b := &Syncer{Client: machine{hub, "b"}}
	da, db := t.TempDir(), t.TempDir()
	ctx := context.Background()
	write(t, da, "old.md", "stale\n")
	a.Sync(ctx, []Project{{project, da}})
	b.Sync(ctx, []Project{{project, db}})

	os.Remove(filepath.Join(da, "old.md"))
	if rep := a.Sync(ctx, []Project{{project, da}}); len(rep.Deleted) != 1 {
		t.Fatalf("delete: %+v", rep)
	}
	if rep := b.Sync(ctx, []Project{{project, db}}); len(rep.Deleted) != 1 || read(t, db, "old.md") != "<missing>" {
		t.Fatalf("b kept a deleted note: %+v", rep)
	}
	if trash, _ := filepath.Glob(filepath.Join(db, TrashDir, "*-old.md")); len(trash) != 1 {
		t.Fatalf("no copy in the trash: %v", trash)
	}
	// The deleted note does not come back from a machine that never changed it.
	a.Sync(ctx, []Project{{project, da}})
	if read(t, da, "old.md") != "<missing>" {
		t.Fatal("the note came back")
	}

	hub.down = true
	write(t, db, "new.md", "written offline\n")
	rep := b.Sync(ctx, []Project{{project, db}})
	if len(rep.Errors) == 0 || read(t, db, "new.md") != "written offline\n" {
		t.Fatalf("offline: %+v", rep)
	}
	hub.down = false
	if rep := b.Sync(ctx, []Project{{project, db}}); len(rep.Pushed) != 1 {
		t.Fatalf("retry: %+v", rep)
	}
	projects := t.TempDir()
	dir := MemoryDir(projects, "/src/app")
	write(t, dir, "n.md", "x")
	b.Sync(ctx, []Project{{project, dir}})
	if got := Known(projects); len(got) != 1 || got[0].ID != project || got[0].Dir != dir {
		t.Fatalf("Known: %+v", got)
	}
}

func TestOnlyNotesSync(t *testing.T) {
	hub := newHub()
	a := &Syncer{Client: machine{hub, "a"}}
	da := t.TempDir()
	write(t, da, "note.md", "x")
	write(t, da, "secret.env", "x")
	write(t, da, ".hidden.md", "x")
	a.Sync(context.Background(), []Project{{project, da}, {"", t.TempDir()}})
	if len(hub.notes) != 1 {
		t.Fatalf("synced more than notes: %v", hub.notes)
	}
}

func TestProjectNames(t *testing.T) {
	for in, want := range map[string]string{
		"git@example.com:acme/app.git":              "git:example.com/acme/app",
		"https://user:pw@Example.com:443/acme/app/": "git:example.com/acme/app",
		"ssh://git@example.com:22/acme/app.git":     "git:example.com/acme/app",
		"":                                          "",
		"/local/path":                               "",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
	if got := EntryName("/home/me/src/my_app"); got != "-home-me-src-my-app" {
		t.Errorf("EntryName = %q", got)
	}
}

func TestFolderWithoutRemote(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if got := FolderID(ctx, dir, false); got != "" {
		t.Errorf("a folder without a remote synced: %q", got)
	}
	if got := FolderID(ctx, dir, true); got != "dir:"+filepath.ToSlash(dir) {
		t.Errorf("by path = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("team-notes\nignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{false, true} {
		if got := FolderID(ctx, dir, local); got != "dir:team-notes" {
			t.Errorf("by key (local=%v) = %q", local, got)
		}
	}
	// A key that is no key is no key: the folder falls back to what it had.
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("has space\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := FolderKey(dir); got != "" {
		t.Errorf("bad key accepted: %q", got)
	}
	if got := PathID("relative/dir"); got != "" {
		t.Errorf("relative path = %q", got)
	}
	if got := PathID("/with space/dir"); got != "" {
		t.Errorf("path with a space = %q", got)
	}
}

// countingClient refuses every put after the first few and counts the puts it was sent.
type countingClient struct {
	machine
	allow, puts int
}

func (c *countingClient) Put(ctx context.Context, p hubproto.MemoryPut) (hubproto.MemoryPutResult, error) {
	c.puts++
	if c.puts > c.allow {
		return hubproto.MemoryPutResult{}, errors.New("429 rate_limited")
	}
	return c.machine.Put(ctx, p)
}

// A refused put stops the others of the sync: one request per note would only hammer a hub that asked for a pause.
func TestRefusedPutHoldsTheRest(t *testing.T) {
	hub := newHub()
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		write(t, dir, string(rune('a'+i))+".md", "note")
	}
	c := &countingClient{machine: machine{hub, "a"}, allow: 3}
	rep := (&Syncer{Client: c}).Sync(context.Background(), []Project{{ID: project, Dir: dir}})
	if c.puts != 4 || len(rep.Pushed) != 3 || len(rep.Errors) != 1 {
		t.Fatalf("puts %d, pushed %d, errors %v", c.puts, len(rep.Pushed), rep.Errors)
	}
	c.allow = 100
	rep = (&Syncer{Client: c}).Sync(context.Background(), []Project{{ID: project, Dir: dir}})
	if len(rep.Pushed) != 17 || len(rep.Errors) != 0 {
		t.Fatalf("next sync pushed %d, errors %v", len(rep.Pushed), rep.Errors)
	}
}
