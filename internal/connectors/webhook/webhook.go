// Package webhook is the incoming webhook connector (docs/connectors.md, «Incoming webhook»): a personal address on the
// hub that a service (GitHub, GitLab, Grafana, Alertmanager, Sentry, n8n, Zapier, Make, anything that posts JSON) sends
// its events to. The hub keeps nothing of a request: it passes it over the machine's link to the runtime
// (connectors.webhook), and here, on the machine, the signature is checked by the connection's secret from the safe,
// the body is kept under the connection's folder for the agent's reads, and the event becomes a webhook.received
// envelope without text, with its kind (alert.firing, ci.failed, pr.opened, issue.assigned…) as a fact.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "webhook"

// EventReceived is its one event type; the kind of what happened is the fact "kind".
const EventReceived = "webhook.received"

// Presets of the services it understands.
const (
	PresetGitHub       = "github"
	PresetGitLab       = "gitlab"
	PresetGrafana      = "grafana"
	PresetAlertmanager = "alertmanager"
	PresetSentry       = "sentry"
	PresetJSON         = "json"
)

// How a service proves a request is its own, for the presets that let the person choose.
const (
	AuthHeader = "header"
	AuthPath   = "path"
)

const (
	// MaxBody is the largest body taken; the hub refuses a larger one before it reaches the machine.
	MaxBody = 1 << 20
	// skew is how far a signed timestamp may be from now (Sentry, Grafana): an older request is a replay.
	skew = 5 * time.Minute
)

// Base is the public address of this machine's webhooks on its hub (https://hub.example.com/api/platform/automations/
// hook/m/<machine>), set by the runtime; "" while the machine is not linked to a hub.
var Base = func() string { return "" }

type hookConn struct{}

func init() { connectors.Register(hookConn{}) }

func (hookConn) Spec() connectors.Spec {
	jsonOnly := &connectors.When{Field: "preset", Values: []string{PresetJSON}}
	choice := func(v, en, ru string) connectors.Choice {
		return connectors.Choice{Value: v, Label: connectors.T(en, ru)}
	}
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Webhook,
		Title: connectors.T("Incoming webhook", "Входящий вебхук"),
		Summary: connectors.T("A personal address services send their events to: alerts, CI, pull requests, issues",
			"Личный адрес, на который сервисы шлют события: алерты, CI, пул-реквесты, задачи"),
		Fields: []connectors.Field{
			{ID: "preset", Type: connectors.FieldChoice, Default: PresetGitHub, Label: connectors.T("Source", "Источник"),
				Choices: []connectors.Choice{
					choice(PresetGitHub, "GitHub", "GitHub"),
					choice(PresetGitLab, "GitLab", "GitLab"),
					choice(PresetGrafana, "Grafana", "Grafana"),
					choice(PresetAlertmanager, "Alertmanager", "Alertmanager"),
					choice(PresetSentry, "Sentry", "Sentry"),
					choice(PresetJSON, "n8n, Zapier, Make or any JSON", "n8n, Zapier, Make или любой JSON"),
				}},
			{ID: "auth", Type: connectors.FieldChoice, Optional: true, Default: AuthHeader,
				When:  &connectors.When{Field: "preset", Values: []string{PresetGrafana, PresetAlertmanager, PresetJSON}},
				Label: connectors.T("Proof", "Проверка"),
				Hint: connectors.T("Grafana, Alertmanager and any JSON: a signature or token in a header, or the secret in the address",
					"Для Grafana, Alertmanager и любого JSON: подпись или токен в заголовке либо секрет в адресе"),
				Choices: []connectors.Choice{
					choice(AuthHeader, "Header", "Заголовок"),
					choice(AuthPath, "Secret in the address", "Секрет в адресе"),
				}},
			{ID: "titleField", Type: connectors.FieldText, Optional: true, Placeholder: "data.title", When: jsonOnly,
				Label: connectors.T("Title field", "Поле заголовка"),
				Hint:  connectors.T("Any JSON: the field to take as the title, dotted", "Для любого JSON: поле заголовка через точку")},
			{ID: "urlField", Type: connectors.FieldText, Optional: true, Placeholder: "data.url", When: jsonOnly,
				Label: connectors.T("Link field", "Поле ссылки"),
				Hint:  connectors.T("Any JSON: the field to take as the link", "Для любого JSON: поле ссылки")},
			{ID: "clientSecret", Type: connectors.FieldPassword, Secret: true, Optional: true,
				When:  &connectors.When{Field: "preset", Values: []string{PresetSentry}},
				Label: connectors.T("Sentry Client Secret", "Client Secret из Sentry"),
				Hint: connectors.T("Sentry signs with its integration's own secret: paste it here",
					"Sentry подписывает секретом своей интеграции: вставьте его сюда")},
			{ID: "secret", Type: connectors.FieldPassword, Secret: true, Generate: true,
				Label: connectors.T("Secret", "Секрет")},
		},
		Events: []string{EventReceived},
		Tools: []connectors.Tool{
			{Name: "events", Summary: "the latest events of the webhook: when, kind, title and id",
				Args: []connectors.Arg{{Name: "limit", Summary: "how many, 20 by default"}}},
			{Name: "event", Summary: "one event whole: the body the service sent and its headers",
				Args: []connectors.Arg{{Name: "id", Summary: "the event's id from events or from an event", Required: true}}},
		},
	}
}

