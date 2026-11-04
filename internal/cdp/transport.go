package cdp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

const MaxMessage = 64 << 20

type Stream struct {
	rw    io.ReadWriteCloser
	r     *bufio.Reader
	write chan struct{}
	once  sync.Once
}

func NewStream(rw io.ReadWriteCloser) *Stream {
	return &Stream{rw: rw, r: bufio.NewReader(rw), write: make(chan struct{}, 1)}
}
func (s *Stream) Close() error                           { var err error; s.once.Do(func() { err = s.rw.Close() }); return err }
func (s *Stream) Read(_ context.Context) ([]byte, error) { return ReadMessage(s.r) }

// ReadMessage accepts only complete NUL-terminated records, with a bound for broken peers.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice(0)
		if len(out)+len(part) > MaxMessage {
			return nil, fmt.Errorf("CDP message exceeds %d bytes", MaxMessage)
		}
		out = append(out, part...)
		if err == nil {
			return out[:len(out)-1], nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
}
func (s *Stream) Write(ctx context.Context, b []byte) error {
	select {
	case s.write <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.write }()
	// A cancelled partial write cannot be resumed by another command.
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	b = append(append(make([]byte, 0, len(b)+1), b...), 0)
	for len(b) > 0 {
		n, err := s.rw.Write(b)
		if err != nil {
			_ = s.Close()
			return err
		}
		if n == 0 {
			_ = s.Close()
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

type Socket struct{ ws *websocket.Conn }

func Dial(ctx context.Context, base string, handler EventHandler) (*Conn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/json/version", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DevTools discovery: %s", resp.Status)
	}
	var v struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return nil, err
	}
	if v.URL == "" {
		return nil, fmt.Errorf("DevTools discovery returned no WebSocket")
	}
	ws, _, err := websocket.Dial(ctx, v.URL, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(MaxMessage)
	return New(&Socket{ws}, handler), nil
}
func (s *Socket) Read(ctx context.Context) ([]byte, error) { _, b, e := s.ws.Read(ctx); return b, e }
func (s *Socket) Write(ctx context.Context, b []byte) error {
	return s.ws.Write(ctx, websocket.MessageText, b)
}
func (s *Socket) Close() error { return s.ws.CloseNow() }
