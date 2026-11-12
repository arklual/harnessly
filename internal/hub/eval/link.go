package eval

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotDelivered is what a Link returns when nothing reached the machine (no connection, the hub restarting): the run
// goes back to the queue. Any other error may come after the runner started, so the run stays claimed and the poll decides.
var ErrNotDelivered = errors.New("eval: the machine did not get the request")

// RunRequest is what a machine's runner is given to start one run: the task with its hidden checks and limits, and the
// arm (the way the harness is set up for this run). Both are JSON documents the runner reads.
type RunRequest struct {
	RunID string          `json:"runId"`
	Task  json.RawMessage `json:"task"`
	Arm   json.RawMessage `json:"arm"`
}

// Check is the verdict of one check of a run.
type Check struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	ExitCode *int   `json:"exitCode"`
	Ms       int    `json:"ms"`
}

// Usage is what the run cost.
type Usage struct {
	USD        *float64 `json:"usd"`
	ModelCalls int      `json:"modelCalls"`
	FirstCtx   *int     `json:"firstCtx"`
	PeakCtx    *int     `json:"peakCtx"`
	TokensIn   int64    `json:"tokensIn"`
	TokensOut  int64    `json:"tokensOut"`
	Subagents  int      `json:"subagents"`
}

// RunState is the runner's report on one run.
type RunState struct {
	RunID       string  `json:"runId"`
	State       string  `json:"state"` // preparing, running, grading, done, failed, timeout, stopped, skipped
	StartedAt   string  `json:"startedAt"`
	FinishedAt  string  `json:"finishedAt"`
	Error       string  `json:"error"`
	Fingerprint string  `json:"fingerprint"`
	Passed      *bool   `json:"passed"`
	WallS       *int    `json:"wallS"`
	Usage       *Usage  `json:"usage"`
	Checks      []Check `json:"checks"`
	Diff        any     `json:"diff"`
}

// Link is the way to a machine's runner. The hub's link to the machine (hub-link) implements it; the scheduler itself
// does not know how a request travels.
type Link interface {
	// Connected says whether the machine can be reached now.
	Connected(machineID string) bool
	// Start hands one run to the machine's runner.
	Start(ctx context.Context, machineID string, req RunRequest) error
	// Poll asks for the state of the runs the machine has; a run it does not know is missing from the answer.
	Poll(ctx context.Context, machineID string, runIDs []string) (map[string]RunState, error)
	// Stop asks the runner to end a run.
	Stop(ctx context.Context, machineID, runID string) error
}

// Gate says whether a feature is on for a person.
type Gate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

var terminalStates = map[string]bool{"done": true, "failed": true, "timeout": true, "stopped": true, "skipped": true}

// newUUID is a random identifier in the shape the runner accepts for a run (36 characters of hex and dashes).
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
