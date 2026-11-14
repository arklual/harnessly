package design

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func kitCardsDir(t *testing.T) string {
	t.Helper()
	forgetKitCards()
	t.Cleanup(forgetKitCards)
	dir := filepath.Join(t.TempDir(), "kits")
	installKitFixture(t, dir, "acme-ui", "1.0.0")
	return dir
}

// The home's kit cards are kept while the kits folder looks the same, and read again when a version arrives or goes.
func TestKitCardsAreKeptUntilTheKitsFolderChanges(t *testing.T) {
	dir := kitCardsDir(t)
	first := CachedKitCards(dir)
	if len(first) != 1 || first[0].Version != "1.0.0" {
		t.Fatalf("cards: %+v", first)
	}
	// A file inside a version is not looked at on every read: the kept cards answer.
	kept := CachedKitCards(dir)
	if &kept[0] != &first[0] {
		t.Fatal("the cards were read again though nothing moved")
	}
	installKitFixture(t, dir, "acme-ui", "1.1.0")
	again := CachedKitCards(dir)
	if len(again) != 1 || again[0].Version != "1.1.0" {
		t.Fatalf("a new version is not seen: %+v", again)
	}
	if err := os.RemoveAll(filepath.Join(dir, "acme-ui")); err != nil {
		t.Fatal(err)
	}
	if gone := CachedKitCards(dir); len(gone) != 0 {
		t.Fatalf("a removed kit is still listed: %+v", gone)
	}
}

// A runtime that just started answers with the cards the last one wrote beside the folder, while it still looks the
// same; a folder that changed meanwhile is read anew.
func TestKitCardsOfTheLastRunAnswerAfterAStart(t *testing.T) {
	dir := kitCardsDir(t)
	CachedKitCards(dir)
	if _, err := os.Stat(kitCardsFile(dir)); err != nil {
		t.Fatalf("the cards are not written: %v", err)
	}
	forgetKitCards()
	// The disk copy is what answers: a title changed in it shows the copy was read, not the kit.
	sig := kitsSignature(dir)
	writeKitCardsDisk(dir, sig, []KitCard{{ID: "acme-ui", Title: "From disk", Version: "1.0.0"}})
	if got := CachedKitCards(dir); len(got) != 1 || got[0].Title != "From disk" {
		t.Fatalf("the last run's cards did not answer: %+v", got)
	}
	// …and the files are looked over once behind it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := CachedKitCards(dir)
		if len(got) == 1 && got[0].Title != "From disk" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background read did not replace the copy: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	forgetKitCards()
	installKitFixture(t, dir, "acme-ui", "1.1.0")
	if got := CachedKitCards(dir); len(got) != 1 || got[0].Version != "1.1.0" {
		t.Fatalf("a stale copy answered: %+v", got)
	}
}

// Kept cards answer past the recheck time, and are read again behind the answer.
func TestKitCardsAreLookedOverAgainInTheBackground(t *testing.T) {
	dir := kitCardsDir(t)
	now := time.Now()
	kitCardsNow = func() time.Time { return now }
	t.Cleanup(func() { kitCardsNow = time.Now })
	first := CachedKitCards(dir)
	now = now.Add(kitCardsRecheck + time.Second)
	if got := CachedKitCards(dir); &got[0] != &first[0] {
		t.Fatal("the recheck made the reader wait")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		kitCardsKept.Lock()
		e := kitCardsKept.by[dir]
		at := e.at
		kitCardsKept.Unlock()
		if at.Equal(now) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the cards were not read again")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
