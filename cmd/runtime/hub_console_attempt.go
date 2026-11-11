package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The sign-in attempt of a console: what the guided page shows besides the terminal. The machine agent owns the truth
// of it (it runs the provider's status check), the page only reads it:
//
//   - POST /api/agent/console-state {"tabId","state","provider","label","expiresAt"} — the agent says where the attempt
//     stands. It answers {"cancelled": bool}: whether the person cancelled on the page. Only states travel, never what
//     the CLI printed; "connected" is the agent's word after the provider's status check, not the callback's delivery.
//   - POST /api/console/<id>/cancel — the person ends this attempt: its terminal is closed (and the process and listener
//     with it), no other console is touched.
//
// An attempt that ended is final: a later note cannot turn a cancelled or expired one into another state.

const labelMax = 60

var attemptStates = map[string]bool{"waiting": true, "connected": true, "failed": true, "expired": true, "cancelled": true}

// consoleState is the machine agent's note.
func (c *hubConsole) consoleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c.agent == nil || !c.agent(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var body struct {
		TabID     string `json:"tabId"`
		State     string `json:"state"`
		Provider  string `json:"provider"`
		Label     string `json:"label"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, consoleBodyMax)).Decode(&body); err != nil || !validConsoleID(body.TabID) ||
		!attemptStates[body.State] || utf8.RuneCountInString(body.Label) > labelMax || len(body.Provider) > 64 || body.ExpiresAt < 0 {
		errReply(w, http.StatusBadRequest, "A tabId, a known state and a short label are required.")
		return
	}
	name := c.sessionName(body.TabID)
	c.holds.mu.Lock()
	h := c.holds.get(body.TabID)
	if !terminalState(h.state) {
		h.provider, h.label, h.expiresAt = body.Provider, strings.TrimSpace(body.Label), body.ExpiresAt
		h.state = body.State
		if terminalState(body.State) {
			h.ended = c.now()
		}
	}
	cancelled := h.state == "cancelled"
	ended := terminalState(h.state)
	c.holds.mu.Unlock()
	if ended && c.tm.HasSession(name) {
		// The attempt is over: its process and listener go with the terminal, this console's only.
		c.tm.KillSession(name)
	}
	jsonReply(w, http.StatusOK, map[string]bool{"cancelled": cancelled})
}

// cancel is the person ending the attempt on the page.
func (c *hubConsole) cancel(w http.ResponseWriter, r *http.Request, id, name string) {
	if sameorigin.CrossSite(r) {
		errReply(w, http.StatusForbidden, "Cross-site request refused.")
		return
	}
	c.holds.mu.Lock()
	h := c.holds.get(id)
	if !terminalState(h.state) {
		h.state, h.ended = "cancelled", c.now()
	}
	c.holds.mu.Unlock()
	if c.tm.HasSession(name) {
		c.tm.KillSession(name)
	}
	agent.Log("info", "hub: console sign-in cancelled on the page")
	jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
}

// attempt is what the page is told of the attempt.
func (c *hubConsole) attempt(id string) map[string]any {
	c.holds.mu.Lock()
	h := c.holds.get(id)
	out := map[string]any{"state": h.state, "provider": h.provider, "label": h.label, "expiresAt": h.expiresAt}
	c.holds.mu.Unlock()
	host, _ := os.Hostname()
	out["machine"] = host
	return out
}
