package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
		text, err := s.Transcribe(ctx, bytes.NewReader(audio), lang, "")
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
	if text, err := s.Transcribe(ctx, bytes.NewReader(audio), "ru", ""); err != nil || text != "" {
		t.Errorf("silence: %q, %v", text, err)
	}

	svc.mu.Lock()
	svc.mode = "error-before-ready"
	svc.mu.Unlock()
	_, err = s.Transcribe(ctx, bytes.NewReader(audio), "ru", "")
	var verr *Error
	if !errors.As(err, &verr) || verr.Message == "" {
		t.Errorf("a service that reports an error: %v", err)
	}

	svc.mu.Lock()
	svc.mode = ""
	svc.mu.Unlock()
	// Bytes that are no audio decode to nothing: the dictation hears silence, as it does on the page.
	if text, err := s.Transcribe(ctx, bytes.NewReader([]byte("this is not audio at all")), "ru", ""); err == nil && strings.HasPrefix(text, "pcm") && !strings.HasPrefix(text, "pcm 0 ") {
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
	_, err = none.Transcribe(ctx, bytes.NewReader([]byte("x")), "ru", "")
	var verr *Error
	if !errors.As(err, &verr) || !verr.Unavailable {
		t.Errorf("without ffmpeg: %v", err)
	}
}

// HRN-674: a long recording given to the service whole took minutes and held its one inference thread; it is
// recognized in pieces of at most segmentMax, every piece a dictation of its own, the texts joined in order.
func TestTranscribeALongRecordingInPieces(t *testing.T) {
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	long := filepath.Join(dir, "long.wav")
	gen := exec.Command(real, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
		"sine=frequency=220:duration=40", "-ac", "1", "-ar", "48000", long)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("this ffmpeg cannot make a tone: %v %s", err, out)
	}
	audio, err := os.ReadFile(long)
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeService{path: filepath.Join(dir, "asr.sock")}
	svc.listen(t)
	defer func() { _ = svc.ln.Close() }()
	s := New(Config{Socket: svc.path, FFmpeg: real})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	text, err := s.Transcribe(ctx, bytes.NewReader(audio), "ru", "")
	if err != nil {
		t.Fatal(err)
	}
	// The fake service answers every pass with "pcm <bytes> <lang> <hash>": one per piece.
	finals := regexp.MustCompile(`pcm (\d+) ru [0-9a-f]{64}`).FindAllStringSubmatch(text, -1)
	if len(finals) < 3 {
		t.Fatalf("40 s in %d pieces: %q", len(finals), text)
	}
	total := 0
	for _, f := range finals {
		n, _ := strconv.Atoi(f[1])
		if n > int(segmentMax/time.Second)*sampleRate*2 {
			t.Errorf("a piece of %d bytes is longer than %s", n, segmentMax)
		}
		total += n
	}
	if want := 40 * sampleRate * 2; total < want-sampleRate || total > want+sampleRate {
		t.Errorf("the pieces hold %d bytes of 16 kHz audio, the recording %d", total, want)
	}
}

func TestSplitPCMCutsAtTheQuietestMoment(t *testing.T) {
	const n = 20 * sampleRate
	pcm := make([]byte, 2*n)
	quiet := 13 * sampleRate
	for i := 0; i < n; i++ {
		v := int16(8000)
		if i >= quiet && i < quiet+960 {
			v = 0
		}
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(v))
	}
	pieces := splitPCM(pcm, 15*sampleRate)
	if len(pieces) != 2 {
		t.Fatalf("%d pieces", len(pieces))
	}
	if at := len(pieces[0]) / 2; at < quiet || at > quiet+960 {
		t.Errorf("cut at sample %d, the pause is at %d", at, quiet)
	}
	if len(pieces[0])+len(pieces[1]) != len(pcm) {
		t.Error("the pieces lost audio")
	}
}
