package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harnessly/internal/jsvalue"
)

// The golden file is what the previous runtime's parser answered on the corpus (testdata/codex/gen). CODEX_GOLDEN and
// CODEX_GOLDEN_DIR run another such file (gen.mjs mass) over its own folder.

type codexStep struct {
	Op   string `json:"op"`
	Size int64  `json:"size"`
	Max  int    `json:"max"`
}

type codexCase struct {
	Name   string          `json:"name"`
	File   string          `json:"file"`
	Kind   string          `json:"kind"`
	Max    int             `json:"max"`
	Before int64           `json:"before"`
	Until  *int64          `json:"until"`
	Steps  []codexStep     `json:"steps"`
	Result json.RawMessage `json:"result"`
}

type codexGolden struct {
	Now     float64     `json:"now"`
	Uploads string      `json:"uploads"`
	Prefix  string      `json:"prefix"`
	Cases   []codexCase `json:"cases"`
}

func entriesJSON(entries []*jsvalue.Object) string {
	arr := make([]any, len(entries))
	for i, e := range entries {
		arr[i] = e
	}
	return stringify(arr)
}

func codexChunkJSON(c Chunk) string {
	o := jsvalue.NewObject("entries", nil, "startByteOffset", c.StartByteOffset, "fileSize", c.FileSize, "hasMore", c.HasMore,
		"errorCount", c.ErrorCount)
	arr := make([]any, len(c.Entries))
	for i, e := range c.Entries {
		arr[i] = e
	}
	o.Set("entries", arr)
	if c.Summary != nil {
		o.Set("summary", *c.Summary)
	}
	return stringify(o)
}

func incrementalJSON(r Incremental) string {
	arr := make([]any, len(r.NewEntries))
	for i, e := range r.NewEntries {
		arr[i] = e
	}
	return stringify(jsvalue.NewObject("newEntries", arr, "newOffset", r.NewOffset, "pendingBuffer", r.PendingBuffer))
}

// jsLength is a result's length as JavaScript counts it, which decides whether gen.mjs kept it or its digest.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func keep(s string) string {
	if jsLength(s) <= 4096 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func runCodexCase(t *testing.T, c codexCase, dir, work string, opts CodexOptions, now float64) []string {
	t.Helper()
	n := 0
	NewID = func() string { n++; return fmt.Sprintf("id%04d", n) }
	file := filepath.Join(dir, c.File)
	subst := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, dir, "$CORPUS"), work, "$WORK")
	}
	switch c.Kind {
	case "content":
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		entries := ParseCodexContent(decodeUTF8(b), opts)
		if entries == nil {
			return []string{"throws"}
		}
		return []string{subst(entriesJSON(entries))}
	case "tail":
		return []string{subst(codexChunkJSON(ReadCodexTail(file, c.Max, opts)))}
	case "before":
		until := int64(-1)
		if c.Until != nil {
			until = *c.Until
		}
		return []string{subst(codexChunkJSON(ReadCodexBefore(file, c.Before, c.Max, until, opts)))}
	case "stats":
		clearCodexStatsCache()
		return []string{subst(stringify(nilOrObject(CodexTimelineStats(file))))}
	case "steps":
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(work, filepath.Base(c.File))
		p := NewCodexParser(target, opts)
		var out []string
		for _, s := range c.Steps {
			if err := os.WriteFile(target, src[:min(s.Size, int64(len(src)))], 0o644); err != nil {
				t.Fatal(err)
			}
			switch s.Op {
			case "tail":
				out = append(out, subst(codexChunkJSON(p.ParseTail(s.Max))))
			case "inc":
				out = append(out, subst(incrementalJSON(p.ParseIncremental())))
			default:
				out = append(out, subst(entriesJSON(p.FlushStale(now))))
			}
		}
		_ = os.Remove(target)
		return out
	}
	t.Fatalf("unknown case kind %q", c.Kind)
	return nil
}

func nilOrObject(o *jsvalue.Object) any {
	if o == nil {
		return nil
	}
	return o
}

func checkCodexGolden(t *testing.T, path, dir string) {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g codexGolden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	prevNow, prevID := Now, NewID
	t.Cleanup(func() { Now, NewID = prevNow, prevID })
	Now = func() float64 { return g.Now }
	opts := CodexOptions{ImageURL: NewCodexImageURL(g.Uploads, g.Prefix)}
	dir, _ = filepath.Abs(dir)
	work := t.TempDir()
	failed := 0
	for _, c := range g.Cases {
		var want []string
		if c.Kind == "steps" {
			if err := json.Unmarshal(c.Result, &want); err != nil {
				t.Fatal(err)
			}
		} else {
			var s string
			if err := json.Unmarshal(c.Result, &s); err != nil {
				t.Fatal(err)
			}
			want = []string{s}
		}
		got := runCodexCase(t, c, dir, work, opts, g.Now)
		for i := range want {
			g := ""
			if i < len(got) {
				g = keep(got[i])
			}
			if g != want[i] {
				failed++
				if failed <= 10 {
					t.Errorf("%s [%d]:\n got %s\nwant %s", c.Name, i, clip(g), clip(want[i]))
				}
				break
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, len(g.Cases))
	}
	t.Logf("%d cases", len(g.Cases))
}

func clip(s string) string {
	if len(s) > 3000 {
		return s[:3000] + "…"
	}
	return s
}

func TestCodexGolden(t *testing.T) {
	checkCodexGolden(t, filepath.Join("testdata", "codex", "golden.json"), filepath.Join("testdata", "codex", "corpus"))
}

func TestCodexGoldenExtra(t *testing.T) {
	path, dir := os.Getenv("CODEX_GOLDEN"), os.Getenv("CODEX_GOLDEN_DIR")
	if path == "" || dir == "" {
		t.Skip("CODEX_GOLDEN and CODEX_GOLDEN_DIR unset")
	}
	checkCodexGolden(t, path, dir)
}
