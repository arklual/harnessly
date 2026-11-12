package hubproto

import "encoding/json"

// The hub's evaluation of a harness (docs/hub/eval.md): the hub's scheduler hands golden tasks to a machine's agent, which
// runs each in a sandbox on the machine, paid from the owner's own subscription, and reports how it went. The names differ
// from the closed cluster's eval.start/eval.poll/eval.stop on purpose: those go to the runtime next door, these are served by the
// agent itself, and a hub link does not get the cluster's.

const (
	// FeatureEvalRun is in `hello` when the agent runs evaluation tasks. It is offered only when the operator switched it on
	// with AGENT_HUB_EVAL=on (a run spends the owner's subscription and is a sandboxed agent session on their machine), the
	// machine can sandbox (Linux with bubblewrap), and the kill switch is not on.
	FeatureEvalRun = "eval.run"

	// OpEvalRunStart hands the agent one run: EvalRunStart in, {ok} out. Asking again for a run the agent already has is
	// not an error. CodeBusy when another run is going: a machine runs one at a time.
	OpEvalRunStart = "eval.run.start"
	// OpEvalRunPoll asks for the state of runs: {"runIds": [...]} in (at most 50), {"runs": {id: state}} out. A run the
	// agent does not know is missing from the answer; a run that was going when the agent restarted is reported failed.
	OpEvalRunPoll = "eval.run.poll"
	// OpEvalRunStop ends a run: {"runId"} in, {ok} out. A run the agent does not have is not an error.
	OpEvalRunStop = "eval.run.stop"

	// EvalRunPollMax bounds the runs one poll asks about.
	EvalRunPollMax = 50
)

// EvalRunStart is the body of eval.run.start. Task and Arm are the catalog's documents as the scheduler read them from the
// hub's tables (docs/hub/eval.md, «The runner»); the agent checks them and ignores keys it does not know.
type EvalRunStart struct {
	RunID string          `json:"runId"`
	Task  json.RawMessage `json:"task"`
	Arm   json.RawMessage `json:"arm"`
}
