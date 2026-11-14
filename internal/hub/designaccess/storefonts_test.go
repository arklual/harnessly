package designaccess

import (
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
	req.Header.Set("Accept-Encoding", "gzip")
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
	if rec.Header().Get("ETag") != "" || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" || rec.Header().Get("Content-Length") != itoa(len(body)) {
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
