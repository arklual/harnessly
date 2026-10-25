package wirefake

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The fake machine's terminals: the bytes a page types come back as output, with a prompt, so the page's terminal can be
// built without a shell behind it. The socket speaks the terminal protocol of internal/terminal.
const (
	termStdin     = 0x00
	termStdout    = 0x01
	termHeartbeat = 0x03
)

var _ wire.TerminalSource = (*Service)(nil)

func (s *Service) Serve(w http.ResponseWriter, r *http.Request, conv, term string) error {
	s.mu.Lock()
	_, err := s.get(conv)
	known := term == "" || s.terms[conv][term] != 0
	s.mu.Unlock()
	if err != nil || !known {
		return convo.ErrNotFound
	}
	ws, aerr := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if aerr != nil {
		return nil
	}
	defer ws.CloseNow()
	name := "the agent's pane"
	if term != "" {
		name = "terminal " + term
	}
	out := func(text string) bool {
		return ws.Write(r.Context(), websocket.MessageBinary, append([]byte{termStdout}, text...)) == nil
	}
	if !out("fake " + name + "\r\n$ ") {
		return nil
	}
	for {
		typ, data, rerr := ws.Read(r.Context())
		if rerr != nil {
			return nil
		}
		if typ != websocket.MessageBinary || len(data) == 0 {
			continue
		}
		switch data[0] {
		case termStdin:
			echo := strings.ReplaceAll(string(data[1:]), "\r", "\r\n")
			if strings.Contains(string(data[1:]), "\r") {
				echo += "$ "
			}
			if !out(echo) {
				return nil
			}
		case termHeartbeat:
			if ws.Write(r.Context(), websocket.MessageBinary, []byte{termHeartbeat}) != nil {
				return nil
			}
		}
	}
}

func (s *Service) List(_ context.Context, conv string) ([]wire.Terminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(conv); err != nil {
		return nil, err
	}
	out := []wire.Terminal{}
	for id, at := range s.terms[conv] {
		out = append(out, wire.Terminal{ID: id, CreatedAt: wire.Ms(at)})
	}
	sortTerminals(out)
	return out, nil
}

func (s *Service) Create(_ context.Context, conv string) (wire.Terminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(conv); err != nil {
		return wire.Terminal{}, err
	}
	if len(s.terms[conv]) >= wire.TerminalsMax {
		return wire.Terminal{}, wire.ErrTerminalLimit
	}
	if s.terms == nil {
		s.terms = map[string]map[string]int64{}
	}
	if s.terms[conv] == nil {
		s.terms[conv] = map[string]int64{}
	}
	s.ids++
	now := time.Now().UnixMilli()
	id := strconv.FormatInt(now, 36) + "f" + strconv.FormatInt(s.ids%36, 36) + "k"
	s.terms[conv][id] = now
	return wire.Terminal{ID: id, CreatedAt: wire.Ms(now)}, nil
}

func (s *Service) Close(_ context.Context, conv, term string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(conv); err != nil {
		return err
	}
	if s.terms[conv][term] == 0 {
		return convo.ErrNotFound
	}
	delete(s.terms[conv], term)
	return nil
}

func sortTerminals(list []wire.Terminal) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && (list[j].CreatedAt < list[j-1].CreatedAt || list[j].CreatedAt == list[j-1].CreatedAt && list[j].ID < list[j-1].ID); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
