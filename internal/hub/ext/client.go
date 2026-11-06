package ext

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

// Errors a call answers.
var (
	// ErrUnsupported means the adapter does not do this: it answered 501 or `not_supported`, or its describe does not
	// list the capability.
	ErrUnsupported = errors.New("extension: not supported")
	// ErrNotFound means the adapter does not know the thing asked for (404).
	ErrNotFound = errors.New("extension: not found")
	// ErrCircuitOpen means the adapter failed several calls in a row and is not called until the pause is over.
	ErrCircuitOpen = errors.New("extension: the circuit is open, the adapter is not called for now")
)

// Error is a refusal or a failure from an adapter with its own code.
type Error struct {
	// Status is the HTTP status the adapter answered with (0 when it did not come over HTTP).
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("extension: %s: %s", e.Code, e.Message)
	case e.Code != "":
		return "extension: " + e.Code
	case e.Message != "":
		return "extension: " + e.Message
	}
	return fmt.Sprintf("extension: status %d", e.Status)
}

type idemKey struct{}

// WithIdempotencyKey carries the key a state-changing call is sent with, so that a retry of the same hub operation is
// the same operation at the adapter. Without one the client makes a random key per call.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idemKey{}, key)
}

// IdempotencyKey reads the key WithIdempotencyKey put in ctx.
func IdempotencyKey(ctx context.Context) string {
	k, _ := ctx.Value(idemKey{}).(string)
	return k
}

// Config is what a Client is built from.
type Config struct {
	// BaseURL is the adapter's address, without a trailing path.
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
	// Breaker opens after this many calls in a row that failed in a way a retry could mend (5xx, a broken connection, a
	// timeout); zero means 5, a negative number turns the breaker off.
	BreakerFailures int
	// BreakerPause is how long an open breaker lets no call through before it lets one try; zero means 30 seconds.
	BreakerPause time.Duration
}

// Client calls one adapter: the token, the timeout, the retries, the idempotency key and the breaker.
type Client struct {
	base     string
	tokens   tokenFile
	client   *http.Client
	attempts int
	backoff  time.Duration
	breaker  *Breaker
}

// NewClient makes a client. The address is checked and the token file is read now, so that a wrong setting stops the
// start and not the first call.
func NewClient(c Config) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("extension: %q is not an http(s) address", c.BaseURL)
	}
	cl := &Client{base: u.String(), tokens: tokenFile{path: c.TokenFile}, client: c.Client, attempts: c.Attempts, backoff: c.Backoff}
	if cl.client == nil {
		t := c.Timeout
		if t <= 0 {
			t = 15 * time.Second
		}
		cl.client = &http.Client{Timeout: t}
	}
	if cl.attempts <= 0 {
		cl.attempts = 3
	}
	if cl.backoff <= 0 {
		cl.backoff = 300 * time.Millisecond
	}
	if c.BreakerFailures >= 0 {
		cl.breaker = NewBreaker(c.BreakerFailures, c.BreakerPause)
	}
	if c.TokenFile != "" {
		if _, err := cl.tokens.get(); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

// Breaker is the circuit breaker of the client, nil when it is turned off.
func (c *Client) Breaker() *Breaker { return c.breaker }

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
		return "", fmt.Errorf("extension token: %w", err)
	}
	if t.val == "" || !fi.ModTime().Equal(t.mod) {
		b, err := os.ReadFile(t.path)
		if err != nil {
			return "", fmt.Errorf("extension token: %w", err)
		}
		t.val, t.mod = strings.TrimSpace(string(b)), fi.ModTime()
		if t.val == "" {
			return "", errors.New("extension token: the file is empty")
		}
	}
	return t.val, nil
}

// Do sends one request and reads a JSON answer into out (nil to drop it). A 5xx and a broken connection are tried again
// with a doubling wait; a call that changes something (mutating) carries an Idempotency-Key, the same on every attempt,
// so a retry never does the work twice at the adapter. The breaker counts a call as failed only when it failed in a way a
// retry could mend: a 4xx is the caller's mistake and does not open it.
func (c *Client) Do(ctx context.Context, method, path string, in any, mutating bool, out any) error {
	if c.breaker != nil && !c.breaker.Allow() {
		return ErrCircuitOpen
	}
	err := c.do(ctx, method, path, in, mutating, out)
	if c.breaker != nil && ctx.Err() == nil {
		c.breaker.Record(!transient(err))
	}
	return err
}

// transient says whether err is the adapter's being unwell rather than a plain answer.
func transient(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Status >= 500
	}
	return !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrNotFound)
}

func (c *Client) do(ctx context.Context, method, path string, in any, mutating bool, out any) error {
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
	wait := c.backoff
	for attempt := 1; attempt <= c.attempts; attempt++ {
		retry, err := c.once(ctx, method, path, body, key, out)
		if err == nil {
			return nil
		}
		last = err
		if !retry || attempt == c.attempts {
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
func (c *Client) once(ctx context.Context, method, path string, body []byte, key string, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
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
	token, err := c.tokens.get()
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.client.Do(req)
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
