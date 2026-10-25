package convo

import (
	"context"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A message from another agent (internal/peers) enters a conversation here and only here: the same outbox as the
// person's messages — so it waits, is typed and is confirmed the same way — under a client id of its own, from a device
// of its own. Nothing in the outbox or the typist knows the difference; the feed tells it by the envelope in the text.
const (
	// PeerClientPrefix starts the client id of every message from another agent.
	PeerClientPrefix = "peer-"
	// PeerDevice is the device such a message is sent from.
	PeerDevice = "peer"
)

// IsPeerClientID says whether a message of the outbox came from another agent.
func IsPeerClientID(id string) bool { return strings.HasPrefix(id, PeerClientPrefix) }

// DeliverPeer puts a message from another agent into the queue of convID: into the running turn (read at the agent's next
// step) when steer and the agent works, at the head of the line otherwise. The answer is the send's own result.
func (r *Runtime) DeliverPeer(ctx context.Context, convID, clientID, text string, steer bool) wire.CmdResult {
	if !IsPeerClientID(clientID) {
		return wire.Refused(wire.RefuseBadRequest)
	}
	// The agent's state as its actor holds it now, not as last published.
	working, err := live(r, ctx, convID, func(c *conv, _ time.Time) (bool, error) {
		return c.rec.State.K == wire.StateWorking, nil
	})
	if err != nil {
		return wire.Refused(refusal(err))
	}
	mode := wire.ModeNow
	if steer && working {
		mode = wire.ModeSteer
	}
	return r.Do(ctx, PeerDevice, wire.SendCmd{OpID: clientID, ClientID: clientID, ConversationID: convID, Text: text,
		Mode: mode, Kind: wire.KindPrompt, AttachmentIDs: []string{}})
}
