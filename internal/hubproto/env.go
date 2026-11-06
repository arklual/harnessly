package hubproto

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// What the hub tells a machine about the environment of its chats (docs/hub/environment.md): the catalog items (skills,
// MCP servers, repositories, documents) the machine's person has in the tenant, after the layers (organization, tenant,
// team) and the person's own switches were put together. Not to be mixed with env.deliver / env.remove, which carry
// environment variables and keys. A machine holds the last list it got and tells which items it could apply.

const (
	OpEnvSet = "env.set"
	// EventEnvState is sent by the agent when the outcome of applying changed without a new env.set.
	EventEnvState = "env.state"
	// FeatureEnvironment is in `hello` when the agent keeps env.set (the feature "env" is the variables and keys).
	FeatureEnvironment = "environment"

	// Statuses of an item on a machine.
	EnvOK      = "ok"
	EnvError   = "error"
	EnvPending = "pending" // kept, but this runtime has no way to load it yet; Message says why

	// EnvPluginDir is where, under the folder of a hub link (the agent's data folder, or <data>/hub for an observe
	// link), the agent applies the list: a Claude Code plugin the runtime loads into new chats.
	EnvPluginDir = "environment/claude-plugin"
)

// Kinds of an environment item and of a tool.
const (
	EnvKindSkill = "skill"
	EnvKindMCP   = "mcp"
	EnvKindRepo  = "repo"
	EnvKindDoc   = "doc"
)

// EnvSet replaces the machine's whole list. Rev grows on the hub with every change of the machine's list; Hash is
// EnvHash(Items), so the machine can tell whether it holds the same list without comparing it.
type EnvSet struct {
	Rev   int64     `json:"rev"`
	Hash  string    `json:"hash"`
	Items []EnvItem `json:"items"`
}

// EnvItem is one item of the list. Source is the item's JSON from the catalog, without a secret: a secret is named in
// source.secretRefs and the machine looks it up by that name. Layer is the highest layer that puts the item in
// (org, tenant, team); Pinned says nobody below may switch it off.
type EnvItem struct {
	ItemID string `json:"itemId"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Layer  string `json:"layer"`
	Pinned bool   `json:"pinned"`
	// Order is the place of a script in the run (1, 2, …: the organization's first, then the tenant's, then the team's,
	// each layer in its editors' order); 0 and absent for every other kind. Only a machine with FeatureEnvSetup gets it.
	Order  int             `json:"order,omitempty"`
	Source json.RawMessage `json:"source"`
}

// EnvState is what a machine holds and how each item went. It is the answer of env.set and the body of the
// env.state event. Hash is the one the machine holds (the hash of the list it applied, "" before the first).
type EnvState struct {
	Rev     int64        `json:"rev"`
	Hash    string       `json:"hash"`
	Applied []EnvApplied `json:"applied"`
}

// EnvApplied is the outcome for one item.
type EnvApplied struct {
	ItemID  string `json:"itemId"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// SortEnvItems puts the items in the order the hash is taken in: by kind, name, id.
func SortEnvItems(items []EnvItem) {
	slices.SortFunc(items, func(a, b EnvItem) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.ItemID, b.ItemID)
	})
}

// CanonicalJSON writes raw with the keys of every object in order and no spaces; "" and null stand for {}. A text that
// is not JSON is returned as is, so a broken source still hashes the same on both sides.
func CanonicalJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// EnvHash is the hash of a list of items: the same set gives the same hash whatever the order of the items or of the
// keys of their sources, and the rev does not take part.
func EnvHash(items []EnvItem) string {
	sorted := make([]EnvItem, len(items))
	for i, it := range items {
		it.Source = CanonicalJSON(it.Source)
		sorted[i] = it
	}
	SortEnvItems(sorted)
	raw, _ := json.Marshal(sorted)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// UsageToolFact is the calls of one tool in one session on one UTC day, as the session stands: a later fact of the
// same (session, day, kind, name) replaces the earlier. ToolKind is skill, mcp, bash or other; ToolName is the skill's
// name, the MCP server's name, or the tool's own name.
type UsageToolFact struct {
	Provider   string `json:"provider"`
	SessionKey string `json:"sessionKey"`
	Day        string `json:"day"` // YYYY-MM-DD, UTC
	ToolKind   string `json:"toolKind"`
	ToolName   string `json:"toolName"`
	Calls      int64  `json:"calls"`
	Errors     int64  `json:"errors"`
}
