package extproviders

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// CheckTimeout bounds a check of a connection.
const CheckTimeout = 15 * time.Second

// Reasons a check did not pass.
const (
	// CheckUnauthorized: the provider refused the key (401, 403).
	CheckUnauthorized = "unauthorized"
	// CheckUnreachable: the address did not answer (no such host, refused, timed out).
	CheckUnreachable = "unreachable"
	// CheckWrongAPI: the address answers, but not as the chosen kind of API does.
	CheckWrongAPI = "wrong-api"
	// CheckRateLimited: the provider says there were too many requests (429).
	CheckRateLimited = "rate-limited"
	// CheckRejected: the provider answered with another error.
	CheckRejected = "rejected"
)

// Check is how a check of a connection went. It holds neither the key nor the provider's body.
type Check struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	// Message is the outcome in words for the person.
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
	Ms      int64  `json:"ms"`
	// Models are the provider's models when it lists them; empty for one that passes the check without a list (the
	// person then types the model's name).
	Models []Model `json:"models"`
}

// Prober asks a provider for its models.
type Prober struct {
	// Client sends the requests; nil is a client with CheckTimeout that follows no redirect (a redirect would carry the
	// key to the host it points at).
	Client *http.Client
}

func (p *Prober) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: CheckTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Headers are the headers a request to a provider of that kind carries.
func Headers(api API, key string) http.Header {
	h := http.Header{"Accept": {"application/json"}}
	if api == APIAnthropic {
		h.Set("anthropic-version", "2023-06-01")
		if key != "" {
			// The compatible vendors read the key from one header or the other.
			h.Set("x-api-key", key)
			h.Set("Authorization", "Bearer "+key)
		}
		return h
	}
	if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	return h
}

// Endpoint is the address of a path of the provider's API: an Anthropic-compatible address is the root the vendors
// document (ANTHROPIC_BASE_URL), with or without a trailing /v1; an OpenAI-compatible one already ends with its
// version.
func Endpoint(base string, api API, path string) string {
	base = strings.TrimRight(base, "/")
	if api == APIAnthropic {
		return strings.TrimSuffix(base, "/v1") + "/v1/" + path
	}
	return base + "/" + path
}

func failure(reason string, status int, ms int64) Check {
	msg := map[string]string{
		CheckUnauthorized: locale.Pick("The provider did not accept the key. Check that it is copied whole and still valid.", "Провайдер не принял ключ. Проверьте, что он скопирован целиком и не отозван."),
		CheckUnreachable:  locale.Pick("The address does not answer. Check it for typos and that the machine can reach it.", "Адрес не отвечает. Проверьте, нет ли опечатки и доступен ли он с этой машины."),
		CheckWrongAPI:     locale.Pick("The address answers, but not as this kind of API does. Try the other kind or check the path.", "Адрес отвечает, но не так, как этот тип API. Попробуйте другой тип или проверьте путь в адресе."),
		CheckRateLimited:  locale.Pick("The provider says there are too many requests. Try again in a minute.", "Провайдер говорит, что запросов слишком много. Попробуйте через минуту."),
		CheckRejected:     locale.Pick("The provider answered with an error. Try again later.", "Провайдер ответил ошибкой. Попробуйте позже."),
	}[reason]
	return Check{Reason: reason, Message: msg, Status: status, Ms: ms, Models: []Model{}}
}

func passed(models []Model, status int, ms int64) Check {
	msg := locale.Pick("Connected.", "Подключено.")
	if len(models) == 0 {
		msg = locale.Pick("Connected. The provider gives no list of models: type the model's name yourself.", "Подключено. Списка моделей провайдер не отдаёт: название модели впишите сами.")
	}
	return Check{OK: true, Message: msg, Status: status, Ms: ms, Models: models}
}

func (p *Prober) do(ctx context.Context, method, url string, h http.Header, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header = h.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res.StatusCode, raw, nil
}

