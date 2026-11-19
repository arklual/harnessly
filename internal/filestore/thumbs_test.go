package filestore

import "testing"

// the thumbs collection takes a board's picture under its project's folder and nothing else.
func TestThumbsPolicy(t *testing.T) {
	p := ThumbPath("shop", "Home.dc.html")
	if id, ok := ThumbID(p); !ok || id != "shop" || !(Thumbs{}).Synced(p) || PolicyFor(KindThumbs).Synced(p) != true {
		t.Fatalf("%s: %q %v", p, id, ok)
	}
	if ThumbPath("shop", "Home.dc.html") != p || ThumbPath("shop", "About.dc.html") == p {
		t.Fatal("the key does not follow the board")
	}
	for _, bad := range []string{"shop.jpg", "shop/x.jpg", "Shop/0123456789abcdef.jpg", "shop/0123456789abcdef.png",
		"shop/a/0123456789abcdef.jpg", "../0123456789abcdef.jpg", "shop/0123456789ABCDEF.jpg"} {
		if (Thumbs{}).Synced(bad) {
			t.Errorf("%q is synced", bad)
		}
	}
	if (Thumbs{}).SkippedDir("shop") || !(Thumbs{}).SkippedDir("shop/a") || !(Thumbs{}).SkippedDir("Bad Dir") {
		t.Fatal("dirs")
	}
}
