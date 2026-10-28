package wire

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Search over the messages of the conversations: the person's lines and the agent's answers, not the tool output.

// MessageRole is who wrote a message found.
type MessageRole string

const (
	MessageUser  MessageRole = "user"
	MessageAgent MessageRole = "agent"
)

// MessageRoles is every MessageRole, in the schema's order.
var MessageRoles = []MessageRole{MessageUser, MessageAgent}

// The query of a search: at least two letters, at most 200 bytes; a page asks for up to SearchLimitMax hits.
const (
	SearchQueryMax   = 200
	SearchLimitMax   = 100
	SearchLimitUsual = 40
)

// MessageHit is one message found: the conversation, the entry of the feed, the turn the entry is in (null for one
// outside any), and the words around the match.
type MessageHit struct {
	ConvID  string      `json:"convId"`
	EntryID string      `json:"entryId"`
	TurnID  *string     `json:"turnId"`
	At      Ms          `json:"at"`
	Role    MessageRole `json:"role"`
	Snippet string      `json:"snippet"`
}

func (h MessageHit) Validate() error {
	err := errors.Join(need("convId", h.ConvID), need("entryId", h.EntryID), in("role", h.Role, MessageRoles), needPtr("turnId", h.TurnID))
	if h.ConvID != "" && !ValidConvID(h.ConvID) {
		err = errors.Join(err, errors.New("convId: not a conversation id"))
	}
	if h.EntryID != "" && !ValidEntryID(h.EntryID) {
		err = errors.Join(err, errors.New("entryId: not an entry id"))
	}
	return err
}

// MessageSearch is the answer of GET /api/v2/search?q=&limit=: the messages that hold every word of the query (each
// word may be the beginning of a word), the newest first, at most three of a conversation. Indexing: the runtime is
// still reading the older conversations, so the answer may lack some.
type MessageSearch struct {
	Query    string       `json:"query"`
	Hits     []MessageHit `json:"hits"`
	Indexing bool         `json:"indexing"`
}

func (m MessageSearch) Validate() error { return each("hits", m.Hits) }

// ChatDigest is the answer of GET /api/v2/conversations/{id}/digest?budget=: what a chat said, cut to the size of a
// context block for another agent. Text holds the title, the first request, the latest turns and the key results;
// Path is the whole conversation as a Markdown file on the machine, which the agent may read for more. Truncated:
// Text leaves out parts of the conversation.
type ChatDigest struct {
	ConvID    string `json:"convId"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Path      string `json:"path"`
	Truncated bool   `json:"truncated"`
}

func (c ChatDigest) Validate() error {
	return errors.Join(need("convId", c.ConvID), need("path", c.Path))
}

// DigestBudgetMax is the largest budget a page may ask for, in characters; DigestBudgetUsual is what it gets without.
const (
	DigestBudgetMax   = 60000
	DigestBudgetUsual = 18000
)

// search serves GET /api/v2/search.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Search == nil {
		refuse(w, RefuseNotFound)
		return
	}
	q := r.URL.Query()
	query, limit := q.Get("q"), SearchLimitUsual
	if len(query) > SearchQueryMax || !utf8.ValidString(query) {
		refuse(w, RefuseBadRequest)
		return
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > SearchLimitMax {
			refuse(w, RefuseBadRequest)
			return
		}
		limit = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.Limits.CmdBudgetMs)*time.Millisecond)
	defer cancel()
	out, err := s.Search(ctx, query, limit)
	switch {
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		jsonlog.Log("warn", "v2: search failed", jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		if out.Hits == nil {
			out.Hits = []MessageHit{}
		}
		out.Query = query
		s.writeChecked(w, r, out)
	}
}

// digest serves GET /api/v2/conversations/{id}/digest.
func (s *Server) digest(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Digest == nil {
		refuse(w, RefuseNotFound)
		return
	}
	budget := DigestBudgetUsual
	if b := r.URL.Query().Get("budget"); b != "" {
		n, err := strconv.Atoi(b)
		if err != nil || n < 1000 || n > DigestBudgetMax {
			refuse(w, RefuseBadRequest)
			return
		}
		budget = n
	}
	out, err := s.Digest(ctx, conv, budget)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		jsonlog.Log("warn", "v2: digest failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		s.writeChecked(w, r, out)
	}
}
