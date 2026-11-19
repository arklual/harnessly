package feed

import (
	"slices"
	"testing"
)

func TestMergePagesKeepsTheLastWrittenLast(t *testing.T) {
	got := mergePages([]string{"a.dc.html", "b.dc.html"}, []string{"a.dc.html"})
	if !slices.Equal(got, []string{"b.dc.html", "a.dc.html"}) {
		t.Fatalf("%v", got)
	}
}
