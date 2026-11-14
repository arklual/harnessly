// Package bgmodel is the model behind the background features: the small jobs harnessly does beside a chat (the
// summary of a notification, «what is going on now», the next-step hints, the background memory). A person picks a
// provider and a model for each (Settings → Background tasks), or leaves it «as now», where the chat's own agent
// decides and nothing here is called.
//
// The client is small on purpose: one chat completion, in the OpenAI-compatible or the Anthropic-compatible form, with
// a timeout and a cap on the answer's tokens. A feature that cannot get its answer falls back to what it did before.
package bgmodel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// Bounds of one call.
const (
	DefaultTimeout   = 12 * time.Second
	MaxTimeout       = 60 * time.Second
	DefaultMaxTokens = 256
	MaxMaxTokens     = 4096
	// MaxPrompt is the most text a call sends (bytes); a longer prompt keeps its end, where a turn's outcome is.
	MaxPrompt = 24 << 10
)

// Reasons a call failed.
const (
	ReasonUnauthorized = "unauthorized" // the provider refused the key
	ReasonUnreachable  = "unreachable"  // the address did not answer
	ReasonTimeout      = "timeout"      // no answer in time
	ReasonRateLimited  = "rate-limited" // 429
	ReasonRejected     = "rejected"     // another error of the provider
	ReasonEmpty        = "empty"        // an answer with no text
	ReasonNoProvider   = "no-provider"  // the provider picked for the feature is gone
)

// Error is a failed call: why, and the provider's status when it answered. It holds neither the key nor the body.
type Error struct {
	Reason string
	Status int
}

func (e *Error) Error() string { return "bgmodel: " + e.Reason }

// ReasonOf is the reason of any error of a call.
func ReasonOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ReasonRejected
}

// Request is one completion.
type Request struct {
	BaseURL string
	API     extproviders.API
	Key     string
	Model   string
	// System is the instruction, Prompt the text it is about.
	System string
	Prompt string
	// MaxTokens caps the answer (DefaultMaxTokens when 0), Timeout the whole call (DefaultTimeout when 0).
	MaxTokens int
	Timeout   time.Duration
	// Quick asks the model to answer without thinking first (an Anthropic-form API; the other form has no common way).
	Quick bool
	// Effort is the reasoning effort the person gave the model (extproviders.ModelEfforts); "" sends none. A quick call
	// does not think whatever it is.
	Effort string
}

// openAIEffort is the reasoning_effort of an OpenAI-form request: the levels above high are high there, except xhigh
// that the newer models take.
func openAIEffort(e string) string {
	switch e {
	case "low", "medium", "high", "xhigh":
		return e
	case "":
		return ""
	}
	return "high"
}

// thinkingBudget is the thinking budget of an Anthropic-form request for an effort; 0 for none.
func thinkingBudget(e string) int {
	switch e {
	case "low":
		return 2048
	case "medium":
		return 8192
	case "high":
		return 16384
	case "xhigh", "max", "ultra":
		return 32000
	}
	return 0
}

// Client makes the calls.
type Client struct {
	// HTTP sends them; nil is a client that follows no redirect (a redirect would carry the key to the host it points
	// at).
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	// Not from the middle of a character.
	for len(s) > 0 && s[0]&0xC0 == 0x80 {
		s = s[1:]
	}
	return s
}

