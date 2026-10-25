package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func idsOf(c Chunk) []string {
	out := make([]string, 0, len(c.Entries))
	for _, o := range c.Entries {
		out = append(out, o.String("id"))
	}
	return out
}

// A read by bytes from the end and the reads of the bytes before it, wherever their windows fall (inside a line,
// on its newline), give the entries of a whole read, each once, in order; each starts at a line's start.
func TestReadByBytesFromTheEndGivesTheWholeRead(t *testing.T) {
	var b strings.Builder
	for i := range 30 {
		ts := fmt.Sprintf("2026-10-12T10:00:%02d.000Z", i)
		fmt.Fprintf(&b, `{"type":"user","uuid":"u-%02d","timestamp":%q,"message":{"role":"user","content":"question %d %s"}}`+"\n", i, ts, i, strings.Repeat("q", i*7))
		fmt.Fprintf(&b, `{"type":"assistant","uuid":"a-%02d","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", i, ts, i)
	}
	text := b.String()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	want := idsOf(Chunk{Entries: ParseContent(text)})
	if len(want) != 60 {
		t.Fatalf("whole read: %d entries", len(want))
	}
	for window := int64(40); window < int64(len(text)); window += 61 {
		c := ReadTailBytes(path, window, 10_000)
		if c.FileSize != int64(len(text)) {
			t.Fatalf("window %d: read to %d of %d", window, c.FileSize, len(text))
		}
		got := idsOf(c)
		start, more := c.StartByteOffset, c.HasMore
		for range 1000 {
			if start != 0 && text[start-1] != '\n' {
				t.Fatalf("window %d: a read starts at %d, inside a line", window, start)
			}
			if !more {
				break
			}
			p := ReadBeforeBytes(path, start, window, 10_000)
			if len(p.Entries) == 0 && p.HasMore {
				// No line fits in the window: a reader asks for a larger one.
				p = ReadBeforeBytes(path, start, int64(len(text)), 10_000)
			}
			got = append(idsOf(p), got...)
			start, more = p.StartByteOffset, p.HasMore
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("window %d: paged %v\nwhole %v", window, got, want)
		}
	}
}

// The last max entries of the window, and the byte the first of them starts at.
func TestReadTailBytesKeepsTheLastMax(t *testing.T) {
	text := ""
	for i := range 10 {
		text += fmt.Sprintf(`{"type":"user","uuid":"u-%d","timestamp":"2026-10-12T10:00:0%d.000Z","message":{"role":"user","content":"q%d"}}`+"\n", i, i, i)
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c := ReadTailBytes(path, 1<<20, 3)
	if got := idsOf(c); len(got) != 3 || !c.HasMore {
		t.Fatalf("%v more %v", got, c.HasMore)
	}
	if !strings.HasPrefix(text[c.StartByteOffset:], `{"type":"user","uuid":"u-7"`) {
		t.Fatalf("starts at %d: %.40s", c.StartByteOffset, text[c.StartByteOffset:])
	}
}
