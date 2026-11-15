package apps

import (
	"strings"
	"testing"
)

// An app's objects take a quick edit by voice through three tools of its own.
func TestQuickEditManifest(t *testing.T) {
	with := func(q string) Problems {
		t.Helper()
		raw := strings.Replace(cashback, `"chat": { "cards": ["card"] }`, `"chat": { "cards": ["card"] }, "quickEdit": `+q, 1)
		if raw == cashback {
			t.Fatal("the fixture did not change")
		}
		_, ps := ParseManifest([]byte(raw))
		return ps
	}
	if ps := with(`{"read": "bank.quick_read", "apply": "bank.quick_apply", "undo": "bank.quick_undo"}`); len(ps.Errors()) != 0 {
		t.Fatalf("a good quickEdit: %v", ps)
	}
	if ps := with(`{"read": "bank.quick_read", "apply": "bank.quick_apply"}`); len(ps.Errors()) != 0 {
		t.Fatalf("undo may be left out: %v", ps)
	}
	for _, c := range []struct{ q, path string }{
		{`{"apply": "bank.quick_apply"}`, "quickEdit.read"},
		{`{"read": "bank.quick_read", "apply": "quick_apply"}`, "quickEdit.apply"},
		{`{"read": "bank.quick_read", "apply": "bank.quick_apply", "undo": "x"}`, "quickEdit.undo"},
	} {
		if problemAt(with(c.q), c.path) == nil {
			t.Errorf("%s: want a problem at %s", c.q, c.path)
		}
	}
	if problemAt(with(`{"read": "bank.quick_read", "apply": "bank.quick_apply", "extra": 1}`), "quickEdit") == nil &&
		len(with(`{"read": "bank.quick_read", "apply": "bank.quick_apply", "extra": 1}`).Errors()) == 0 {
		t.Error("an unknown field of quickEdit is refused")
	}
}
