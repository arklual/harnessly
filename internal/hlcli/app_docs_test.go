package hlcli

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ly-solutions/harnessly/docs"
)

func docsCLI(t *testing.T) (*cli, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return &cli{env: Env{Stdout: &out, Stderr: &out}}, &out
}

func docsFixture() fs.FS {
	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return fstest.MapFS{
		"apps.md":                      file("# Apps\n"),
		"build-apps/README.md":         file("# Overview\n"),
		"build-apps/02-views.md":       file("text\n# Views\nbody\n"),
		"apps/demo/harnessly/app.json": file("{}\n"),
	}
}

func TestDocsListNamesEveryPageInOrder(t *testing.T) {
	c, out := docsCLI(t)
	if err := c.appDocsFrom(docsFixture(), nil); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	view, over, ref := strings.Index(got, "views"), strings.Index(got, "overview"), strings.Index(got, "reference")
	if view < 0 || over < 0 || ref < 0 || over > view || view > ref {
		t.Fatalf("the list: %q", got)
	}
}

func TestDocsPrintsAPageAndNamesThePagesOnAMiss(t *testing.T) {
	c, out := docsCLI(t)
	if err := c.appDocsFrom(docsFixture(), []string{"views"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "# Views") {
		t.Fatalf("the page: %q", out)
	}
	err := c.appDocsFrom(docsFixture(), []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "views") {
		t.Fatalf("a miss: %v", err)
	}
}

func TestDocsExportKeepsTheLayoutAndWritesOverNothing(t *testing.T) {
	c, _ := docsCLI(t)
	dir := filepath.Join(t.TempDir(), "out")
	if err := c.appDocsFrom(docsFixture(), []string{"export", dir}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"apps.md", "build-apps/02-views.md", "apps/demo/harnessly/app.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if err := c.appDocsFrom(docsFixture(), []string{"export", dir}); err == nil {
		t.Fatal("an export into a folder with files went through")
	}
}

var mdLink = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)

// The guide ships in the binary and a link that leads nowhere is a dead end for an agent with nothing else to read.
func TestTheGuideHasTitlesAndItsLinksLead(t *testing.T) {
	pages, err := docsPages(docs.Guide)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatalf("the guide has %d pages", len(pages))
	}
	for _, p := range pages {
		if p.Title == "" {
			t.Errorf("%s has no title", p.File)
		}
		raw, err := fs.ReadFile(docs.Guide, p.File)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(raw), -1) {
			to := m[1]
			if strings.Contains(to, "://") || strings.HasPrefix(to, "mailto:") {
				continue
			}
			target := path.Join(path.Dir(p.File), to)
			if _, err := fs.Stat(docs.Guide, target); err != nil {
				t.Errorf("%s links to %s: %v", p.File, to, err)
			}
		}
	}
}
