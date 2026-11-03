package mcpconn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// protocolVersion is the MCP revision the runtime asks for.
const protocolVersion = "2025-06-18"

// maxBody caps what is read of one answer or one stream.
const maxBody = 8 << 20

// userAgent of every request the package sends.
const userAgent = "harnessly-mcp/1"

// Limits of tools/list paging.
const (
	maxToolPages = 50
	maxTools     = 2000
)

// statusError is an HTTP answer that is not what MCP expects. 401 and 403 wrap ErrDenied, the rest ErrUnreachable.
type statusError struct {
	Status int
	Header http.Header
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s: status %d", Reason(e.Unwrap()), e.Status)
}

func (e *statusError) Unwrap() error {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return ErrDenied
	}
	return ErrUnreachable
}

// errNotMCP marks an answer that is not MCP, so a probe tries the other transport.
var errNotMCP = errors.New("not an MCP server")

// notMCP: the server answered, but not with MCP.
func notMCP(why string) error { return fmt.Errorf("%w: %w (%s)", ErrUnreachable, errNotMCP, why) }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// reply finds the answer to request id in one JSON-RPC payload (an object or a batch); ok is false when the payload
// is something else (a notification, a request of the server's).
func reply(raw []byte, id int) (json.RawMessage, bool, error) {
	raw = bytes.TrimSpace(raw)
	var msgs []rpcMsg
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &msgs); err != nil {
			return nil, false, notMCP("bad JSON-RPC batch")
		}
	} else {
		var m rpcMsg
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, false, notMCP("bad JSON-RPC message")
		}
		msgs = []rpcMsg{m}
	}
	want := strconv.Itoa(id)
	for _, m := range msgs {
		if m.Method != "" || string(bytes.TrimSpace(m.ID)) != want {
			continue
		}
		if m.Error != nil {
			return nil, true, fmt.Errorf("%w: JSON-RPC error %d", ErrUnreachable, m.Error.Code)
		}
		if m.Result == nil {
			return nil, true, notMCP("reply without result")
		}
		return m.Result, true, nil
	}
	return nil, false, nil
}

// readEvents reads a text/event-stream and calls fn for each event until fn says stop or the stream ends.
func readEvents(r io.Reader, fn func(event, data string) (stop bool, err error)) error {
	sc := bufio.NewScanner(io.LimitReader(r, maxBody))
	sc.Buffer(make([]byte, 0, 64<<10), maxBody)
	var event string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(data) > 0 {
				stop, err := fn(event, strings.Join(data, "\n"))
				if err != nil || stop {
					return err
				}
			}
			event, data = "", nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if len(data) > 0 {
		_, err := fn(event, strings.Join(data, "\n"))
		return err
	}
	return nil
}

func mediaType(h http.Header) string {
	mt, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return strings.ToLower(mt)
}

// session is one MCP conversation with a server.
type session interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	setProtocol(v string)
	close()
}

// open starts a session on a server through its transport.
func open(ctx context.Context, client *http.Client, t Transport, rawURL string, header http.Header) (session, error) {
	if t == SSE {
		return openSSE(ctx, client, rawURL, header)
	}
	return &httpSession{client: client, url: rawURL, header: header}, nil
}

// httpSession speaks streamable HTTP: every message is a POST, answers are JSON or an event stream.
type httpSession struct {
	client    *http.Client
	url       string
	header    http.Header
	sessionID string
	protocol  string
	nextID    int
}

func (s *httpSession) setProtocol(v string) { s.protocol = v }

func (s *httpSession) request(ctx context.Context, msg rpcMsg) (*http.Response, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	for k, v := range s.header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", userAgent)
	if s.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	if s.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", s.protocol)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, redactErr(err))
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		res.Body.Close()
		return nil, &statusError{Status: res.StatusCode, Header: res.Header}
	}
	if id := res.Header.Get("Mcp-Session-Id"); id != "" {
		s.sessionID = id
	}
	return res, nil
}

