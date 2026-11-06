package hubproto

import (
	"crypto/ed25519"
	"time"
)

// Messages between agents of different machines (docs/hub/peers.md). A runtime gives a message for a chat of another
// machine to its agent; the agent signs the request with the machine's key (POST /agent/peers/send) and the hub, which
// knows both machines, hands it over the other machine's link (peers.deliver). The sender is who the hub says it is:
// the machine the signature proves, and a person of that machine's space.

// PeersDomain is the first line of the message signed to send a message to another machine or to list the chats of the
// others: a signature made for the dial, the unlink or the sites is good for nothing here, and this one for nothing else.
const PeersDomain = "harnessly-agent-peers-v1"

// Operations the hub asks of the agent for the messages between machines.
const (
	// OpPeersDeliver hands a message to a chat of the machine: {sender: PeerSender, message: PeerInbound}, answered with
	// a PeerResult.
	OpPeersDeliver = "peers.deliver"
	// OpPeersDirectory lists the chats of the machine another machine may write to: {sender: PeerSender}, answered with
	// PeerChats.
	OpPeersDirectory = "peers.directory"
)

// PeerRefPrefix starts the machine part of a chat's ref the hub gives out: "m-<machine id>/<conversation id>".
const PeerRefPrefix = "m-"

// PeerSender is who a message comes from as the hub vouches for it. Same JSON as the runtime's peers.Sender.
type PeerSender struct {
	Person      string `json:"person"`
	PersonName  string `json:"personName"`
	Machine     string `json:"machine"`
	MachineName string `json:"machineName"`
}

// PeerChat is a chat another machine may write to. Same JSON as the runtime's peers.Chat.
type PeerChat struct {
	Ref         string `json:"ref"`
	Conv        string `json:"conversationId"`
	Title       string `json:"title"`
	Machine     string `json:"machine"`
	MachineName string `json:"machineName"`
	Owner       string `json:"owner"`
	State       string `json:"state"`
}

// PeerOutbound is a message a runtime sends to a chat of another machine. Same JSON as the runtime's peers.Outbound.
type PeerOutbound struct {
	ID        string `json:"id"`
	To        string `json:"to"`
	FromConv  string `json:"fromConversationId"`
	FromTitle string `json:"fromTitle"`
	Text      string `json:"text"`
	Via       string `json:"via"`
}

// PeerInbound is a message for a chat of the machine. Same JSON as the runtime's peers.Inbound.
type PeerInbound struct {
	ID        string `json:"id"`
	To        string `json:"conversationId"`
	FromConv  string `json:"fromConversationId"`
	FromTitle string `json:"fromTitle"`
	Text      string `json:"text"`
	Via       string `json:"via"`
}

// PeerResult is what came of a message. Same JSON as the runtime's peers.Result.
type PeerResult struct {
	ID     string    `json:"id"`
	State  string    `json:"state"`
	Reason string    `json:"reason,omitempty"`
	Detail string    `json:"detail,omitempty"`
	To     *PeerChat `json:"to,omitempty"`
}

// The states and reasons of a PeerResult the hub and the agent give themselves (the runtime has more).
const (
	PeerDelivered   = "delivered"
	PeerRefused     = "refused"
	PeerOff         = "off"
	PeerRateLimited = "rate-limited"
	PeerBadRef      = "bad-ref"
	PeerNoRelay     = "no-relay"
	PeerUnreachable = "unreachable"
	PeerUnverified  = "unverified"
	PeerEmpty       = "empty"
	PeerTooLong     = "too-long"
)

// PeerRefusal is a refused PeerResult.
func PeerRefusal(id, reason, detail string) PeerResult {
	return PeerResult{ID: id, State: PeerRefused, Reason: reason, Detail: detail}
}

// PeersDeliver is the parameters of peers.deliver.
type PeersDeliver struct {
	Sender  PeerSender  `json:"sender"`
	Message PeerInbound `json:"message"`
}

// PeersDirectory is the parameters of peers.directory.
type PeersDirectory struct {
	Sender PeerSender `json:"sender"`
}

// PeerChats is the answer of peers.directory and of GET /agent/peers/directory.
type PeerChats struct {
	Chats []PeerChat `json:"chats"`
}

// PeersSend is the body of POST /agent/peers/send: the message, and the person of the machine the runtime sends for
// (believed only as far as the hub can check it).
type PeersSend struct {
	Person  string       `json:"person"`
	Message PeerOutbound `json:"message"`
}

// SignPeers makes the Authorization header of a machine's request about messages between machines: the shape of a
// dial's, signed under PeersDomain.
func SignPeers(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(PeersDomain, priv, machineID, now, host)
}

// VerifyPeers checks a header made by SignPeers.
func VerifyPeers(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(PeersDomain, pub, a, host, now)
}