// open builds and sends the request of a call; stream asks the provider for server-sent events. The caller closes the
// body and calls cancel, which holds the call's timeout.
func (c *Client) open(ctx context.Context, r Request, stream bool) (res *http.Response, cancel context.CancelFunc, err error) {
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	maxTokens = min(maxTokens, MaxMaxTokens)
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel = context.WithTimeout(ctx, min(timeout, MaxTimeout))
	prompt := tail(r.Prompt, MaxPrompt)

	var body map[string]any
	path := "chat/completions"
	if r.API == extproviders.APIAnthropic {
		path = "messages"
		body = map[string]any{"model": r.Model, "max_tokens": maxTokens, "messages": []map[string]string{{"role": "user", "content": prompt}}}
		if r.System != "" {
			body["system"] = r.System
		}
		// A quick call wants the answer, not the model's thinking first: a second or two saved on a reply of a line.
		if r.Quick {
			body["thinking"] = map[string]string{"type": "disabled"}
		} else if budget := thinkingBudget(r.Effort); budget > 0 {
			// The answer comes after the thinking, within max_tokens.
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
			body["max_tokens"] = maxTokens + budget
		}
	} else {
		messages := []map[string]string{}
		if r.System != "" {
			messages = append(messages, map[string]string{"role": "system", "content": r.System})
		}
		messages = append(messages, map[string]string{"role": "user", "content": prompt})
		body = map[string]any{"model": r.Model, "max_tokens": maxTokens, "messages": messages}
		if e := openAIEffort(r.Effort); e != "" && !r.Quick {
			body["reasoning_effort"] = e
		}
	}
	if stream {
		body["stream"] = true
	}
	raw, err := json.Marshal(body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, extproviders.Endpoint(r.BaseURL, r.API, path), bytes.NewReader(raw))
	if err != nil {
		cancel()
		return nil, nil, &Error{Reason: ReasonUnreachable}
	}
	req.Header = extproviders.Headers(r.API, r.Key)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	res, err = c.http().Do(req)
	if err != nil {
		defer cancel()
		if extproviders.IsTimeout(err) || ctx.Err() != nil {
			return nil, nil, &Error{Reason: ReasonTimeout}
		}
		return nil, nil, &Error{Reason: ReasonUnreachable}
	}
	switch status := res.StatusCode; {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		err = &Error{Reason: ReasonUnauthorized, Status: status}
	case status == http.StatusTooManyRequests:
		err = &Error{Reason: ReasonRateLimited, Status: status}
	case status < 200 || status > 299:
		err = &Error{Reason: ReasonRejected, Status: status}
	}
	if err != nil {
		res.Body.Close()
		cancel()
		return nil, nil, err
	}
	return res, cancel, nil
}

// maxAnswer bounds what is read of one answer.
const maxAnswer = 1 << 20

// Complete asks the model once and gives the text of its answer.
func (c *Client) Complete(ctx context.Context, r Request) (string, error) {
	res, cancel, err := c.open(ctx, r, false)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer res.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return "", &Error{Reason: ReasonTimeout, Status: res.StatusCode}
	}
	text := strings.TrimSpace(textOf(r.API, answer))
	if text == "" {
		return "", &Error{Reason: ReasonEmpty, Status: res.StatusCode}
	}
	return text, nil
}

// Stream asks the model once and hands over the text as it comes: onDelta gets every non-empty piece, and the whole
// text, trimmed, is the answer. A provider that ignores «stream» and answers in one body gives one piece.
func (c *Client) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	res, cancel, err := c.open(ctx, r, true)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer res.Body.Close()
	body := io.LimitReader(res.Body, maxAnswer)
	var all strings.Builder
	if mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type")); mt != "text/event-stream" {
		answer, err := io.ReadAll(body)
		if err != nil {
			return "", &Error{Reason: ReasonTimeout, Status: res.StatusCode}
		}
		all.WriteString(textOf(r.API, answer))
		if all.Len() > 0 && onDelta != nil {
			onDelta(all.String())
		}
	} else {
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64<<10), maxAnswer)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				break
			}
			if piece := deltaOf(r.API, []byte(data)); piece != "" {
				all.WriteString(piece)
				if onDelta != nil {
					onDelta(piece)
				}
			}
		}
		if err := sc.Err(); err != nil {
			if all.Len() == 0 || ctx.Err() != nil {
				return "", &Error{Reason: ReasonTimeout, Status: res.StatusCode}
			}
		}
	}
	text := strings.TrimSpace(all.String())
	if text == "" {
		return "", &Error{Reason: ReasonEmpty, Status: res.StatusCode}
	}
	return text, nil
}

// deltaOf is the text of one event: choices[0].delta.content of the OpenAI form, the text_delta of the Anthropic one.
func deltaOf(api extproviders.API, raw []byte) string {
	if api == extproviders.APIAnthropic {
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal(raw, &ev) != nil || ev.Type != "content_block_delta" || (ev.Delta.Type != "" && ev.Delta.Type != "text_delta") {
			return ""
		}
		return ev.Delta.Text
	}
	var ev struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &ev) != nil || len(ev.Choices) == 0 {
		return ""
	}
	return ev.Choices[0].Delta.Content
}

// textOf is the text of an answer: choices[0].message.content of the OpenAI form (a string, or parts with text), the
// text blocks of the Anthropic one.
func textOf(api extproviders.API, raw []byte) string {
	type part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	join := func(parts []part) string {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "" || p.Type == "text" || p.Type == "output_text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	if api == extproviders.APIAnthropic {
		var doc struct {
			Content []part `json:"content"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			return ""
		}
		return join(doc.Content)
	}
	var doc struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.Choices) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(doc.Choices[0].Message.Content, &s) == nil {
		return s
	}
	var parts []part
	if json.Unmarshal(doc.Choices[0].Message.Content, &parts) == nil {
		return join(parts)
	}
	return ""
}
