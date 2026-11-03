package hubproto

import (
	"reflect"
	"testing"
)

func TestMRKey(t *testing.T) {
	if got := MRKey("GitLab.Example.com", "team%2Fapp", "5"); got != "gitlab.example.com/team/app/!5" {
		t.Error(got)
	}
	if got := MRKeyOfURL("https://Git.example.com/a/b%20c/-/merge_requests/8?x=1"); got != "git.example.com/a/b c/!8" {
		t.Error(got)
	}
	if got := MRKeyOfURL("https://github.com/a/b/pull/1"); got != "" {
		t.Error(got)
	}
	text := "https://h/a/-/merge_requests/1 https://h/a/-/merge_requests/1/diffs https://h/a/b/-/merge_requests/22."
	if got := MRKeysIn(text); !reflect.DeepEqual(got, []string{"h/a/!1", "h/a/b/!22"}) {
		t.Error(got)
	}
	if !IsMRKey("h/a/!1") || IsMRKey("https://h/a/!1") || IsMRKey("a!1") {
		t.Error("IsMRKey")
	}
}
