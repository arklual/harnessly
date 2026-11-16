package wire

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/sites"
)

func TestLooksPicturesRoundTrip(t *testing.T) {
	blobs, err := sites.NewFolderBlobs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)
	w := httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodPost, "/api/v2/me/looks", bytes.NewReader(png)), blobs, "")
	var out struct{ Hash string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Hash) != 64 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	hash, ok := LookPath("me/looks/" + out.Hash)
	if !ok || hash != out.Hash {
		t.Fatal("path not read")
	}
	w = httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodGet, "/api/v2/me/looks/"+hash, nil), blobs, hash)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("get: %d %v", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodPost, "/api/v2/me/looks", bytes.NewReader([]byte("<html><script>x</script>"))), blobs, "")
	if w.Code == 200 {
		t.Fatal("a page taken for a picture")
	}
	if _, ok := LookPath("me/looks/../../etc"); ok {
		t.Fatal("a path that is no hash")
	}
}
