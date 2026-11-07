package main

import (
	"io/fs"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/design"
)

// The examples folder lays the projects the library is seeded with: the two lists are one.
func TestDemoProjectsAreTheEmbeddedOnes(t *testing.T) {
	entries, err := fs.ReadDir(demoProjects, "demo")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.IsDir() {
			got = append(got, e.Name())
		}
	}
	want := slices.Clone(design.DemoProjects)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("embedded %v, design.DemoProjects %v", got, want)
	}
}
