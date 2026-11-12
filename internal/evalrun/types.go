// Package evalrun is the machine's side of the hub's evaluation (docs/hub/eval.md): it runs one golden task of the
// catalog in a sandbox on the person's machine and reports how it went. A clean copy of the task's base commit with no
// history, dependencies from the stores the machine already has, an agent session inside a bubblewrap sandbox that hides
// the person's home (no token of theirs is readable there), an access-only copy of the subscription login, `env -i`, then
// the task's hidden tests graded by exit code in the same sandbox. The copy and the sandbox home are removed afterwards.
package evalrun

import "regexp"

// RunID is the shape of a run's id (a UUID).
var RunID = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// Check is a hidden check of a task: a shell command in the sandbox, passed when it exits 0.
type Check struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

// Limits bound one run: wall time in seconds and the agent's budget in US dollars.
type Limits struct {
	WallS int     `json:"wallS"`
	USD   float64 `json:"usd"`
}

// Task is one golden task as the hub's catalog has it.
type Task struct {
	Key    string  `json:"key"`
	Repo   string  `json:"repo"`
	Base   string  `json:"base"`
	Prompt string  `json:"prompt"`
	Checks []Check `json:"checks"`
	// Hidden are files written into the copy after the session, before the checks (the tests the agent never saw).
	Hidden map[string]string `json:"hidden"`
	// Forbidden are globs of files the agent must not have touched.
	Forbidden []string `json:"forbidden"`
	Limits    Limits   `json:"limits"`
	// Setup is bash run in the sandbox before the session; what it leaves becomes part of the base commit.
	Setup string `json:"setup"`
	// Layout "workspace" puts the copy under a workspace folder of its own, as a source checkout lives.
	Layout string `json:"layout"`
	// Profile "danger" runs without the eval profile's own prohibitions: only the harness's hooks stand between the agent
	// and the action; the sandbox still hides the home and the network tools stay denied.
	Profile string `json:"profile"`
}

// Arm is the way the harness is set up for a run.
type Arm struct {
	Key    string  `json:"key"`
	Model  string  `json:"model"`
	Effort *string `json:"effort"`
	// Harness is "personal" (the person's plugins, skills and CLAUDE.md) or "repo" (only what the repository brings).
	Harness string `json:"harness"`
	// Agent is "claude" (default) or "codex".
	Agent string `json:"agent"`
	// Variant "inline" runs without subagents.
	Variant string `json:"variant"`
}

// Request is what the hub hands over to start a run.
type Request struct {
	RunID string `json:"runId"`
	Task  Task   `json:"task"`
	Arm   Arm    `json:"arm"`
}

// Check result of one hidden check.
type CheckResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	ExitCode *int   `json:"exitCode"`
	Ms       int    `json:"ms"`
}

// Usage is what the session cost.
type Usage struct {
	USD        *float64 `json:"usd"`
	ModelCalls int      `json:"modelCalls"`
	FirstCtx   *int     `json:"firstCtx"`
	PeakCtx    *int     `json:"peakCtx"`
	TokensIn   int64    `json:"tokensIn"`
	TokensOut  int64    `json:"tokensOut"`
	Subagents  int      `json:"subagents"`
}

// Diff counts what the agent changed.
type Diff struct {
	Files     int      `json:"files"`
	Added     int      `json:"added"`
	Removed   int      `json:"removed"`
	Forbidden []string `json:"forbidden"`
}

// Terminal states of a run; the others are preparing, running and grading.
const (
	Done    = "done"
	Failed  = "failed"
	Timeout = "timeout"
	Stopped = "stopped"
	Skipped = "skipped"
)

// State is the report on one run; the runner keeps it in the run's folder as state.json.
type State struct {
	RunID       string         `json:"runId"`
	State       string         `json:"state"`
	StartedAt   string         `json:"startedAt"`
	UpdatedAt   string         `json:"updatedAt"`
	FinishedAt  string         `json:"finishedAt"`
	Error       string         `json:"error"`
	Fingerprint string         `json:"fingerprint"`
	SessionID   string         `json:"conversationId"`
	Passed      *bool          `json:"passed"`
	WallS       *int           `json:"wallS"`
	Usage       *Usage         `json:"usage"`
	Checks      []CheckResult  `json:"checks"`
	Diff        *Diff          `json:"diff"`
	Timings     map[string]int `json:"timings"`
}

// Terminal says whether the run is over.
func (s State) Terminal() bool {
	switch s.State {
	case Done, Failed, Timeout, Stopped, Skipped:
		return true
	}
	return false
}
