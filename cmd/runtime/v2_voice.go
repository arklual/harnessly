package main

import (
	"context"
	"errors"
	"io"

	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Voice is wire.Server.Voice on the machine's dictation: the decoder and the recognition service of the Legacy
// page's microphone, for a whole recording.
func v2Voice(s *voice.Server) func(context.Context, io.Reader, string) (string, error) {
	return func(ctx context.Context, audio io.Reader, lang string) (string, error) {
		text, err := s.Transcribe(ctx, audio, lang)
		var verr *voice.Error
		if errors.As(err, &verr) && verr.Unavailable {
			return "", wire.ErrVoiceUnavailable
		}
		return text, err
	}
}
