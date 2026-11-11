// Package appmcp is the stdio side of the harnessly-app MCP server: a child of the agent that passes its
// JSON-RPC to the runtime over HTTP and outlives the runtime's restarts.
//
// An agent that talks to the runtime's Streamable HTTP endpoint itself loses the server at a release. Claude Code
// reopens the GET stream once the runtime is gone; a runtime that is still starting answers 405 (it cannot place the
// caller in a chat yet), which the client takes for "this server has no stream" and never asks again, so
// tools/list_changed stops for the rest of the chat; and a runtime that stays down past the client's few attempts is
// dropped altogether. A stdio server stays connected for as long as its process lives, so the waiting is done here:
// the stream is reopened with pauses for ever, the client is told to ask tools/list again after every reopening, and
// a call made while the runtime is away waits for it.
package appmcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The environment the launch's MCP config gives the proxy (internal/apps launch.go). The token travels there and not
// on the command line, which every process of the machine can read.
const (
	EnvURL     = "HARNESSLY_APP_MCP_URL"
	EnvHeaders = "HARNESSLY_APP_MCP_HEADERS"
)

// The headers of a tool call's idempotency. The proxy gives every tools/call a key of its own (KeyHeader).
// The runtime keeps a window's command under that key, so the same call made again finds its command and its answer
// and makes no second one. A call is made again only when its answer was lost with the connection (the runtime
// restarted), only to a runtime that says it knows the keys (KeysHeader on its answers), and marked RepeatHeader: the
// runtime then runs nothing a second time, it answers from its journal or says the answer is lost.
const (
	KeyHeader    = "X-Harnessly-Call-Key"
	RepeatHeader = "X-Harnessly-Call-Repeat"
	KeysHeader   = "X-Harnessly-Call-Keys"
)

// Command is the harnessly subcommand that runs the proxy.
const Command = "app-mcp"

const (
	// DefaultWait is how long a request waits for a runtime that does not answer before the agent is told so.
	DefaultWait = 20 * time.Second
	// DefaultRetry is the pause between two tries of a request, and the first pause before the stream is reopened.
	DefaultRetry = 300 * time.Millisecond
	// DefaultMaxPause is the longest pause before the stream is reopened.
	DefaultMaxPause = 5 * time.Second
)

const rpcServerError = -32000

var listChanged = []byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)

// Proxy passes the JSON-RPC lines of In to the runtime at URL and writes the answers, and what the runtime's stream
// says, as lines to Out.
type Proxy struct {
	// URL is the runtime's MCP endpoint of the server, Headers what every request carries (the token).
	URL     string
	Headers map[string]string
	In      io.Reader
	Out     io.Writer
	// Client makes the requests; nil is one without a deadline (a tool may wait for the person) that gives up a dial
	// after three seconds.
	Client *http.Client
	// Wait, Retry, MaxPause: zero is the default of each.
	Wait, Retry, MaxPause time.Duration

	out      sync.Mutex
	streamed sync.Once
}

// FromEnv is the proxy the launch's config describes; an error names what the config lacks.
func FromEnv(getenv func(string) string, in io.Reader, out io.Writer) (*Proxy, error) {
	url := strings.TrimSpace(getenv(EnvURL))
	if url == "" {
		return nil, fmt.Errorf("%s is not set: `harnessly %s` is started by the chat's MCP config, not by hand", EnvURL, Command)
	}
	headers := map[string]string{}
	if raw := getenv(EnvHeaders); raw != "" {
		if err := json.Unmarshal([]byte(raw), &headers); err != nil {
			return nil, fmt.Errorf("%s is not a JSON object of strings", EnvHeaders)
		}
	}
	return &Proxy{URL: url, Headers: headers, In: in, Out: out}, nil
}

func (p *Proxy) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return defaultClient
}

var defaultClient = &http.Client{Transport: &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     90 * time.Second,
}}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Run serves until In ends (the agent closed the server) or ctx is done; the requests in flight are then given up.
func (p *Proxy) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	lines, ended := make(chan []byte), make(chan error, 1)
	go func() {
		in := bufio.NewReaderSize(p.In, 64<<10)
		for {
			line, err := in.ReadBytes('\n')
			if line = bytes.TrimSpace(line); len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				ended <- err
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-ended:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case msg := <-lines:
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.handle(ctx, msg, &wg)
			}()
		}
	}
}

type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

func (e envelope) asks() bool { return len(e.ID) > 0 && string(e.ID) != "null" && e.Method != "" }

