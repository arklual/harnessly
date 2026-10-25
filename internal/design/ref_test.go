package design

import (
	"path/filepath"
	"testing"
)

func TestRefOf(t *testing.T) {
	root := legacyLibrary(t)
	l := testLibrary(t, root)
	cases := map[string]string{
		"admin":                     "admin",
		"  post ":                   "post",
		"ADMIN":                     "admin", // the title, letter case aside
		filepath.Join(root, "post"): "post",
		filepath.Join(root, "admin", "project", "All.dc.html"): "admin",
		filepath.Join(root, "admin", "..", "post"):             "post",
		"nothing": "",
		filepath.Join(root, "not-a-project", "x"): "",
		root:                                   "",
		"/elsewhere/admin/project/All.dc.html": "",
		"admin/project/All.dc.html":            "",
		"":                                     "",
	}
	for in, want := range cases {
		if got := l.RefOf(in); got != want {
			t.Errorf("RefOf(%q) = %q, want %q", in, got, want)
		}
	}
	if got := l.DesignOfPath(filepath.Join(root, "files", "project", "assets", "x.png")); got != "files" {
		t.Errorf("DesignOfPath: %q", got)
	}
}
