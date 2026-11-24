package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// Give the shared store a revision, so publication cannot accidentally fall back to a runtime pin.
func seedStoredDesignForSnapshot(t *testing.T, s *hubtest.Stack, space, machine string) {
	t.Helper()
	ctx := context.Background()
	if s.Hub.Store == nil {
		t.Fatal("expected the Hub store to be on")
	}
	room, err := s.Hub.Store.DesignRoom(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"title":"Shop"}`
	sum := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(sum[:])
	if err := room.PutBlob(ctx, sha, strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if _, conflict, err := room.Commit(ctx, filestore.CommitRequest{Path: "shop/artifact.json", Sha: sha, Size: int64(len(body)), Author: "owner"}, machine); err != nil || conflict {
		t.Fatalf("seed store design: conflict=%v, err=%v", conflict, err)
	}
}

// Run with RUNTIME_DESIGN_CHROME set to exercise the complete owner runtime and Hub with Chrome.
func TestSingleHostnamePrototypeSnapshotWithOwnerOffline(t *testing.T) {
	if os.Getenv("RUNTIME_DESIGN_CHROME") == "" {
		t.Skip("set RUNTIME_DESIGN_CHROME to the installed browser")
	}
	s := hubtest.New(t, hubtest.Env("HUB_DESIGN_SNAPSHOT_LINKS", "on"), hubtest.Env("HUB_STORE", "on"))
	owner := s.SignIn("owner@example.com")
	outsider := s.SignIn("outsider@example.com")
	space, _ := owner.NewSpace("Studio")
	a, runtime := designMachine(t, s, owner, space)
	for page, color := range map[string]string{"Main.dc.html": "#dc1020", "Secret.dc.html": "#1828dc"} {
		if err := os.WriteFile(filepath.Join(runtime.root, "shop", "project", page), []byte("<html><body style='background:"+color+"'>Demo</body></html>"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	created(a, "shop", owner.Email)
	s.Eventually("runtime enforces ownership", func() bool { role, ok := snapshotFor(a, owner.Email, "shop"); return ok && role == "owner" })
	seedStoredDesignForSnapshot(t, s, space, a.MachineID)
	base := "/m/" + a.MachineID + "/api/platform/access/design/shop/links"
	input := map[string]any{"forWhom": "Preview", "rights": "view", "ttl": "1d", "version": "pinned", "pages": []string{"Main.dc.html", "Secret.dc.html"}}
	if other := outsider.Do("POST", base, input); other.Code == 201 {
		t.Fatal("unrelated publisher")
	}
	made := owner.Do("POST", base, input)
	if made.Code != 201 {
		t.Fatalf("publish %s\n%s", made, s.Logs.String())
	}
	var link struct {
		URL  string
		Link accessLink
	}
	made.JSON(&link)
	if !strings.HasPrefix(link.URL, s.URL+"/guest/design/snap-") {
		t.Fatalf("unexpected guest origin")
	}
	a.Close()
	guest := s.Browser()
	path := strings.TrimPrefix(link.URL, s.URL)
	for _, page := range []string{"", "?page=1"} {
		v := guest.Do("GET", path+page, nil)
		if v.Code != 200 || !strings.Contains(string(v.Body), "data:image/jpeg;base64,") || strings.Contains(string(v.Body), "<script") {
			t.Fatalf("offline page %s: HTTP %d", page, v.Code)
		}
	}
	if first, second := guestPageColor(t, link.URL), guestPageColor(t, link.URL+"?page=1"); first == second {
		t.Fatal("both pages rendered the same color")
	}
	if head := guest.Do("HEAD", path, nil); head.Code != 200 || len(head.Body) > 0 {
		t.Fatal("HEAD")
	}
	if other := guest.Do("GET", path+"?page=2", nil); other.Code != 404 {
		t.Fatal("unknown page")
	}
	if r := owner.Do("PATCH", base+"/"+link.Link.ID, map[string]any{"disabled": true}); r.Code != 200 {
		t.Fatalf("revoke: %s", r)
	}
	if v := guest.Do("GET", path, nil); v.Code != 404 {
		t.Fatalf("revoked link: HTTP %d", v.Code)
	}
}

func guestPageColor(t *testing.T, url string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	file := filepath.Join(t.TempDir(), "guest.png")
	args := []string{"--headless=new", "--incognito", "--disable-gpu", "--disable-extensions", "--disable-background-networking", "--no-first-run", "--window-size=800,600", "--user-data-dir=" + t.TempDir(), "--screenshot=" + file}
	args = append(args, strings.Fields(os.Getenv("RUNTIME_DESIGN_CHROME_ARGS"))...)
	cmd := exec.CommandContext(ctx, os.Getenv("RUNTIME_DESIGN_CHROME"), append(args, url)...)
	if err := cmd.Run(); err != nil {
		t.Fatalf("fresh Chrome screenshot failed: %v", err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pic, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	red, _, blue, _ := pic.At(pic.Bounds().Dx()/2, pic.Bounds().Dy()/2).RGBA()
	if red > 40000 && blue < 16000 {
		return true
	}
	if blue > 40000 && red < 16000 {
		return false
	}
	t.Fatalf("no published page visible in signed-out Chrome (red=%d blue=%d)", red, blue)
	return false
}
