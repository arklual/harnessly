package voice

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// Error is a recognition that did not happen. Unavailable: this machine cannot recognize at all (no ffmpeg, no
// recognition service, a model that did not load), as opposed to a recording that failed.
type Error struct {
	Unavailable bool
	Message     string
}

func (e *Error) Error() string { return e.Message }

// Available says whether this machine is set up to recognize: ffmpeg is there and the recognition service has a socket.
// It does not say the service is ready; a model still loading is waited for, up to ReadyTimeout.
func (s *Server) Available() bool {
	if s.ffmpeg() == "" {
		return false
	}
	st, err := os.Stat(s.cfg.Socket)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// transcribeChunk is how much of a recording is handed to the decoder at once.
const transcribeChunk = 32 << 10

// Transcribe recognizes a whole recording (whatever container ffmpeg reads: webm/opus, mp4/aac, wav) and returns its
// text, empty for silence. It runs the same decoder and the same recognition service as the dictation socket, with the
// recording as one start, the audio and a stop; lang is "ru", "en" or "auto" ("" is "ru"). The recording stays in memory
// and is not written to disk.
func (s *Server) Transcribe(ctx context.Context, audio io.Reader, lang string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ss := &session{s: s, events: make(chan event, 256), out: make(chan []byte, 256), ctx: ctx}
	looped := make(chan struct{})
	go func() { defer close(looped); ss.loop() }()
	defer func() {
		// The session ends the way a socket's does: its reader says closed. Whatever it still runs is stopped by the cancel.
		select {
		case ss.events <- event{kind: "closed"}:
		case <-time.After(time.Second):
		}
		cancel()
		select {
		case <-looped:
		case <-time.After(5 * time.Second):
		}
	}()

	start, _ := json.Marshal(map[string]string{"type": "start", "lang": lang})
	ss.deliver(ctx, event{kind: "client", data: start})
	buf := make([]byte, transcribeChunk)
	for {
		n, err := audio.Read(buf)
		if n > 0 {
			ss.deliver(ctx, event{kind: "binary", data: append([]byte(nil), buf[:n]...)})
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", &Error{Message: msgCannotDecode.String() + err.Error()}
		}
	}
	ss.deliver(ctx, event{kind: "client", data: []byte(`{"type":"stop"}`)})

	for {
		select {
		case raw, ok := <-ss.out:
			if !ok {
				return "", &Error{Message: msgRestarted.String()}
			}
			var msg struct {
				Type    string          `json:"type"`
				Text    *string         `json:"text"`
				Message json.RawMessage `json:"message"`
				Code    string          `json:"code"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "final":
				if msg.Text == nil {
					return "", nil
				}
				return strings.TrimSpace(*msg.Text), nil
			case "error":
				text := string(msg.Message)
				var asString string
				if json.Unmarshal(msg.Message, &asString) == nil {
					text = asString
				}
				return "", &Error{Unavailable: msg.Code == "unavailable", Message: text}
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
