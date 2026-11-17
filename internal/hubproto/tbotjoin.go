package hubproto

// A machine asks the hub for the join token of a Teleport machine identity (tbot) its operator prepared for a person
// The machine made a key pair and keeps the private key; it sends only the public one. The link proves the
// machine, not a person, so the hub creates nothing by it: the person the bot belongs to sees the request in the hub and
// approves or declines it under their own session. On approval the hub creates the join token bound to that key, and the
// machine starts tbot.

const (
	// EventTbotJoin carries {"event":"tbot.join", …TbotJoin}; the hub answers it with OpTbotJoined.
	EventTbotJoin = "tbot.join"
	// OpTbotJoined is the hub's word on a request: a TbotJoinState. It comes as the answer to EventTbotJoin and again
	// when the person approves or revokes it while the machine is linked.
	OpTbotJoined = "tbot.joined"
)

// The states of a request as the machine hears them.
const (
	// TbotJoinPending: the request waits for the person.
	TbotJoinPending = "pending"
	// TbotJoinApproved: the token exists; the machine may start tbot.
	TbotJoinApproved = "approved"
	// TbotJoinDeclined: the person said no to this key; the same key asks no more.
	TbotJoinDeclined = "declined"
	// TbotJoinRevoked: the token was taken away; the machine stops starting tbot.
	TbotJoinRevoked = "revoked"
	// TbotJoinRefused: the hub keeps no request: the shape is wrong, the bot is unknown, or the feature is off.
	TbotJoinRefused = "refused"
)

// TbotJoin is a machine's request, as the operator's setup wrote it to the machine (join.json).
type TbotJoin struct {
	// Bot is the machine identity the token joins as: agent-<login>.
	Bot string `json:"bot"`
	// Token is the name of the join token: <bot>-<machine>.
	Token string `json:"token"`
	// Machine is the host the request comes from, as the setup named it.
	Machine string `json:"machine,omitempty"`
	// PublicKey is the public half of the key tbot joins with, in the SSH form ("ssh-ed25519 AAAA…").
	PublicKey string `json:"publicKey"`
}

// TbotJoinState is the hub's word on a request.
type TbotJoinState struct {
	Token  string `json:"token"`
	Status string `json:"status"`
	// Code names a refusal for a program; Message says it in words.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
