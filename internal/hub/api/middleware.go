package api

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// statusWriter remembers the status of a response for the access log. It passes on everything a response can do
// beyond writing: flushing (event streams), taking the connection over (WebSocket), and what http.ResponseController
// reaches through Unwrap.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the response cannot be hijacked")
	}
	if !w.wrote {
		w.status, w.wrote = http.StatusSwitchingProtocols, true
	}
	return h.Hijack()
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// quiet are the paths too chatty to log when they go well.
func quiet(path string) bool {
	return path == "/healthz" || path == "/readyz" || strings.HasSuffix(path, "/events") || strings.HasPrefix(path, "/assets/")
}

// accessLog writes one line per request: method, route, status, time, address. The route is the pattern that took the
// request (/api/invites/{token}), never the raw path: an invitation token is a segment of it. Never the query (it can
// carry a ticket, an OAuth code or a state), the headers, or the body.
func accessLog(log *slog.Logger, clientIP func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		level := slog.LevelDebug
		switch {
		case sw.status >= 500:
			level = slog.LevelWarn
		case sw.status >= 400 && !quiet(r.URL.Path):
			level = slog.LevelInfo
		}
		log.Log(r.Context(), level, "request", "method", r.Method, "route", httpx.Route(r), "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "ip", clientIP(r))
	})
}

// recoverer turns a panic of a handler into a 500 and a log line with the stack, instead of a dropped connection.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("handler panicked", "method", r.Method, "route", httpx.Route(r), "panic", v, "stack", string(debug.Stack()))
				httpx.WriteError(w, r, httpx.ErrInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders are set on everything the hub answers itself. What a machine's runtime answers keeps its own.
func securityHeaders(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/m/") && !strings.HasPrefix(r.URL.Path, "/sites/") {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			if secure {
				// Not includeSubDomains: the machines' own origins are subdomains of another name, but a hub on a
				// subdomain of a bigger site should not decide for its siblings.
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
		}
		next.ServeHTTP(w, r)
	})
}
