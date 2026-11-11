package agent

import (
	"encoding/json"
	"os"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// docsOptIn hands the documents of a machine linked to a hub as an observer to that hub: publishes, access changes,
// bindings and the agent's replies go to the hub instead of the cluster, and the hub may hand comments to the chats.
const docsOptIn = "AGENT_HUB_DOCS"

// docsServed says the hub keeps this machine's documents: on its own link always; on an observe link only when the
// operator opted in with AGENT_HUB_DOCS=on. The kill switch turns it off either way.
func (h *hub) docsServed() bool {
	if h.policy.Disabled() {
		return false
	}
	return !h.observe || os.Getenv(docsOptIn) == "on"
}

// hubKeepsDocs says the documents of this machine go to the hub rather than to the cluster.
func (a *Agent) hubKeepsDocs() bool { return a.hub != nil && a.hub.docsServed() }

// sendDoc sends a document event to whoever keeps the documents: the hub, or the cluster as before. False when the hub
// keeps them and its link is down: the event is not sent to the cluster instead, which would split a document's
// threads between two places.
func (a *Agent) sendDoc(ev any) bool {
	if !a.hubKeepsDocs() {
		a.broadcast(ev)
		return true
	}
	frame, err := marshal(ev)
	if err != nil {
		Log("error", "event not encoded", F{"error", err.Error()})
		return false
	}
	links := a.hub.hubLinks()
	if len(links) == 0 {
		return false
	}
	links[0].sendEvent(docEventName(ev), frame)
	return true
}

// docEventName is the name of a document event, the kind it goes as when the hub takes batches.
func docEventName(ev any) string {
	switch e := ev.(type) {
	case evDocPublish:
		return e.Event
	case evDocAcl:
		return e.Event
	case evDocBound:
		return e.Event
	case evDocReply:
		return e.Event
	}
	return ""
}

// docsConnected says whoever keeps the documents is linked now.
func (a *Agent) docsConnected() bool {
	if a.hubKeepsDocs() {
		return len(a.hub.hubLinks()) > 0
	}
	return a.connected()
}

// opDocs serves the hub's doc.result (the answer to a publish or an access change) and docs.deliver (a comment for the
// chat a document is bound to).
func (h *hub) opDocs(op string, req map[string]json.RawMessage) (any, func(), error) {
	if err := h.allow(op, h.docsServed(), "documents"); err != nil {
		return nil, nil, err
	}
	if op == hubproto.OpDocResult {
		requestID, _ := field[string](req, "requestId")
		status, _ := field[float64](req, "status")
		h.a.settleDoc(requestID, DocResult{Status: int(status), Body: req["body"]})
		return map[string]bool{"settled": true}, nil, nil
	}
	if h.a.cfg.RuntimePort == 0 {
		return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "no runtime to hand comments to on this machine")
	}
	res, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/docs/deliver", body(req))
	if err != nil {
		return nil, nil, hubproto.Errf(hubproto.CodeFailed, "%s", err.Error())
	}
	return res, nil, nil
}
