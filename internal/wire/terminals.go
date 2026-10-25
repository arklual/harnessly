package wire

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// ErrTerminalLimit is a conversation that already has TerminalsMax own terminals.
var ErrTerminalLimit = errors.New("wire: the conversation has too many terminals")

// TerminalSource is what the machine gives the pages that show a conversation's terminals: its agent's pane, and shells
// of its own. A source reports a conversation or a terminal it does not have as an error the Server's NotFound knows.
type TerminalSource interface {
	// Serve runs the terminal socket of the conversation's agent pane (terminal == "") or of one of its own terminals
	// until either side closes it. It checks what it was asked for before it upgrades the request: an error is answered
	// as a refusal, and nothing is written by then.
	Serve(w http.ResponseWriter, r *http.Request, conversation, terminal string) error
	List(ctx context.Context, conversation string) ([]Terminal, error)
	Create(ctx context.Context, conversation string) (Terminal, error)
	// Close ends an own terminal and its shell.
	Close(ctx context.Context, conversation, terminal string) error
}

// terminalSocket serves GET /api/v2/terminal?conversation=<id>[&terminal=<id>][&cols=&rows=&clientId=]: a WebSocket that
// speaks the terminal protocol (README, item 39).
func (s *Server) terminalSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	conv, term := q.Get("conversation"), q.Get("terminal")
	switch {
	case s.Terminals == nil:
		refuse(w, RefuseNotFound)
		return
	case !ValidConvID(conv), term != "" && !ValidTerminalID(term):
		refuse(w, RefuseBadRequest)
		return
	}
	if err := s.Terminals.Serve(w, r, conv, term); err != nil {
		s.terminalRefusal(w, r, err)
	}
}

// terminals serves the own terminals of a conversation: GET lists, POST makes one, DELETE .../{id} ends one.
func (s *Server) terminals(w http.ResponseWriter, r *http.Request, conv string, rest []string) {
	if s.Terminals == nil {
		refuse(w, RefuseNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		list, err := s.Terminals.List(ctx, conv)
		if err != nil {
			s.terminalRefusal(w, r, err)
			return
		}
		out := Terminals(list)
		if out == nil {
			out = Terminals{}
		}
		s.writeChecked(w, r, out)
	case len(rest) == 0 && r.Method == http.MethodPost:
		t, err := s.Terminals.Create(ctx, conv)
		if err != nil {
			s.terminalRefusal(w, r, err)
			return
		}
		s.writeChecked(w, r, t)
	case len(rest) == 1 && r.Method == http.MethodDelete:
		if !ValidTerminalID(rest[0]) {
			refuse(w, RefuseBadRequest)
			return
		}
		if err := s.Terminals.Close(ctx, conv, rest[0]); err != nil {
			s.terminalRefusal(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(rest) > 1:
		refuse(w, RefuseNotFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) terminalRefusal(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case errors.Is(err, ErrTerminalLimit):
		refuse(w, RefuseConflict)
	case errors.Is(err, context.DeadlineExceeded):
		refuse(w, RefuseTimeout)
	default:
		agent.Log("warn", "v2: terminals failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}

// writeChecked answers 200 with v, or an internal refusal when v is out of contract.
func (s *Server) writeChecked(w http.ResponseWriter, r *http.Request, v interface{ Validate() error }) {
	if verr := v.Validate(); verr != nil {
		agent.Log("error", "v2: an answer out of contract", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: verr.Error()})
		refuse(w, RefuseInternal)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
