package wire

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/looks"
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

func TestLooksFxScripts(t *testing.T) {
	if LookFxMagic != looks.FxMagic {
		t.Fatalf("magic differs: %q vs %q", LookFxMagic, looks.FxMagic)
	}
	blobs, err := sites.NewFolderBlobs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put := func(body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		ServeLooks(w, httptest.NewRequest(http.MethodPut, "/api/v2/me/looks", bytes.NewReader(body)), blobs, "")
		return w
	}
	script := []byte(LookFxMagic + "hx.on('frame', () => {});\n")
	w := put(script)
	var out struct{ Hash string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Hash) != 64 {
		t.Fatalf("script put: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodGet, "/api/v2/me/looks/"+out.Hash, nil), blobs, out.Hash)
	h := w.Header()
	if w.Code != 200 || h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || h.Get("Cache-Control") != "private, max-age=31536000, immutable" ||
		!bytes.Equal(w.Body.Bytes(), script) {
		t.Fatalf("script get: %d %v", w.Code, h)
	}
	// the biggest script goes in, one byte more does not; a picture's limit stays its own
	big := append([]byte(LookFxMagic), bytes.Repeat([]byte("a"), MaxLookFx-len(LookFxMagic))...)
	if w := put(big); w.Code != 200 {
		t.Fatalf("a script of the largest size: %d %s", w.Code, w.Body)
	}
	if w := put(append(big, 'a')); w.Code == 200 {
		t.Fatal("a script over the limit taken")
	}
	if w := put(append([]byte(LookFxMagic), 0xff, 0xfe, 'x')); w.Code == 200 {
		t.Fatal("a script that is not UTF-8 taken")
	}
	if w := put([]byte("// hx-fx v1\nalert(1)")); w.Code == 200 {
		t.Fatal("a script without the exact magic taken")
	}
	if w := put([]byte("<!doctype html><script>x</script>")); w.Code == 200 {
		t.Fatal("html taken")
	}
	if w := put(append([]byte("<html>"), []byte(LookFxMagic)...)); w.Code == 200 {
		t.Fatal("html with the magic after it taken")
	}
	// a picture bigger than a script may be is still taken up to its own limit
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, MaxLookFx+10)...)
	if w := put(png); w.Code != 200 {
		t.Fatalf("a picture over the script limit refused: %d %s", w.Code, w.Body)
	}
}

func TestTheFxFrameIsASandboxWithoutNetwork(t *testing.T) {
	hash, ok := LookPath("me/looks/fx-frame")
	if !ok || hash != LookFxFrame {
		t.Fatalf("LookPath(fx-frame) = %q, %v", hash, ok)
	}
	w := httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodGet, "/api/v2/me/looks/fx-frame", nil), nil, hash)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", w.Code, w.Header().Get("Content-Type"))
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "sandbox allow-scripts"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("csp %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "connect-src") || strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("csp %q opens the network or the origin", csp)
	}
	if !strings.Contains(w.Body.String(), "e.source!==parent") {
		t.Fatal("the frame must take code from its parent only")
	}
	w = httptest.NewRecorder()
	ServeLooks(w, httptest.NewRequest(http.MethodPut, "/api/v2/me/looks/fx-frame", nil), nil, hash)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT fx-frame: %d", w.Code)
	}
}
