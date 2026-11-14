package gitpush

import (
	"reflect"
	"testing"
)

func TestCommitLinks(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"To https://gitlab.example.com/acme/web.git\n   fc896f915..730b8d5d8  HEAD -> main\n",
			[]string{"https://gitlab.example.com/acme/web/-/commit/730b8d5d8"}},
		{"To https://oauth2:secret@gitlab.example.com/acme/web.git\n + 1111111...2222222 feat -> feat (forced update)\n",
			[]string{"https://gitlab.example.com/acme/web/-/commit/2222222"}},
		{"To git@github.com:acme/app.git\n   abcdef1..abcdef2  main -> main\n",
			[]string{"https://github.com/acme/app/commit/abcdef2"}},
		{"To https://gitlab.example.com/acme/web.git\n * [new branch]      feat -> feat\n", nil},
		{"To https://gitlab.example.com/acme/web.git\n ! [rejected]        main -> main (fetch first)\n", nil},
		{"Everything up-to-date\n", nil},
	}
	for _, c := range cases {
		if got := CommitLinks(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q → %v, want %v", c.text, got, c.want)
		}
	}
}
