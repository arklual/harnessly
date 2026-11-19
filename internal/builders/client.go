package builders

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrNoAgent: this machine has no agent that dials a hub, so there is no pool to ask.
var ErrNoAgent = errors.New("this machine is not connected to a hub (no agent.json in the data folder)")

// Client talks to the machine agent's /jobs routes on the loopback, which sign for the hub.
type Client struct {
	Base   string // http://127.0.0.1:<port>
	Token  string
	Header string // the header the agent reads its local token from
	HTTP   *http.Client
}

// NewLocal reads the agent's port and token from agent.json in the data folder.
func NewLocal(dataDir, tokenHeader string) (*Client, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "agent.json"))
	if err != nil {
		return nil, ErrNoAgent
	}
	var info struct {
		Port  any    `json:"port"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &info) != nil || info.Token == "" {
		return nil, ErrNoAgent
	}
	port := ""
	switch p := info.Port.(type) {
	case float64:
		port = strconv.FormatFloat(p, 'f', -1, 64)
	case string:
		port = p
	}
	if port == "" || port == "0" {
		return nil, ErrNoAgent
	}
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	return &Client{Base: "http://127.0.0.1:" + port, Token: info.Token, Header: tokenHeader, HTTP: &http.Client{}}, nil
}

// Builder is one machine of the pool.
type Builder struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Caps    []string `json:"caps"`
	Slots   int      `json:"slots"`
	Running int      `json:"running"`
	Waiting int      `json:"waiting"`
	Online  bool     `json:"online"`
	Ready   bool     `json:"ready"`
	Note    string   `json:"note"`
}

// File is a result file.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Job is a job as the hub reports it.
type Job struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Need   struct {
		Kind string   `json:"kind"`
		Caps []string `json:"caps"`
	} `json:"need"`
	Choice struct {
		Mode    string `json:"mode"`
		Machine string `json:"machine"`
	} `json:"choice"`
	Builder *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"builder"`
	Local       bool `json:"local"`
	LocalLimits *struct {
		TimeoutSec int    `json:"timeoutSec"`
		Note       string `json:"note"`
	} `json:"localLimits"`
	Reason      string   `json:"reason"`
	ExitCode    *int     `json:"exitCode"`
	Error       string   `json:"error"`
	Log         string   `json:"log"`
	Outputs     []File   `json:"outputs"`
	Argv        []string `json:"argv"`
	OutputGlobs []string `json:"outputGlobs"`
	TimeoutSec  int      `json:"timeoutSec"`
	CreatedAt   string   `json:"createdAt"`
	StartedAt   *string  `json:"startedAt"`
	FinishedAt  *string  `json:"finishedAt"`
}

// Final says the job is over.
func (j Job) Final() bool {
	return j.Status == "done" || j.Status == "failed" || j.Status == "cancelled"
}

// Request is what Submit sends.
type Request struct {
	Title string            `json:"title,omitempty"`
	Need  Need              `json:"need"`
	Argv  []string          `json:"argv"`
	Env   map[string]string `json:"env,omitempty"`
	// Outputs are globs relative to the job's folder.
	Outputs    []string `json:"outputs,omitempty"`
	TimeoutSec int      `json:"timeoutSec,omitempty"`
	HasInput   bool     `json:"hasInput"`
}

// Need is what the job asks of its place.
type Need struct {
	Kind    string   `json:"kind"`
	Caps    []string `json:"caps,omitempty"`
	Machine string   `json:"machine,omitempty"`
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body io.Reader, ct string) (*http.Response, error) {
	u := c.Base + "/jobs" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(c.Header, c.Token)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the machine agent does not answer: %w", err)
	}
	return res, nil
}

func (c *Client) json(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	ct := ""
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ct = bytes.NewReader(raw), "application/json"
	}
	res, err := c.do(ctx, method, path, q, body, ct)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode != http.StatusOK {
		return apiError(res.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// StatusError is an answer of the agent or the hub that was not 200.
type StatusError struct {
	Status int
	Text   string
}

func (e *StatusError) Error() string { return e.Text }

// NoPool says err means there is no pool to ask: the agent is in no hub (503), or the hub has no builders (404), or the
// agent is not there. The job then runs here.
func NoPool(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusServiceUnavailable || se.Status == http.StatusNotFound
	}
	return errors.Is(err, ErrNoAgent) || isConnRefused(err)
}

func isConnRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "connection refused")
}

// apiError reads the hub's or the agent's error body into words.
func apiError(status int, data []byte) error {
	return &StatusError{Status: status, Text: apiText(status, data)}
}

func apiText(status int, data []byte) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	_ = json.Unmarshal(data, &e)
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.Detail)
	}
	switch {
	case e.Error != "" && msg != "":
		return fmt.Sprintf("%s: %s", e.Error, msg)
	case e.Error != "":
		return e.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}

// Builders lists the pool.
func (c *Client) Builders(ctx context.Context) ([]Builder, error) {
	var out struct {
		Builders []Builder `json:"builders"`
	}
	err := c.json(ctx, http.MethodGet, "/builders", nil, nil, &out)
	return out.Builders, err
}

// Submit hands the hub a job.
func (c *Client) Submit(ctx context.Context, r Request) (Job, error) {
	var out struct {
		Job Job `json:"job"`
	}
	err := c.json(ctx, http.MethodPost, "", nil, r, &out)
	return out.Job, err
}

// Get reads a job.
func (c *Client) Get(ctx context.Context, id string) (Job, error) {
	var out struct {
		Job Job `json:"job"`
	}
	err := c.json(ctx, http.MethodGet, "/"+url.PathEscape(id), nil, nil, &out)
	return out.Job, err
}

// List reads the space's latest jobs.
func (c *Client) List(ctx context.Context, limit int) ([]Job, error) {
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	err := c.json(ctx, http.MethodGet, "", q, nil, &out)
	return out.Jobs, err
}

// Cancel ends a job.
func (c *Client) Cancel(ctx context.Context, id string) (Job, error) {
	var out struct {
		Job Job `json:"job"`
	}
	err := c.json(ctx, http.MethodPost, "/"+url.PathEscape(id)+"/cancel", nil, struct{}{}, &out)
	return out.Job, err
}

// Finish tells the hub how a local run ended.
func (c *Client) Finish(ctx context.Context, id string, exit int, errText, log string) error {
	return c.json(ctx, http.MethodPost, "/"+url.PathEscape(id)+"/finish", nil,
		map[string]any{"exitCode": exit, "error": errText, "log": log}, nil)
}

// PutInput sends the input archive.
func (c *Client) PutInput(ctx context.Context, id string, r io.Reader) error {
	res, err := c.do(ctx, http.MethodPut, "/"+url.PathEscape(id)+"/input", nil, r, "application/gzip")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return apiError(res.StatusCode, data)
	}
	return nil
}

// Download copies a result file into dst.
func (c *Client) Download(ctx context.Context, id, rel string, dst io.Writer) error {
	res, err := c.do(ctx, http.MethodGet, "/"+url.PathEscape(id)+"/out", url.Values{"path": {rel}}, nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return apiError(res.StatusCode, data)
	}
	_, err = io.Copy(dst, res.Body)
	return err
}

// PollEvery is how often Run asks the hub how a job stands.
var PollEvery = 2 * time.Second
