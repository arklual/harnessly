package designview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

func pictureRoom(t *testing.T, kind string, files map[string]string) PictureRooms {
	t.Helper()
	ctx := context.Background()
	srv, err := filestore.OpenFolder(t.TempDir(), func(*http.Request, string) (filestore.Access, error) { return filestore.Access{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	room, err := srv.Room(ctx, "s1-"+kind)
	if err != nil {
		t.Fatal(err)
	}
	for p, body := range files {
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
		if _, _, err := room.Commit(ctx, filestore.CommitRequest{Path: p, Sha: sha, Size: int64(len(body))}, "m1"); err != nil {
			t.Fatal(p, err)
		}
	}
	return func(context.Context, string) (PictureRoom, error) { return room, nil }
}

// the hub reads the covers and the board pictures of a real store by their paths, and lists the covers.
func TestPicturesFromTheStore(t *testing.T) {
	ctx := context.Background()
	v := New(Options{
		Rooms:  func(context.Context, string) (Room, error) { return newMemRoom(), nil },
		Covers: pictureRoom(t, filestore.KindCovers, map[string]string{"shop.jpg": "cover-shop", "land.jpg": "cover-land"}),
		Thumbs: pictureRoom(t, filestore.KindThumbs, map[string]string{filestore.ThumbPath("shop", "Home.dc.html"): "thumb-home"}),
		Dir:    t.TempDir(),
	})
	got, err := v.CoverVersions(ctx, "s1")
	if err != nil || len(got) != 2 || got["shop"] == "" || got["land"] == "" {
		t.Fatalf("cover versions: %v %v", got, err)
	}
	if b, err := v.Cover(ctx, "s1", "shop", 1<<20); err != nil || string(b) != "cover-shop" {
		t.Fatalf("cover: %q %v", b, err)
	}
	if b, _ := v.Cover(ctx, "s1", "shop", 3); b != nil {
		t.Fatalf("a cover over the limit: %q", b)
	}
	b, sha, err := v.Thumb(ctx, "s1", "shop", "Home.dc.html", 1<<20)
	if err != nil || string(b) != "thumb-home" || !filestore.ValidSha(sha) {
		t.Fatalf("thumb: %q %q %v", b, sha, err)
	}
	if b, _, err := v.Thumb(ctx, "s1", "shop", "About.dc.html", 1<<20); b != nil || err != nil {
		t.Fatalf("a board with no picture: %q %v", b, err)
	}
	if b, _, _ := v.Thumb(ctx, "../x", "shop", "Home.dc.html", 1<<20); b != nil {
		t.Fatal("a bad space was read")
	}
}
