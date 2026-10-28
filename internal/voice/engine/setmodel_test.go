package engine

import "testing"

func TestSetModelSendsAReadyEngineBackToIdle(t *testing.T) {
	e := New(Config{Dir: t.TempDir(), Model: "base"})
	e.st = Status{State: "ready"}
	if e.SetModel("base") {
		t.Fatal("the same model changed something")
	}
	if !e.SetModel("tiny") {
		t.Fatal("a new model changed nothing")
	}
	if st := e.Status(); st.State != "idle" {
		t.Fatalf("state %q", st.State)
	}
	if m := e.pick(); m.File != Models["tiny"].File {
		t.Fatalf("pick %s", m.File)
	}
}
