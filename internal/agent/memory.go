package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The memory of this machine's person, kept by the hub (docs/hub/memory-sync.md). The runtime is the client
// (RUNTIME_MEMORY_SYNC_URL=http://127.0.0.1:<agent port>/memory): it signs its request with the pair's secret under
// MemoryRelayAudience, the agent checks it and hands it to the hub signed with the machine's key. The hub decides whose
// memory it is; nothing the runtime sends names a person.

// MemoryRelayAudience: a runtime asks its agent to read or write its person's memory in the hub.
const MemoryRelayAudience = "harnessly-memory-relay"

// memoryAllowed: the hub link is the machine's own and the kill switch is off. An observe link (the machine is run by
// its own cluster) takes part only when the operator opted in on the machine with AGENT_HUB_MEMORY=on.
func (h *hub) memoryAllowed() bool {
	if h.policy.Disabled() {
		return false
	}
	if h.observe {
		return os.Getenv("AGENT_HUB_MEMORY") == "on"
	}
	return true
}

// memoryRoute is POST /memory/pull and /memory/put on the loopback.
func (a *Agent) memoryRoute(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	secret := a.sessionSecret()
	if !ok || token == "" || secret == nil {
		reply(w, http.StatusForbidden, map[string]string{"error": "not this machine's runtime"})
		return
	}
	if _, ok := VerifiedAudience(secret, token, MemoryRelayAudience, time.Now()); !ok {
		reply(w, http.StatusForbidden, map[string]string{"error": "not this machine's runtime"})
		return
	}
	h := a.hub
	if h == nil {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "this machine is not in a hub"})
		return
	}
	if !h.memoryAllowed() {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "memory sync is off on this machine"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2*hubproto.MemoryMaxNote+64<<10))
	if err != nil || !json.Valid(raw) {
		reply(w, http.StatusBadRequest, map[string]string{"error": "body"})
		return
	}
	path := hubproto.MemoryPullPath
	if r.URL.RequestURI() == "/memory/put" {
		path = hubproto.MemoryPutPath
	}
	var out json.RawMessage
	err = h.signedCall(r.Context(), hubproto.SignMemory, path, json.RawMessage(raw), &out)
	switch {
	case errors.Is(err, errPeersNotServed):
		reply(w, http.StatusNotImplemented, map[string]string{"error": "the hub does not keep memory (HUB_MEMORY_SYNC is off)"})
	case err != nil:
		reply(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		h.audit.Record("memory."+strings.TrimPrefix(path, "/agent/memory/"), true, nil)
		reply(w, http.StatusOK, out)
	}
}
