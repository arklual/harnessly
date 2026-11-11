package launch

import (
	"os"
	"path/filepath"
	"testing"
)

// The headlines read are kept in a file: after a restart a transcript that did not grow is not opened again, and one
// that grew is read from where the last read stood.
func TestHeadlinesAreKeptOverARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	prompt := func(ts, text string) string {
		return `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"` + text + `"}}` + "\n"
	}
	body := prompt("2026-10-01T10:00:00.000Z", "first") + `{"type":"ai-title","aiTitle":"Named"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "headlines.json")
	h := &Headlines{File: file}
	want := h.Read(path)
	if want.Title == nil || *want.Title != "Named" || want.LastPromptAt == nil {
		t.Fatalf("headline %+v", want)
	}
	h.Flush()

	// The transcript is out of reach for reading: the headline still comes, from the file.
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	again := &Headlines{File: file}
	got := again.Read(path)
	if got.Title == nil || *got.Title != "Named" || got.LastPromptAt == nil || *got.LastPromptAt != *want.LastPromptAt {
		t.Fatalf("after a restart: %+v, want %+v", got, want)
	}

	// It grew: the new lines are read.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if _, err := fh.WriteString(prompt("2026-10-02T10:00:00.000Z", "second")); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	grown := (&Headlines{File: file}).Read(path)
	if grown.LastPromptAt == nil || *grown.LastPromptAt <= *want.LastPromptAt || grown.Title == nil || *grown.Title != "Named" {
		t.Fatalf("after the transcript grew: %+v", grown)
	}
}
