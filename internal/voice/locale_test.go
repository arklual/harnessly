package voice

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishDictationMessages(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := msgNoFfmpeg.String(); got != "ffmpeg was not found on this machine: server-side transcription is unavailable." {
		t.Errorf("ffmpeg: %q", got)
	}
	if got := msgRestarted.String(); got != "The recognition service restarted, try again" {
		t.Errorf("restarted: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := msgRestarted.String(); got != "Сервис распознавания перезапустился, попробуйте ещё раз" {
		t.Errorf("ru restarted: %q", got)
	}
}
