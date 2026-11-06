package provider

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// HTTPConfig is what the HTTP provider is built from: the address, the token file, the timeout and the attempts of the
// extension frame (internal/hub/ext). The circuit breaker is off unless BreakerFailures is set, so that the provider
// behaves as it did before the frame.
type HTTPConfig = ext.Config

// HTTP is a Provider that speaks the protocol of docs/hub/provider.md.
type HTTP struct {
	c *ext.Client
}

// NewHTTP makes the HTTP provider.
func NewHTTP(c HTTPConfig) (*HTTP, error) {
	if c.BreakerFailures == 0 {
		c.BreakerFailures = -1
	}
	cl, err := ext.NewClient(c)
	if err != nil {
		return nil, err
	}
	return &HTTP{c: cl}, nil
}

func (h *HTTP) Name() string { return "http" }

func (h *HTTP) Plans(ctx context.Context, m Machine) ([]Plan, error) {
	var out struct {
		Plans []Plan `json:"plans"`
	}
	if err := h.do(ctx, http.MethodGet, "/machines/"+url.PathEscape(m.Ref())+"/plans", nil, false, &out); err != nil {
		return nil, err
	}
	return out.Plans, nil
}

func (h *HTTP) Quote(ctx context.Context, m Machine, want Resources) (Price, error) {
	var out struct {
		Price *Price `json:"price"`
	}
	if err := h.do(ctx, http.MethodPost, "/machines/"+url.PathEscape(m.Ref())+"/quote", want, false, &out); err != nil {
		return Price{}, err
	}
	if out.Price == nil || out.Price.Currency == "" {
		return Price{}, &Error{Code: "bad_answer", Message: "the quote has no price"}
	}
	if out.Price.Period == "" {
		out.Price.Period = "month"
	}
	return *out.Price, nil
}

func (h *HTTP) Resize(ctx context.Context, m Machine, want Resources) (OpRef, error) {
	return h.op(ctx, "/machines/"+url.PathEscape(m.Ref())+"/resize", want)
}

func (h *HTTP) Reboot(ctx context.Context, m Machine) (OpRef, error) {
	return h.op(ctx, "/machines/"+url.PathEscape(m.Ref())+"/reboot", struct{}{})
}

func (h *HTTP) PowerOff(ctx context.Context, m Machine) (OpRef, error) {
	return h.op(ctx, "/machines/"+url.PathEscape(m.Ref())+"/power-off", struct{}{})
}

func (h *HTTP) PowerOn(ctx context.Context, m Machine) (OpRef, error) {
	return h.op(ctx, "/machines/"+url.PathEscape(m.Ref())+"/power-on", struct{}{})
}

func (h *HTTP) op(ctx context.Context, path string, body any) (OpRef, error) {
	var out OpRef
	if err := h.do(ctx, http.MethodPost, path, body, true, &out); err != nil {
		return OpRef{}, err
	}
	if out.ID == "" {
		return OpRef{}, &Error{Code: "bad_answer", Message: "the answer has no opId"}
	}
	return out, nil
}

func (h *HTTP) Op(ctx context.Context, ref OpRef) (OpStatus, error) {
	var out OpStatus
	if err := h.do(ctx, http.MethodGet, "/ops/"+url.PathEscape(ref.ID), nil, false, &out); err != nil {
		return OpStatus{}, err
	}
	switch out.State {
	case StatePending, StateRunning, StateDone, StateFailed:
		return out, nil
	}
	return OpStatus{}, &Error{Code: "bad_answer", Message: "unknown state " + out.State}
}

func (h *HTTP) ConsoleURL(ctx context.Context, m Machine) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := h.do(ctx, http.MethodGet, "/machines/"+url.PathEscape(m.Ref())+"/console", nil, false, &out); err != nil {
		return "", err
	}
	if u, err := url.Parse(out.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", &Error{Code: "bad_answer", Message: "the console address is not an http(s) link"}
	}
	return out.URL, nil
}

// do is one call through the extension client.
func (h *HTTP) do(ctx context.Context, method, path string, in any, mutating bool, out any) error {
	return h.c.Do(ctx, method, path, in, mutating, out)
}
