package filestore

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestKitsPolicySyncsOnlyVersionFolders(t *testing.T) {
	ok := []string{"acme-ds/1.0.0/design-system.json", "acme-ds/1.2.3-rc.1/themes/x/tokens.json", "a/0.0.1/fonts/Inter.woff2"}
	bad := []string{"acme-ds/1.0.0", "acme-ds/design-system.json", "Acme/1.0.0/a.json", "acme-ds/draft/a.json", "acme-ds/1.0/a.json",
		".1.0.0.prep/acme/a.json", "acme-ds/.1.0.1.prep/a.json", "acme-ds/1.0.0/.git/config", "acme-ds/1.0.0/CLAUDE.md",
		"acme-ds/1.0.0/a.json~", "acme-ds/1.0.0/../x", "/acme-ds/1.0.0/a.json", "acme-ds/1.0.0/node_modules/x.js"}
	p := PolicyFor("kits-space1")
	if _, isKits := p.(Kits); !isKits {
		t.Fatalf("the policy of a kits room is %T", p)
	}
	for _, f := range ok {
		if !p.Synced(f) {
			t.Errorf("%s should sync", f)
		}
	}
	for _, f := range bad {
		if p.Synced(f) {
			t.Errorf("%s must not sync", f)
		}
	}
	if !KnownKind(KindKits) || !p.SkippedDir(".1.0.0.prep") || p.SkippedDir("acme-ds/1.0.0/themes") || !p.SkippedDir("acme-ds/1.0.0/.cache") {
		t.Error("skipped folders")
	}
}

// A published kit version never changes: not by a write on the right base, not by a delete; the same bytes again are no
// news, and a file of another version is new.
func TestKitVersionsAreImmutable(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store("kits-space1")
		e, conflict := write(t, s, "acme-ds/1.0.0/tokens.json", 0, "one")
		if conflict || e.Rev == 0 {
			t.Fatalf("first write: %+v %v", e, conflict)
		}
		if _, c := write(t, s, "acme-ds/1.0.0/tokens.json", e.Rev, "two"); !c {
			t.Error("a changed file of a published version was accepted")
		}
		if _, c := write(t, s, "acme-ds/1.0.0/tokens.json", 0, "two"); !c {
			t.Error("a stale write over a published file was accepted")
		}
		if again, c := write(t, s, "acme-ds/1.0.0/tokens.json", e.Rev, "one"); c || again.Rev != e.Rev {
			t.Errorf("the same bytes again: %+v conflict %v", again, c)
		}
		del, c, err := s.Commit(bg, CommitRequest{Path: "acme-ds/1.0.0/tokens.json", Base: e.Rev, Deleted: true, Author: "t"}, "m1")
		if err != nil || !c || del.Deleted {
			t.Errorf("a published file was deleted: %+v %v %v", del, c, err)
		}
		if cur, _ := s.meta.Current(bg, "acme-ds/1.0.0/tokens.json"); cur == nil || cur.Deleted || cur.Rev != e.Rev {
			t.Errorf("the file changed: %+v", cur)
		}
		if _, c := write(t, s, "acme-ds/1.0.1/tokens.json", 0, "two"); c {
			t.Error("a new version was refused")
		}
	})
}

// The design library stays as it was: files change and go.
func TestDesignFilesStillChange(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store(DefaultRoom)
		e, _ := write(t, s, "deck/a.html", 0, "one")
		if e2, c := write(t, s, "deck/a.html", e.Rev, "two"); c || e2.Rev == e.Rev {
			t.Errorf("an edit was refused: %+v %v", e2, c)
		}
	})
}

func sealOf(t *testing.T, files map[string]string) (string, []byte) {
	t.Helper()
	var list []KitSealedFile
	for p, body := range files {
		list = append(list, KitSealedFile{Path: p, Size: int64(len(body)), SHA256: sum([]byte(body))})
	}
	raw, err := json.Marshal(NewKitSeal(list))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), raw
}

// The seal may come before its files: it is taken when well formed. After it the folder takes only the files it lists
// with the bytes it lists; the files it lists stay as they are.
func TestKitSealClosesTheVersionToUnlistedFiles(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store("kits-space1")
		files := map[string]string{"design-system.json": "ds", "tokens.json": "tk"}
		body, _ := sealOf(t, files)
		write(t, s, "acme/1.0.0/design-system.json", 0, "ds")
		e, conflict := write(t, s, "acme/1.0.0/"+KitSealFile, 0, body) // early: tokens.json is still on its way
		if conflict || e.Rev == 0 {
			t.Fatalf("the seal: %+v %v", e, conflict)
		}
		// a listed file still comes; a file with other bytes, and one the seal does not list, do not
		sha := put(t, s, "other")
		_, _, err := s.Commit(bg, CommitRequest{Path: "acme/1.0.0/tokens.json", Sha: sha, Tag: sha, Size: 5, Author: "t"}, "m1")
		if !errors.Is(err, ErrSealed) {
			t.Errorf("a listed file with other bytes: %v", err)
		}
		_, _, err = s.Commit(bg, CommitRequest{Path: "acme/1.0.0/extra.js", Sha: sha, Tag: sha, Size: 5, Author: "t"}, "m1")
		if !errors.Is(err, ErrSealed) {
			t.Errorf("a new file in a sealed version: %v", err)
		}
		if _, c := write(t, s, "acme/1.0.0/tokens.json", 0, "tk"); c {
			t.Error("a listed file was refused")
		}
		if _, c := write(t, s, "acme/1.0.1/extra.js", 0, "evil"); c {
			t.Error("another version was closed")
		}
		if _, c := write(t, s, "acme/1.0.0/tokens.json", 1, "changed"); !c {
			t.Error("a sealed file was changed")
		}
		// a seal over files already in the room closes it only if it lists them as they are
		write(t, s, "other/1.0.0/a.json", 0, "a")
		write(t, s, "other/1.0.0/b.json", 0, "b")
		partial, _ := sealOf(t, map[string]string{"a.json": "a"}) // b.json is in the room and not listed
		if _, _, err := s.Commit(bg, CommitRequest{Path: "other/1.0.0/" + KitSealFile, Sha: put(t, s, partial), Tag: sum([]byte(partial)), Size: int64(len(partial)), Author: "t"}, "m1"); !errors.Is(err, ErrSealed) {
			t.Errorf("a seal that leaves a file of the room out: %v", err)
		}
		// a seal that is not one
		if _, _, err := s.Commit(bg, CommitRequest{Path: "acme/2.0.0/" + KitSealFile, Sha: sha, Tag: sha, Size: 5, Author: "t"}, "m1"); !errors.Is(err, ErrSealed) {
			t.Errorf("a seal that is not a seal: %v", err)
		}
	})
}

// Two machines lay out the same version with different bytes: the first seal wins, the second is a conflict.
func TestKitSealFirstWinsOnARace(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		s := r.store("kits-space1")
		write(t, s, "acme/2.0.0/a.json", 0, "from-a")
		bodyA, _ := sealOf(t, map[string]string{"a.json": "from-a"})
		if _, c := write(t, s, "acme/2.0.0/"+KitSealFile, 0, bodyA); c {
			t.Fatal("the first seal lost")
		}
		// machine B has other bytes in the same file and its own seal
		if _, c := write(t, s, "acme/2.0.0/a.json", 0, "from-b"); !c {
			t.Error("the second machine's file replaced a sealed one")
		}
		bodyB, _ := sealOf(t, map[string]string{"a.json": "from-b"})
		if _, c := write(t, s, "acme/2.0.0/"+KitSealFile, 0, bodyB); !c {
			t.Error("the second seal was taken")
		}
	})
}
