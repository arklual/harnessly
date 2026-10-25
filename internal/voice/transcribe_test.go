package voice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTranscribeAWholeRecording(t *testing.T) {
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	audio, err := os.ReadFile("testdata/tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	svc := &fakeService{path: filepath.Join(dir, "asr.sock")}
	svc.listen(t)
	defer func() { _ = svc.ln.Close() }()
	s := New(Config{Socket: svc.path, FFmpeg: real})
	if !s.Available() {
		t.Fatal("ffmpeg and a socket, and not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, lang := range []string{"ru", "en", "auto"} {
		text, err := s.Transcribe(ctx, bytes.NewReader(audio), lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if !strings.HasPrefix(text, "pcm ") || !strings.Contains(text, " "+lang+" ") {
			t.Errorf("%s: heard %q", lang, text)
		}
	}

	svc.mu.Lock()
	svc.mode = "no-text"
	svc.mu.Unlock()
	if text, err := s.Transcribe(ctx, bytes.NewReader(audio), "ru"); err != nil || text != "" {
		t.Errorf("silence: %q, %v", text, err)
	}

	svc.mu.Lock()
	svc.mode = "error-before-ready"
	svc.mu.Unlock()
	_, err = s.Transcribe(ctx, bytes.NewReader(audio), "ru")
	var verr *Error
	if !errors.As(err, &verr) || verr.Message == "" {
		t.Errorf("a service that reports an error: %v", err)
	}

	svc.mu.Lock()
	svc.mode = ""
	svc.mu.Unlock()
	// Bytes that are no audio decode to nothing: the dictation hears silence, as it does on the page.
	if text, err := s.Transcribe(ctx, bytes.NewReader([]byte("this is not audio at all")), "ru"); err == nil && strings.HasPrefix(text, "pcm") && !strings.HasPrefix(text, "pcm 0 ") {
		t.Errorf("garbage was recognized: %q", text)
	}
}

func TestTranscribeOnAMachineWithoutDictation(t *testing.T) {
	dir := t.TempDir()
	none := New(Config{Socket: filepath.Join(dir, "none.sock"), FFmpeg: "/nonexistent/ffmpeg"})
	if none.Available() {
		t.Error("no ffmpeg, no socket, and available")
	}
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	noSocket := New(Config{Socket: filepath.Join(dir, "none.sock"), FFmpeg: real})
	if noSocket.Available() {
		t.Error("no socket and available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = none.Transcribe(ctx, bytes.NewReader([]byte("x")), "ru")
	var verr *Error
	if !errors.As(err, &verr) || !verr.Unavailable {
		t.Errorf("without ffmpeg: %v", err)
	}
}
