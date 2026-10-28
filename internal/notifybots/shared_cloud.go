package notifybots

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// CloudRoute is the machine agent's loopback route that carries a call to the cloud (internal/agent/notifyrelay.go).
const CloudRoute = "/notify/call"

// ErrNoAgent is what Cloud.Call returns when no machine agent runs for this account: there is no cloud to ask.
var ErrNoAgent = errNoCloud

// Cloud is the cloud's shared bot, reached through the machine agent: the cloud knows the person by the machine's
// link and holds the bot's token and the person's chat; this side holds neither.
type Cloud struct {
	// Call posts a body to the agent's loopback route and returns its status and body (agent.CallLocal); it returns
	// ErrNoAgent (or an error wrapping it) when the account has no agent.
	Call func(route string, body any) (int, []byte, error)
}

func (c *Cloud) do(ctx context.Context, body map[string]any, out any) error {
	type result struct {
		status int
		raw    []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, raw, err := c.Call(CloudRoute, body)
		done <- result{status, raw, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		return refuse(ReasonOffline)
	}
	if errors.Is(r.err, errNoCloud) {
		return errNoCloud
	}
	if r.err != nil {
		return refuse(ReasonOffline)
	}
	if r.status != http.StatusOK {
		// The agent's own refusal: no link to the cloud now (503), or a call it does not take.
		if r.status == http.StatusServiceUnavailable {
			return refuse(ReasonOffline)
		}
		return &Refusal{Reason: ReasonFailed, Detail: "agent: HTTP " + http.StatusText(r.status)}
	}
	var answer struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(r.raw, &answer) != nil {
		return refuse(ReasonFailed)
	}
	if answer.Status == http.StatusGatewayTimeout {
		// The cloud did not answer: it is away, or an older one that does not know the call.
		return refuse(ReasonOffline)
	}
	if answer.Status < 200 || answer.Status > 299 {
		var refusal struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(answer.Body, &refusal)
		if refusal.Error == "" {
			refusal.Error = ReasonFailed
		}
		return &Refusal{Reason: refusal.Error, Detail: refusal.Detail}
	}
	if json.Unmarshal(answer.Body, out) != nil {
		return refuse(ReasonFailed)
	}
	return nil
}

func (c *Cloud) state(ctx context.Context, body map[string]any) (SharedState, error) {
	var st SharedState
	err := c.do(ctx, body, &st)
	return st, err
}

func (c *Cloud) State(ctx context.Context) (SharedState, error) {
	return c.state(ctx, map[string]any{"action": "status"})
}

func (c *Cloud) Link(ctx context.Context) (SharedState, error) {
	return c.state(ctx, map[string]any{"action": "link"})
}

func (c *Cloud) Unlink(ctx context.Context) (SharedState, error) {
	return c.state(ctx, map[string]any{"action": "unlink"})
}

func (c *Cloud) SetInstruction(ctx context.Context, text string) (SharedState, error) {
	return c.state(ctx, map[string]any{"action": "instruction", "instruction": text})
}

func (c *Cloud) Send(ctx context.Context, parts []Part, silent bool) (SharedSent, error) {
	var sent SharedSent
	err := c.do(ctx, map[string]any{"action": "send", "parts": parts, "silent": silent}, &sent)
	return sent, err
}
