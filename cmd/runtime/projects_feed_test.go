package main

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestProjectDiffTellsEveryKindOfChange(t *testing.T) {
	last := map[string]hubproto.Project{
		"a": {ID: "a", Name: "one", Directories: []string{"/w/a"}},
		"b": {ID: "b", Name: "two"},
		"c": {ID: "c", Name: "three"},
		"d": {ID: "d", Name: "four"},
	}
	list := []hubproto.Project{
		{ID: "a", Name: "one", Directories: []string{"/w/a2"}},
		{ID: "b", Name: "deux"},
		{ID: "c", Name: "three"},
		{ID: "e", Name: "five"},
	}
	got := map[string]string{}
	for _, e := range projectDiff(last, list) {
		got[e.Project.ID] = e.Kind
	}
	want := map[string]string{"a": hubproto.ProjectsPath, "b": hubproto.ProjectsRenamed, "e": hubproto.ProjectsCreated, "d": hubproto.ProjectsDeleted}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}
