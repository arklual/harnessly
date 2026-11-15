package chatgpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Model is one model the signed-in account may use, as GET /v1/models lists it for display.
type Model struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Efforts are the reasoning levels the model takes, when the catalog says; empty: no choice is offered.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	// Images and Files say the model takes pictures and files in a message, when the catalog says so.
	Images bool `json:"images"`
	Files  bool `json:"files"`
}

type catalogModel struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility"`
	Reasoning   []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
	DefaultReasoning string   `json:"default_reasoning_level"`
	Modalities       []string `json:"input_modalities"`
}

// modelCache keeps an account's catalog for a while: the picker asks for it on every new chat.
type modelCache struct {
	mu   sync.Mutex
	byAc map[string]modelsAt
}

type modelsAt struct {
	models []Model
	at     time.Time
}

const modelsFresh = 10 * time.Minute

// listModels reads the account's catalog and keeps the models meant for display, in the server's order.
func (s *Service) listModels(ctx context.Context, key string) ([]Model, error) {
	s.models.mu.Lock()
	if c, ok := s.models.byAc[key]; ok && s.now().Sub(c.at) < modelsFresh {
		s.models.mu.Unlock()
		return c.models, nil
	}
	s.models.mu.Unlock()
	token, err := s.oauth.token(ctx, key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.ep.API, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fail(CodeNetwork, http.StatusBadGateway, "")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4*httpBodyMax))
	if res.StatusCode != http.StatusOK {
		return nil, apiFailure(res.StatusCode, raw)
	}
	var body struct {
		Models []catalogModel `json:"models"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, fail(CodeUpstream, http.StatusBadGateway, "the models' list does not read")
	}
	out := make([]Model, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Visibility != "list" || m.Slug == "" {
			continue
		}
		mm := Model{Slug: m.Slug, Name: m.DisplayName, DefaultEffort: m.DefaultReasoning}
		if mm.Name == "" {
			mm.Name = m.Slug
		}
		for _, r := range m.Reasoning {
			if r.Effort != "" && !slices.Contains(mm.Efforts, r.Effort) {
				mm.Efforts = append(mm.Efforts, r.Effort)
			}
		}
		if mm.DefaultEffort != "" && !slices.Contains(mm.Efforts, mm.DefaultEffort) {
			mm.DefaultEffort = ""
		}
		mm.Images = slices.Contains(m.Modalities, "image")
		mm.Files = slices.Contains(m.Modalities, "file") || slices.Contains(m.Modalities, "pdf")
		out = append(out, mm)
	}
	s.models.mu.Lock()
	s.models.byAc[key] = modelsAt{models: out, at: s.now()}
	s.models.mu.Unlock()
	return out, nil
}

// apiFailure reads an error of the API: a Responses error object, or a direct-admission {"detail": …}.
func apiFailure(status int, raw []byte) *Error {
	var body struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
		Detail any `json:"detail"`
	}
	_ = json.Unmarshal(raw, &body)
	code, detail := "", ""
	if body.Error != nil {
		code, detail = body.Error.Code, body.Error.Message
		if body.Error.Param != "" {
			detail += " (" + body.Error.Param + ")"
		}
	} else if d, ok := body.Detail.(string); ok {
		detail = d
	}
	return failureOf(status, code, detail)
}

// failureOf maps an API error to the page's code: the plan's own codes first, then the status.
func failureOf(status int, code, detail string) *Error {
	switch code {
	case "subscription_sharing_usage_limit_exceeded":
		return fail(CodeUsageLimit, http.StatusTooManyRequests, detail)
	case "subscription_sharing_user_not_eligible":
		return fail(CodeNotEligible, http.StatusForbidden, detail)
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		return fail(CodeUnavailable, http.StatusServiceUnavailable, detail)
	case "subscription_sharing_unsupported_capability":
		return fail(CodeUnsupported, http.StatusBadRequest, detail)
	case "subscription_sharing_invalid_user":
		return fail(CodeSignedOut, http.StatusUnauthorized, detail)
	case "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		return fail(CodeNoPlan, http.StatusForbidden, detail)
	}
	switch {
	case status == http.StatusUnauthorized:
		return fail(CodeSignedOut, http.StatusUnauthorized, detail)
	case status == http.StatusTooManyRequests:
		return fail(CodeUsageLimit, http.StatusTooManyRequests, detail)
	case status == http.StatusServiceUnavailable || status >= 500:
		return fail(CodeUnavailable, http.StatusServiceUnavailable, detail)
	case status == http.StatusForbidden:
		return fail(CodeNotEligible, http.StatusForbidden, detail)
	}
	if code != "" {
		detail = code + ": " + detail
	}
	return fail(CodeUpstream, http.StatusBadGateway, detail)
}

// turnRequest is one Responses request: the whole conversation in input.
type turnRequest struct {
	Model        string          `json:"model"`
	Instructions string          `json:"instructions,omitempty"`
	Input        []inputItem     `json:"input"`
	Reasoning    *reasoningParam `json:"reasoning,omitempty"`
	Store        bool            `json:"store"`
	Stream       bool            `json:"stream"`
}

type reasoningParam struct {
	Effort string `json:"effort"`
}

type inputItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

// instructions are the few words every turn starts with: a plain conversation, no tools.
const instructions = "You are ChatGPT, talking with the person inside harnessly. Reply in the language of the person's last message. Use Markdown where it helps."

// streamTurn sends one turn and calls onDelta with each piece of text as it comes. It returns once the response
// completed; a failure before or during the stream is an *Error.
func (s *Service) streamTurn(ctx context.Context, token string, body turnRequest, onDelta func(string)) error {
	body.Store, body.Stream = false, true
	if body.Instructions == "" {
		body.Instructions = instructions
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.ep.API, "/")+"/responses", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := s.stream.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail(CodeNetwork, http.StatusBadGateway, "")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, httpBodyMax))
		return apiFailure(res.StatusCode, b)
	}
	return readEvents(ctx, res.Body, onDelta)
}

// event is the part of a Responses stream event the chat reads.
type event struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Response *struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Incomplete *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
}

// readEvents reads a server-sent event stream through response.completed.
func readEvents(ctx context.Context, r io.Reader, onDelta func(string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var data strings.Builder
	dispatch := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		payload := data.String()
		data.Reset()
		if payload == "[DONE]" {
			return true, nil
		}
		var e event
		if json.Unmarshal([]byte(payload), &e) != nil {
			return false, nil
		}
		switch e.Type {
		case "response.output_text.delta":
			if e.Delta != "" {
				onDelta(e.Delta)
			}
		case "response.completed":
			return true, nil
		case "response.failed":
			code, msg := "", ""
			if e.Response != nil && e.Response.Error != nil {
				code, msg = e.Response.Error.Code, e.Response.Error.Message
			}
			return true, failureOf(0, code, msg)
		case "response.incomplete":
			reason := ""
			if e.Response != nil && e.Response.Incomplete != nil {
				reason = e.Response.Incomplete.Reason
			}
			return true, fail(CodeIncomplete, http.StatusBadGateway, reason)
		case "error":
			return true, failureOf(0, e.Code, e.Message)
		}
		return false, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			done, err := dispatch()
			if done || err != nil {
				return err
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(v, " "))
		}
	}
	if done, err := dispatch(); done || err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fail(CodeNetwork, http.StatusBadGateway, "the stream broke")
	}
	return fail(CodeIncomplete, http.StatusBadGateway, "the stream ended before response.completed")
}

// Complete is one answer to one prompt on the active account's plan, outside any chat: the voice's quick edits ask
// with it. An empty model is the plan's lightest (a «Luna» when the plan has one), at its lowest depth.
func (s *Service) Complete(ctx context.Context, model, instructions, prompt string) (string, error) {
	key, err := s.accs.active()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", fail(CodeSignedOut, http.StatusUnauthorized, "")
	}
	effort := ""
	if list, err := s.listModels(ctx, key); err == nil && len(list) > 0 {
		pick := list[len(list)-1]
		for _, m := range list {
			if model != "" && m.Slug == model || model == "" && strings.Contains(strings.ToLower(m.Slug), "luna") {
				pick = m
				break
			}
		}
		model = pick.Slug
		if len(pick.Efforts) > 0 {
			effort = pick.Efforts[0]
		}
	}
	if model == "" {
		return "", fail(CodeBadRequest, http.StatusBadRequest, "model")
	}
	token, err := s.oauth.token(ctx, key)
	if err != nil {
		return "", err
	}
	req := turnRequest{Model: model, Instructions: instructions, Input: []inputItem{{
		Type: "message", Role: "user", Content: []contentPart{{Type: "input_text", Text: prompt}},
	}}}
	if effort != "" {
		req.Reasoning = &reasoningParam{Effort: effort}
	}
	var b strings.Builder
	err = s.streamTurn(ctx, token, req, func(d string) { b.WriteString(d) })
	return b.String(), err
}
