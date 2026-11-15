package designaccess

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A stylesheet of the store carries the fonts beside it as data: addresses: the board's sandbox would ask for them in
// CORS mode without a cookie, which the proxy in front of the hub turns away.
func TestStoreSheetCarriesItsFonts(t *testing.T) {
	files := map[string]string{
		"_ds/kit/fonts/fonts.css": `@font-face{font-family:Inter;src:url('./inter.woff2') format('woff2')}
@font-face{font-family:Inter;src:url("./inter.woff2?v=2#x")}
@font-face{font-family:Mono;src:url(mono.ttf)}
@font-face{font-family:Gone;src:url(./gone.woff2)}
@font-face{font-family:Big;src:url(./big.woff2)}
@font-face{font-family:Out;src:url(../../../../secret.woff2)}
@font-face{font-family:Lib;src:url(./__x/a.woff2)}
.a{background:url(./bg.png)}
@import url(./more.css);
.b{src:url(data:font/woff2;base64,AA==)}
.c{src:url('./inter.woff2")}`,
		"_ds/kit/fonts/inter.woff2": "INTER",
		"_ds/kit/fonts/mono.ttf":    "MONO",
		"_ds/kit/fonts/big.woff2":   strings.Repeat("B", inlineFontMax+1),
	}
	asked := map[string]int{}
	read := func(w http.ResponseWriter, r *http.Request, segs []string) error {
		p := strings.Join(segs, "/")
		asked[p]++
		if r.Header.Get("Accept-Encoding") != "" || r.Header.Get("If-None-Match") != "" {
			t.Errorf("%s read with %v", p, r.Header)
		}
		body, ok := files[p]
		if !ok {
			http.NotFound(w, r)
			return nil
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("ETag", `"x"`)
		w.Header().Set("Cache-Control", "private, no-cache")
		_, _ = w.Write([]byte(body))
		return nil
	}
	req := httptest.NewRequest("GET", "/f/_ds/kit/fonts/fonts.css", nil)
	req.Header.Set("If-None-Match", `"x"`)
	rec := httptest.NewRecorder()
	if err := serveSheet(rec, req, strings.Split("_ds/kit/fonts/fonts.css", "/"), read); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	inter := `url("data:font/woff2;base64,` + base64.StdEncoding.EncodeToString([]byte("INTER")) + `")`
	mono := `url("data:font/ttf;base64,` + base64.StdEncoding.EncodeToString([]byte("MONO")) + `")`
	if rec.Code != 200 || strings.Count(body, inter) != 2 || !strings.Contains(body, mono) {
		t.Fatalf("the fonts are not inside: %d %s", rec.Code, body)
	}
	if asked["_ds/kit/fonts/inter.woff2"] != 1 {
		t.Errorf("a font named twice is read once: %v", asked)
	}
	for _, kept := range []string{"url(./gone.woff2)", "url(./big.woff2)", "url(../../../../secret.woff2)", "url(./__x/a.woff2)",
		"url(./bg.png)", "@import url(./more.css)", "url(data:font/woff2;base64,AA==)", `url('./inter.woff2")`} {
		if !strings.Contains(body, kept) {
			t.Errorf("%s should stay as it was: %s", kept, body)
		}
	}
	if etag := rec.Header().Get("ETag"); etag == `"x"` || !strings.HasPrefix(etag, `"s-`) || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" || rec.Header().Get("Content-Length") != itoa(len(body)) {
		t.Errorf("headers: %v", rec.Header())
	}

	// What the store does not have is answered as the store answers it.
	missing := httptest.NewRecorder()
	if err := serveSheet(missing, httptest.NewRequest("GET", "/f/none.css", nil), []string{"none.css"}, read); err != nil || missing.Code != 404 {
		t.Errorf("a missing sheet: %d %v", missing.Code, err)
	}
	head := httptest.NewRecorder()
	if err := serveSheet(head, httptest.NewRequest("HEAD", "/f/x", nil), strings.Split("_ds/kit/fonts/fonts.css", "/"), read); err != nil || head.Code != 200 || head.Body.Len() != 0 {
		t.Errorf("HEAD: %d %d %v", head.Code, head.Body.Len(), err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// The viewer gzips a file for a request that names no encoding, which is how serveSheet reads: the sheet must go out as
// plain CSS, its fonts unpacked into it, never as packed bytes under text/css (a board came without styles).
func TestStoreSheetUnpacksWhatTheViewerPacked(t *testing.T) {
	files := map[string]string{
		"lib/a.css":   `.a{color:red}@font-face{font-family:F;src:url(./f.ttf)}`,
		"lib/f.ttf":   "FONT",
		"lib/big.css": "x",
	}
	read := func(w http.ResponseWriter, r *http.Request, segs []string) error {
		p := strings.Join(segs, "/")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(files[p]))
		_ = zw.Close()
		if p == "lib/big.css" {
			w.Header().Set("Content-Encoding", "br")
		} else {
			w.Header().Set("Content-Encoding", "gzip")
		}
		_, _ = w.Write(buf.Bytes())
		return nil
	}
	rec := httptest.NewRecorder()
	if err := serveSheet(rec, httptest.NewRequest("GET", "/f/lib/a.css", nil), []string{"lib", "a.css"}, read); err != nil {
		t.Fatal(err)
	}
	font := `url("data:font/ttf;base64,` + base64.StdEncoding.EncodeToString([]byte("FONT")) + `")`
	if body := rec.Body.String(); !strings.HasPrefix(body, ".a{color:red}") || !strings.Contains(body, font) || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("the sheet is not plain CSS: %v %q", rec.Header(), body)
	}
	// An encoding the hub does not unpack goes on as it came, with its header.
	other := httptest.NewRecorder()
	if err := serveSheet(other, httptest.NewRequest("GET", "/f/lib/big.css", nil), []string{"lib", "big.css"}, read); err != nil {
		t.Fatal(err)
	}
	if other.Header().Get("Content-Encoding") != "br" {
		t.Errorf("an unknown encoding lost its header: %v", other.Header())
	}
}

// The sheet of a revision goes out named by its hash, packed for a browser that takes gzip, and a browser that holds
// it gets 304: fonts.css of a kit is about a megabyte that blocks a board's first paint.
func TestStoreSheetValidatorAndPacking(t *testing.T) {
	css := ".a{}" + strings.Repeat("@font-face{font-family:F;src:url(./f.ttf)}", 40)
	read := func(w http.ResponseWriter, r *http.Request, segs []string) error {
		if strings.Join(segs, "/") == "f.ttf" {
			_, _ = w.Write([]byte(strings.Repeat("FONT", 500)))
			return nil
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(css))
		return nil
	}
	out, pass, err := buildSheet(httptest.NewRequest("GET", "/f/a.css", nil), []string{"a.css"}, read)
	if err != nil || pass != nil || out == nil {
		t.Fatalf("build: %v %v", pass, err)
	}
	cache := newSheetCache(1 << 20)
	key := sheetKey("sp", "p", 3, []string{"a.css"})
	cache.put(key, out)
	got := cache.get(key)
	if got != out || cache.get(sheetKey("sp", "p", 4, []string{"a.css"})) != nil {
		t.Fatal("a sheet is kept by its revision")
	}

	packed := httptest.NewRequest("GET", "/f/a.css", nil)
	packed.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	got.write(rec, packed)
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != itoa(rec.Body.Len()) {
		t.Fatalf("packed: %d %v", rec.Code, rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	_, _ = plain.ReadFrom(zr)
	if plain.String() != string(out.body) || !strings.Contains(plain.String(), "data:font/ttf;base64,") {
		t.Fatalf("packed body is not the sheet")
	}

	held := httptest.NewRequest("GET", "/f/a.css", nil)
	held.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	again := httptest.NewRecorder()
	got.write(again, held)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("a held sheet: %d", again.Code)
	}

	small := newSheetCache(out.size() * 4)
	small.put("a", out)
	small.put("b", out)
	small.put("c", out)
	small.put("d", out)
	small.put("e", out)
	if small.get("a") != nil || small.get("e") == nil || small.used > small.max {
		t.Fatalf("the oldest sheet is let go: used %d of %d", small.used, small.max)
	}
}
