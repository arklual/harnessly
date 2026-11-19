package hubproto

import (
	"crypto/ed25519"
	"encoding/json"
	"time"
)

// A person's routines (recurring tasks, HRN-1653, docs/hub/routines.md) from their machine: the runtime asks its agent,
// the agent signs the call with the machine's key under RoutinesDomain, and the hub acts for the machine's owner. Nothing
// in the call names a person.

// RoutinesDomain is the first line of the message signed for a routines call.
const RoutinesDomain = "harnessly-agent-routines-v1"

// RoutinesPath is where the hub answers it.
const RoutinesPath = "/agent/routines"

// RoutinesCall is one call: op is list, get, create, update, run, delete or runs; id names the routine for all but
// list and create; body is the rule (create, update) as the hub's automations API takes it.
type RoutinesCall struct {
	Op    string          `json:"op"`
	ID    string          `json:"id,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
	Limit int             `json:"limit,omitempty"`
}

// SignRoutines makes the Authorization header value of a routines call.
func SignRoutines(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(RoutinesDomain, priv, machineID, now, host)
}

// VerifyRoutines is VerifyDial for the header SignRoutines made.
func VerifyRoutines(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(RoutinesDomain, pub, a, host, now)
}
