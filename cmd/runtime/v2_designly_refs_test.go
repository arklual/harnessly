package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A design attached to a chat's message is named with its folder and the rules of a design dialog, and the chat is
// linked to it: the library lists the chat among the design's own.
func TestAnAttachedDesignIsNamedAndLinked(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	root, data := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "landing", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	art := `{"v":1,"title":"Landing","boards":[{"title":"Home","path":"Home.dc.html","w":null,"h":null}]}`
	if err := os.WriteFile(filepath.Join(root, "landing", design.ArtifactFile), []byte(art), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "landing", "project", "Home.dc.html"), []byte("<main>home</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := design.New(design.Config{LibraryDir: root, DataDir: data, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	d := &v2Designly{api: &design.API{L: lib}, file: filepath.Join(data, "designly.json")}

	line, code := d.Line("tab-a", wire.MessageRef{Kind: wire.RefDesign, ID: "landing", Title: "Landing"})
	if code != "" {
		t.Fatal(code)
	}
	for _, want := range []string{`Design "Landing"`, filepath.Join(root, "landing"), "Home.dc.html", filepath.Join(data, "design", "library", "landing.md"), "acme design page"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line has no %q: %s", want, line)
		}
	}
	d.Line("tab-b", wire.MessageRef{Kind: wire.RefDesign, ID: "landing"})
	d.Line("tab-a", wire.MessageRef{Kind: wire.RefDesign, ID: "landing"})
	st := d.state()
	if !attachedTo(st, "tab-a", "landing") || !attachedTo(st, "tab-b", "landing") || attachedTo(st, "tab-c", "landing") {
		t.Fatalf("links: %+v", st.AttachedDesigns)
	}
	if got := st.AttachedDesigns["tab-a"]; len(got) != 1 {
		t.Fatalf("linked twice: %v", got)
	}
	if _, code := d.Line("tab-a", wire.MessageRef{Kind: wire.RefDesign, ID: "nope"}); code != wire.RefuseNotFound {
		t.Fatalf("gone: %q", code)
	}
}

func TestAChatKeepsItsNewestDesigns(t *testing.T) {
	var st designlyState
	for i := range attachedMax + 3 {
		attachDesign(&st, "tab-a", "d"+strings.Repeat("x", i))
	}
	got := st.AttachedDesigns["tab-a"]
	if len(got) != attachedMax || got[len(got)-1] != "d"+strings.Repeat("x", attachedMax+2) {
		t.Fatalf("%d: %v", len(got), got[len(got)-1])
	}
}
