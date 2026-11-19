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

func TestTheVideoToolsAreCardsOfTheSameKind(t *testing.T) {
	if !IsGenerate("mcp__harnessly__generate_video") || !IsWait("wait_video") || IsGenerate("cancel_video") {
		t.Fatal("the video tools' names")
	}
	got, ok := ParseReport("job_id: ab12\nDone: 1 video(s). Higgsfield · Kling 3.0 Std · 5 s · ≈ $0.42.\n1. /data/media/ab12/video-1.mp4\nThe person already sees it.")
	if !ok || got.JobID != "ab12" || got.State != Done || len(got.Paths) != 1 || got.Paths[0] != "/data/media/ab12/video-1.mp4" {
		t.Errorf("%+v", got)
	}
	got, _ = ParseReport("job_id: ab12\nStill drawing: the video is being made at Higgsfield.\n")
	if got.State != Drawing {
		t.Errorf("%+v", got)
	}
}
