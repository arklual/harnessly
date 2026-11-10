package journal

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The headers the page sends with its requests: the trace of the action they belong to and the chat it is in.
const (
	TraceHeader = "X-Hl-Trace"
	ChatHeader  = "X-Hl-Chat"
)

// Scope is what a request knows of where it belongs; a handler deeper in may learn the chat (an agent's MCP call is told
// by its connection).
type Scope struct {
	mu    sync.Mutex
	trace string
	chat  string
	app   string
	view  string
	tool  string
	note  string
	level string
}

type scopeKey struct{}

// WithScope is ctx with a scope of the trace and chat given (either may be "").
func WithScope(ctx context.Context, trace, chat string) (context.Context, *Scope) {
	s := &Scope{trace: short(trace), chat: short(chat)}
	return context.WithValue(ctx, scopeKey{}, s), s
}

// ScopeOf is the scope of a request's context, nil when there is none.
func ScopeOf(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeKey{}).(*Scope)
	return s
}

// SetChat tells the scope of ctx the chat (no scope: nothing).
func SetChat(ctx context.Context, chat string) {
	if s := ScopeOf(ctx); s != nil && chat != "" {
		s.mu.Lock()
		s.chat = short(chat)
		s.mu.Unlock()
	}
}

// SetApp tells the scope of ctx the app.
func SetApp(ctx context.Context, app string) {
	if s := ScopeOf(ctx); s != nil && app != "" {
		s.mu.Lock()
		s.app = short(app)
		s.mu.Unlock()
	}
}

// SetCall tells the scope of ctx what the request is about: the app's view, the tool or method it calls and, for a
// failure, a few words of why (never a value or a body).
func SetCall(ctx context.Context, view, tool, note string) {
	if s := ScopeOf(ctx); s != nil {
		s.mu.Lock()
		if view != "" {
			s.view = short(view)
		}
		if tool != "" {
			s.tool = short(tool)
		}
		if note != "" {
			s.note = note
		}
		s.mu.Unlock()
	}
}

// SetLevel raises the level of the request's line: a refusal answered with 200 (a v2 command's) is a warning still.
func SetLevel(ctx context.Context, level string) {
	if s := ScopeOf(ctx); s != nil {
		s.mu.Lock()
		if rank(level) > rank(s.level) {
			s.level = level
		}
		s.mu.Unlock()
	}
}

func (s *Scope) raised() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.level
}

func (s *Scope) call() (view, tool, note string) {
	if s == nil {
		return "", "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view, s.tool, s.note
}

// Of is the trace, chat and app of a scope.
func (s *Scope) Of() (trace, chat, app string) {
	if s == nil {
		return "", "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trace, s.chat, s.app
}

// Fill puts the scope of ctx into a line's empty trace, chat and app.
func Fill(ctx context.Context, e Entry) Entry {
	trace, chat, app := ScopeOf(ctx).Of()
	if e.Trace == "" {
		e.Trace = trace
	}
	if e.Chat == "" {
		e.Chat = chat
	}
	if e.App == "" {
		e.App = app
	}
	return e
}

// NewTrace is a fresh trace id.
func NewTrace(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// HTTPOptions say which requests the middleware writes down.
type HTTPOptions struct {
	// Quiet is a request whose success is not written (polls, streams, the journal's own routes); its failure is.
	Quiet func(r *http.Request) bool
	// Slow is how long a GET takes to be written as info (default 3 s).
	Slow time.Duration
}

// HTTP writes down the runtime's requests: a failure (4xx a warning, 5xx an error), a request that changes something
// (not GET, HEAD or OPTIONS) and a slow GET as info. A socket (a hijacked connection) and a stream are not written. The
// request's context carries a scope with the page's trace and chat.
func HTTP(next http.Handler, opts HTTPOptions) http.Handler {
	if opts.Slow <= 0 {
		opts.Slow = 3 * time.Second
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := Default()
		if j == nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx, scope := WithScope(r.Context(), r.Header.Get(TraceHeader), r.Header.Get(ChatHeader))
		rec := &recorder{ResponseWriter: w, status: 200}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))
		if rec.hijacked || rec.streamed {
			return
		}
		dur := time.Since(start)
		level := ""
		switch {
		case rec.status >= 500:
			level = Error
		case rec.status >= 400:
			level = Warn
		case opts.Quiet != nil && opts.Quiet(r):
		case r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions:
			level = Info
		case dur >= opts.Slow:
			level = Info
		}
		if up := scope.raised(); up != "" && (level == "" || rank(up) > rank(level)) {
			level = up
		}
		if level == "" {
			return
		}
		trace, chat, app := scope.Of()
		view, tool, note := scope.call()
		j.Add(Entry{At: start.UnixMilli(), Level: level, Source: SourceRuntime, Event: "http", Method: r.Method,
			URL: r.URL.Path, Status: rec.status, Dur: dur.Milliseconds(), Trace: trace, Chat: chat, App: app,
			View: view, Tool: tool, Text: note})
	})
}

type recorder struct {
	http.ResponseWriter
	status   int
	wrote    bool
	hijacked bool
	streamed bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
		if strings.HasPrefix(r.Header().Get("Content-Type"), "text/event-stream") {
			r.streamed = true
		}
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	r.streamed = true
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("journal: the response cannot be hijacked")
	}
	r.hijacked = true
	return h.Hijack()
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
