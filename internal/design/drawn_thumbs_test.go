package design

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// the runtime puts into the store the newest picture drawn of each board, and draws nothing for it.
func TestDrawnThumbsAreTheNewestOfEachBoard(t *testing.T) {
	_, l, root := loadCheckGolden(t)
	if got := l.DrawnThumbs("alpha"); len(got) != 0 {
		t.Fatalf("nothing drawn yet: %+v", got)
	}
	dir := filepath.Join(root, "alpha", ThumbDirname)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h := thumbHash("Main.dc.html")
	for i, n := range []string{h + "-new.jpg", h + "-old.jpg", h + "-tmp.png", "0000-x.jpg"} {
		f := filepath.Join(dir, n)
		if err := os.WriteFile(f, []byte("\xff\xd8"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
	got := l.DrawnThumbs("alpha")
	if len(got) != 1 || got[0].Board != "Main.dc.html" || got[0].Name != h+"-new.jpg" || got[0].File != filepath.Join(dir, h+"-new.jpg") {
		t.Fatalf("drawn: %+v", got)
	}
	if l.DrawnThumbs("../alpha") != nil {
		t.Fatal("a bad id")
	}
	if n := len(l.thumbs().inflight); n != 0 {
		t.Fatalf("%d drawings started", n)
	}
}