// Check asks the provider at base for its models with key, and says how it went.
func (p *Prober) Check(ctx context.Context, base string, api API, key string) Check {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	start := time.Now()
	ms := func() int64 { return time.Since(start).Milliseconds() }
	h := Headers(api, key)
	status, raw, err := p.do(ctx, http.MethodGet, Endpoint(base, api, "models"), h, nil)
	if err != nil {
		return failure(CheckUnreachable, 0, ms())
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure(CheckUnauthorized, status, ms())
	case status == http.StatusTooManyRequests:
		return failure(CheckRateLimited, status, ms())
	case status >= 200 && status < 300:
		models, ok := parseModels(raw)
		if !ok {
			return failure(CheckWrongAPI, status, ms())
		}
		return passed(models, status, ms())
	case status >= 500:
		return failure(CheckRejected, status, ms())
	}
	// No list of models at this address (404 and the like). An Anthropic-compatible vendor may have none and still
	// take messages: a message to a model that does not exist is answered with the API's own error by one that speaks
	// it, and with a 401 by one that does not take the key.
	if api != APIAnthropic {
		return failure(CheckWrongAPI, status, ms())
	}
	body, _ := json.Marshal(map[string]any{"model": "harnessly-check", "max_tokens": 1, "messages": []map[string]string{{"role": "user", "content": "ping"}}})
	status, raw, err = p.do(ctx, http.MethodPost, Endpoint(base, api, "messages"), h, body)
	if err != nil {
		return failure(CheckUnreachable, 0, ms())
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure(CheckUnauthorized, status, ms())
	case status == http.StatusTooManyRequests:
		return failure(CheckRateLimited, status, ms())
	case status >= 500:
		return failure(CheckRejected, status, ms())
	case speaksAnthropic(raw):
		return passed([]Model{}, status, ms())
	}
	return failure(CheckWrongAPI, status, ms())
}

// parseModels reads {"data":[{"id":…}]} (OpenAI, Anthropic, OpenRouter) or {"models":[{"id"|"name":…}]}; ok is false
// for anything else.
func parseModels(raw []byte) ([]Model, bool) {
	var doc struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
			modelWindow
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			modelWindow
		} `json:"models"`
	}
	if json.Unmarshal(raw, &doc) != nil || (doc.Data == nil && doc.Models == nil) {
		return nil, false
	}
	out := []Model{}
	seen := map[string]bool{}
	add := func(id, label string, window int) {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > MaxModelID || seen[id] || len(out) >= MaxModels {
			return
		}
		seen[id] = true
		if label == id || len(label) > MaxModelID {
			label = ""
		}
		if window < 0 || window > MaxWindow {
			window = 0
		}
		out = append(out, Model{ID: id, Label: label, Window: window})
	}
	for _, m := range doc.Data {
		label := m.DisplayName
		if label == "" {
			label = m.Name
		}
		add(m.ID, label, m.window())
	}
	for _, m := range doc.Models {
		if m.ID == "" {
			add(m.Name, "", m.window())
		} else {
			add(m.ID, m.Name, m.window())
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, true
}

// modelWindow is the context window as the vendors list it: context_length (OpenRouter, DeepSeek), context_window
// (Groq), max_context_length, max_model_len (vLLM), top_provider.context_length (OpenRouter's served one).
type modelWindow struct {
	ContextLength    int `json:"context_length"`
	ContextWindow    int `json:"context_window"`
	MaxContextLength int `json:"max_context_length"`
	MaxModelLen      int `json:"max_model_len"`
	TopProvider      *struct {
		ContextLength int `json:"context_length"`
	} `json:"top_provider"`
}

func (w modelWindow) window() int {
	for _, n := range []int{w.ContextLength, w.ContextWindow, w.MaxContextLength, w.MaxModelLen} {
		if n > 0 {
			return n
		}
	}
	if w.TopProvider != nil {
		return w.TopProvider.ContextLength
	}
	return 0
}

// speaksAnthropic: the body is the Anthropic API's error or message.
func speaksAnthropic(raw []byte) bool {
	var doc struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	return doc.Type == "error" && len(doc.Error) > 0 || doc.Type == "message"
}

// IsTimeout says whether err is a request that ran out of time.
func IsTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}
