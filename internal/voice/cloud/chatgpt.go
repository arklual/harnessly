package cloud

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	// chatGPTURL is not a documented API: OpenAI's plan usage covers the Responses API only and says the transcription
	// API is not part of it. It can change or close at any time, so this engine is only ever the person's own pick.
	chatGPTURL = "https://chatgpt.com/backend-api/transcribe"
	// chatGPTClient names this client as what it is; it does not pass for one of OpenAI's own apps.
	chatGPTClient = "harnessly-voice"
	// chatGPTHoldMax caps a recording, which is held in memory until the stop: 30 minutes of 16 kHz PCM16.
	chatGPTHoldMax = 30 * 60 * 16000 * 2
	// chatGPTWait is how long the answer may take.
	chatGPTWait = 60 * time.Second
)

// ChatGPT sends the whole recording, once it ends, to the transcription behind the ChatGPT plan, on the Codex CLI's
// sign-in (opts.Key is its token, opts.Folder its account). It has no stream, so no partial text comes while the person
// speaks, and it takes no prompt, so opts.Hints go unused.
func ChatGPT(ctx context.Context, opts Options, pcm <-chan []byte, ev Events) {
	if opts.Key == "" {
		ev.Fail("Нет входа в Codex по подписке ChatGPT: войдите командой codex login.")
		return
	}
	ev.Ready()
	var audio []byte
	for b := range pcm {
		if len(audio)+len(b) > chatGPTHoldMax {
			ev.Fail("Запись длиннее 30 минут: распознавание по подписке её не примет.")
			return
		}
		audio = append(audio, b...)
	}
	if ctx.Err() != nil {
		return
	}
	if len(audio) == 0 {
		ev.Final("")
		return
	}
	text, failure := chatGPTTranscribe(ctx, opts, audio)
	switch {
	case ctx.Err() != nil:
	case failure != "":
		ev.Fail(failure)
	default:
		ev.Final(text)
	}
}

// chatGPTTranscribe posts the recording and gives its text, or the failure in the person's words.
func chatGPTTranscribe(ctx context.Context, opts Options, audio []byte) (text, failure string) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	hdr.Set("Content-Type", "audio/wav")
	part, _ := form.CreatePart(hdr)
	_, _ = part.Write(chatGPTWav(audio))
	if opts.Lang == "ru" || opts.Lang == "en" {
		_ = form.WriteField("language", opts.Lang)
	}
	_ = form.Close()

	url := opts.Endpoint
	if url == "" {
		url = chatGPTURL
	}
	ctx, cancel := context.WithTimeout(ctx, chatGPTWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", "Не удалось отправить запись на распознавание."
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+opts.Key)
	req.Header.Set("User-Agent", chatGPTClient)
	if opts.Folder != "" {
		req.Header.Set("ChatGPT-Account-Id", opts.Folder)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", "ChatGPT не ответил вовремя: попробуйте ещё раз."
		}
		return "", "ChatGPT недоступен: проверьте сеть."
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", "ChatGPT недоступен: проверьте сеть."
	}
	if resp.StatusCode != http.StatusOK {
		return "", chatGPTStatusMessage(resp.StatusCode)
	}
	var out struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Text == nil {
		return "", "ChatGPT ответил не так, как ожидалось: возможно, этот способ распознавания изменился."
	}
	return strings.TrimSpace(*out.Text), ""
}

// chatGPTStatusMessage is a refusal in the person's words. The answer's body is not repeated: nothing of it is needed.
func chatGPTStatusMessage(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "ChatGPT не принял вход Codex: войдите заново командой codex login."
	case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return fmt.Sprintf("ChatGPT отказал в распознавании (%d): возможно, этот способ закрыт. Выберите другую модель в «Голосовом вводе».", status)
	case http.StatusTooManyRequests:
		return "Лимит распознавания по подписке исчерпан: попробуйте позже или выберите другую модель."
	}
	return fmt.Sprintf("ChatGPT не распознал запись (%d): попробуйте ещё раз.", status)
}

// chatGPTWav puts PCM (mono, 16 kHz, 16-bit little-endian) in a WAV file.
func chatGPTWav(pcm []byte) []byte {
	const sampleRate, blockAlign = 16000, 2
	var b bytes.Buffer
	b.Grow(44 + len(pcm))
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate*blockAlign))
	_ = binary.Write(&b, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}