func (s *httpSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	res, err := s.request(ctx, rpcMsg{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch mediaType(res.Header) {
	case "text/event-stream":
		var out json.RawMessage
		err := readEvents(res.Body, func(event, data string) (bool, error) {
			if event != "" && event != "message" {
				return false, nil
			}
			result, ok, err := reply([]byte(data), id)
			if err != nil {
				return true, err
			}
			if ok {
				out = result
			}
			return ok, nil
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, notMCP("stream ended without a reply")
		}
		return out, nil
	case "application/json":
		raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		result, ok, err := reply(raw, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, notMCP("no reply to the request")
		}
		return result, nil
	default:
		return nil, notMCP("answer is neither JSON nor an event stream")
	}
}

func (s *httpSession) notify(ctx context.Context, method string, params any) error {
	res, err := s.request(ctx, rpcMsg{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.Body.Close()
}

func (s *httpSession) close() {}

// sseSession speaks the legacy HTTP+SSE transport: a GET stream announces an endpoint, messages are POSTed there and
// answers come back on the stream.
type sseSession struct {
	client   *http.Client
	header   http.Header
	endpoint string
	protocol string
	events   chan sseEvent
	cancel   context.CancelFunc
	nextID   int
}

type sseEvent struct {
	event, data string
	err         error
}

func openSSE(ctx context.Context, client *http.Client, rawURL string, header http.Header) (*sseSession, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, redactErr(err))
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		res.Body.Close()
		cancel()
		return nil, &statusError{Status: res.StatusCode, Header: res.Header}
	}
	if mediaType(res.Header) != "text/event-stream" {
		res.Body.Close()
		cancel()
		return nil, notMCP("GET is not an event stream")
	}
	s := &sseSession{client: client, header: header, events: make(chan sseEvent, 16), cancel: func() {
		cancel()
	}}
	go func() {
		defer close(s.events)
		defer res.Body.Close()
		err := readEvents(res.Body, func(event, data string) (bool, error) {
			select {
			case s.events <- sseEvent{event: event, data: data}:
				return false, nil
			case <-streamCtx.Done():
				return true, nil
			}
		})
		if err != nil {
			select {
			case s.events <- sseEvent{err: err}:
			case <-streamCtx.Done():
			}
		}
	}()
	base := res.Request.URL
	for {
		ev, err := s.next(ctx)
		if err != nil {
			s.close()
			return nil, err
		}
		if ev.event != "endpoint" {
			continue
		}
		ep, err := base.Parse(strings.TrimSpace(ev.data))
		if err != nil || ep.Scheme != base.Scheme || ep.Host != base.Host {
			s.close()
			return nil, notMCP("endpoint on another origin")
		}
		s.endpoint = ep.String()
		return s, nil
	}
}

func (s *sseSession) next(ctx context.Context) (sseEvent, error) {
	select {
	case ev, ok := <-s.events:
		if !ok {
			return sseEvent{}, notMCP("stream ended")
		}
		if ev.err != nil {
			return sseEvent{}, ev.err
		}
		return ev, nil
	case <-ctx.Done():
		return sseEvent{}, fmt.Errorf("%w: %v", ErrUnreachable, ctx.Err())
	}
}

func (s *sseSession) setProtocol(v string) { s.protocol = v }

func (s *sseSession) post(ctx context.Context, msg rpcMsg) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	for k, v := range s.header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, redactErr(err))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &statusError{Status: res.StatusCode, Header: res.Header}
	}
	return nil
}

func (s *sseSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	if err := s.post(ctx, rpcMsg{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: params}); err != nil {
		return nil, err
	}
	for {
		ev, err := s.next(ctx)
		if err != nil {
			return nil, err
		}
		if ev.event != "" && ev.event != "message" {
			continue
		}
		result, ok, err := reply([]byte(ev.data), id)
		if err != nil {
			return nil, err
		}
		if ok {
			return result, nil
		}
	}
}

func (s *sseSession) notify(ctx context.Context, method string, params any) error {
	return s.post(ctx, rpcMsg{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *sseSession) close() { s.cancel() }

// serverInfo is what initialize says about the server.
type serverInfo struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// handshake runs initialize and notifications/initialized.
func handshake(ctx context.Context, s session) (serverInfo, error) {
	raw, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "harnessly", "version": "1"},
	})
	if err != nil {
		return serverInfo{}, err
	}
	var init struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      serverInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &init); err != nil || init.ProtocolVersion == "" {
		return serverInfo{}, notMCP("bad initialize result")
	}
	s.setProtocol(init.ProtocolVersion)
	if err := s.notify(ctx, "notifications/initialized", nil); err != nil {
		return serverInfo{}, err
	}
	return init.ServerInfo, nil
}

// listTools pages through tools/list.
func listTools(ctx context.Context, s session) ([]Tool, error) {
	tools := []Tool{}
	cursor := ""
	for range maxToolPages {
		var params any
		if cursor != "" {
			params = map[string]string{"cursor": cursor}
		}
		raw, err := s.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools []struct {
				Name        string `json:"name"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, notMCP("bad tools/list result")
		}
		for _, t := range page.Tools {
			if t.Name == "" || len(tools) >= maxTools {
				continue
			}
			tools = append(tools, Tool{Name: t.Name, Description: oneLine(t.Description)})
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return tools, nil
}

// oneLine is the first line of s, at most 240 characters.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if utf8.RuneCountInString(s) > 240 {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:239])) + "…"
	}
	return s
}

// redactErr strips the query of a URL in a transport error, where a token could hide.
func redactErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			u.RawQuery = ""
			u.User = nil
			return &url.Error{Op: ue.Op, URL: u.String(), Err: ue.Err}
		}
	}
	return err
}

// timeoutCtx bounds one network step.
func timeoutCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}
