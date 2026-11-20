package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
)

func TestCLITokenFirstHeaderDecides(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("secret\n"), 0o600)
	ok := cliTokenFirst(dir, []string{"x-new-token", "x-old-token"})
	for _, c := range []struct {
		headers map[string]string
		want    bool
	}{
		{map[string]string{"x-new-token": "secret"}, true},
		{map[string]string{"x-old-token": "secret"}, true},
		{map[string]string{"x-new-token": "nope", "x-old-token": "secret"}, false},
		{map[string]string{"x-new-token": ""}, false},
		{map[string]string{}, false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := ok(r); got != c.want {
			t.Errorf("%v: %v", c.headers, got)
		}
	}
	empty := cliTokenFirst(t.TempDir(), []string{"x-new-token"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("x-new-token", "")
	if empty(r) {
		t.Fatal("no token file lets nobody in")
	}
}

// `design panel … show --board "<title>"` sends the pane the board's file, not its title, else the viewer
// answers «Такого макета нет».
func TestBoardPlaceIsTheBoardFile(t *testing.T) {
	lib := design.New(design.Config{LibraryDir: t.TempDir(), DataDir: t.TempDir(), Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	p, err := lib.CreateProject("Приложения")
	if err != nil {
		t.Fatal(err)
	}
	page, err := lib.CreatePage(p.ID, design.PageSpec{Title: "Приложения — загрузка сейчас"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(page.File, ".dc.html") {
		t.Fatalf("page file: %q", page.File)
	}
	for _, named := range []string{page.Title, page.ID, page.File} {
		if got := boardPlace(lib, p.ID, named); got != page.File {
			t.Errorf("board %q goes to the pane as %q, want %q", named, got, page.File)
		}
	}
	if got := boardPlace(lib, p.ID, ""); got != "" {
		t.Errorf("no board: %q", got)
	}
	if got := boardPlace(lib, "no-such-project", "Main"); got != "Main" {
		t.Errorf("unknown project keeps the name: %q", got)
	}
}
