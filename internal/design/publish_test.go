package design

import (
	"errors"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/sitesclient"
)

func publishPaths(files []sitesclient.DeployFile) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		out[f.Path] = string(f.Data)
	}
	return out
}

func TestPublishSnapshotTakesThePagesAndLeavesTheRest(t *testing.T) {
	a, _ := publicAPI(t, "")
	board := a.L.ReadArtifact("p1").Boards[0].Path

	title, files, err := a.L.PublishSnapshot("p1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Shop" {
		t.Fatalf("title %q", title)
	}
	got := publishPaths(files)
	for _, want := range []string{board, "index.html", "support.js", "assets/a.css"} {
		if _, ok := got[boardFile(want)]; !ok {
			t.Errorf("%s is not in the snapshot: %v", want, got)
		}
	}
	for path := range got {
		if strings.HasPrefix(path, ".") || strings.Contains(path, "/.") || path == "leak.txt" || strings.HasPrefix(path, "link/") {
			t.Errorf("%s must not be published", path)
		}
	}
	if _, ok := got["assets/notes.bin"]; ok {
		t.Error("a file of an unknown type is published")
	}
}

func TestPublishSnapshotLimitedToPages(t *testing.T) {
	a, _ := publicAPI(t, "")
	_, files, err := a.L.PublishSnapshot("p1", []string{"no-such-page.html"})
	if !errors.Is(err, ErrPublishEmpty) {
		t.Fatalf("a choice with no page: %v %v", files, err)
	}

	board := a.L.ReadArtifact("p1").Boards[0].Path
	_, files, err = a.L.PublishSnapshot("p1", []string{board})
	if err != nil {
		t.Fatal(err)
	}
	got := publishPaths(files)
	// index.html is a page of the project that was not chosen: it stays home and a new one opens the chosen page.
	if strings.Contains(got["index.html"], "<html>index</html>") || !strings.Contains(got["index.html"], boardFile(board)) {
		t.Fatalf("index of a limited snapshot: %q", got["index.html"])
	}
}

func TestPublishIndexListsOrOpensThePages(t *testing.T) {
	one := publishIndex("Shop <b>", []Board{{Title: "Main", Path: "Main.dc.html?A"}})
	if !strings.Contains(one, `http-equiv="refresh"`) || !strings.Contains(one, `url=Main.dc.html"`) || strings.Contains(one, "<b>") {
		t.Fatalf("one page opens at once, the title is escaped: %s", one)
	}
	many := publishIndex("Shop", []Board{{Title: "Main", Path: "a.html"}, {Title: "Cart", Path: "b.html"}, {Title: "Again", Path: "b.html?X"}})
	if strings.Count(many, "<li>") != 2 || !strings.Contains(many, `href="b.html"`) {
		t.Fatalf("pages are listed once each: %s", many)
	}
}
