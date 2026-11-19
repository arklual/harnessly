package bgmodel

import "testing"

func TestFolderFromAnswer(t *testing.T) {
	folders := []FolderInfo{{ID: "p1", Name: "admin"}, {ID: "proj-2", Name: "учеба"}}
	cases := []struct{ in, want string }{
		{"p1", "p1"},
		{"  `proj-2`\n", "proj-2"},
		{"id: p1", "p1"},
		{"none", ""},
		{"None.", ""},
		{"", ""},
		{"made-up", ""},
		{"I think admin", ""},
	}
	for _, c := range cases {
		if got := FolderFromAnswer(c.in, folders); got != c.want {
			t.Errorf("FolderFromAnswer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
