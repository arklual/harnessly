package designkit

import (
	"encoding/json"
	"os"
	"testing"
)

// The cases both searches run (this and web/…/designly/kit/kit-search.test.ts): a query must return its nodes first,
// in that order.
func TestSearchCasesSharedWithTheKitScreen(t *testing.T) {
	raw, err := os.ReadFile("testdata/search-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Index []IndexEntry `json:"index"`
		Cases []struct {
			Q    string   `json:"q"`
			Want []string `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Cases {
		got := Search(f.Index, c.Q, 0)
		if len(got) < len(c.Want) {
			t.Errorf("%q: got %d results, want %v", c.Q, len(got), c.Want)
			continue
		}
		for i, w := range c.Want {
			if got[i].Node != w {
				var nodes []string
				for _, g := range got {
					nodes = append(nodes, g.Node)
				}
				t.Errorf("%q: got %v, want %v first", c.Q, nodes, c.Want)
				break
			}
		}
		if len(c.Want) == 0 && len(got) != 0 {
			t.Errorf("%q: want nothing, got %d", c.Q, len(got))
		}
	}
}
