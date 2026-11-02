package cloud

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	openAIURL = "wss://api.openai.com/v1/realtime?intent=transcription"
	// openAIChunk is ~150 ms of 24 kHz PCM16 per append message.
	openAIChunk = 3600 * 2
	// openAIHoldMax caps the audio kept before the session is ready: 30 s of 24 kHz PCM16.
	openAIHoldMax = 30 * 24000 * 2

	openAIReadyWait = 15 * time.Second
	openAIDrainWait = 10 * time.Second
)

// OpenAI streams the PCM to OpenAI's Realtime transcription API and reports the text through ev.
func OpenAI(ctx context.Context, opts Options, pcm <-chan []byte, ev Events) {
	if opts.Key == "" {
		ev.Fail("Не указан ключ OpenAI: добавьте его в настройках.")
		return
	}
	url := opts.Endpoint
	if url == "" {
		url = openAIURL
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	ws, resp, err := websocket.Dial(dctx, url, &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + opts.Key}},
		CompressionMode: websocket.CompressionDisabled,
	})
	dcancel()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		ev.Fail(openAIHandshakeMessage(status))
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 20)

	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	in := make(chan openAIEvent, 64)
	go func() {
		for {
			_, data, err := ws.Read(rctx)
			var e openAIEvent
			if err != nil {
				e = openAIEvent{readErr: err}
			} else if json.Unmarshal(data, &e) != nil {
				continue
			}
			select {
			case in <- e:
			case <-rctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	send := func(v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		wctx, wcancel := context.WithTimeout(rctx, 10*time.Second)
		defer wcancel()
		return ws.Write(wctx, websocket.MessageText, b) == nil
	}
	lost := func() {
		if ctx.Err() == nil {
			ev.Fail("Соединение с OpenAI оборвалось.")
		}
	}

	if !send(openAISession(opts)) {
		lost()
		return
	}

	var (
		ready, stopping, draining, commitPending bool
		audioSent                                bool
		rs                                       resampler
		held, pending                            []byte
		items                                    = map[string]*openAIItem{}
		order                                    []string
		lastPartial                              string
		pcmCh                                    = pcm
		readyC                                   = time.After(openAIReadyWait)
		drainC                                   <-chan time.Time
	)

	sendChunks := func(all bool) bool {
		for len(pending) >= openAIChunk || (all && len(pending) > 0) {
			n := min(openAIChunk, len(pending))
			msg := map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pending[:n])}
			if !send(msg) {
				return false
			}
			audioSent = true
			pending = pending[n:]
		}
		return true
	}
	text := func() string {
		parts := make([]string, 0, len(order))
		for _, id := range order {
			if t := strings.TrimSpace(items[id].text); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	partial := func() {
		if t := text(); t != lastPartial {
			lastPartial = t
			ev.Partial(t)
		}
	}
	item := func(id string) *openAIItem {
		it := items[id]
		if it == nil {
			it = &openAIItem{}
			items[id] = it
			order = append(order, id)
		}
		return it
	}
	allDone := func() bool {
		for _, it := range items {
			if !it.done {
				return false
			}
		}
		return true
	}
	finish := func() {
		ev.Final(text())
		ws.Close(websocket.StatusNormalClosure, "")
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-readyC:
			ev.Fail("OpenAI не ответил на подключение. Попробуйте ещё раз.")
			return
		case <-drainC:
			finish()
			return
		case b, ok := <-pcmCh:
			if !ok {
				pcmCh, stopping = nil, true
				break
			}
			out := rs.write(b)
			if !ready {
				held = append(held, out...)
				if len(held) > openAIHoldMax {
					held = held[len(held)-openAIHoldMax:]
				}
				break
			}
			pending = append(pending, out...)
			if !sendChunks(false) {
				lost()
				return
			}
		case e := <-in:
			if e.readErr != nil {
				lost()
				return
			}
			switch e.Type {
			case "session.created", "session.updated":
				if !ready {
					ready, readyC = true, nil
					ev.Ready()
					pending, held = held, nil
					if !sendChunks(false) {
						lost()
						return
					}
				}
			case "error":
				if e.Error.Code == "input_audio_buffer_commit_empty" {
					commitPending = false
					break
				}
				ev.Fail(openAIErrorMessage(e.Error.Code))
				return
			case "input_audio_buffer.committed":
				if _, known := items[e.ItemID]; !known {
					item(e.ItemID)
					// Place after the previous item when the server says so.
					if e.PreviousItemID != "" {
						for i, id := range order[:len(order)-1] {
							if id == e.PreviousItemID {
								copy(order[i+2:], order[i+1:len(order)-1])
								order[i+1] = e.ItemID
								break
							}
						}
					}
				}
				commitPending = false
			case "conversation.item.input_audio_transcription.delta":
				if it := item(e.ItemID); !it.done {
					it.text += e.Delta
				}
				partial()
			case "conversation.item.input_audio_transcription.completed":
				it := item(e.ItemID)
				it.text, it.done = e.Transcript, true
				partial()
			case "conversation.item.input_audio_transcription.failed":
				it := item(e.ItemID)
				it.text, it.done = "", true
				partial()
			}
		}

		if stopping && ready && !draining {
			draining = true
			drainC = time.After(openAIDrainWait)
			if !sendChunks(true) {
				lost()
				return
			}
			if audioSent {
				commitPending = true
				if !send(map[string]string{"type": "input_audio_buffer.commit"}) {
					lost()
					return
				}
			}
		}
		if draining && !commitPending && allDone() {
			finish()
			return
		}
	}
}

type openAIItem struct {
	text string
	done bool
}

type openAIEvent struct {
	Type           string `json:"type"`
	ItemID         string `json:"item_id"`
	PreviousItemID string `json:"previous_item_id"`
	Delta          string `json:"delta"`
	Transcript     string `json:"transcript"`
	Error          struct {
		Code string `json:"code"`
	} `json:"error"`
	readErr error
}

// openAISession builds the session.update message for a transcription session.
func openAISession(opts Options) map[string]any {
	tr := map[string]any{"model": "gpt-4o-transcribe"}
	if opts.Lang == "ru" || opts.Lang == "en" {
		tr["language"] = opts.Lang
	}
	if len(opts.Hints) > 0 {
		words := strings.Join(opts.Hints, ", ")
		if opts.Lang == "en" {
			tr["prompt"] = "Speech about software. Write these words exactly as spelled: " + words + "."
		} else {
			tr["prompt"] = "Речь о программировании. Эти слова пишутся так: " + words + "."
		}
	}
	return map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{"input": map[string]any{
				"format":        map[string]any{"type": "audio/pcm", "rate": 24000},
				"transcription": tr,
				"turn_detection": map[string]any{
					"type": "server_vad", "threshold": 0.5, "prefix_padding_ms": 300, "silence_duration_ms": 600,
				},
				"noise_reduction": map[string]any{"type": "near_field"},
			}},
		},
	}
}

