package hubproto

import "encoding/json"

// A change of a person's own layer of the environment from their machine: their own skill or MCP server, or
// a switch of an item for themselves. The hub makes it for the person the machine is theirs (its creator) and only when
// the organization has the own layer; it reaches only that person's machines. The link proves the machine, not the
// person: whoever works on the person's machine changes the person's own layer, and only it.

const (
	// EventEnvOwn carries {"event":"env.own", …EnvOwnRequest}; the hub answers each with OpEnvOwned.
	EventEnvOwn = "env.own"
	// OpEnvOwned is the hub's answer: an EnvOwnResult.
	OpEnvOwned = "env.owned"
	// FeatureEnvOwn is in `hello` when the agent sends env.own and takes the answers.
	FeatureEnvOwn = "env.own"
)

// Actions of EnvOwnRequest.
const (
	EnvOwnAdd    = "add"    // a new own item: Kind, Name, Hint, Source
	EnvOwnUpdate = "update" // an own item's Name, Hint or Source (ItemID)
	EnvOwnRemove = "remove" // an own item taken out (ItemID)
	EnvOwnSwitch = "switch" // an item of the machine's list switched off or back on for the person (ItemID, Off)
)

// EnvOwnRequest is one change.
type EnvOwnRequest struct {
	RequestID string          `json:"requestId"`
	Action    string          `json:"action"`
	ItemID    string          `json:"itemId,omitempty"`
	Kind      string          `json:"kind,omitempty"` // skill or mcp
	Name      *string         `json:"name,omitempty"`
	Hint      *string         `json:"hint,omitempty"`
	Source    json.RawMessage `json:"source,omitempty"`
	Off       bool            `json:"off,omitempty"`
}

// EnvOwnResult is the hub's answer. Status is an HTTP-like code: 200 done, 201 added, 400 not valid, 403 the machine
// has no owner, 404 no such item or no own layer in the organization, 409 the name is taken; Message says it in words.
type EnvOwnResult struct {
	RequestID string `json:"requestId"`
	Status    int    `json:"status"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	ItemID    string `json:"itemId,omitempty"`
}