func (p *Proxy) handle(ctx context.Context, msg []byte, wg *sync.WaitGroup) {
	var env envelope
	_ = json.Unmarshal(msg, &env)
	key := ""
	if env.Method == "tools/call" && env.asks() {
		raw := make([]byte, 16)
		_, _ = rand.Read(raw)
		key = hex.EncodeToString(raw)
	}
	answers, err := p.post(ctx, msg, key)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		if env.asks() {
			p.write(refusal(env, err))
		}
	} else {
		for _, a := range answers {
			p.write(a)
		}
	}
	// The stream opens once the client has said it is ready, as a Streamable HTTP client opens its own: a notification
	// sent before that would come ahead of the handshake.
	if env.Method == "notifications/initialized" {
		p.streamed.Do(func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.stream(ctx)
			}()
		})
	}
}

// errAway: the runtime did not take the request within Wait.
type errAway struct {
	waited time.Duration
	last   error
}

func (e errAway) Error() string {
	return fmt.Sprintf("harnessly's runtime on this machine did not answer for %d s (%v). It restarts at a release and "+
		"is usually back within a minute: the call was not made, repeat it a little later.", int(e.waited.Seconds()+0.5), e.last)
}

// errLost: the request reached the runtime and the connection ended before the answer.
type errLost struct{ err error }

func (e errLost) Error() string {
	if inner, ok := e.err.(errLost); ok {
		return inner.Error()
	}
	return fmt.Sprintf("harnessly's runtime restarted while this call was running (%v), so its answer is lost and the "+
		"call may or may not have been done. Check what it changed before repeating it.", e.err)
}

// The ways one try of a request ends without an answer.
const (
	tryFailed  = iota // an answer that is a refusal: asking again changes nothing
	tryNotSent        // the request did not reach the runtime: asking again repeats nothing
	tryLost           // the request reached the runtime and the connection ended before the answer
)

