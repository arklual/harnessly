package design

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

type mapMockups map[string]MockupBoard

func (m mapMockups) Mockup(key string) (MockupBoard, bool) { b, ok := m[key]; return b, ok }
func (m mapMockups) SetMockup(key string, b MockupBoard) error {
	m[key] = b
	return nil
}

const chatMockup = `<!doctype html><html><head><title>t</title><style>.card{border-radius:12px;background:#fff}</style>
<link rel="stylesheet" href="https://fonts.example.com/inter.css"><script>alert(1)</script></head>
<body class="page" style="background:#f1f2f4"><div class="card" onclick="steal()">Декабрь {{ x }}</div>
<style>.row{gap:8px}</style><iframe src="https://example.com"></iframe><x-import from="https://example.com/x.jsx"></x-import>
<a href="javascript:alert(1)">ссылка</a><script src="https://example.com/a.js"></script></body></html>`

func TestBoardFromHTMLKeepsTheLookAndCutsWhatRuns(t *testing.T) {
	board := string(BoardFromHTML(chatMockup))
	for _, want := range []string{
		`<script src="./support.js"></script>`, "<x-dc>", "<helmet>", mockupBase,
		".card{border-radius:12px;background:#fff}", ".row{gap:8px}", `href="https://fonts.example.com/inter.css"`,
		`<main class="board page" style="background:#f1f2f4">`, `<div class="card">Декабрь {` + "\u200b" + `{ x }}</div>`, "ссылка",
	} {
		if !strings.Contains(board, want) {
			t.Errorf("board lacks %q:\n%s", want, board)
		}
	}
	for _, cut := range []string{"alert(1)", "steal()", "<iframe", "x.jsx", "a.js", "<title>", "javascript:", "x-import"} {
		if strings.Contains(board, cut) {
			t.Errorf("board keeps %q:\n%s", cut, board)
		}
	}
	// The body's style is the board's, not a second style element in the markup.
	main := board[strings.Index(board, "<main"):]
	if strings.Contains(main, "<style>") {
		t.Errorf("a style stays in the markup:\n%s", main)
	}
	// A fragment (the usual answer) is a board too.
	if frag := string(BoardFromHTML(`<div style="padding:8px">Шаг 1</div>`)); !strings.Contains(frag, `<main class="board">`+"\n"+`<div style="padding:8px">Шаг 1</div>`) {
		t.Errorf("fragment:\n%s", frag)
	}
}

func TestBoardOfMockupIsMadeOnce(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	store := mapMockups{}
	spec := MockupSpec{Conv: "c1", HTML: chatMockup, Title: "Декабрь · Mass/Pro"}
	first, err := l.BoardOfMockup(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewProject || first.Existing || first.ProjectTitle != "Декабрь · Mass/Pro" || first.Title != "Декабрь · Mass/Pro" {
		t.Fatalf("first: %+v", first)
	}
	b, err := os.ReadFile(filepath.Join(l.ProjectDir(first.Project), first.File))
	if err != nil || string(b) != string(BoardFromHTML(chatMockup)) {
		t.Fatalf("board file: %v %q", err, b)
	}
	again, err := l.BoardOfMockup(spec, store)
	if err != nil || !again.Existing || again.Project != first.Project || again.Page != first.Page {
		t.Fatalf("second press: %+v %v", again, err)
	}
	if list, _ := l.ListPages(first.Project); len(list.Pages) != 1 {
		t.Fatalf("pages after two presses: %+v", list.Pages)
	}
	if n := len(l.TakenArtifactIDs()); n != 1 {
		t.Fatalf("projects after two presses: %d", n)
	}

	// The chip of the press tells the agent the mockup is the board now; a name that is no page of it tells nothing.
	art := l.ReadArtifact(first.Project)
	if note := l.MockupNote(art, first.File); !strings.Contains(note, first.File) || !strings.Contains(note, "Sinopia Design") || !strings.Contains(note, "design panel "+first.Project) {
		t.Fatalf("note: %q", note)
	}
	if note := l.MockupNote(art, "../../etc/passwd"); note != "" {
		t.Fatalf("a foreign board: %q", note)
	}

	// Another mockup of the chat goes to the chat's project as a new page.
	other, err := l.BoardOfMockup(MockupSpec{Conv: "c1", HTML: "<p>v2</p>", Title: "Вариант", Candidates: []string{first.Project}}, store)
	if err != nil || other.NewProject || other.Project != first.Project || other.Page == first.Page {
		t.Fatalf("second mockup: %+v %v", other, err)
	}

	// The board removed: the press makes it again rather than pointing at nothing.
	if _, err := l.RemovePage(first.Project, first.Page); err != nil {
		t.Fatal(err)
	}
	remade, err := l.BoardOfMockup(spec, store)
	if err != nil || remade.Existing || remade.Page == first.Page {
		t.Fatalf("after removal: %+v %v", remade, err)
	}
}

func TestBoardOfMockupLeavesOthersProjectsAlone(t *testing.T) {
	root := t.TempDir()
	l := accessLibrary(t, root)
	theirs, err := l.CreateProject("Чужой")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, theirs.ID, OwnerFile), otherV2)
	if err := l.SetAccess(snapshot(map[string]Grant{theirs.ID: {Role: "view", Owner: "k@x", OwnerName: "Олег"}},
		map[string]string{theirs.ID: "view"})); err != nil {
		t.Fatal(err)
	}
	got, err := l.BoardOfMockup(MockupSpec{Conv: "c2", HTML: "<p>x</p>", Title: "Мой", Candidates: []string{theirs.ID, "../etc"}}, mapMockups{})
	if err != nil || got.Project == theirs.ID || !got.NewProject {
		t.Fatalf("board: %+v %v", got, err)
	}
	if list, _ := l.ListPages(theirs.ID); len(list.Pages) != 0 {
		t.Fatalf("their project got a page: %+v", list.Pages)
	}
	if _, err := l.BoardOfMockup(MockupSpec{Conv: "c2", HTML: "  "}, mapMockups{}); err != ErrMockupEmpty {
		t.Fatalf("empty: %v", err)
	}
}

// `design new --kit` pins a kit installed on the machine; a kit that is not installed is refused before anything is made.
func TestNewProjectKitPin(t *testing.T) {
	root := t.TempDir()
	kits := t.TempDir()
	l := New(Config{LibraryDir: root, KitsDir: kits, Texts: Texts{CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	if p, msg := l.kitPinFor("acme-ui"); p != nil || msg == "" {
		t.Fatalf("not installed: %+v %q", p, msg)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	ds := []byte(`{"kit":{"id":"acme-ui","version":"0.1.0"}}`)
	_ = tw.WriteHeader(&tar.Header{Name: "design-system.json", Mode: 0o644, Size: int64(len(ds)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(ds)
	_ = tw.Close()
	if _, err := designkit.Install(kits, "acme-ui", "0.1.0", &buf, designkit.Options{}); err != nil {
		t.Fatal(err)
	}
	p, msg := l.kitPinFor("acme-ui")
	if p == nil || p.ID != "acme-ui" || p.Version != "0.1.0" || len(p.Mounts) != 0 {
		t.Fatalf("pin: %+v %q", p, msg)
	}
	if p, _ := l.kitPinFor("acme-ui/dark"); p != nil {
		t.Fatalf("a theme the kit has not: %+v", p)
	}
}
