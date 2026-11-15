package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// protocol is how one route's control socket speaks: where it is, how it signs in, what its frames mean and how a
// result goes back.
type protocol interface {
	liveURL(id string) string
	headers() http.Header
	// parse is what the page sees of a frame, and the frames to answer it with at once.
	parse(raw []byte) ([]Event, [][]byte)
	say(text, task string, speak bool) [][]byte
	// typed is what the person typed instead of saying: the voice takes it as their words.
	typed(text string) [][]byte
}

// subscriptionWire is ChatGPT's voice route of Codex: the call's id goes into the socket's path, a task is a
// delegation, and its result goes back on the speakable channel in pieces of 500 bytes.
type subscriptionWire struct {
	tok     Token
	session string
	live    string
}

func (w *subscriptionWire) liveURL(id string) string {
	return strings.TrimRight(w.live, "/") + "/" + id
}

func (w *subscriptionWire) headers() http.Header {
	h := http.Header{}
	setHeaders(h, w.tok, w.session)
	return h
}

func (w *subscriptionWire) parse(raw []byte) ([]Event, [][]byte) {
	if e, ok := parseSubscription(raw); ok {
		return []Event{e}, nil
	}
	return nil, nil
}

func (w *subscriptionWire) say(text, task string, speak bool) [][]byte {
	channel := "speakable"
	if !speak {
		channel = "commentary"
	}
	var frames [][]byte
	for _, part := range chunks(text, chunkBytes) {
		msg := map[string]any{"channel": channel, "content": []map[string]string{{"type": "input_text", "text": part}}}
		if task != "" {
			msg["type"] = "delegation.context.append"
			msg["delegation_item_id"] = task
		} else {
			msg["type"] = "session.context.append"
		}
		raw, _ := json.Marshal(msg)
		frames = append(frames, raw)
	}
	return frames
}

func (w *subscriptionWire) typed(text string) [][]byte {
	// "Answer it out loud" made the voice answer requests itself instead of handing them over.
	return w.say("The person typed this instead of saying it; treat it as their words: "+text, "", true)
}

// handOver is the key route's one tool: what the voice gives the chat's agent to do.
const handOver = "hand_over"

// keySession is the public Realtime API's session of a call: transcripts of both sides, and in the agent mode the tool
// the voice hands tasks over with.
func keySession(model, voice, mode, instructions string) map[string]any {
	sess := map[string]any{
		"type":         "realtime",
		"model":        model,
		"instructions": instructions,
		"audio": map[string]any{
			"input": map[string]any{
				"transcription":  map[string]any{"model": "gpt-4o-mini-transcribe"},
				"turn_detection": map[string]any{"type": "semantic_vad"},
			},
			"output": map[string]any{"voice": voice},
		},
	}
	// Both modes hand over: a chat with an agent its work and questions, a ChatGPT chat what it writes on screen.
	desc := "Hand a task or a question over to the agent of this chat: it thinks, sees the screen, the board, the files and the machine, and acts."
	if mode == ModeChat {
		desc = "Hand over what the person wants written or made (a plan, a text, a list): the chat writes it on screen and its answer comes back."
	}
	// The digest's reader answers itself: no tool to hand over with.
	if mode == ModeDigest {
		return sess
	}
	sess["instructions"] = instructions + "\n\nTo hand something over, call the hand_over tool."
	sess["tools"] = []map[string]any{{
		"type":        "function",
		"name":        handOver,
		"description": desc,
		"parameters": map[string]any{
			"type":       "object",
			"properties": map[string]any{"task": map[string]any{"type": "string", "description": "One clear, self-contained instruction in the person's language."}},
			"required":   []string{"task"},
		},
	}}
	sess["tool_choice"] = "auto"
	return sess
}

// keyCallRequest is the public Realtime API's call: the offer and the session as a form, with the key.
func keyCallRequest(ctx context.Context, endpoint, key, sdp string, session map[string]any) (*http.Request, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("sdp", sdp)
	sess, _ := json.Marshal(session)
	_ = form.WriteField("session", string(sess))
	_ = form.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

// keyWire is the public Realtime API: the call's id goes into the socket's query, a task is a call of hand_over, which
// is answered at once (the voice says it passed it on) and whose result comes later as a note the voice tells.
type keyWire struct {
	key  string
	live string
}

func (w *keyWire) liveURL(id string) string {
	return strings.TrimRight(w.live, "/") + "?call_id=" + url.QueryEscape(id)
}

func (w *keyWire) headers() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+w.key)
	return h
}

func frame(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func (w *keyWire) parse(raw []byte) ([]Event, [][]byte) {
	var f struct {
		Type       string          `json:"type"`
		Delta      string          `json:"delta"`
		Transcript string          `json:"transcript"`
		Name       string          `json:"name"`
		CallID     string          `json:"call_id"`
		Arguments  string          `json:"arguments"`
		Error      json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil, nil
	}
	switch f.Type {
	case "session.created":
		return []Event{{Type: "ready"}}, nil
	case "conversation.item.input_audio_transcription.delta":
		if f.Delta != "" {
			return []Event{{Type: "user", Text: f.Delta}}, nil
		}
	case "conversation.item.input_audio_transcription.completed":
		return []Event{{Type: "turn", Role: "user", Text: f.Transcript}}, nil
	case "response.output_audio_transcript.delta":
		if f.Delta != "" {
			return []Event{{Type: "assistant", Text: f.Delta}}, nil
		}
	case "response.output_audio_transcript.done":
		return []Event{{Type: "turn", Role: "assistant", Text: f.Transcript}}, nil
	case "response.function_call_arguments.done":
		if f.Name != handOver || f.CallID == "" {
			return nil, nil
		}
		var args struct {
			Task string `json:"task"`
		}
		_ = json.Unmarshal([]byte(f.Arguments), &args)
		ack := [][]byte{
			frame(map[string]any{"type": "conversation.item.create", "item": map[string]any{
				"type": "function_call_output", "call_id": f.CallID,
				"output": `{"status":"handed over; the result will come later"}`,
			}}),
			frame(map[string]any{"type": "response.create"}),
		}
		return []Event{{Type: "task", ID: f.CallID, Text: args.Task}}, ack
	case "error":
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(f.Error, &e)
		return []Event{{Type: "error", Message: short(e.Message)}}, nil
	}
	return nil, nil
}

func (w *keyWire) typed(text string) [][]byte {
	return [][]byte{
		frame(map[string]any{"type": "conversation.item.create", "item": map[string]any{
			"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": text}},
		}}),
		frame(map[string]any{"type": "response.create"}),
	}
}

func (w *keyWire) say(text, task string, speak bool) [][]byte {
	note := text
	if task != "" {
		note = "The result of the task you handed over: " + text
	}
	frames := [][]byte{frame(map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "message", "role": "system", "content": []map[string]string{{"type": "input_text", "text": note}},
	}})}
	if speak {
		frames = append(frames, frame(map[string]any{"type": "response.create"}))
	}
	return frames
}
