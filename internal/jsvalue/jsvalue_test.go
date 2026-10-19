package jsvalue

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

// testdata/cases.json is node's answer for each input (testdata/gen.mjs).
func TestMatchesNode(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct{ Input, Compact, Pretty string }
		Bad   []string
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for _, c := range golden.Cases {
		v, err := Parse([]byte(c.Input))
		if err != nil {
			t.Errorf("%s: %v", c.Input, err)
			continue
		}
		if got := string(Stringify(v, "")); got != c.Compact {
			t.Errorf("compact %s\n got %s\nwant %s", c.Input, got, c.Compact)
		}
		if got := string(Stringify(v, "  ")); got != c.Pretty {
			t.Errorf("pretty %s\n got %s\nwant %s", c.Input, got, c.Pretty)
		}
	}
	for _, b := range golden.Bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Errorf("accepted %q", b)
		}
	}
}

func TestObjectSemantics(t *testing.T) {
	o := NewObject("a", 1, "b", "x", "c", true)
	o.Set("a", 2)
	o.Delete("b")
	o.Set("d", nil)
	o.Set("b", []string{"y"})
	o.Set("c", Undefined)
	if got := string(Stringify(o, "")); got != `{"a":2,"d":null,"b":["y"]}` {
		t.Fatal(got)
	}
	c := Clone(o).(*Object)
	c.Array("b")[0] = "z"
	if o.Array("b")[0] != "y" {
		t.Fatal("clone shares arrays")
	}
	src := NewObject("z", 1, "a", 2)
	o.Replace(src)
	if !reflect.DeepEqual(o.Keys(), []string{"z", "a"}) {
		t.Fatal(o.Keys())
	}
	if !SameValue(NewObject("a", 1, "b", NewObject("x", 1, "y", 2)), NewObject("b", NewObject("y", 2, "x", 1), "a", 1)) {
		t.Fatal("SameValue ignores key order")
	}
	if Equal(NewObject("a", 1, "b", 2), NewObject("b", 2, "a", 1)) {
		t.Fatal("Equal must see key order")
	}
}

// With node on the machine, a few documents are also compared live.
func TestAgainstLiveNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	doc := `{"root":{"type":"split","ratio":33.33,"children":[{"type":"pane","id":"pane-a","tabs":[{"id":"tab-1","order":0,"name":"Имя \u0007","x":1e-7}],"activeTabId":null},{"type":"pane","id":"p","tabs":[],"activeTabId":"t"}]},"10":1,"9":2,"activePaneId":"pane-a","n":-0}`
	out, err := exec.Command("node", "-e", `process.stdout.write(JSON.stringify(JSON.parse(process.argv[1]), null, 2))`, doc).Output()
	if err != nil {
		t.Fatal(err)
	}
	v, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(Stringify(v, "  ")); got != string(out) {
		t.Fatalf("got\n%s\nwant\n%s", got, out)
	}
}
