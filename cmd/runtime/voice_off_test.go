package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// GitHub #122: a machine without dictation says why, so the composer can explain it.
func TestVoiceOffReason(t *testing.T) {
	dir := t.TempDir()
	missing := voice.New(voice.Config{FFmpeg: filepath.Join(dir, "no-ffmpeg"), Socket: filepath.Join(dir, "s.sock")})
	if got := voiceOffReason(missing); got != wire.VoiceOffFFmpeg {
		t.Fatalf("no ffmpeg: got %q", got)
	}
	bin := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	noEngine := voice.New(voice.Config{FFmpeg: bin, Socket: filepath.Join(dir, "s.sock")})
	if got := voiceOffReason(noEngine); got != wire.VoiceOffEngine {
		t.Fatalf("no engine: got %q", got)
	}
}
