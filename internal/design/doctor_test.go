package design

import (
	"context"
	stdimage "image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stepByID(r DoctorReport, id string) *DoctorStep {
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			return &r.Steps[i]
		}
	}
	return nil
}

// A machine without ffmpeg fails the first step with its code, and the rest say they did not run.
func TestRenderDoctorMissingFFmpeg(t *testing.T) {
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	l := New(Config{Chrome: os.Args[0]})
	defer l.StopWatching()
	r := l.RenderDoctor(context.Background())
	if r.OK {
		t.Fatal("a machine without ffmpeg passed")
	}
	f := r.FailedStep()
	if f == nil || f.Code != DoctorFFmpegMissing || f.Hint == "" {
		t.Fatalf("first failure = %+v", f)
	}
	for _, id := range []string{"frame", "encode", "probe"} {
		if s := stepByID(r, id); s == nil || !s.Skipped {
			t.Errorf("step %s must be skipped: %+v", id, s)
		}
	}
}

func TestRenderDoctorMissingChrome(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	l := New(Config{})
	defer l.StopWatching()
	if l.findChrome() != "" {
		t.Skip("a browser at a fixed place")
	}
	r := l.RenderDoctor(context.Background())
	if f := r.FailedStep(); f == nil || f.Code != DoctorChromeMissing {
		t.Fatalf("first failure = %+v", f)
	}
}

// A browser that starts but draws nothing is caught by the picture, not by "the program exists".
func TestFrameStatsTellsFlatFromDrawn(t *testing.T) {
	flat := newFlat(64, 64)
	if c, b := frameStats(flat); c > 2 || b > 0 {
		t.Errorf("flat frame: %d colours, %d bright", c, b)
	}
}

// With the switch off, readiness is as before: always true, no check is started.
func TestEngineReadyUnchangedWithoutSwitch(t *testing.T) {
	t.Setenv(ReadyProbeEnv, "")
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	l := New(Config{})
	defer l.StopWatching()
	s := l.Engine()
	if !s.Ready || s.Render != nil {
		t.Fatalf("status = %+v", s)
	}
}

// With the switch on, readiness follows the doctor: the first answer is "checking", then the failure is cached.
func TestEngineReadyFollowsDoctor(t *testing.T) {
	t.Setenv(ReadyProbeEnv, "1")
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	l := New(Config{})
	defer l.StopWatching()
	first := l.Engine()
	if first.Ready || first.Render == nil || !first.Render.Checking {
		t.Fatalf("first status = %+v", first.Render)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := l.Engine()
		if s.Render != nil && !s.Render.Checking {
			if s.Ready || s.Render.OK || (s.Render.Code != DoctorFFmpegMissing && s.Render.Code != DoctorChromeMissing) || s.Render.Hint == "" {
				t.Fatalf("status = %+v", s.Render)
			}
			// cached: asking again does not start another check
			l.ready.mu.Lock()
			rep := l.ready.rep
			l.ready.mu.Unlock()
			l.Engine()
			l.ready.mu.Lock()
			same := l.ready.rep == rep
			l.ready.mu.Unlock()
			if !same {
				t.Fatal("the result was not cached")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no result")
}

// The real thing, where a browser and ffmpeg exist.
func TestRenderDoctorInChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a browser")
	}
	l := New(Config{Texts: Texts{Tag: "test-design"}})
	defer l.StopWatching()
	if l.findChrome() == "" || DeckFFmpeg() == "" || DoctorFFprobe() == "" {
		t.Skip("no browser, ffmpeg or ffprobe on this machine")
	}
	r := l.RenderDoctor(context.Background())
	if !r.OK {
		t.Fatalf("doctor failed: %+v", r.FailedStep())
	}
	for _, id := range []string{"tools", "frame", "encode", "probe"} {
		if s := stepByID(r, id); s == nil || !s.OK {
			t.Errorf("step %s: %+v", id, s)
		}
	}
}

func newFlat(w, h int) *stdimage.RGBA { return stdimage.NewRGBA(stdimage.Rect(0, 0, w, h)) }
