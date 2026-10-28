package sites

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TicketTTL is how long a ticket can be redeemed; a ticket opens one host, once.
const TicketTTL = 60 * time.Second

// SessionTTL is how long a site's cookie holds.
const SessionTTL = 12 * time.Hour

// Tickets trades a one-time ticket for the person it was made for. The hub backs it with its in-memory ticket store;
// a standalone server verifies tickets signed by the gateway that did the signing in (HMACTickets).
type Tickets interface {
	// Redeem is true once, for the host the ticket was made for.
	Redeem(ticket, host string) (userID string, ok bool)
}

var b64 = base64.RawURLEncoding

// sign is payload.signature, both base64url; the signature covers the purpose tag so that a ticket is never a cookie.
func sign(key []byte, purpose string, payload []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(payload)
	return b64.EncodeToString(payload) + "." + b64.EncodeToString(m.Sum(nil))
}

func verify(key []byte, purpose, signed string) ([]byte, bool) {
	p, s, ok := strings.Cut(signed, ".")
	if !ok {
		return nil, false
	}
	payload, err := b64.DecodeString(p)
	if err != nil {
		return nil, false
	}
	got, err := b64.DecodeString(s)
	if err != nil {
		return nil, false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(payload)
	if !hmac.Equal(got, m.Sum(nil)) {
		return nil, false
	}
	return payload, true
}

// HMACTickets are tickets signed with a key the gateway and the sites server share. The gateway authenticates the
// person by its own means, checks that they may open the site, and sends the browser to the site's address with the
// ticket; the server redeems it for a cookie.
type HMACTickets struct {
	key []byte
	now func() time.Time

	mu   sync.Mutex
	used map[string]time.Time // nonce → expiry
}

// NewHMACTickets makes the ticket service; now may be nil.
func NewHMACTickets(key []byte, now func() time.Time) *HMACTickets {
	if now == nil {
		now = time.Now
	}
	return &HMACTickets{key: key, now: now, used: map[string]time.Time{}}
}

type ticketBody struct {
	U string `json:"u"`
	H string `json:"h"`
	E int64  `json:"e"`
	N string `json:"n"`
}

// Issue makes a ticket for userID to open host.
func (t *HMACTickets) Issue(userID, host string) string {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	b, _ := json.Marshal(ticketBody{U: userID, H: HostName(host), E: t.now().Add(TicketTTL).Unix(), N: b64.EncodeToString(nonce)})
	return sign(t.key, "ticket", b)
}

// Redeem checks the signature, the host, the expiry and that the ticket has not been used.
func (t *HMACTickets) Redeem(ticket, host string) (string, bool) {
	payload, ok := verify(t.key, "ticket", ticket)
	if !ok {
		return "", false
	}
	var b ticketBody
	if json.Unmarshal(payload, &b) != nil || b.N == "" {
		return "", false
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for n, exp := range t.used {
		if !now.Before(exp) {
			delete(t.used, n)
		}
	}
	if _, seen := t.used[b.N]; seen {
		return "", false
	}
	// Used up whatever the outcome below: a wrong try burns the ticket.
	t.used[b.N] = time.Unix(b.E, 0)
	if b.H != HostName(host) || !now.Before(time.Unix(b.E, 0)) || b.U == "" {
		return "", false
	}
	return b.U, true
}

// Sessions are the cookies a site's origin keeps after a ticket: host-only, signed, bound to the host they were made for.
type Sessions struct {
	key    []byte
	secure bool
	now    func() time.Time
}

// NewSessions makes the cookie service; now may be nil.
func NewSessions(key []byte, secure bool, now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{key: key, secure: secure, now: now}
}

// CookieName is __Host-hl_site on https (host-only, Path=/, Secure: a sibling site cannot plant it), hl_site on http.
func (s *Sessions) CookieName() string {
	if s.secure {
		return "__Host-hl_site"
	}
	return "hl_site"
}

type sessionBody struct {
	U string `json:"u"`
	H string `json:"h"`
	E int64  `json:"e"`
}

// Set starts the person's session on host.
func (s *Sessions) Set(w http.ResponseWriter, host, userID string) {
	exp := s.now().Add(SessionTTL)
	b, _ := json.Marshal(sessionBody{U: userID, H: HostName(host), E: exp.Unix()})
	http.SetCookie(w, &http.Cookie{
		Name: s.CookieName(), Value: sign(s.key, "session", b), Path: "/", HttpOnly: true, Secure: s.secure,
		// Lax, not Strict: the person arrives from a link on another site (the hub, a chat), and Strict would not send the
		// cookie on that first navigation.
		SameSite: http.SameSiteLaxMode, Expires: exp, MaxAge: int(SessionTTL.Seconds()),
	})
}

// Read is the person of the request's cookie, when it is valid for host.
func (s *Sessions) Read(r *http.Request, host string) (userID string, ok bool) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return "", false
	}
	payload, ok := verify(s.key, "session", c.Value)
	if !ok {
		return "", false
	}
	var b sessionBody
	if json.Unmarshal(payload, &b) != nil || b.U == "" || b.H != HostName(host) || !s.now().Before(time.Unix(b.E, 0)) {
		return "", false
	}
	return b.U, true
}
