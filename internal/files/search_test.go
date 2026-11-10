package files

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// «Найти в файлах» (HRN-679): lines with their place, the case ignored for a query in small letters, and nothing from
// service folders, credentials, binaries or links.
func TestSearchTextFindsLinesAndKeepsOut(t *testing.T) {
	dir := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("src/a.ts", "const x = 1;\n    const Needle = 2;\nnothing\n")
	put("b.md", "Строка с needle внутри\n")
	put("node_modules/m/index.js", "needle\n")
	put(".git/HEAD", "needle\n")
	put(".env", "NEEDLE=1\n")
	put("secret.txt", "needle\n")
	put("bin.dat", "needle\x00\x01")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(filepath.Join(outside, "o.txt"), filepath.Join(dir, "link.txt"))
	_ = os.Symlink(outside, filepath.Join(dir, "linkdir"))

	hits, truncated := SearchText(context.Background(), dir, "needle", func(abs string) bool {
		return strings.HasSuffix(abs, "secret.txt")
	})
	if truncated {
		t.Error("truncated")
	}
	var got []string
	for _, h := range hits {
		got = append(got, h.Path+":"+strconv.Itoa(h.Line)+":"+strconv.Itoa(h.Col)+":"+h.Text)
	}
	want := "b.md:1:10:Строка с needle внутри,src/a.ts:2:11:const Needle = 2;"
	if strings.Join(got, ",") != want {
		t.Errorf("hits = %q\nwant  %q", strings.Join(got, ","), want)
	}
	// A capital letter makes the case count.
	if hits, _ := SearchText(context.Background(), dir, "Needle", nil); len(hits) != 1 || hits[0].Path != "src/a.ts" {
		t.Errorf("case-sensitive hits = %+v", hits)
	}
}

func TestSearchTextCutsALongLineAroundTheMatch(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("a", 500) + "needle" + strings.Repeat("b", 500)
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, _ := SearchText(context.Background(), dir, "needle", nil)
	if len(hits) != 1 || !strings.Contains(hits[0].Text, "needle") || len([]rune(hits[0].Text)) > searchMaxLine+2 {
		t.Fatalf("hit = %+v", hits)
	}
	if hits[0].Col != 501 {
		t.Errorf("col = %d", hits[0].Col)
	}
}
