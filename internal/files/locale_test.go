package files

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishEditorAndPreviewRefusals(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	tmp, root := project(t)
	cases := map[string]string{
		"../outside/private.txt": "The path is outside the project.",
		"escape/private.txt":     "The link leads outside the project.",
		"a\x00b":                 "Only files of the current project can be opened.",
		"missing.txt":            "The file or folder no longer exists.",
	}
	for rel, want := range cases {
		if _, err := ReadProjectFile(root, rel); err == nil || err.Text != want {
			t.Errorf("read %q: %v, want %q", rel, err, want)
		}
	}
	write(t, filepath.Join(root, "bin"), "A\x00B")
	write(t, filepath.Join(root, "invalid"), "\xff")
	write(t, filepath.Join(root, "over"), strings.Repeat("a", editorMaxBytes+1))
	for rel, want := range map[string]string{
		"bin":     "This file is not a text file.",
		"invalid": "The editor only supports UTF-8 text.",
		"over":    "The editor supports files up to 1 MB.",
	} {
		if _, err := ReadProjectFile(root, rel); err == nil || err.Text != want {
			t.Errorf("read %q: %v, want %q", rel, err, want)
		}
	}
	if _, err := WriteProjectFile(root, "hello.txt", "x", "stale-revision", false); err == nil || cyrillic.MatchString(err.Text) {
		t.Errorf("write: %v", err)
	}

	a := NewAccess([]string{tmp}, nil)
	if got := a.Preview("relative.png"); got.Text != "An absolute path is required" {
		t.Errorf("relative: %+v", got)
	}
	if got := a.Preview(filepath.Join(tmp, "missing.png")); got.Text != "File not found" {
		t.Errorf("missing: %+v", got)
	}
	write(t, filepath.Join(tmp, "noext"), "x")
	if got := a.Preview(filepath.Join(tmp, "noext")); got.Text != "Preview exists only for pictures, video, md, html and pdf" {
		t.Errorf("type: %+v", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := a.Preview("relative.png"); got.Text != "Нужен абсолютный путь" {
		t.Errorf("ru relative: %+v", got)
	}
}
