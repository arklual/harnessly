package main

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// a page in RUNTIME_WEB_DIR that calls a route this runtime lacks gives way to the compiled-in page.
func TestWebDirServedOnlyWhenTheRuntimeAnswersEveryCall(t *testing.T) {
	own := fstest.MapFS{"index.html": {}, "web-release.json": {Data: []byte(`{"schema":1,"version":"r","routes":[["api","apps"],["api","platform","chats"]]}`)}}
	page := func(calls string) fstest.MapFS {
		return fstest.MapFS{"index.html": {}, "web-release.json": {Data: []byte(`{"schema":1,"version":"p","calls":` + calls + `}`)}}
	}
	cases := []struct {
		name          string
		dir, embedded fstest.MapFS
		want          bool
	}{
		{"every call served", page(`[["api","apps","x"],["api","platform","chats"]]`), own, true},
		{"newer page", page(`[["api","apps"],["api","link-meta"]]`), own, false},
		{"no contract in the directory: as before", fstest.MapFS{"index.html": {}}, own, true},
		{"no page compiled in: the directory is all there is", page(`[["api","link-meta"]]`), nil, true},
		{"the runtime's page has no contract", page(`[["api","apps"]]`), fstest.MapFS{"index.html": {}}, false},
		{"unreadable contract", fstest.MapFS{"web-release.json": {Data: []byte(`{`)}}, own, false},
	}
	for _, c := range cases {
		var embedded fs.FS // a nil map in the interface is not a nil page
		if c.embedded != nil {
			embedded = c.embedded
		}
		if got := webDirServed(c.dir, embedded, c.name); got != c.want {
			t.Errorf("%s: served = %v, want %v", c.name, got, c.want)
		}
	}
}
