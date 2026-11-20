package imagegen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestImageProviderNormalize(t *testing.T) {
	for _, provider := range []Provider{"", Codex, Antigravity} {
		r, err := Normalize(Request{Provider: provider, Prompt: "a bird"})
		if err != nil || r.Provider == "" {
			t.Fatalf("provider %q: %+v, %v", provider, r, err)
		}
	}
	for _, r := range []Request{
		{Provider: "unknown", Prompt: "a bird"},
		{Provider: Antigravity, Prompt: "a bird", Transparent: true},
		{Provider: Antigravity, Prompt: "remove background", Mode: ModeCut},
	} {
		if _, err := Normalize(r); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("accepted unsupported request %+v: %v", r, err)
		}
	}
}

func TestImageProviderSelection(t *testing.T) {
	s := &Service{Dir: t.TempDir(), DefaultProvider: Antigravity, AntigravityBinary: "/nonexistent/agy", Binary: "/nonexistent/codex"}
	if _, err := s.Start(Request{Prompt: "icon", Transparent: true}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("configured default was not validated: %v", err)
	}
	for _, provider := range []Provider{"", Codex} {
		snap, err := s.Start(Request{Provider: provider, Prompt: "a bird"})
		if err != nil {
			t.Fatal(err)
		}
		fin, _ := s.Wait(context.Background(), snap.ID)
		if provider == "" {
			if fin.Provider != Antigravity || fin.State != Failed || !strings.Contains(fin.Error, "agy") {
				t.Fatalf("did not use configured ACP provider: %+v", fin)
			}
		} else if fin.Provider != Codex || fin.State != Failed || !strings.Contains(fin.Error, "codex") {
			t.Fatalf("explicit provider did not override default: %+v", fin)
		}
	}
	args := jsvalue.NewObject("prompt", "x", "provider", "unknown")
	result := (Tools{S: s}).Call(context.Background(), ToolGenerate, args)
	if bad, _ := result.Get("isError"); bad != true {
		t.Fatalf("MCP ignored provider: %v", result)
	}
}

func TestAntigravityCollector(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	copyTo := t.TempDir()
	s := &Service{Dir: t.TempDir()}
	j := &job{snap: Snapshot{ID: "test-job-12345678", Provider: Antigravity, State: Running, Images: []Image{}}}
	s.jobs = map[string]*job{j.snap.ID: j}
	r := Request{Provider: Antigravity, Count: 1, CopyTo: copyTo, Name: "test", SaveOK: func(string) bool { return true }}
	collect := s.antigravityCollector(j, r, out)
	// Header-looking garbage, directories and a symlink to a real image outside the session must not be imported.
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	var raw bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	im.Set(1, 1, color.RGBA{R: 255, A: 255})
	if err := jpeg.Encode(&raw, im, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.jpg")); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(root, "broken.jpg")
	if err := os.WriteFile(broken, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Second)
	_ = os.Chtimes(broken, old, old)
	if n := collect(root); n != 0 {
		t.Fatalf("imported invalid/out-of-session image: %d", n)
	}
	// Replacing the incomplete file with valid bytes must be retried, not marked seen on failure.
	if err := os.WriteFile(broken, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(broken, old, old)
	if n := collect(root); n != 1 {
		t.Fatalf("did not collect completed JPEG: %d", n)
	}
	if n := collect(root); n != 1 {
		t.Fatalf("duplicate output: %d", n)
	}
	if len(j.snap.Images) != 1 || filepath.Ext(j.snap.Images[0].Path) != ".jpg" || j.snap.Images[0].Copy != filepath.Join(copyTo, "test-1.jpg") {
		t.Fatalf("bad imported/copy metadata: %+v", j.snap)
	}
	got, err := os.ReadFile(j.snap.Images[0].Path)
	if err != nil || !bytes.Equal(got, raw.Bytes()) {
		t.Fatalf("original JPEG changed: %v", err)
	}
	linkedDir := filepath.Join(t.TempDir(), "session")
	if err := os.Symlink(root, linkedDir); err != nil {
		t.Fatal(err)
	}
	if n := s.antigravityCollector(j, r, out)(linkedDir); n != 0 {
		t.Fatal("followed a symlinked session directory")
	}
}

// Explicit opt-in integration smoke test; never consumes a subscription during ordinary tests.
func TestAntigravityLive(t *testing.T) {
	if os.Getenv("HARNESSLY_IMAGEGEN_LIVE") != "1" {
		t.Skip("set HARNESSLY_IMAGEGEN_LIVE=1 to test the official signed-in CLI")
	}
	s := &Service{Dir: t.TempDir(), AntigravityBinary: os.Getenv("HARNESSLY_AGY_BIN")}
	request := Request{Provider: Antigravity, Prompt: "One small blue ceramic teapot on a plain cream studio background. No text, no logos.", Count: 1, Shape: Square}
	if source := os.Getenv("HARNESSLY_IMAGEGEN_SOURCE"); source != "" {
		request.Source, request.SourceOK = source, func(string) bool { return true }
		request.Prompt = "Change only the ceramic object's colour to vivid blue. Keep its shape, scene and lighting unchanged."
	}
	request, err := Normalize(request)
	if err != nil {
		t.Fatal(err)
	}
	j := &job{snap: Snapshot{ID: "live-job-12345678", Provider: Antigravity, State: Running}}
	s.jobs = map[string]*job{j.snap.ID: j}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := s.runAntigravity(ctx, request, t.TempDir(), s.antigravityCollector(j, request, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if len(j.snap.Images) != 1 {
		t.Fatalf("live generation: %+v", j.snap)
	}
	if _, err := os.Stat(j.snap.Images[0].Path); err != nil {
		t.Fatal(err)
	}
	t.Logf("official ACP returned %s", filepath.Ext(j.snap.Images[0].Path))
}
