package automations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A person's incoming webhook (docs/connectors.md, «Incoming webhook»): POST <automations>/hook/m/<machine>/<connection>
// [/<secret>]. The hub keeps nothing of the request: it passes the body and the headers that prove where it came from
// over the machine's link (connectors.webhook), where the runtime checks the signature by the connection's secret from
// the machine's safe, keeps the body for the agent and makes the event without text that comes back to the rules as
// automations.event. Here: a size limit, a rate limit per address, and answers without details. The body and the path
// are never logged; the hub holds them only for the time of the call.

const (
	webhookMaxBody = 1 << 20
	webhookTimeout = 15 * time.Second
	// webhookRate requests a minute per address, webhookBurst at once.
	webhookRate  = 60
	webhookBurst = 30
)

var (
	webhookMachineRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	webhookConnRe    = regexp.MustCompile(`^cx[0-9a-f]{12}$`)
	webhookSecretRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// webhookHeaders are passed to the machine: what the presets read and check. Nothing else of the request goes.
var webhookHeaders = []string{"Content-Type", "User-Agent", "Authorization",
	"X-Hub-Signature-256", "X-GitHub-Event", "X-GitHub-Delivery",
	"X-Gitlab-Token", "X-Gitlab-Event", "X-Gitlab-Event-UUID", "Idempotency-Key",
	"X-Grafana-Alerting-Signature", "X-Grafana-Alerting-Signature-Timestamp",
	"Sentry-Hook-Signature", "Sentry-Hook-Timestamp", "Sentry-Hook-Resource", "Request-ID",
	"X-Signature-256", "X-Signature", "X-Webhook-Signature", "X-Request-Id", "X-Delivery-Id"}

// webhookStatuses are the answers a sender may get; anything else from a machine is 503.
var webhookStatuses = map[int]bool{http.StatusOK: true, http.StatusAccepted: true, http.StatusBadRequest: true,
	http.StatusUnauthorized: true, http.StatusRequestEntityTooLarge: true, http.StatusServiceUnavailable: true}

type bucket struct {
	tokens float64
	at     time.Time
}

// limiter is a token bucket per webhook address, in memory: a flood of one address does not reach the machine.
type limiter struct {
	mu sync.Mutex
	m  map[string]*bucket
}

var webhookLimits = &limiter{m: map[string]*bucket{}}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 10000 {
		for k, b := range l.m {
			if now.Sub(b.at) > 10*time.Minute {
				delete(l.m, k)
			}
		}
	}
	b := l.m[key]
	if b == nil {
		b = &bucket{tokens: webhookBurst, at: now}
		l.m[key] = b
	}
	b.tokens = min(webhookBurst, b.tokens+now.Sub(b.at).Minutes()*webhookRate)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func webhookAnswer(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	switch status {
	case http.StatusOK, http.StatusAccepted:
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		_, _ = w.Write([]byte(`{"ok":false}`))
	}
}

// webhook takes a request to a person's webhook address and passes it to the machine.
func (s *Service) webhook(w http.ResponseWriter, r *http.Request) {
	machine, conn, secret := r.PathValue("machine"), r.PathValue("conn"), r.PathValue("secret")
	if !webhookMachineRe.MatchString(machine) || !webhookConnRe.MatchString(conn) || (secret != "" && !webhookSecretRe.MatchString(secret)) {
		webhookAnswer(w, http.StatusUnauthorized)
		return
	}
	if !webhookLimits.allow(machine+"/"+conn, s.now()) {
		w.Header().Set("Retry-After", "60")
		webhookAnswer(w, http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookMaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			webhookAnswer(w, http.StatusRequestEntityTooLarge)
			return
		}
		webhookAnswer(w, http.StatusBadRequest)
		return
	}
	if s.links == nil {
		webhookAnswer(w, http.StatusServiceUnavailable)
		return
	}
	header := map[string]string{}
	for _, name := range webhookHeaders {
		if v := r.Header.Get(name); v != "" && len(v) <= 2048 {
			header[name] = v
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), webhookTimeout)
	defer cancel()
	raw, err := s.links.Do(ctx, machine, hubproto.OpConnectorsWebhook,
		map[string]any{"connection": conn, "path": secret, "header": header, "body": body}, webhookTimeout)
	if err != nil {
		// The machine is away or refused: the sender tries again later. The reason stays out of the answer.
		s.log.Info("webhook not passed to the machine", "machine", machine, "connection", conn, "error", err.Error())
		webhookAnswer(w, http.StatusServiceUnavailable)
		return
	}
	var out struct {
		Status int `json:"status"`
	}
	if json.Unmarshal(raw, &out) != nil || !webhookStatuses[out.Status] {
		out.Status = http.StatusServiceUnavailable
	}
	s.log.Info("webhook passed to the machine", "machine", machine, "connection", conn, "status", out.Status)
	webhookAnswer(w, out.Status)
}
