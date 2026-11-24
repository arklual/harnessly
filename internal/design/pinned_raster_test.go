package design

import (
	"encoding/json"
	"image/jpeg"
	"path/filepath"
	"testing"
)

func TestPublisherCapturesThePinnedPageAndItsAssets(t *testing.T) {
	a, root := scopeAPI(t)
	path := "p1/__raster/" + rasterPageKey("One.dc.html")
	if w := publicGet(a, "GET", path, scopeToken(t, nil)); w.Code != 404 {
		t.Fatal("an ordinary reader may start capture")
	}
	if a.L.findChrome() == "" {
		t.Skip("no Chrome installed")
	}
	writeFile(t, filepath.Join(root, "p1", "project", "One.dc.html"), `<!doctype html><html><head><link rel="stylesheet" href="assets/a.css"></head><body>Page one</body></html>`)
	css := filepath.Join(root, "p1", "project", "assets", "a.css")
	writeFile(t, css, `html,body{background:rgb(222,24,40)}`)
	w := publicGet(a, "GET", "p1/"+PublicRevDir, scopeToken(t, map[string]any{"pin": true}))
	var pin struct{ Rev string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &pin) != nil || pin.Rev == "" {
		t.Fatalf("pin failed %d", w.Code)
	}
	writeFile(t, css, `html,body{background:blue}`)
	limited := scopeToken(t, map[string]any{"rev": pin.Rev, "pages": []string{"Two.dc.html"}, "capture": true})
	if w := publicGet(a, "GET", path, limited); w.Code != 404 {
		t.Fatal("page outside published subset")
	}
	owner := scopeToken(t, map[string]any{"rev": pin.Rev, "pages": []string{"One.dc.html"}, "capture": true})
	if raw := publicGet(a, "GET", "p1/One.dc.html", owner); raw.Code != 404 {
		t.Fatal("capture grant may read raw markup")
	}
	w = publicGet(a, "GET", path, owner)
	if w.Code != 200 {
		t.Fatalf("capture status %d %s", w.Code, w.Body.String())
	}
	img, err := jpeg.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	x, y := img.Bounds().Dx()/2, img.Bounds().Dy()/2
	red, _, blue, _ := img.At(x, y).RGBA()
	if red < 40000 || blue > 20000 {
		t.Fatalf("pinned stylesheet not rendered, red=%d blue=%d", red, blue)
	}
}