// post sends one message and returns the JSON-RPC messages of the answer (none for a notification). A runtime that
// cannot be reached, or answers 502-504 as a gate in front of a starting one does, is asked again until Wait is over.
// A request that was sent and then lost is not sent twice as it is: a tool call may not be repeatable. A tool call
// (key is its idempotency key) is made again marked as a repeat, when the runtime that came back knows the keys.
func (p *Proxy) post(ctx context.Context, msg []byte, key string) ([][]byte, error) {
	wait, retry := orDefault(p.Wait, DefaultWait), orDefault(p.Retry, DefaultRetry)
	start := time.Now()
	var lost error
	for {
		how := tryNotSent
		var err error
		var answers [][]byte
		if lost != nil {
			// The first try may have run: only a runtime that knows the keys is asked again.
			var knows bool
			if knows, how, err = p.knowsKeys(ctx); err == nil && !knows {
				return nil, errLost{lost}
			}
		}
		if err == nil {
			if answers, _, how, err = p.postOnce(ctx, msg, key, lost != nil); err == nil {
				return answers, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		switch {
		case how == tryFailed:
			return nil, err
		case how == tryLost && key == "":
			return nil, err
		case how == tryLost && lost == nil:
			// The runtime went away under the call: the wait for it starts now.
			lost, start = err, time.Now()
		}
		if time.Since(start)+retry > wait {
			if lost != nil {
				return nil, errLost{lost}
			}
			return nil, errAway{waited: time.Since(start), last: err}
		}
		if !sleep(ctx, retry) {
			return nil, ctx.Err()
		}
	}
}

var probe = []byte(`{"jsonrpc":"2.0","id":"harnessly-app-mcp","method":"ping"}`)

// knowsKeys asks the runtime that is there now whether it keeps tool calls by their keys.
func (p *Proxy) knowsKeys(ctx context.Context) (bool, int, error) {
	_, header, how, err := p.postOnce(ctx, probe, "", false)
	if err != nil {
		return false, how, err
	}
	return header.Get(KeysHeader) != "", tryFailed, nil
}

func (p *Proxy) request(ctx context.Context, method string, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	} else {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

func (p *Proxy) postOnce(ctx context.Context, msg []byte, key string, repeat bool) (answers [][]byte, header http.Header, how int, err error) {
	req, err := p.request(ctx, http.MethodPost, msg)
	if err != nil {
		return nil, nil, tryFailed, err
	}
	if key != "" {
		req.Header.Set(KeyHeader, key)
		if repeat {
			req.Header.Set(RepeatHeader, "1")
		}
	}
	res, err := p.client().Do(req)
	if err != nil {
		if notSent(err) {
			return nil, nil, tryNotSent, err
		}
		return nil, nil, tryLost, errLost{err}
	}
	defer res.Body.Close()
	header = res.Header
	switch {
	case res.StatusCode == http.StatusBadGateway || res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusGatewayTimeout:
		return nil, header, tryNotSent, fmt.Errorf("the runtime answered %d", res.StatusCode)
	case res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusNoContent:
		return nil, header, tryFailed, nil
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		err := readEvents(res.Body, func(data []byte) { answers = append(answers, data) })
		if len(answers) == 0 && err != nil {
			return nil, header, tryLost, errLost{err}
		}
		return answers, header, tryFailed, nil
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, header, tryLost, errLost{err}
	}
	var line bytes.Buffer
	if json.Compact(&line, body) != nil {
		// Not JSON-RPC: a refusal of the runtime's gate (403 with its own body), which the agent reads as words.
		return nil, header, tryFailed, fmt.Errorf("the runtime answered %d: %s", res.StatusCode, clip(string(bytes.TrimSpace(body)), 300))
	}
	if res.StatusCode >= 400 && !bytes.Contains(line.Bytes(), []byte(`"jsonrpc"`)) {
		return nil, header, tryFailed, fmt.Errorf("the runtime answered %d: %s", res.StatusCode, clip(line.String(), 300))
	}
	return [][]byte{line.Bytes()}, header, tryFailed, nil
}

// notSent: the request never left this process (nothing listens, the dial timed out), so asking again repeats nothing.
func notSent(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// refusal is the answer the agent gets in place of the runtime's. A tool call is answered as a tool's own error, in
// words the model reads; any other request as a JSON-RPC error.
func refusal(env envelope, err error) []byte {
	var body any
	if env.Method == "tools/call" {
		body = map[string]any{"jsonrpc": "2.0", "id": env.ID, "result": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true,
		}}
	} else {
		body = map[string]any{"jsonrpc": "2.0", "id": env.ID, "error": map[string]any{"code": rpcServerError, "message": err.Error()}}
	}
	raw, _ := json.Marshal(body)
	return raw
}

func (p *Proxy) write(line []byte) {
	p.out.Lock()
	defer p.out.Unlock()
	_, _ = p.Out.Write(append(append(make([]byte, 0, len(line)+1), line...), '\n'))
}

// stream keeps the runtime's GET stream open and passes its messages on. Whenever it is opened after a failure or an
// earlier stream's end, the tools may have changed unseen (apps turned on while the runtime was away, a stream the
// runtime refused while it started), so the client is told to ask tools/list again.
func (p *Proxy) stream(ctx context.Context) {
	first, maxPause := orDefault(p.Retry, DefaultRetry), orDefault(p.MaxPause, DefaultMaxPause)
	pause, missed := first, false
	for ctx.Err() == nil {
		got := p.streamOnce(ctx, missed)
		missed = true
		if got == streamOpened {
			pause = first
		}
		if !sleep(ctx, pause) {
			return
		}
		limit := maxPause
		if got == streamRefused {
			// A runtime that is there and gives no stream (the caller is in no chat) is asked ever more rarely: placing
			// a caller costs it a look at every pane.
			limit = maxPause * refusedPauses
		}
		if pause *= 2; pause > limit {
			pause = limit
		}
	}
}

const (
	streamAway = iota
	streamRefused
	streamOpened
)

// refusedPauses is how many times longer than MaxPause the pause grows while the runtime refuses the stream.
const refusedPauses = 6

// streamOnce opens the stream and reads it to its end. streamAway: the runtime did not answer; streamRefused: it
// opened none (405: it has no stream for a caller it cannot place in a chat, which right after its start is every
// caller).
func (p *Proxy) streamOnce(ctx context.Context, missed bool) int {
	req, err := p.request(ctx, http.MethodGet, nil)
	if err != nil {
		return streamAway
	}
	res, err := p.client().Do(req)
	if err != nil {
		return streamAway
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		return streamRefused
	}
	if missed {
		p.write(listChanged)
	}
	_ = readEvents(res.Body, p.write)
	return streamOpened
}

// readEvents calls each for the data of every server-sent event of r, as one line of JSON.
func readEvents(r io.Reader, each func([]byte)) error {
	rd := bufio.NewReaderSize(r, 64<<10)
	var data []byte
	flush := func() {
		if msg := bytes.TrimSpace(data); len(msg) > 0 {
			var line bytes.Buffer
			if json.Compact(&line, msg) == nil {
				each(line.Bytes())
			}
		}
		data = data[:0]
	}
	for {
		raw, err := rd.ReadBytes('\n')
		line := bytes.TrimRight(raw, "\r\n")
		switch {
		case len(raw) > 0 && len(line) == 0:
			flush()
		case bytes.HasPrefix(line, []byte("data:")):
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" "))...)
		}
		if err != nil {
			flush()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
