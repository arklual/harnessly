package background

import (
	"strings"
	"testing"
	"unsafe"
)

// What a scan keeps of a transcript line is a copy: an id cut out of a megabyte line must not keep the line alive
// for as long as the tab is open.
func TestScanKeepsNoSliceOfTheLine(t *testing.T) {
	line := `{"type":"user","message":{"content":"<task-id>abc123</task-id><status>completed</status>` + strings.Repeat("x", 1<<20) + `"}}`
	s := newScan()
	s.open.set(Task{ID: "abc123", Kind: Agent})
	s.lines([]string{line}, 1)
	lo, hi := uintptr(unsafe.Pointer(unsafe.StringData(line))), uintptr(unsafe.Pointer(unsafe.StringData(line)))+uintptr(len(line))
	inside := func(x string) bool {
		p := uintptr(unsafe.Pointer(unsafe.StringData(x)))
		return p >= lo && p < hi
	}
	if len(s.ended) != 1 {
		t.Fatalf("ended %d", len(s.ended))
	}
	for id := range s.done {
		if inside(id) {
			t.Fatalf("done key %q points into the line", id)
		}
	}
	e := s.ended[0]
	if inside(e.ID) || inside(e.Status) {
		t.Fatal("ended task points into the line")
	}
}
