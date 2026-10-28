package link

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

var (
	errBadDial     = httpx.New(http.StatusUnauthorized, "unauthenticated", "Подпись не принята")
	errRevoked     = httpx.New(http.StatusForbidden, "forbidden", "Машина отозвана").With("reason", "revoked")
	errDialFlooded = httpx.RateLimited(60)
)

const (
	// maxNonces bounds what is remembered per machine: a legitimate agent dials a few times a minute at the very most.
	maxNonces = 256
	// failedDials is how many bad dials one address may make in failWindow.
	failedDials = 60
	failWindow  = 10 * time.Minute
)

// Hooks are what the hub does around a link. They run off the link's goroutines; Event hooks one at a time per link.
type Hooks struct {
	// Connected runs once the link is registered.
	Connected func(l *Link, m store.Machine)
	// Event runs for each event the agent sends, in order.
	Event func(l *Link, m store.Machine, ev Event)
	// Unlink runs when a machine asked, signed with its own key, to be removed (DELETE /agent/v1): the hub revokes it.
	Unlink func(r *http.Request, m store.Machine) error
	// Disconnected runs when the link ends; current says it was still the machine's link (not replaced by a newer one).
	Disconnected func(l *Link, m store.Machine, current bool)
}

// Dialer is the HTTP handler of GET /agent/v1: it checks the signed dial and runs the link.
type Dialer struct {
	DB      *store.DB
	Reg     *Registry
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	Now     func() time.Time
	Hooks   Hooks
	// Host is the host the agents dial, in the spelling of hubproto.DialHost (from the hub's public URL).
	Host string
	// MinAgentVersion refuses older agents (426); empty accepts all.
	MinAgentVersion string
	// ClientIP gives the address of whoever made a request, the trusted proxies applied.
	ClientIP func(r *http.Request) string

	mu     sync.Mutex
	nonces map[string]map[string]time.Time
}

func (d *Dialer) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dialer) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

// remember records a nonce of a machine; false when it was seen inside the memory (a replayed dial).
func (d *Dialer) remember(machineID, nonce string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.nonces == nil {
		d.nonces = map[string]map[string]time.Time{}
	}
	seen := d.nonces[machineID]
	if seen == nil {
		seen = map[string]time.Time{}
		d.nonces[machineID] = seen
	}
	for n, at := range seen {
		if now.Sub(at) > hubproto.NonceMemory {
			delete(seen, n)
		}
	}
	if _, replay := seen[nonce]; replay || len(seen) >= maxNonces {
		return false
	}
	seen[nonce] = now
	return true
}

// Sweep forgets the nonces of machines that have not dialed lately; call it now and then.
func (d *Dialer) Sweep() {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, seen := range d.nonces {
		for n, at := range seen {
			if now.Sub(at) > hubproto.NonceMemory {
				delete(seen, n)
			}
		}
		if len(seen) == 0 {
			delete(d.nonces, id)
		}
	}
}

func (d *Dialer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m, err := d.admit(r, hubproto.VerifyDial, true)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	protocol, _ := strconv.Atoi(r.Header.Get("X-Harnessly-Protocol"))
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return // Accept has answered
	}
	l := New(conn, m.ID, d.log())
	l.AgentVersion, l.Protocol = strings.TrimSpace(r.Header.Get("X-Harnessly-Agent-Version")), protocol
	if protocol < hubproto.Version {
		d.log().Info("agent speaks an old protocol, closing", "machine", m.ID, "protocol", protocol)
		_ = conn.Close(websocket.StatusCode(hubproto.CloseProtocolOld), "protocol 2 required")
		return
	}
	d.Reg.Add(l)
	d.log().Info("agent linked", "machine", m.ID, "version", l.AgentVersion)
	if d.Hooks.Connected != nil {
		go d.Hooks.Connected(l, m)
	}
	err = l.Serve(context.Background(), func(ev Event) {
		if d.Hooks.Event != nil {
			d.Hooks.Event(l, m, ev)
		}
	})
	current := d.Reg.Remove(l)
	d.log().Info("agent link ended", "machine", m.ID, "current", current, "reason", describe(err))
	if d.Hooks.Disconnected != nil {
		d.Hooks.Disconnected(l, m, current)
	}
}

// ServeUnlink is the handler of DELETE /agent/v1: the machine, proving itself with the signature of its own key,
// asks to be removed from the hub, which is what `harnessly-agent unlink` and the installer's --uninstall do. An
// answer of 204 means the machine is no longer the hub's; a machine already removed answers 403.
func (d *Dialer) ServeUnlink(w http.ResponseWriter, r *http.Request) {
	m, err := d.admit(r, hubproto.VerifyUnlink, false)
	if err == nil && d.Hooks.Unlink != nil {
		err = d.Hooks.Unlink(r, m)
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func describe(err error) string {
	if err == nil {
		return ""
	}
	if code := websocket.CloseStatus(err); code != -1 {
		return "closed " + strconv.Itoa(int(code))
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return err.Error()
}

// admit checks the signed dial: the machine, its key, the time, the host and a nonce not seen before.
func (d *Dialer) admit(r *http.Request, verify func(ed25519.PublicKey, hubproto.DialAuth, string, time.Time) error, checkVersion bool) (store.Machine, error) {
	ip := ratelimit.IPKey(d.ClientIP(r))
	if ok, _ := d.Limiter.Peek("dialfail:"+ip, failedDials, failWindow); !ok {
		return store.Machine{}, errDialFlooded
	}
	bad := func(reason string, machineID string) (store.Machine, error) {
		d.Limiter.Allow("dialfail:"+ip, failedDials, failWindow)
		d.log().Info("agent dial refused", "reason", reason, "machine", machineID, "ip", d.ClientIP(r))
		return store.Machine{}, errBadDial
	}
	a, err := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
	if err != nil {
		return bad("header", "")
	}
	m, err := d.DB.S().MachineByID(r.Context(), a.MachineID)
	if errors.Is(err, store.ErrNotFound) {
		return bad("unknown machine", a.MachineID)
	}
	if err != nil {
		return store.Machine{}, err
	}
	pub, err := base64.StdEncoding.DecodeString(m.KeyPub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return bad("stored key", m.ID)
	}
	now := d.now()
	if err := verify(ed25519.PublicKey(pub), a, d.Host, now); err != nil {
		return bad(err.Error(), m.ID)
	}
	// The signature is good: from here on it is the machine's owner talking, and what is said may be honest.
	if m.RevokedAt != nil {
		return store.Machine{}, errRevoked
	}
	if !d.remember(m.ID, a.Nonce, now) {
		return bad("replayed nonce", m.ID)
	}
	if v := strings.TrimSpace(r.Header.Get("X-Harnessly-Agent-Version")); checkVersion && d.MinAgentVersion != "" && OlderThan(v, d.MinAgentVersion) {
		return store.Machine{}, httpx.New(hubproto.StatusUpgradeAgent, "upgrade_required", "Обновите агента: нужна версия "+d.MinAgentVersion+" или новее")
	}
	return m, nil
}

// OlderThan says whether version a is older than min, comparing dotted numbers ("v1.4.2" and "1.4.2-dev" read as
// 1.4.2). A version that is not numbers (a "dev" build) is never older.
func OlderThan(a, min string) bool {
	av, ok1 := parseVersion(a)
	mv, ok2 := parseVersion(min)
	if !ok1 || !ok2 {
		return false
	}
	for i := range av {
		if av[i] != mv[i] {
			return av[i] < mv[i]
		}
	}
	return false
}

func parseVersion(s string) (v [3]int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return v, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
