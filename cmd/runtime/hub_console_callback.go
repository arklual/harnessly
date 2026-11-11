package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// A sign-in in a console whose CLI waits for the provider's redirect on a loopback port (`http://127.0.0.1:<port>/…`):
// the browser that finishes the sign-in runs on the person's computer, so the redirect lands on the wrong machine. The
// person pastes the address the browser was sent to into the console page, and the runtime makes that one request to the
// listener of that console's login process, on this machine.
//
//   - Only a port a process of that console listens on, on a loopback host the address names (localhost, 127.0.0.1, ::1).
//   - Once per console: the first request that reached the listener uses it up, whatever it answered.
//   - Bound to the person: the first signed-in person who opened the console holds it; another one is refused.
//   - The address carries the provider's code: it is never logged, and the listener's answer is not sent back, only its
//     status.

const (
	callbackURLMax  = 8 << 10
	callbackTimeout = 15 * time.Second
	callbackBodyMax = 64 << 10
)

// consoleHold is what a console is bound to: its person and whether its callback was used.
type consoleHold struct {
	owner string
	used  bool
	// What the machine agent said of the attempt: its provider, the name to show, when it expires (Unix ms) and where
	// it stands. state is "" until it has said; "connected", "failed", "expired" and "cancelled" end the attempt.
	provider, label, state string
	expiresAt              int64
	ended                  time.Time
}

// terminalState says the state ends the attempt.
func terminalState(s string) bool {
	return s == "connected" || s == "failed" || s == "expired" || s == "cancelled"
}

type consoleHolds struct {
	mu sync.Mutex
	m  map[string]*consoleHold
}

func (h *consoleHolds) get(id string) *consoleHold {
	if h.m == nil {
		h.m = map[string]*consoleHold{}
	}
	c := h.m[id]
	if c == nil {
		c = &consoleHold{}
		h.m[id] = c
	}
	return c
}

// endedKeep is how long the page can still read how an ended attempt ended.
const endedKeep = 10 * time.Minute

// drop forgets the consoles that are not running any more; one that ended in a state is kept a while for its page.
func (h *consoleHolds) drop(alive func(id string) bool, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, c := range h.m {
		switch {
		case terminalState(c.state):
			if now.Sub(c.ended) > endedKeep {
				delete(h.m, id)
			}
		case !alive(id):
			delete(h.m, id)
		}
	}
}

// person is the signed-in person of the request ("" when the session names none, as on a machine without a hub's
// sign-in).
func (c *hubConsole) person(r *http.Request) string {
	if c.claims == nil {
		return ""
	}
	sub, _ := c.claims(r)["sub"].(string)
	return sub
}

// claim binds the console to the request's person when no one holds it yet, and says whether that person may use it.
func (c *hubConsole) claim(id string, r *http.Request) bool {
	who := c.person(r)
	c.holds.mu.Lock()
	defer c.holds.mu.Unlock()
	h := c.holds.get(id)
	if h.owner == "" {
		h.owner = who
	}
	return h.owner == who
}

// listening are the ports the console's processes listen on.
func (c *hubConsole) listening(name string) []int {
	if c.ports != nil {
		return c.ports(name)
	}
	return nil
}

// callbackState is what the page is told about the console's callback: the ports it may send one to, or that it was
// sent.
func (c *hubConsole) callbackState(id, name string) map[string]any {
	c.holds.mu.Lock()
	used := c.holds.get(id).used
	c.holds.mu.Unlock()
	if used {
		return map[string]any{"used": true, "ports": []int{}}
	}
	ports := c.listening(name)
	if ports == nil {
		ports = []int{}
	}
	return map[string]any{"used": false, "ports": ports}
}

var errNotLoopback = errors.New("not a loopback address")

// loopbackTarget checks the pasted address: http on a loopback host, with a port. It gives the port and the request's
// path and query.
func loopbackTarget(raw string) (int, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.User != nil || u.Opaque != "" {
		return 0, "", errNotLoopback
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return 0, "", errNotLoopback
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0, "", errNotLoopback
	}
	return port, u.RequestURI(), nil
}

// callback is `POST /api/console/<id>/callback` {"url": "http://localhost:<port>/…"}.
func (c *hubConsole) callback(w http.ResponseWriter, r *http.Request, id, name string) {
	if sameorigin.CrossSite(r) {
		errReply(w, http.StatusForbidden, "Cross-site request refused.")
		return
	}
	if !c.claim(id, r) {
		errReply(w, http.StatusForbidden, "This sign-in belongs to another person.")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, callbackURLMax+1024)).Decode(&body); err != nil || len(body.URL) > callbackURLMax {
		errReply(w, http.StatusBadRequest, "A JSON body with the address is required.")
		return
	}
	port, target, err := loopbackTarget(body.URL)
	if err != nil {
		errReply(w, http.StatusBadRequest, "Paste the address that starts with http://localhost or http://127.0.0.1.")
		return
	}
	if !c.tm.HasSession(name) {
		errReply(w, http.StatusGone, "The terminal has ended.")
		return
	}
	if !slices.Contains(c.listening(name), port) {
		errReply(w, http.StatusConflict, "Nothing in this terminal waits on that port.")
		return
	}
	c.holds.mu.Lock()
	h := c.holds.get(id)
	if h.used {
		c.holds.mu.Unlock()
		errReply(w, http.StatusConflict, "The sign-in address was already delivered.")
		return
	}
	h.used = true
	c.holds.mu.Unlock()

	status, err := c.relay(r.Context(), port, target)
	if err != nil {
		// Nothing reached the listener: the person may try again.
		c.holds.mu.Lock()
		h.used = false
		c.holds.mu.Unlock()
		agent.Log("warn", "hub: console callback not delivered", agent.F{K: "port", V: port})
		errReply(w, http.StatusBadGateway, "The sign-in process did not answer.")
		return
	}
	agent.Log("info", "hub: console callback delivered", agent.F{K: "port", V: port}, agent.F{K: "status", V: status})
	jsonReply(w, http.StatusOK, map[string]any{"ok": status < 400, "status": status})
}

// relay makes the one GET to the listener, on the loopback address it took (IPv4, then IPv6), with the Host the
// browser would have sent, and gives the status. Redirects are not followed: the listener's answer is the end.
func (c *hubConsole) relay(ctx context.Context, port int, target string) (int, error) {
	client := &http.Client{
		Timeout:       callbackTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
	var last error
	for _, host := range []string{"127.0.0.1", "::1"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+target, nil)
		if err != nil {
			return 0, err
		}
		req.Host = "localhost:" + strconv.Itoa(port)
		res, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, callbackBodyMax))
		_ = res.Body.Close()
		return res.StatusCode, nil
	}
	return 0, last
}
