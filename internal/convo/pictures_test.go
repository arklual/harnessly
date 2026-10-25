package convo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The pictures the messages name are listed when they may be shown, the latest named last, and served by an id of that
// list only, with the folders they are checked against again.
func TestPicturesTheMessagesNameAreListed(t *testing.T) {
	r := newRig(t)
	scratch, outside := t.TempDir(), t.TempDir()
	r.rt.d.PictureRoots = []string{scratch}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	shot, pasted, secret := filepath.Join(scratch, "home.png"), filepath.Join(scratch, "up", "1-image.png"), filepath.Join(outside, "secret.png")
	for _, p := range []string{shot, pasted, secret} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.open()
	r.settle(wire.StateReady)
	r.user("глянь " + pasted + " и " + secret)
	r.say("Готово.\n\nСкриншот: " + shot + "\n\nА этого нет: " + filepath.Join(scratch, "gone.png"))
	r.tick(picturesEvery)
	r.tick(picturesEvery)
	snap, err := r.rt.Conversation(context.Background(), r.id)
	if err != nil || len(snap.Pictures) != 2 || snap.Pictures[0].Text != pasted || snap.Pictures[1].Text != shot {
		t.Fatalf("pictures: %+v %v", snap.Pictures, err)
	}
	if len(r.events(wire.EvPicturesSet)) == 0 {
		t.Fatal("no pictures_set")
	}
	path, roots, err := r.rt.PicturePath(context.Background(), r.id, snap.Pictures[1].ID)
	if err != nil || path != shot || len(roots) != 2 || roots[0] != "/w" || roots[1] != scratch {
		t.Fatalf("path: %q %q %v", path, roots, err)
	}
	for _, id := range []string{"", "nope", shot, secret} {
		if _, _, err := r.rt.PicturePath(context.Background(), r.id, id); !errors.Is(err, wire.ErrNoShot) {
			t.Fatalf("%q: %v", id, err)
		}
	}
	// A file that goes is not listed on the next look.
	if err := os.Remove(pasted); err != nil {
		t.Fatal(err)
	}
	r.tick(picturesRecheck)
	r.tick(picturesEvery)
	if snap, _ := r.rt.Conversation(context.Background(), r.id); len(snap.Pictures) != 1 || snap.Pictures[0].Text != shot {
		t.Fatalf("after removal: %+v", snap.Pictures)
	}
}
