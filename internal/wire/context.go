package wire

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The agent's window: Claude's usual one, and the long one ([1m]) a model may run with.
const (
	ContextWindow     = 200_000
	ContextWindowLong = 1_000_000
)

// ConvContext is the answer of GET /api/v2/conversations/{id}/context: how full the agent's window is — the tokens its
// last answer was asked with (0 before the first) of its window.
type ConvContext struct {
	UsedTokens  int64 `json:"usedTokens"`
	LimitTokens int64 `json:"limitTokens"`
}

func (c ConvContext) Validate() error {
	var errs []error
	if c.UsedTokens < 0 {
		errs = append(errs, errors.New("usedTokens: below 0"))
	}
	if c.LimitTokens < 1 {
		errs = append(errs, errors.New("limitTokens: below 1"))
	}
	return errors.Join(errs...)
}

// context serves GET /api/v2/conversations/{id}/context.
func (s *Server) context(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Context == nil {
		refuse(w, RefuseNotFound)
		return
	}
	out, err := s.Context(ctx, conv)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		jsonlog.Log("warn", "v2: context failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		s.writeChecked(w, r, out)
	}
}
