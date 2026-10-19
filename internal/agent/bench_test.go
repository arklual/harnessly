package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func benchFile(b *testing.B) string {
	file := filepath.Join(b.TempDir(), "s.jsonl")
	line := `{"type":"assistant","message":{"content":"` + strings.Repeat("lorem ipsum ", 80) + `"}}` + "\n"
	if err := os.WriteFile(file, []byte(strings.Repeat(line, (8<<20)/len(line))), 0o600); err != nil {
		b.Fatal(err)
	}
	return file
}

func BenchmarkReadLines(b *testing.B) {
	file := benchFile(b)
	b.SetBytes(1 << 20)
	for b.Loop() {
		if _, err := ReadLines(file, 0, 1<<20); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadResponse(b *testing.B) {
	file := benchFile(b)
	b.SetBytes(1 << 20)
	for b.Loop() {
		c, data, release, _ := readLines(file, 0, 1<<20)
		_ = encodeReadAnswer(json.RawMessage("7"), c, data)
		release()
	}
}
