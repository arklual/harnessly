package agent

import "os"

// historyOptIn hands the chats of a machine linked to a hub as an observer to that hub as well: the agent
// says `history` in its hello and forwards the runtime's chat replica to the hub beside the cluster, which keeps
// getting its own copy as before.
const historyOptIn = "AGENT_HUB_HISTORY"

func historyOptedIn() bool { return os.Getenv(historyOptIn) == "on" }

// historyServed: the agent reads the runtime's chats for the hub. A hub's own machine with a runtime next to it always;
// an observe link only when the operator opted in with AGENT_HUB_HISTORY=on. The kill switch turns it off either way
// on an observe link (the frames stop with the hub link, which the switch closes).
func (h *hub) historyServed() bool {
	if h.a.cfg.RuntimePort == 0 {
		return false
	}
	if !h.observe {
		return true
	}
	return historyOptedIn() && !h.policy.Disabled() && h.a.hubReplica != nil
}

// historyReplica is the replica the hub's history.start and history.ack belong to: the agent's own on a hub link, the
// hub's apart from the cluster's on an observe link.
func (h *hub) historyReplica() *replica {
	if h.observe {
		return h.a.hubReplica
	}
	return h.a.replica
}
