// Package httpx is the hub's HTTP plumbing: JSON bodies, the error shape of docs/hub/api.md, client addresses.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Error is a failure with the HTTP status and the machine-readable code api.md names. Message is Russian, for people.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// New makes an Error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// With returns a copy of e that also carries details[key] = value.
func (e *Error) With(key string, value any) *Error {
	c := *e
	c.Details = make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		c.Details[k] = v
	}
	c.Details[key] = value
	return &c
}

// The errors of api.md, ready to return.
var (
	ErrUnauthenticated = New(http.StatusUnauthorized, "unauthenticated", "Войдите, чтобы продолжить")
	ErrForbidden       = New(http.StatusForbidden, "forbidden", "Нет доступа")
	ErrCrossSite       = New(http.StatusForbidden, "cross_site", "Запрос с другого сайта")
	ErrSignupClosed    = New(http.StatusForbidden, "signup_closed", "Регистрация только по приглашению")
	ErrNotFound        = New(http.StatusNotFound, "not_found", "Не найдено")
	ErrExpired         = New(http.StatusGone, "expired", "Срок действия истёк")
	ErrProviderOff     = New(http.StatusUnprocessableEntity, "provider_off", "Этот способ не включён")
	ErrMachineOffline  = New(http.StatusBadGateway, "machine_offline", "Машина не на связи")
	ErrMachineTimeout  = New(http.StatusGatewayTimeout, "machine_timeout", "Машина не ответила вовремя")
	ErrInternal        = New(http.StatusInternalServerError, "internal", "Что-то пошло не так")
)

// ErrMachineUntrusted is a machine that may not run on the hub's own origin: a hub without HUB_RUNTIME_DOMAIN opens only
// the machines its administrators connected.
var ErrMachineUntrusted = New(http.StatusForbidden, "machine_untrusted",
	"Хаб работает без отдельного адреса для машин (HUB_RUNTIME_DOMAIN), поэтому открывает только машины, подключённые его администратором. Администратору нужно задать HUB_RUNTIME_DOMAIN")

// Invalid is a 400 invalid_request about one field.
func Invalid(field, message string) *Error {
	return New(http.StatusBadRequest, "invalid_request", message).With("field", field)
}

// Conflict is a 409 conflict with the reason in details.
func Conflict(reason, message string) *Error {
	return New(http.StatusConflict, "conflict", message).With("reason", reason)
}

// RateLimited is a 429 with Retry-After seconds.
func RateLimited(retryAfterSeconds int) *Error {
	return New(http.StatusTooManyRequests, "rate_limited", "Слишком много попыток, подождите").With("retryAfter", retryAfterSeconds)
}

type errorBody struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteJSON answers status with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Route names a request for a log: the pattern of the route that took it (/api/invites/{token}), not its path, which
// can carry a secret (an invitation token is the last segment of the address it travels in). A request that no route
// has taken (it has not reached the mux, or it is a machine's own origin) is named by SafePath.
func Route(r *http.Request) string {
	if p := r.Pattern; p != "" {
		if _, path, ok := strings.Cut(p, " "); ok {
			p = path
		}
		return p
	}
	return SafePath(r.URL.Path)
}

// SafePath is a path with every segment that could be a secret replaced by :redacted: any long run of letters, digits,
// dashes and underscores, which is what a token, a ticket or a key looks like. Short words (api, machines, events)
// stay, so the line still says what was asked.
func SafePath(path string) string {
	parts := strings.Split(path, "/")
	for i, seg := range parts {
		if len(seg) >= 16 && strings.Trim(seg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.~=") == "" {
			parts[i] = ":redacted"
		}
	}
	return strings.Join(parts, "/")
}

// WriteError answers err in the api.md shape; anything that is not an *Error is a 500 whose text stays in the log.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if !errors.As(err, &he) && errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		// The caller went away while the request was served (a closed tab, an agent that redialled): nobody to answer, and
		// not a failure of the hub.
		slog.Debug("request abandoned by its caller", "method", r.Method, "route", Route(r))
		return
	}
	if !errors.As(err, &he) {
		slog.Error("request failed", "method", r.Method, "route", Route(r), "error", err)
		he = ErrInternal
	}
	if he.Status == http.StatusTooManyRequests {
		if n, ok := he.Details["retryAfter"].(int); ok {
			w.Header().Set("Retry-After", strconv.Itoa(n))
		}
	}
	WriteJSON(w, he.Status, errorBody{Error: he.Code, Message: he.Message, Details: he.Details})
}

// NoContent answers 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Handler is a handler that returns its failure.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

// MaxBody bounds a JSON request body.
const MaxBody = 1 << 20

// Decode reads the JSON body of r into v. An empty body is accepted as {} so that "{}" and nothing mean the same;
// unknown fields are ignored so that the UI may send more than the hub reads.
func Decode(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, MaxBody)
	b, err := io.ReadAll(body)
	if err != nil {
		return New(http.StatusBadRequest, "invalid_request", "Не удалось прочитать запрос")
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		b = []byte("{}")
	}
	if err := json.Unmarshal(b, v); err != nil {
		return New(http.StatusBadRequest, "invalid_request", "Некорректный JSON")
	}
	return nil
}

// IsJSON says whether the request declares a JSON body.
func IsJSON(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

// ClientIP is the address of the person behind r. X-Forwarded-For is believed only when the connection itself comes
// from a trusted proxy, and then the right-most address that is not itself a trusted proxy wins.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !contains(trusted, peer) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			continue
		}
		a = a.Unmap()
		if !contains(trusted, a) {
			return a.String()
		}
	}
	return peer.String()
}

func contains(prefixes []netip.Prefix, a netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// TrustedForwardedProto says whether the request reached a trusted proxy over https.
func TrustedForwardedProto(r *http.Request, trusted []netip.Prefix) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a, err := netip.ParseAddr(host); err == nil && contains(trusted, a.Unmap()) {
		if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
			return p
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// Time formats a time the way the hub API sends times: RFC 3339 in UTC.
func Time(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// TimePtr is Time for an optional time; nil stays nil (JSON null).
func TimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := Time(*t)
	return &s
}
