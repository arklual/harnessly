package main

import (
	"path/filepath"
	"testing"
)

func TestVoiceSocketChoice(t *testing.T) {
	if isSocket(defaultVoiceSocket) {
		t.Skip("a recognition service is listening at the usual place on this machine")
	}
	dir := t.TempDir()
	t.Cleanup(closeVoiceEngines)

	t.Setenv("RUNTIME_VOICE_SOCKET", "/somewhere/asr.sock")
	if got := voiceSocket(dir); got != "/somewhere/asr.sock" {
		t.Fatalf("a named service is used as it is: %q", got)
	}

	t.Setenv("RUNTIME_VOICE_SOCKET", "")
	t.Setenv("RUNTIME_VOICE_ENGINE", "off")
	if got := voiceSocket(dir); got != defaultVoiceSocket {
		t.Fatalf("the engine is off: %q", got)
	}
	if newVoice(dir).Available() {
		t.Fatal("dictation is announced with the engine off and no service")
	}

	// With nothing named, the runtime brings its own: a socket in the data folder, announced once ffmpeg is there.
	t.Setenv("RUNTIME_VOICE_ENGINE", "")
	t.Setenv("RUNTIME_VOICE_WHISPER_BIN", "/bin/true") // eligible on any machine; nothing is run until a dictation
	t.Setenv("RUNTIME_VOICE_FFMPEG_BIN", "/bin/true")
	want := filepath.Join(dir, "voice", "asr.sock")
	if got := voiceSocket(dir); got != want || !isSocket(got) {
		t.Fatalf("the engine's socket: %q", got)
	}
	if !newVoice(dir).Available() {
		t.Fatal("dictation is not announced with the engine and ffmpeg in place")
	}
	t.Setenv("RUNTIME_VOICE_FFMPEG_BIN", "/nonexistent/ffmpeg")
	if newVoice(dir).Available() {
		t.Fatal("dictation is announced without ffmpeg")
	}
}
