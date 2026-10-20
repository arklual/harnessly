//go:build webui

package main

import (
	"io/fs"
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
