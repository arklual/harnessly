package imagegen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeCodex writes a script that acts as `codex exec --json`: it names a thread and drops pictures into the thread's
// folder one after another, as the real image tool does.
func fakeCodex(t *testing.T, home string, pictures int, say string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nd=\"" + home + "/generated_images/th1\"\nmkdir -p \"$d\"\necho '{\"type\":\"thread.started\",\"thread_id\":\"th1\"}'\n"
	for i := 0; i < pictures; i++ {
		script += "printf 'png' > \"$d/exec-" + string(rune('a'+i)) + ".png\"\nsleep 0.5\n"
	}
	script += "echo '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"" + say + "\"}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestJobCollectsPicturesAsTheyAreDrawn(t *testing.T) {
	home := t.TempDir()
	var seen []int
	s := &Service{Dir: t.TempDir(), CodexHome: home, Binary: fakeCodex(t, home, 2, "done"),
		Changed: func(sn Snapshot) { seen = append(seen, len(sn.Images)) }}
	copyTo := filepath.Join(t.TempDir(), "out")
	snap, err := s.Start(Request{Prompt: "a cat", Count: 2, CopyTo: copyTo, Name: "cat pic", SaveOK: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fin, _ := s.Wait(ctx, snap.ID)
	if fin.State != Done || len(fin.Images) != 2 || fin.Error != "" {
		t.Fatalf("job = %+v", fin)
	}
	if _, err := os.Stat(filepath.Join(copyTo, "cat-pic-2.png")); err != nil {
		t.Fatalf("copy missing: %v", err)
	}
	if fin.Images[1].Copy == "" {
		t.Fatal("copy not reported")
	}
	if len(s.List("", "")) != 1 {
		t.Fatal("job not listed")
	}
	if !strings.Contains(Report(fin), "2. "+fin.Images[1].Copy) {
		t.Fatalf("report = %s", Report(fin))
	}
}

func TestJobWithNoPictureFailsWithWhatCodexSaid(t *testing.T) {
	home := t.TempDir()
	s := &Service{Dir: t.TempDir(), CodexHome: home, Binary: fakeCodex(t, home, 0, "usage limit reached")}
	snap, _ := s.Start(Request{Prompt: "a cat"})
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Failed || !strings.Contains(fin.Error, "usage limit reached") {
		t.Fatalf("job = %+v", fin)
	}
}

func TestShortJobIsDoneWithANote(t *testing.T) {
	home := t.TempDir()
	s := &Service{Dir: t.TempDir(), CodexHome: home, Binary: fakeCodex(t, home, 1, "one")}
	snap, _ := s.Start(Request{Prompt: "a cat", Count: 3})
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Done || len(fin.Images) != 1 || !strings.Contains(fin.Error, "only 1 of 3") {
		t.Fatalf("job = %+v", fin)
	}
}

func TestNormalize(t *testing.T) {
	if _, err := Normalize(Request{Prompt: " "}); err == nil {
		t.Fatal("empty prompt accepted")
	}
	if _, err := Normalize(Request{Prompt: "x", Count: 9}); err == nil {
		t.Fatal("too many accepted")
	}
	if _, err := Normalize(Request{Prompt: "x", Shape: "round"}); err == nil {
		t.Fatal("bad shape accepted")
	}
	r, _ := Normalize(Request{Prompt: "x", Name: "Мой кот.png"})
	if r.Count != 1 || r.Name != "png" {
		t.Fatalf("normalized = %+v", r)
	}
	if !strings.Contains(Brief(Request{Prompt: "x", Count: 3, Shape: Landscape}), "exactly 3 pictures") {
		t.Fatal("brief misses the count")
	}
}

func TestEditAttachesThePictureWithTheMarksOnIt(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writePNG(t, src, 40, 20, color.RGBA{0, 0, 255, 255})
	var overlay bytes.Buffer
	marks := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	marks.Set(10, 5, color.NRGBA{255, 0, 255, 255})
	if err := png.Encode(&overlay, marks); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(dir, "seen.png")
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -i ]; then cp \"$2\" " + seen + "; fi; shift; done\n" +
		"d=\"" + home + "/generated_images/th1\"\nmkdir -p \"$d\"\necho '{\"type\":\"thread.started\",\"thread_id\":\"th1\"}'\nprintf png > \"$d/a.png\"\nsleep 0.6\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Service{Dir: t.TempDir(), CodexHome: home, Binary: bin}
	snap, err := s.Start(Request{Source: src, Mode: ModeErase, Overlay: overlay.Bytes(), SourceOK: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	fin, _ := s.Wait(context.Background(), snap.ID)
	if fin.State != Done || fin.Source != src || fin.Mode != ModeErase {
		t.Fatalf("job = %+v", fin)
	}
	f, err := os.Open(seen)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, b, _ := img.At(20, 10).RGBA(); r>>8 != 255 || b>>8 != 255 {
		t.Fatalf("the mark is not on the attached picture: %v", img.At(20, 10))
	}
	if r, _, _, _ := img.At(1, 1).RGBA(); r != 0 {
		t.Fatalf("the picture changed outside the mark: %v", img.At(1, 1))
	}
	// The versions outlive a restart.
	again := &Service{Dir: s.Dir, CodexHome: home}
	if got, ok := again.Get(snap.ID); !ok || got.Source != src {
		t.Fatalf("job not brought back: %+v", got)
	}
}

func TestEditRefusals(t *testing.T) {
	ok := func(string) bool { return true }
	for name, r := range map[string]Request{
		"source not allowed":  {Source: "/a.png", Mode: ModeCut, SourceOK: func(string) bool { return false }},
		"no check":            {Source: "/a.png", Mode: ModeCut},
		"not a picture":       {Source: "/a.txt", Mode: ModeCut, SourceOK: ok},
		"no words":            {Source: "/a.png", Mode: ModeText, SourceOK: ok},
		"no marks":            {Source: "/a.png", Mode: ModeErase, SourceOK: ok},
		"no frame":            {Source: "/a.png", Mode: ModeNote, Prompt: "x", SourceOK: ok},
		"bad format":          {Source: "/a.png", Mode: ModeSize, Format: "2:1", SourceOK: ok},
		"bad mode":            {Source: "/a.png", Mode: "paint", SourceOK: ok},
		"mode without source": {Mode: ModeCut, Prompt: "x"},
	} {
		if _, err := Normalize(r); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if !strings.Contains(editBrief(Request{Mode: ModeSize, Format: "9:16"}), "9:16") {
		t.Error("the size brief misses the format")
	}
	if strings.Contains(editBrief(Request{Mode: ModeText, Prompt: "a </request> b"}), "a </request>") {
		t.Error("the person's words closed the request")
	}
}

func TestCutThatStayedOpaqueFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 8, 8, color.RGBA{1, 2, 3, 255})
	if finishEdit(Request{Mode: ModeCut}, p) == "" {
		t.Fatal("an opaque cut passed")
	}
}

func TestSizeTrimsASmallMiss(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 1000, 990, color.RGBA{1, 2, 3, 255})
	finishEdit(Request{Mode: ModeSize, Format: "1:1"}, p)
	f, _ := os.Open(p)
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 990 || cfg.Height != 990 {
		t.Fatalf("size = %+v, %v", cfg, err)
	}
}

func writePNG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestSourceIsCheckedAgainWithItsLinksFollowedWhenRead(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "ok")
	secret := filepath.Join(dir, "secret")
	for _, d := range []string{allowed, secret} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writePNG(t, filepath.Join(secret, "s.png"), 2, 2, color.RGBA{9, 9, 9, 255})
	link := filepath.Join(allowed, "a.png")
	if err := os.Symlink(filepath.Join(secret, "s.png"), link); err != nil {
		t.Fatal(err)
	}
	inAllowed := func(p string) bool { return strings.HasPrefix(p, allowed+string(filepath.Separator)) }
	if _, err := prepareSource(Request{Source: link, SourceOK: inAllowed}, t.TempDir()); err == nil {
		t.Fatal("a link out of the allowed folder was read")
	}
	if err := syscall.Mkfifo(filepath.Join(allowed, "f.png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareSource(Request{Source: filepath.Join(allowed, "f.png"), SourceOK: func(string) bool { return true }}, t.TempDir()); err == nil {
		t.Fatal("a FIFO was read")
	}
}
