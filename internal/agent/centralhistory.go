package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// centralHistoryRoute relays one authenticated runtime request to its hub. The machine identity chooses the person.
func (a *Agent) centralHistoryRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, http.StatusForbidden, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	var in hubproto.HistoryRead
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in) != nil {
		reply(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if a.hub == nil || a.hub.observe || a.hub.policy.Disabled() {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "this machine is not connected to a history hub"})
		return
	}
	var out hubproto.HistoryPage
	err := a.hub.signedCall(r.Context(), hubproto.SignHistory, hubproto.HistoryReadPath, in, &out)
	switch {
	case errors.Is(err, errPeersNotServed):
		reply(w, http.StatusNotImplemented, map[string]string{"error": "the hub does not keep chat history"})
	case err != nil:
		reply(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		a.hub.audit.Record("history.read", true, nil)
		reply(w, http.StatusOK, out)
	}
}
