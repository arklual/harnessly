package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotebookTextLimitAndRevisionSave(t *testing.T) {
	root := t.TempDir()
	content := `{"nbformat":4,"nbformat_minor":5,"metadata":{},"cells":[],"saved_output":"` + strings.Repeat("a", editorMaxBytes+100) + `"}`
	write(t, filepath.Join(root, "report.IPYNB"), content)
	read, ee := ReadProjectFile(root, "report.IPYNB")
	if ee != nil {
		t.Fatal(ee)
	}
	revision := read[2].V.(string)
	if read[1].V != content {
		t.Fatal("notebook content was truncated")
	}
	next := content + "\n"
	saved, ee := SaveProjectFile(root, "report.IPYNB", next, revision)
	if ee != nil || saved[1].V != next {
		t.Fatalf("save notebook: %v", ee)
	}
	if _, ee := SaveProjectFile(root, "report.IPYNB", content, revision); ee == nil || ee.Status != 409 {
		t.Fatalf("stale revision accepted: %v", ee)
	}
	write(t, filepath.Join(root, "report.txt"), content)
	if _, ee := ReadProjectFile(root, "report.txt"); ee == nil || ee.Status != 415 {
		t.Fatalf("ordinary text limit raised: %v", ee)
	}
}

func TestNotebookTextLimitStillRejectsOversizeAndBinary(t *testing.T) {
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "large.ipynb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(NotebookMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ee := ReadProjectFile(root, "large.ipynb"); ee == nil || ee.Status != 415 || !strings.Contains(ee.Text, "16") {
		t.Fatalf("oversize notebook accepted: %v", ee)
	}
	for name, text := range map[string]string{"binary.ipynb": "a\x00b", "invalid.ipynb": "\xff"} {
		write(t, filepath.Join(root, name), text)
		if _, ee := ReadProjectFile(root, name); ee == nil || ee.Status != 415 {
			t.Errorf("%s accepted: %v", name, ee)
		}
	}
}
