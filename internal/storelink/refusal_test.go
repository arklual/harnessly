package storelink

import "testing"

func TestDesignRefusalGroupIsTheProject(t *testing.T) {
	for rel, want := range map[string]string{
		"admin/project/a.dc.html":  "project:admin",
		"Admin/meta/project.json":  "project:admin",
		"admin/artifact.json":      "project:admin",
		"admin/.owner.json":        "",
		"admin/.Owner.JSON":        "",
		".folders/shelf.json":      "",
		"loose.txt":                "",
		"admin/":                   "",
		"Проект/project/a.dc.html": "folder:Проект",
		"Проект/.owner.json":       "folder:Проект",
	} {
		if got := designRefusalGroup(rel); got != want {
			t.Errorf("%s: %q, want %q", rel, got, want)
		}
	}
}
