// Package cloud holds the streaming recognizers that send the audio to a provider (OpenAI Realtime transcription,
// Yandex SpeechKit v3). They are used only when the person picked that model in Settings. Each is a function over a
// channel of PCM and a set of callbacks, so internal/voice can drive it like the machine's own recognition service.
package cloud

import "context"

// Options is what a recognition is started with.
type Options struct {
	// Lang is "ru", "en" or "auto".
	Lang string
	// Key is the provider's API key (for ChatGPT, the Codex sign-in's token); Folder is Yandex's folder id ("" when the
	// key needs none) or the ChatGPT account.
	Key    string
	Folder string
	// Hints are the person's dictionary words, passed to a provider that takes them as a prompt.
	Hints []string
	// Endpoint overrides the provider's address; for tests only (the runtime never takes it from a person).
	Endpoint string
}

// Events are what a recognition reports. Ready is called once, when the provider accepted the session and audio may
// flow; Partial carries the whole text so far; Final the whole text, once, after the audio ended (the last call);
// Fail ends the recognition with a message in the person's words (the last call, instead of Final).
type Events struct {
	Ready   func()
	Partial func(text string)
	Final   func(text string)
	Fail    func(message string)
}

// Func recognizes the PCM (mono, 16 kHz, 16-bit little-endian) that comes on pcm until it is closed, and returns when
// it called Final or Fail. A cancelled ctx stops it at once without either call.
type Func func(ctx context.Context, opts Options, pcm <-chan []byte, ev Events)
