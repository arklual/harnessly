package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Do asks the agent of a machine for op and maps every way that can go wrong to the error the hub answers with:
// no link (502 machine_offline), no answer in time (504 machine_timeout), the agent's own refusals (MapError).
func (r *Registry) Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error) {
	l := r.Get(machineID)
	if l == nil {
		return nil, httpx.ErrMachineOffline
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := l.Call(ctx, op, params)
	if err != nil {
		return nil, MapError(err, op)
	}
	return raw, nil
}

// DoInto is Do that decodes the result into out.
func (r *Registry) DoInto(ctx context.Context, machineID, op string, params, out any, timeout time.Duration) error {
	raw, err := r.Do(ctx, machineID, op, params, timeout)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return httpx.New(http.StatusBadGateway, "machine_error", "Агент ответил непонятно").With("agentError", "unreadable answer to "+op)
	}
	return nil
}

// MapError turns what a Call returned into an API error (docs/hub/api.md, "Agent link": the table of agent codes).
func MapError(err error, op string) error {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he
	}
	switch {
	case errors.Is(err, ErrClosed):
		return httpx.ErrMachineOffline
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.ErrMachineTimeout
	case errors.Is(err, context.Canceled):
		return httpx.New(http.StatusBadGateway, "machine_offline", "Запрос прерван")
	}
	var ae *hubproto.Error
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Code {
	case hubproto.CodePolicyDenied:
		return httpx.New(http.StatusForbidden, "machine_policy", "Запрещено настройками машины").With("op", op).With("agentError", ae.Text)
	case hubproto.CodeUnsupported:
		return httpx.New(http.StatusUnprocessableEntity, "provider_off", "Машина этого не умеет").With("op", op)
	case hubproto.CodeBusy:
		reason := "busy"
		if op == hubproto.OpLoginStart {
			reason = "login_running"
		}
		return httpx.Conflict(reason, "Уже выполняется другая операция на этой машине").With("agentError", ae.Text)
	case hubproto.CodeNotFound:
		return httpx.New(http.StatusNotFound, "not_found", "Машина не нашла такую операцию").With("agentError", ae.Text)
	case hubproto.CodeInvalid:
		return httpx.New(http.StatusBadRequest, "invalid_request", "Машина не приняла запрос").With("agentError", ae.Text)
	default:
		return httpx.New(http.StatusBadGateway, "machine_error", "Машина не смогла выполнить операцию").With("agentError", ae.Text)
	}
}

// Times rewrites the agent's epoch-millisecond times (the fields named "at" and "expiresAt", anywhere in the value) as
// RFC 3339 strings in UTC, the form of the hub API. Everything else passes through unchanged.
func Times(raw json.RawMessage) json.RawMessage {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	out, err := json.Marshal(timeify(v))
	if err != nil {
		return raw
	}
	return out
}

func timeify(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if n, ok := e.(json.Number); ok && (k == "at" || k == "expiresAt") {
				if ms, err := strconv.ParseInt(n.String(), 10, 64); err == nil && ms > 0 {
					x[k] = time.UnixMilli(ms).UTC().Format(time.RFC3339)
					continue
				}
			}
			x[k] = timeify(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = timeify(e)
		}
		return x
	}
	return v
}
