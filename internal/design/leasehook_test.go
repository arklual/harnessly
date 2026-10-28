package design

import (
	"path/filepath"
	"testing"
)

func TestBoardWritesNameTheBoardsAToolCallWrites(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Plan", "")
	board := filepath.Join(root, "p1", ProjectDirname, "Main.dc.html")
	cases := []struct {
		name string
		in   GuardInput
		want []BoardWrite
	}{
		{"edit by absolute path", GuardInput{ToolName: "Edit", ToolInput: toolInput(board)}, []BoardWrite{{"p1", "Main.dc.html"}}},
		{"write by relative path", GuardInput{ToolName: "Write", Cwd: filepath.Join(root, "p1", ProjectDirname), ToolInput: toolInput("New.dc.html")}, []BoardWrite{{"p1", "New.dc.html"}}},
		{"a read is no write", GuardInput{ToolName: "Read", ToolInput: toolInput(board)}, nil},
		{"not a board", GuardInput{ToolName: "Edit", ToolInput: toolInput(filepath.Join(root, "p1", ProjectDirname, "notes.md"))}, nil},
		{"not in a project", GuardInput{ToolName: "Edit", ToolInput: toolInput(filepath.Join(root, "x.dc.html"))}, nil},
	}
	for _, c := range cases {
		got := BoardWrites(c.in)
		if len(got) != len(c.want) || (len(got) == 1 && got[0] != c.want[0]) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func toolInput(file string) (ti struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
	Command      string `json:"command"`
}) {
	ti.FilePath = file
	return
}
