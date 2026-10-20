//go:build webui

package main

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/web"
)

// The release runtime carries the page; machines that run it in front of the previous runtime must not see it unless
// they ask for it (RUNTIME_WEB=on). Run with `go test -tags webui -run TestWebUIBuild ./cmd/runtime` after building web/.
func TestWebUIBuild(t *testing.T) {
	files := web.Dist()
	if files == nil {
		t.Fatal("the webui build carries the page")
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIME_WEB_DIR", "")
	t.Setenv("RUNTIME_WEB", "")
	if webConfigFromEnv(false) != nil {
		t.Fatal("in front of the previous runtime the page stays off")
	}
	if webConfigFromEnv(true) == nil {
		t.Fatal("standalone serves the compiled page")
	}
	t.Setenv("RUNTIME_WEB", "on")
	if webConfigFromEnv(false) == nil {
		t.Fatal("RUNTIME_WEB=on serves it in front of the previous runtime")
	}
	t.Setenv("RUNTIME_WEB", "off")
	if webConfigFromEnv(true) != nil {
		t.Fatal("RUNTIME_WEB=off serves no page")
	}
}

// The compiled-in page goes out gzipped to a browser that takes it: the main bundle is the bulk of a first open.
func TestWebUIGzipsTheBundle(t *testing.T) {
	t.Setenv("RUNTIME_WEB_DIR", "")
	t.Setenv("RUNTIME_WEB", "")
	t.Setenv("RUNTIME_APP_BASE_PATH", "")
	c := webConfigFromEnv(true)
	if c == nil {
		t.Fatal("standalone serves the compiled page")
	}
	var plain, sent, jsPlain, jsSent int
	err := fs.WalkDir(c.files, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		pw := serveWeb(c, "GET", "/agents/"+p, nil)
		gw := serveWeb(c, "GET", "/agents/"+p, http.Header{"Accept-Encoding": {"gzip, br"}})
		if pw.Code != 200 || gw.Code != 200 {
			t.Errorf("%s: %d %d", p, pw.Code, gw.Code)
		}
		plain += pw.Body.Len()
		sent += gw.Body.Len()
		if strings.HasPrefix(p, "assets/index-") && strings.HasSuffix(p, ".js") {
			jsPlain, jsSent = pw.Body.Len(), gw.Body.Len()
			if gw.Header().Get("Content-Encoding") != "gzip" {
				t.Errorf("%s is not gzipped", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if jsPlain == 0 || jsSent*2 > jsPlain {
		t.Fatalf("main bundle: %d bytes plain, %d sent", jsPlain, jsSent)
	}
	t.Logf("main bundle %d -> %d bytes; all assets %d -> %d bytes", jsPlain, jsSent, plain, sent)
}