func openAIHandshakeMessage(status int) string {
	switch status {
	case 0:
		return "Не удалось подключиться к OpenAI. Проверьте сеть."
	case http.StatusUnauthorized:
		return "OpenAI не принял ключ. Проверьте его в настройках."
	case http.StatusTooManyRequests:
		return "У OpenAI исчерпан лимит или закончились средства на счёте."
	}
	return fmt.Sprintf("OpenAI отказал в подключении (код %d).", status)
}

// openAIErrorMessage maps a server error code to a message; the server's own text is never shown (it may quote the key).
func openAIErrorMessage(code string) string {
	switch code {
	case "invalid_api_key":
		return "OpenAI не принял ключ. Проверьте его в настройках."
	case "rate_limit_exceeded", "insufficient_quota":
		return "У OpenAI исчерпан лимит или закончились средства на счёте."
	}
	return "OpenAI вернул ошибку распознавания."
}

// resampler converts PCM16 mono 16 kHz to 24 kHz by linear interpolation. Output m sits at input position 2m/3, so
// positions are exact integers arithmetic; state carries across chunks, including a sample split between chunks.
type resampler struct {
	carry    byte
	hasCarry bool
	prev     int16
	n        int64 // input samples consumed
	m        int64 // next output index
}

// write consumes a chunk (little-endian PCM16) and returns the 24 kHz samples that are now computable.
func (r *resampler) write(b []byte) []byte {
	if r.hasCarry && len(b) > 0 {
		b = append([]byte{r.carry}, b...)
		r.hasCarry = false
	}
	if len(b)%2 == 1 {
		r.carry, r.hasCarry = b[len(b)-1], true
		b = b[:len(b)-1]
	}
	out := make([]byte, 0, len(b)*3/2+6)
	for i := 0; i+1 < len(b); i += 2 {
		s := int16(binary.LittleEndian.Uint16(b[i:]))
		if r.n > 0 {
			// Output m with floor(2m/3) == n-1 lies between prev and s.
			for 2*r.m/3 == r.n-1 {
				frac := float64(2*r.m%3) / 3
				v := float64(r.prev) + (float64(s)-float64(r.prev))*frac
				out = binary.LittleEndian.AppendUint16(out, uint16(int16(v+0.5*sign(v))))
				r.m++
			}
		}
		r.prev = s
		r.n++
	}
	return out
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
