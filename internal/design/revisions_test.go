package design

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// HRN-880: a library that syncs with the hub's store reads its history, returns a board to a version and pins a guest
// link from the store's revisions, not from git in HistoryDir.

type fakeRevisions struct {
	revs  []Revision // oldest first
	blobs map[string][]byte
}

func (f *fakeRevisions) put(path, body, machine string, at int64) {
	sha := sha256Hex([]byte(body))
	if f.blobs == nil {
		f.blobs = map[string][]byte{}
	}
	f.blobs[sha] = []byte(body)
	f.revs = append(f.revs, Revision{Rev: int64(len(f.revs) + 1), Path: path, Sha: sha, Machine: machine, Author: "Mate", At: at})
}

func (f *fakeRevisions) History(_ context.Context, path string) ([]Revision, error) {
	var out []Revision
	for i := len(f.revs) - 1; i >= 0; i-- {
		if f.revs[i].Path == path {
			out = append(out, f.revs[i])
		}
	}
	return out, nil
}

func (f *fakeRevisions) HistoryUnder(_ context.Context, prefix string, limit int) ([]Revision, error) {
	var out []Revision
	for i := len(f.revs) - 1; i >= 0 && len(out) < limit; i-- {
		if strings.HasPrefix(f.revs[i].Path, prefix) {
			out = append(out, f.revs[i])
		}
	}
	return out, nil
}

func (f *fakeRevisions) Snapshot(_ context.Context, prefix string, at int64) ([]Revision, error) {
	last := map[string]Revision{}
	for _, r := range f.revs {
		if r.Rev <= at && strings.HasPrefix(r.Path, prefix) {
			last[r.Path] = r
		}
	}
	var out []Revision
	for _, r := range last {
		if !r.Deleted {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (f *fakeRevisions) Read(_ context.Context, sha string) ([]byte, error) {
	if b, ok := f.blobs[sha]; ok {
		return b, nil
	}
	return nil, errors.New("no blob")
}

func TestStoreRevisionIDsLookLikeCommitsAndReadBack(t *testing.T) {
	id := StoreRevID(4242)
	if len(id) != 40 || !fullSHA.MatchString(id) {
		t.Fatalf("a store revision id %q does not pass where a commit does", id)
	}
	if n, ok := storeRevOf(id); !ok || n != 4242 {
		t.Fatalf("storeRevOf(%q) = %d %v", id, n, ok)
	}
	if _, ok := storeRevOf("3f2a9c0d1e4b5a6978c0d1e2f3a4b5c6d7e8f901"); ok {
		t.Fatal("a git commit read as a store revision")
	}
	if _, ok := ParseRestoreTarget(id); !ok {
		t.Fatal("--at does not take a store revision")
	}
}

func TestHistoryRestoreAndPinComeFromTheStore(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	l.cfg.DataDir = t.TempDir()
	board := "proj/project/Main.dc.html"
	writeFile(t, filepath.Join(root, filepath.FromSlash(board)), "v3")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r := &fakeRevisions{}
	r.put(board, "v1", "vm-owner", now.Add(-3*time.Hour).UnixMilli())
	r.put("proj/project/Other.dc.html", "o1", "vm-owner", now.Add(-3*time.Hour).UnixMilli())
	r.put(board, "v2", "vm-mate", now.Add(-time.Hour).UnixMilli())
	r.put(board, "v3", "vm-mate", now.Add(-time.Minute).UnixMilli())
	l.UseRevisions(r)

	file := "Main.dc.html"
	h, err := l.readHistory("proj", &file, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.entries) != 3 || h.entries[0].ID != StoreRevID(4) || h.unsaved {
		t.Fatalf("a board's history from the store: %+v unsaved=%v", h.entries, h.unsaved)
	}
	if h.entries[1].Runtime == nil || *h.entries[1].Runtime != "vm-mate" {
		t.Fatalf("the machine that made a version is not said: %+v", h.entries[1])
	}
	all, err := l.readHistory("proj", nil, 20)
	if err != nil || len(all.entries) != 4 {
		t.Fatalf("a project's history from the store: %+v %v", all.entries, err)
	}

	// Back to the version of two hours ago, then to an exact one.
	res, err := l.restoreBoard("proj", file, "2h", restoreActor{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, filepath.FromSlash(board))); got != "v1" || res.revision != StoreRevID(1) {
		t.Fatalf("restored %q to %s, want v1 from revision 1", got, res.revision)
	}
	if _, err := l.restoreBoard("proj", file, StoreRevID(3), restoreActor{}, now); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, filepath.FromSlash(board))); got != "v2" {
		t.Fatalf("restored to an exact revision: %q", got)
	}
	if _, err := l.restoreBoard("proj", file, StoreRevID(2), restoreActor{}, now); err == nil {
		t.Fatal("a revision of another board restored this one")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatal("a git repository appeared beside a library with a store")
	}

	// A guest link pins the newest revision, and the pinned copy is the project as it was then.
	pin, err := l.pinRevision()
	if err != nil || pin != StoreRevID(4) {
		t.Fatalf("pin %q %v", pin, err)
	}
	r.put(board, "v4 after the pin", "vm-mate", now.UnixMilli())
	pinned, ok := l.pinnedLibrary("proj", pin)
	if !ok {
		t.Fatal("the pinned version did not open")
	}
	if got := readFile(t, filepath.Join(pinned.Root(), filepath.FromSlash(board))); got != "v3" {
		t.Fatalf("the pinned copy holds %q, want v3", got)
	}
	if got := readFile(t, filepath.Join(pinned.Root(), "proj", "project", "Other.dc.html")); got != "o1" {
		t.Fatalf("the pinned copy lacks another board: %q", got)
	}
}

func TestInAStoreLibraryTheOwnerIsAPersonNotAUnixUser(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	legacy := Owner{User: "agent", V: 1}
	who, err := l.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Mine(who) {
		t.Fatal("without a store a file naming the Unix user is the runtime's, as before")
	}
	l.UseRevisions(&fakeRevisions{})
	who, err = l.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Mine(who) {
		t.Fatal("in the store's library another machine's runtime running as the same Unix user owns the project")
	}
	mate, err := l.IdentityOf("mate@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !(Owner{User: "someone-else", Person: "mate@example.com", V: 2}).Mine(mate) {
		t.Fatal("the person named in the owner file is not its owner")
	}
}
