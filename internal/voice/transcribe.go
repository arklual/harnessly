package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

// segmentMax is the longest piece of a recording recognized in one pass. The recognition service reads a pass in time
// that grows faster than the audio (HRN-674: a 2:39 recording given whole did not come back in three minutes, and the
// service's one inference thread was held for everyone on the machine meanwhile), the live dictation never gives it
// more than 12 s at once; a whole recording is cut the same way.
const segmentMax = 15 * time.Second

// Transcribe recognizes a whole recording (whatever container ffmpeg reads: webm/opus, mp4/aac, wav) and returns its
// text, empty for silence. The recording is decoded once, cut into pieces of at most segmentMax at its quietest moments,
// and every piece is recognized as a dictation of its own, with the engine the person picked ("" or "local" is the
// machine's service), as the live dictation does; lang is "ru", "en" or "auto" ("" is "ru"). Nothing is written to disk.
func (s *Server) Transcribe(ctx context.Context, audio io.Reader, lang, engine string) (string, error) {
	bin := s.ffmpeg()
	if bin == "" {
		return "", &Error{Unavailable: true, Message: msgNoFfmpeg.String()}
	}
	// A body that broke off (or is over the limit) is the request's error, as it came.
	raw, err := io.ReadAll(audio)
	if err != nil {
		return "", err
	}
	pcm, err := decodeAll(ctx, bin, raw)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &Error{Message: msgCannotDecode.String() + err.Error()}
	}
	var said []string
	for _, piece := range splitPCM(pcm, int(segmentMax/time.Second)*sampleRate) {
		text, err := s.transcribeOne(ctx, bytes.NewReader(wavOf(piece)), lang, engine)
		if err != nil {
			return "", err
		}
		if text != "" {
			said = append(said, text)
		}
	}
	return strings.Join(said, " "), nil
}

// decodeAll turns a recording into mono 16 kHz PCM16LE with ffmpeg.
func decodeAll(ctx context.Context, bin string, raw []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1", "pipe:1")
	cmd.Stdin = bytes.NewReader(raw)
	var out bytes.Buffer
	var stderr tail
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		return nil, fmt.Errorf("ffmpeg exited (%v): %s", err, lines[len(lines)-1])
	}
	return out.Bytes(), nil
}

// splitPCM cuts PCM16LE into pieces of at most max samples, each cut at the quietest 30 ms of the second half of its
// window, so a word is not cut through where a pause is near.
func splitPCM(pcm []byte, max int) [][]byte {
	const frame = 480
	var pieces [][]byte
	for len(pcm)/2 > max {
		best, bestAt := -1.0, max
		for at := max / 2; at+frame <= max; at += frame {
			var sum float64
			for i := at; i < at+frame; i++ {
				v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
				sum += v * v
			}
			if best < 0 || sum < best {
				best, bestAt = sum, at
			}
		}
		pieces = append(pieces, pcm[:2*bestAt])
		pcm = pcm[2*bestAt:]
	}
	if len(pcm) > 0 {
		pieces = append(pieces, pcm)
	}
	return pieces
}

// wavOf wraps PCM16LE mono 16 kHz in a WAV header, the container the decoder reads it from.
func wavOf(pcm []byte) []byte {
	b := make([]byte, 44, 44+len(pcm))
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], sampleRate)
	binary.LittleEndian.PutUint32(b[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	return append(b, pcm...)
}

// transcribeChunk is how much of a recording is handed to the decoder at once.
const transcribeChunk = 32 << 10

// transcribeOne recognizes one piece of a recording (whatever container ffmpeg reads) in one pass: the same decoder and
// the same recognition service as the dictation socket, with the piece as one start, the audio and a stop.
func (s *Server) transcribeOne(ctx context.Context, audio io.Reader, lang, engine string) (string, error) {
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

	start, _ := json.Marshal(map[string]string{"type": "start", "lang": lang, "engine": engine})
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
