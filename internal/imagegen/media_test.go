package imagegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeHiggsfield stands for Higgsfield: each call writes a picture, or fails when asked to.
type fakeHiggsfield struct {
	off    bool
	failOn int
	dir    string

	mu    sync.Mutex
	calls []MediaDraw
}

func (f *fakeHiggsfield) Available() bool { return !f.off }

func (f *fakeHiggsfield) Draw(_ context.Context, d MediaDraw) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, d)
	n := len(f.calls)
	f.mu.Unlock()
	if n == f.failOn {
		return "", errors.New("Higgsfield refused: too few credits")
	}
	return f.write(n)
}

func (f *fakeHiggsfield) write(n int) (string, error) {
	p := filepath.Join(f.dir, fmt.Sprintf("out-%d.png", n))
	return p, writePNGErr(p, 8, 8, color.RGBA{0, 200, 0, 255})
}

func TestHiggsfieldDrawsEachPictureThroughTheMediaProvider(t *testing.T) {
	media := &fakeHiggsfield{dir: t.TempDir()}
	s := &Service{Dir: t.TempDir(), Media: media, Binary: "/nonexistent/codex"}
	snap, err := s.Start(Request{Provider: Higgsfield, Model: "nano_banana_pro", Prompt: "a fox", Count: 3, Shape: Landscape})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Provider != Higgsfield || snap.Model != "nano_banana_pro" {
		t.Fatalf("snapshot does not name the provider and model: %+v", snap)
	}
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Done || len(fin.Images) != 3 || fin.Error != "" {
		t.Fatalf("job: %+v", fin)
	}
	for _, im := range fin.Images {
		if !strings.HasPrefix(im.Path, filepath.Join(s.Dir, snap.ID)) || filepath.Ext(im.Path) != ".png" {
			t.Fatalf("picture is not kept with the job: %+v", im)
		}
	}
	for _, c := range media.calls {
		if c.Model != "nano_banana_pro" || c.Prompt != "a fox" || c.Ratio != "16:9" || c.Reference != "" {
			t.Fatalf("provider was asked %+v", c)
		}
	}
}

func TestHiggsfieldKeepsWhatWasDrawnWhenOnePictureFails(t *testing.T) {
	media := &fakeHiggsfield{dir: t.TempDir(), failOn: 2}
	s := &Service{Dir: t.TempDir(), Media: media}
	snap, err := s.Start(Request{Provider: Higgsfield, Prompt: "a fox", Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Done || len(fin.Images) != 1 || !strings.Contains(fin.Error, "too few credits") {
		t.Fatalf("job: %+v", fin)
	}
}

func TestHiggsfieldEditSendsThePictureAsAReference(t *testing.T) {
	media := &fakeHiggsfield{dir: t.TempDir()}
	src := filepath.Join(t.TempDir(), "src.png")
	writePNG(t, src, 40, 30, color.RGBA{0, 0, 255, 255})
	s := &Service{Dir: t.TempDir(), Media: media}
	snap, err := s.Start(Request{Provider: Higgsfield, Source: src, Prompt: "make it red", SourceOK: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Done || len(media.calls) != 1 {
		t.Fatalf("job: %+v, calls %+v", fin, media.calls)
	}
	c := media.calls[0]
	if c.Reference == "" || c.Ratio != "4:3" || !strings.Contains(c.Prompt, "make it red") || strings.Contains(c.Prompt, "image tool") {
		t.Fatalf("edit was asked as %+v", c)
	}
}

func TestHiggsfieldRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		s *Service
		r Request
	}{
		"not connected":  {&Service{Dir: t.TempDir(), Media: &fakeHiggsfield{off: true}}, Request{Provider: Higgsfield, Prompt: "x"}},
		"no media":       {&Service{Dir: t.TempDir()}, Request{Provider: Higgsfield, Prompt: "x"}},
		"transparent":    {&Service{Dir: t.TempDir(), Media: &fakeHiggsfield{}}, Request{Provider: Higgsfield, Prompt: "x", Transparent: true}},
		"model of codex": {&Service{Dir: t.TempDir(), Media: &fakeHiggsfield{}}, Request{Provider: Codex, Model: "nano_banana_pro", Prompt: "x"}},
		"bad model":      {&Service{Dir: t.TempDir(), Media: &fakeHiggsfield{}}, Request{Provider: Higgsfield, Model: "../x", Prompt: "x"}},
	} {
		if _, err := tc.s.Start(tc.r); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNearestRatio(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want string
	}{{100, 100, "1:1"}, {1920, 1080, "16:9"}, {1000, 1500, "2:3"}, {1024, 768, "4:3"}, {0, 5, ""}} {
		if got := nearestRatio(tc.w, tc.h); got != tc.want {
			t.Errorf("%dx%d: %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}

func writePNGErr(path string, w, h int, c color.RGBA) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}
