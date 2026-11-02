package imagecard

import "testing"

func TestParseReport(t *testing.T) {
	r, ok := ParseReport("job_id: j1\nDone: 2 picture(s).\n1. /d/a b.png\n2. /d/c.png\nThe person already sees these.")
	if !ok || r.JobID != "j1" || r.State != Done || len(r.Paths) != 2 || r.Paths[0] != "/d/a b.png" {
		t.Fatalf("%+v %v", r, ok)
	}
	if r, _ = ParseReport("job_id: j2\nStill drawing: 0 of 2 ready."); r.State != Drawing {
		t.Fatalf("%+v", r)
	}
	if r, _ = ParseReport("job_id: j3\nFailed: nope"); r.State != Failed || r.Error != "nope" {
		t.Fatalf("%+v", r)
	}
	if _, ok = ParseReport("no such job: x"); ok {
		t.Fatal("not a report")
	}
}