// address is the connection's public address without a secret in it.
func address(in connectors.Instance) string {
	base := strings.TrimRight(Base(), "/")
	if base == "" {
		return ""
	}
	return base + "/" + in.ID
}

func preset(in connectors.Instance) string { return in.Config["preset"] }

// pathAuth says the secret is the last part of the address (a sender that cannot sign or set a header).
func pathAuth(in connectors.Instance) bool {
	switch preset(in) {
	case PresetGrafana, PresetAlertmanager, PresetJSON:
		return in.Config["auth"] == AuthPath
	}
	return false
}

func (hookConn) Check(_ context.Context, in connectors.Instance) (connectors.Account, error) {
	switch preset(in) {
	case PresetGitHub, PresetGitLab, PresetGrafana, PresetAlertmanager, PresetSentry, PresetJSON:
	default:
		return connectors.Account{}, connectors.Sayf("Choose the source")
	}
	if preset(in) == PresetSentry {
		if v, err := in.Secret("clientSecret"); err != nil || v == "" {
			return connectors.Account{}, connectors.Sayf("Sentry signs with its integration's Client Secret: paste it")
		}
	}
	for _, f := range []string{"titleField", "urlField"} {
		if v := in.Config[f]; v != "" && !fieldPathOK(v) {
			return connectors.Account{}, connectors.Sayf("%s: a field like data.title", f)
		}
	}
	addr := address(in)
	if addr == "" {
		return connectors.Account{}, connectors.Sayf("This machine is not linked to a hub: a webhook needs the hub's public address")
	}
	return connectors.Account{Label: addr}, nil
}

// Reveal is what the person sees once: the address to give the service, and the secret it signs with.
func (hookConn) Reveal(in connectors.Instance) ([]connectors.Shown, error) {
	secret, err := in.Secret("secret")
	if err != nil {
		return nil, err
	}
	addr := address(in)
	if pathAuth(in) {
		return []connectors.Shown{{Label: connectors.T("Address with the secret", "Адрес с секретом"), Value: addr + "/" + secret}}, nil
	}
	out := []connectors.Shown{{Label: connectors.T("Address", "Адрес"), Value: addr}}
	if preset(in) != PresetSentry {
		out = append(out, connectors.Shown{Label: connectors.T("Secret", "Секрет"), Value: secret})
	}
	return out, nil
}

// verify checks that a request comes from the service: a signature or a token by the connection's secret, compared
// in constant time. Any failure is ErrUnauthorized, without saying which part was wrong.
func verify(in connectors.Instance, req connectors.Incoming, now time.Time) error {
	key := "secret"
	if preset(in) == PresetSentry {
		key = "clientSecret"
	}
	secret, err := in.Secret(key)
	if err != nil || secret == "" {
		return connectors.ErrUnauthorized
	}
	h := req.Header
	if pathAuth(in) {
		return check(equal(req.Path, secret))
	}
	if req.Path != "" {
		return connectors.ErrUnauthorized
	}
	switch preset(in) {
	case PresetGitHub:
		got, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256=")
		return check(ok && signed(secret, req.Body, got))
	case PresetGitLab:
		return check(equal(h.Get("X-Gitlab-Token"), secret))
	case PresetSentry:
		if !fresh(h.Get("Sentry-Hook-Timestamp"), now) {
			return connectors.ErrUnauthorized
		}
		return check(signed(secret, req.Body, h.Get("Sentry-Hook-Signature")))
	case PresetGrafana:
		if sig := h.Get("X-Grafana-Alerting-Signature"); sig != "" {
			if ts := h.Get("X-Grafana-Alerting-Signature-Timestamp"); ts != "" {
				if !fresh(ts, now) {
					return connectors.ErrUnauthorized
				}
				return check(signed(secret, append([]byte(ts+":"), req.Body...), sig))
			}
			return check(signed(secret, req.Body, sig))
		}
		return check(bearer(h, secret))
	case PresetAlertmanager:
		return check(bearer(h, secret))
	case PresetJSON:
		for _, name := range []string{"X-Signature-256", "X-Hub-Signature-256", "X-Signature", "X-Webhook-Signature"} {
			if sig := h.Get(name); sig != "" {
				sig = strings.TrimPrefix(sig, "sha256=")
				return check(signed(secret, req.Body, sig))
			}
		}
		return check(bearer(h, secret))
	}
	return connectors.ErrUnauthorized
}

