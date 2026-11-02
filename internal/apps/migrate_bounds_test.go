package apps

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A copy of a value into itself doubles the document at every step: it is stopped by the size as it grows, long before
// the memory is spent, and the same goes for too many operations over the steps and for a migration that takes too long.
func TestMigrationIsBounded(t *testing.T) {
	doc := json.RawMessage(`{"d":{"v":"` + strings.Repeat("x", 20<<10) + `"}}`)
	var doubling []PatchOp
	for i := 0; i < 40; i++ {
		doubling = append(doubling, PatchOp{Op: "copy", From: "/d", Path: "/d/e"})
	}
	start := time.Now()
	if _, err := ApplyPatch(doc, doubling); !errors.Is(err, ErrMigration) || !strings.Contains(err.Error(), "grow") {
		t.Fatalf("a doubling copy: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the doubling was stopped late: %v", time.Since(start))
	}

	t.Run("adds that pass the cap", func(t *testing.T) {
		big := json.RawMessage(`"` + strings.Repeat("y", 30<<10) + `"`)
		var ops []PatchOp
		for i := 0; i < 5; i++ {
			ops = append(ops, PatchOp{Op: "add", Path: "/k" + strconv.Itoa(i), Value: big})
		}
		if _, err := ApplyPatch(json.RawMessage(`{}`), ops); !errors.Is(err, ErrMigration) {
			t.Fatalf("%v", err)
		}
	})

	t.Run("a document within the cap that moves and shrinks passes", func(t *testing.T) {
		small := json.RawMessage(`{"a":"` + strings.Repeat("z", 40<<10) + `"}`)
		ops := []PatchOp{{Op: "move", From: "/a", Path: "/b"}, {Op: "copy", From: "/b", Path: "/c"}, {Op: "remove", Path: "/b"}}
		if _, err := ApplyPatch(small, ops); err != nil {
			t.Fatalf("%v", err)
		}
	})

	t.Run("the operations count over all the steps", func(t *testing.T) {
		b := newPatchBudget(2)
		step := make([]PatchOp, 6_000)
		for i := range step {
			step[i] = PatchOp{Op: "test", Path: "", Value: json.RawMessage(`{}`)}
		}
		out, err := applyPatchWith(json.RawMessage(`{}`), step, b)
		if err != nil {
			t.Fatalf("the first step: %v", err)
		}
		if _, err := applyPatchWith(out, step, b); !errors.Is(err, ErrMigration) || !strings.Contains(err.Error(), "operations") {
			t.Fatalf("the second step: %v", err)
		}
	})

	t.Run("time", func(t *testing.T) {
		b := newPatchBudget(2)
		b.deadline = time.Now().Add(-time.Second)
		if _, err := applyPatchWith(json.RawMessage(`{}`), []PatchOp{{Op: "test", Path: "", Value: json.RawMessage(`{}`)}}, b); !errors.Is(err, ErrMigration) || !strings.Contains(err.Error(), "long") {
			t.Fatalf("%v", err)
		}
	})

	t.Run("a document over the cap is not started", func(t *testing.T) {
		if _, err := ApplyPatch(json.RawMessage(`"`+strings.Repeat("q", 101<<10)+`"`), nil); !errors.Is(err, ErrMigration) {
			t.Fatalf("%v", err)
		}
	})
}
