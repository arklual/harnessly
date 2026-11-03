package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// HTTPConfig is what the HTTP provider is built from.
type HTTPConfig struct {
	// BaseURL is the service's address, without a trailing path.
	BaseURL string
	// TokenFile holds the bearer token; it is read again when it changes on disk, so a rotated token needs no restart.
	TokenFile string
	// Client is the HTTP client; nil makes one with Timeout.
	Client *http.Client
	// Timeout bounds one attempt; zero means 15 seconds.
	Timeout time.Duration
	// Attempts is how many times a call that failed with a 5xx or a broken connection is tried; zero means 3.
	Attempts int
	// Backoff is the wait before the second attempt, doubled for each next; zero means 300 ms.
	Backoff time.Duration
}

// HTTP is a Provider that speaks the protocol of docs/hub/provider.md.
type HTTP struct {
	base     string
	tokens   tokenFile
	client   *http.Client
	attempts int
	backoff  time.Duration
}

// NewHTTP makes the HTTP provider.
func NewHTTP(c HTTPConfig) (*HTTP, error) {
	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("provider: %q is not an http(s) address", c.BaseURL)
	}
	h := &HTTP{base: u.String(), tokens: tokenFile{path: c.TokenFile}, client: c.Client, attempts: c.Attempts, backoff: c.Backoff}
	if h.client == nil {
		t := c.Timeout
		if t <= 0 {
			t = 15 * time.Second
		}
		h.client = &http.Client{Timeout: t}
	}
	if h.attempts <= 0 {
		h.attempts = 3
	}
	if h.backoff <= 0 {
		h.backoff = 300 * time.Millisecond
	}
	if c.TokenFile != "" {
		if _, err := h.tokens.get(); err != nil {
			return nil, err
		}
	}
	return h, nil
}

type tokenFile struct {
	path string
	mu   sync.Mutex
	val  string
	mod  time.Time
}

func (t *tokenFile) get() (string, error) {
	if t.path == "" {
		return "", nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fi, err := os.Stat(t.path)
	if err != nil {
		return "", fmt.Errorf("provider token: %w", err)
	}
	if t.val == "" || !fi.ModTime().Equal(t.mod) {
		b, err := os.ReadFile(t.path)
		if err != nil {
			return "", fmt.Errorf("provider token: %w", err)
		}
		t.val, t.mod = strings.TrimSpace(string(b)), fi.ModTime()
		if t.val == "" {
			return "", errors.New("provider token: the file is empty")
		}
	}
	return t.val, nil
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

// do sends one request, retrying a 5xx or a broken connection with a doubling wait. A state-changing call carries an
// Idempotency-Key, the same on every attempt, so a retry never does the work twice at the provider.
func (h *HTTP) do(ctx context.Context, method, path string, in any, mutating bool, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	key := ""
	if mutating {
		if key = IdempotencyKey(ctx); key == "" {
			var b [16]byte
			_, _ = rand.Read(b[:])
			key = hex.EncodeToString(b[:])
		}
	}
	var last error
	wait := h.backoff
	for attempt := 1; attempt <= h.attempts; attempt++ {
		retry, err := h.once(ctx, method, path, body, key, out)
		if err == nil {
			return nil
		}
		last = err
		if !retry || attempt == h.attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	return last
}

// once is one attempt; retry says whether another may help.
func (h *HTTP) once(ctx context.Context, method, path string, body []byte, key string, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	token, err := h.tokens.get()
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := h.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return true, err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out == nil || len(bytes.TrimSpace(raw)) == 0 {
			return false, nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return false, &Error{Status: res.StatusCode, Code: "bad_answer", Message: "the answer is not JSON"}
		}
		return false, nil
	}
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	switch {
	case res.StatusCode == http.StatusNotImplemented || e.Error == "not_supported":
		return false, ErrUnsupported
	case res.StatusCode == http.StatusNotFound:
		return false, ErrNotFound
	}
	return res.StatusCode >= 500, &Error{Status: res.StatusCode, Code: e.Error, Message: e.Message}
}
