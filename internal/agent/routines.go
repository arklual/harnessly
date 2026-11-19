package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// RoutinesRoute is the loopback route of the person's routines (docs/hub/routines.md): the runtime posts a
// RoutinesCall with the local token, the agent signs it with the machine's key and hands it to the hub, which acts for
// the machine's owner.
const RoutinesRoute = "/routines/call"

// routinesServed: the machine's own hub link, or an observe link whose operator opted in to the hub's automations.
func (h *hub) routinesServed() bool {
	if h == nil || h.policy.Disabled() {
		return false
	}
	return !h.observe || automationsOptedIn()
}

func (a *Agent) routinesRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, http.StatusForbidden, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	var in hubproto.RoutinesCall
	if json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&in) != nil || in.Op == "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if !a.hub.routinesServed() {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "this machine is not connected to a hub that runs routines"})
		return
	}
	var out json.RawMessage
	err := a.hub.signedCall(r.Context(), hubproto.SignRoutines, hubproto.RoutinesPath, in, &out)
	switch {
	case errors.Is(err, errPeersNotServed):
		reply(w, http.StatusNotImplemented, map[string]string{"error": "the hub does not run routines (HUB_AUTOMATIONS is off)"})
	case err != nil:
		reply(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		a.hub.audit.Record("routines."+in.Op, true, nil)
		reply(w, http.StatusOK, out)
	}
}
