package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Remote is a recognition server of the person's own that speaks OpenAI's speech-to-text API
// (POST <base>/audio/transcriptions, multipart: file, model, language): OpenAI itself, a self-hosted
// faster-whisper-server, speaches, LocalAI, Groq and the like. With one set the engine downloads and runs nothing:
// every recording is sent there whole, once the person stops (no partial texts, so a paid service is asked once per
// recording). The key stays in the runtime: it goes only into the request's Authorization header.
type Remote struct {
	URL   string `json:"url"`
	Model string `json:"model"`
	Key   string `json:"key,omitempty"`
}

var (
	msgRemoteURL    = locale.T("The server address must start with http:// or https://", "Адрес сервера должен начинаться с http:// или https://")
	msgRemoteModel  = locale.T("Name the model the server recognizes with (for example whisper-1).", "Укажите модель, которой распознаёт сервер (например whisper-1).")
	msgRemoteKey    = locale.T("The key has characters a key cannot have.", "В ключе есть символы, которых в ключе быть не может.")
	msgRemoteAnswer = locale.T("The recognition server answered: ", "Сервер распознавания ответил: ")
	msgRemoteDown   = locale.T("The recognition server cannot be reached: ", "Сервер распознавания недоступен: ")
)

// transcriptionsPath is what an address that names only the API's base gets appended.
const transcriptionsPath = "/audio/transcriptions"

// Endpoint is the address a recording is posted to: the URL as given when it already ends in /audio/transcriptions,
// else that path appended (https://api.openai.com/v1 → https://api.openai.com/v1/audio/transcriptions).
func (r Remote) Endpoint() string {
	u := strings.TrimRight(strings.TrimSpace(r.URL), "/")
	if strings.HasSuffix(u, transcriptionsPath) {
		return u
	}
	return u + transcriptionsPath
}

// Check is nil when the server can be asked: an http(s) address with a host and no password in it, a model name
// and a key of printable characters.
func (r Remote) Check() error {
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(r.URL) > 2048 {
		return errors.New(msgRemoteURL.String())
	}
	m := strings.TrimSpace(r.Model)
	if m == "" || len(m) > 200 || strings.ContainsFunc(m, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
		return errors.New(msgRemoteModel.String())
	}
	if len(r.Key) > 4096 || strings.ContainsFunc(r.Key, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
		return errors.New(msgRemoteKey.String())
	}
	return nil
}

// SameOrigin says two server addresses are the same origin: scheme, host name (any case) and port, the scheme's
// default port when none is written. A saved key goes only to the origin it was saved for.
func SameOrigin(a, b string) bool {
	origin := func(raw string) (string, bool) {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			return "", false
		}
		scheme := strings.ToLower(u.Scheme)
		port := u.Port()
		if port == "" {
			switch scheme {
			case "http":
				port = "80"
			case "https":
				port = "443"
			}
		}
		return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port, true
	}
	oa, okA := origin(a)
	ob, okB := origin(b)
	return okA && okB && oa == ob
}

// MaskKey is how a key is shown back: its last four characters, the rest dots; "" for no key.
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 12 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

// SetRemote makes the engine send recordings to r (nil: recognize here again). A running whisper-server is stopped.
func (e *Engine) SetRemote(r *Remote) {
	e.mu.Lock()
	if r != nil {
		c := *r
		r = &c
	}
	e.remote = r
	e.mu.Unlock()
	if r != nil {
		e.srvMu.Lock()
		if e.proc != nil {
			e.kill(e.proc)
			e.proc = nil
		}
		e.srvMu.Unlock()
	}
}

// remoteServer is the server recordings go to, nil when the engine recognizes here.
func (e *Engine) remoteServer() *Remote {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.remote
}

// UsesRemote says the recordings go to a server of the person's own.
func (e *Engine) UsesRemote() bool { return e.remoteServer() != nil }

// inferRemote posts a recording to the person's server and returns its text.
func (e *Engine) inferRemote(ctx context.Context, r *Remote, pcm []byte, lang string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	hdr.Set("Content-Type", "audio/wav")
	part, _ := w.CreatePart(hdr)
	_, _ = part.Write(wav(padPCM(pcm)))
	_ = w.WriteField("model", strings.TrimSpace(r.Model))
	_ = w.WriteField("response_format", "json")
	_ = w.WriteField("temperature", "0")
	if lang == "ru" || lang == "en" {
		_ = w.WriteField("language", lang)
	}
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint(), &body)
	if err != nil {
		return "", errors.New(msgRemoteURL.String())
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if r.Key != "" {
		req.Header.Set("Authorization", "Bearer "+r.Key)
	}
	resp, err := e.cfg.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New(msgRemoteDown.String() + hideKey(err.Error(), r.Key))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out struct {
		Text  string          `json:"text"`
		Error json.RawMessage `json:"error"`
	}
	parsed := json.Unmarshal(raw, &out) == nil
	if resp.StatusCode != http.StatusOK || !parsed {
		return "", errors.New(msgRemoteAnswer.String() + resp.Status + remoteReason(out.Error, raw, r.Key))
	}
	return clean(out.Text), nil
}

// remoteReason is the server's own words about a failure, short and without the key in them.
func remoteReason(field json.RawMessage, raw []byte, key string) string {
	var reason string
	var obj struct {
		Message string `json:"message"`
	}
	switch {
	case json.Unmarshal(field, &obj) == nil && obj.Message != "":
		reason = obj.Message
	case json.Unmarshal(field, &reason) == nil:
	default:
		reason = string(raw)
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if len(reason) > 300 {
		reason = reason[:300] + "…"
	}
	reason = hideKey(reason, key)
	if reason == "" {
		return ""
	}
	return ": " + reason
}

func hideKey(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, MaskKey(key))
}
