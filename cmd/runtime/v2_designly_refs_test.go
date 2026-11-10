package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A design attached to a chat's message is named with its folder and the rules of a design dialog. Naming it is no work
// on it: the chat does not become one of the design's chats (HRN-675).
func TestAnAttachedDesignIsNamedNotLinked(t *testing.T) {
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
	if st := d.state(); len(st.CreatedFrom) != 0 {
		t.Fatalf("a named design made the chat its own: %v", st.CreatedFrom)
	}
	if _, code := d.Line("tab-a", wire.MessageRef{Kind: wire.RefDesign, ID: "nope"}); code != wire.RefuseNotFound {
		t.Fatalf("gone: %q", code)
	}
}

// The links of the older rule (a design named in a message) are dropped by the next write of the state file, and the
// record of the chats started from a design stays bounded.
func TestTheOldLinksGoAndTheRecordIsBounded(t *testing.T) {
	data := t.TempDir()
	file := filepath.Join(data, "designly.json")
	old := `{"kinds":{},"pinned":{},"attachedDesigns":{"tab-x":["landing"]}}`
	if err := os.WriteFile(file, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &v2Designly{file: file}
	if err := d.change(func(st *designlyState) { markCreatedFrom(st, "tab-a", "landing") }); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "attachedDesigns") || !strings.Contains(string(raw), `"tab-a": "landing"`) {
		t.Fatalf("state: %s", raw)
	}
	var st designlyState
	for i := range createdFromMax + 3 {
		markCreatedFrom(&st, fmt.Sprintf("tab-%05d", i), "landing")
	}
	if len(st.CreatedFrom) != createdFromMax || st.CreatedFrom["tab-00000"] != "" {
		t.Fatalf("%d kept", len(st.CreatedFrom))
	}
}

// a deck another runtime of the shared library made is a deck here too. This runtime's state file has no
// kind for it; the deck's own settings file in the project's folder tells it.
func TestADeckOfAnotherRuntimeIsADeck(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	for _, id := range []string{"talk", "landing"} {
		if err := os.MkdirAll(filepath.Join(root, id, "meta"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "talk", filepath.FromSlash(design.DeckSettingsFile)), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := design.New(design.Config{LibraryDir: root, DataDir: data, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	var st designlyState
	if got := kindOf(st, lib, "talk"); got != wire.DesignSlides {
		t.Fatalf("a deck with no kind in the state: %q", got)
	}
	if got := kindOf(st, lib, "landing"); got != wire.DesignSite {
		t.Fatalf("a site: %q", got)
	}
	st.Kinds = map[string]wire.DesignKind{"landing": wire.DesignSlides}
	if got := kindOf(st, lib, "landing"); got != wire.DesignSlides {
		t.Fatalf("the state's own kind: %q", got)
	}
}

// a video is a deck folder with meta/video.json; it is a video, whatever the state file says.
func TestAVideoIsToldBeforeADeck(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "teaser", "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{design.DeckSettingsFile, design.VideoSettingsFile} {
		if err := os.WriteFile(filepath.Join(root, "teaser", filepath.FromSlash(f)), []byte(`{"v":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib := design.New(design.Config{LibraryDir: root, DataDir: data, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	st := designlyState{Kinds: map[string]wire.DesignKind{"teaser": wire.DesignSlides}}
	if got := kindOf(st, lib, "teaser"); got != wire.DesignVideo {
		t.Fatalf("a video: %q", got)
	}
	if err := os.Remove(filepath.Join(root, "teaser", filepath.FromSlash(design.VideoSettingsFile))); err != nil {
		t.Fatal(err)
	}
	if got := kindOf(st, lib, "teaser"); got != wire.DesignSlides {
		t.Fatalf("a deck without video.json: %q", got)
	}
}
