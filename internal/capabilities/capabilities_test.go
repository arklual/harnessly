package capabilities

import "testing"

func TestDocumentIsSortedAndTyped(t *testing.T) {
	d := New("runtime").
		Switch("b", true, "", "").
		Switch("a", false, "not set", "X_Y").
		Absent("c", "no gateway").
		Document()
	if d.Server != "runtime" || len(d.Capabilities) != 3 {
		t.Fatalf("document = %+v", d)
	}
	a, b, c := d.Capabilities[0], d.Capabilities[1], d.Capabilities[2]
	if a.ID != "a" || a.Status != NeedsConfig || a.Configure != "X_Y" || a.Reason == "" {
		t.Errorf("a = %+v", a)
	}
	if b.ID != "b" || b.Status != On || b.Reason != "" {
		t.Errorf("b = %+v", b)
	}
	if c.ID != "c" || c.Status != Off || c.Configure != "" {
		t.Errorf("c = %+v", c)
	}
}
