package feed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// An update names the source entries its read took, so what is gathered beside the feed needs no read of its own: a
// first read gives all it holds, a later one only what the transcript gained.
func TestUpdateNamesTheEntriesItsReadTook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	line := func(uuid, kind, text string) string {
		return `{"uuid":"` + uuid + `","timestamp":"2026-01-01T00:00:00.000Z","type":"` + kind + `","message":{"role":"` + kind + `","content":"` + text + `"}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(line("00000000-0000-4000-8000-000000000001", "user", "hello")), 0o600); err != nil {
		t.Fatal(err)
	}
	f := New("tab-one", wire.ProviderClaude, &ClaudeSource{Path: path})
	if got := f.Sources(); got != nil {
		t.Fatalf("sources before a read: %d", len(got))
	}
	u := f.Load()
	if !u.Whole || len(u.Read) != 1 || len(f.Sources()) != 1 {
		t.Fatalf("first read: whole %v, read %d, held %d", u.Whole, len(u.Read), len(f.Sources()))
	}
	if u = f.Poll(); u.Whole || len(u.Read) != 0 {
		t.Fatalf("a read that gained nothing: whole %v, read %d", u.Whole, len(u.Read))
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(line("00000000-0000-4000-8000-000000000002", "user", "again")); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	u = f.Poll()
	if u.Whole || len(u.Read) != 1 || u.Read[0].String("type") != "user-message" || len(f.Sources()) != 2 {
		t.Fatalf("a read that gained a line: whole %v, read %d, held %d", u.Whole, len(u.Read), len(f.Sources()))
	}
}