func check(ok bool) error {
	if ok {
		return nil
	}
	return connectors.ErrUnauthorized
}

func equal(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func bearer(h http.Header, secret string) bool {
	got, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer ")
	return ok && equal(strings.TrimSpace(got), secret)
}

// signed checks an HMAC-SHA256 of body, given in hex or base64.
func signed(secret string, body []byte, given string) bool {
	given = strings.TrimSpace(given)
	if given == "" {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	want := m.Sum(nil)
	if raw, err := hex.DecodeString(given); err == nil {
		return hmac.Equal(raw, want)
	}
	if raw, err := base64.StdEncoding.DecodeString(given); err == nil {
		return hmac.Equal(raw, want)
	}
	return false
}

// fresh says a signed timestamp (unix seconds or milliseconds) is near now; an absent one is not.
func fresh(ts string, now time.Time) bool {
	n, err := strconv.ParseFloat(strings.TrimSpace(ts), 64)
	if err != nil || n <= 0 {
		return false
	}
	if n > 1e12 {
		n /= 1000
	}
	at := time.Unix(int64(n), 0)
	return at.After(now.Add(-skew)) && at.Before(now.Add(skew))
}

// Receive checks a request, keeps it under the connection's folder and answers its events (none for a ping).
func (hookConn) Receive(_ context.Context, in connectors.Instance, req connectors.Incoming) ([]connectors.Event, error) {
	now := time.Now()
	if len(req.Body) > MaxBody {
		return nil, connectors.ErrBadArgs
	}
	if err := verify(in, req, now); err != nil {
		return nil, err
	}
	var body any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return nil, fmt.Errorf("%w: the body is not JSON", connectors.ErrBadArgs)
	}
	items := parse(in, req.Header, body)
	key := deliveryKey(req)
	st := store{dir: in.Dir}
	events, err := st.keep(key, now, req, items)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// deliveryKey names one request: the service's own delivery id, else the hash of the body. A repeat of either is a
// replay and is taken once.
func deliveryKey(req connectors.Incoming) string {
	id := ""
	for _, name := range []string{"X-GitHub-Delivery", "X-Gitlab-Event-UUID", "Request-ID", "Idempotency-Key", "X-Request-Id", "X-Delivery-Id"} {
		if v := strings.TrimSpace(req.Header.Get(name)); v != "" {
			id = name + ":" + v
			break
		}
	}
	sum := sha256.New()
	if id != "" {
		sum.Write([]byte(id))
	} else {
		sum.Write(req.Body)
	}
	return hex.EncodeToString(sum.Sum(nil))[:32]
}

func (hookConn) Read(_ context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	st := store{dir: in.Dir}
	switch tool {
	case "events":
		limit := 20
		if v, err := strconv.Atoi(args["limit"]); err == nil && v > 0 {
			limit = min(v, 200)
		}
		list, err := st.list(limit)
		if err != nil {
			return connectors.Result{}, err
		}
		if len(list) == 0 {
			return connectors.Result{Text: "no events yet"}, nil
		}
		var b strings.Builder
		for _, e := range list {
			fmt.Fprintf(&b, "%s  %s  id=%s", e.At.UTC().Format(time.RFC3339), e.Kind, e.Key)
			if e.Title != "" {
				fmt.Fprintf(&b, "  %s", e.Title)
			}
			if e.URL != "" {
				fmt.Fprintf(&b, "  %s", e.URL)
			}
			b.WriteString("\n")
		}
		return connectors.Result{Text: fmt.Sprintf("%d events, newest first", len(list)), Data: b.String(), From: "webhook " + in.Name}, nil
	case "event":
		key := strings.SplitN(args["id"], ":", 2)[0]
		if key == "" {
			return connectors.Result{}, connectors.ErrBadArgs
		}
		rec, err := st.get(key)
		if err != nil {
			return connectors.Result{}, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "received: %s\nkind: %s\n", rec.At.UTC().Format(time.RFC3339), rec.Kind)
		for _, k := range sortedKeys(rec.Header) {
			fmt.Fprintf(&b, "%s: %s\n", k, rec.Header[k])
		}
		b.WriteString("\n")
		b.Write(pretty(rec.Body))
		return connectors.Result{Text: "the request as the service sent it", Data: b.String(), From: "webhook " + in.Name}, nil
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

func pretty(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return out
}
