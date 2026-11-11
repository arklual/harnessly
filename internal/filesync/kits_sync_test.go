package filesync

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

func kitAgent(t *testing.T, url, root, state, token, machine string) *Agent {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{Root: root, StateDir: state, Store: NewClient(url, filestore.KindKits, staticToken(token), nil), Machine: machine,
		Policy: filestore.PolicyFor(filestore.KindKits), ConflictDir: ".conflicts"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

// Two machines lay out the same kit version with different bytes: the first seal wins on the room and the other machine's
// copy ends up in .conflicts with the winner's bytes in place; a file the seal does not list is never sent.
func TestKitVersionRaceFirstSealWinsOtherGoesToConflicts(t *testing.T) {
	dir := t.TempDir()
	srvStore, err := filestore.OpenFolder(filepath.Join(dir, "hub"), authorize)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(srvStore.Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(srvStore.Close)
	ra, rb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	a := kitAgent(t, srv.URL, ra, filepath.Join(dir, "sa"), tokenA, "vm-a")
	b := kitAgent(t, srv.URL, rb, filepath.Join(dir, "sb"), tokenB, "vm-b")
	ctx := context.Background()

	seal := func(root string, files map[string]string) {
		var list []filestore.KitSealedFile
		for p, body := range files {
			write(t, root, "acme/1.0.0/"+p, body)
			list = append(list, filestore.KitSealedFile{Path: p, Size: int64(len(body)), SHA256: sha([]byte(body))})
		}
		raw, _ := json.Marshal(filestore.NewKitSeal(list))
		write(t, root, "acme/1.0.0/"+filestore.KitSealFile, string(raw))
	}
	seal(ra, map[string]string{"tokens.json": "from-a"})
	seal(rb, map[string]string{"tokens.json": "from-b"})
	write(t, rb, "acme/1.0.0/extra.js", "not in b's seal")

	// The seal is early on a first pass (its files are not all there yet): the room says no and the agent tries again.
	for i := 0; i < 3; i++ {
		must(t, a.SyncOnce(ctx))
	}
	for i := 0; i < 3; i++ {
		must(t, b.SyncOnce(ctx))
	}
	if got := read(t, rb, "acme/1.0.0/tokens.json"); got != "from-a" {
		t.Fatalf("the loser keeps %q, want the winner's bytes", got)
	}
	found := false
	_ = filepath.WalkDir(filepath.Join(rb, ".conflicts"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(p, "tokens.json") {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("the loser's bytes were not kept in .conflicts")
	}
	if cur, _ := srvStoreRoom(t, srvStore).Current(ctx, "acme/1.0.0/extra.js"); cur != nil {
		t.Error("a file the seal does not list reached the room")
	}
}

func srvStoreRoom(t *testing.T, s *filestore.Server) *filestore.Store {
	t.Helper()
	st, err := s.Room(context.Background(), filestore.KindKits)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
