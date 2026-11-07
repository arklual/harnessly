package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlineScriptInViewIsAWarning(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := &Package{Dir: dir}
	cases := map[string]bool{
		`<script>alert(1)</script>`:                                            true,
		`<script type="module">import x from './a.js'</script>`:                true,
		`<script src="/__hl/app-sdk.js"></script><script src="a.js"></script>`: false,
		`<script type="application/json">{"a":1}</script>`:                     false,
	}
	for page, want := range cases {
		write("ui/index.html", page)
		var ps Problems
		p.checkInlineScripts("views[0].entry", "ui/index.html", &ps)
		got := len(ps) == 1 && ps[0].Warning && strings.Contains(ps[0].Message, "<script>")
		if got != want {
			t.Errorf("%s: warned %v, want %v (%v)", page, got, want, ps)
		}
	}
}
