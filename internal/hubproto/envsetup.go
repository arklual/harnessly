package hubproto

import (
	"regexp"
	"strings"
	"time"
)

// Machine setup (docs/hub/environment.md, «Machine setup»): besides what the chats load (skills, MCP servers, documents,
// the reference skill of a repository), an environment list can say what the machine itself is to have: repositories
// cloned, logins of the git forges' CLIs (access) and Linux scripts run in order. The agent keeps the list as before
// (environment.json of the link folder); the runtime, which runs as the person and holds the safe, does the setup and
// writes how it went next to the list; the agent tells the hub. The hub asks for a run (update, check, a sandbox try)
// with env.run and gets the run's steps and logs back as env.run events.

const (
	// EnvKindScript is a Linux script the machine runs, in the order of the list.
	EnvKindScript = "script"
	// EnvKindAccess is the login of a forge's CLI (glab for GitLab, gh for GitHub) with the person's token from the safe.
	EnvKindAccess = "access"

	// FeatureEnvSetup is in `hello` when the agent hands the setup kinds to its runtime and serves env.run. The hub sends
	// script and access items, and the order of the list, only to such a machine: an older agent would refuse a list
	// whose items it cannot hash the same way. It is the machine's own choice: an own link follows the policy `setup` (a
	// policy file without the key means off), an observe link needs AGENT_HUB_SETUP=on, and the agent advertises the
	// feature only while the runtime's setup runner heartbeats.
	FeatureEnvSetup = "environment.setup"

	// OpEnvRun asks the machine for a run: {runId, mode, items?}. Answered at once with {runId, accepted}; the run's
	// progress and outcome come as env.run events.
	OpEnvRun = "env.run"
	// EventEnvRun carries a run as it stands: {"event":"env.run","run":{…EnvRun}}. Sent when a step changes and when the
	// run ends; a later frame of the same runId replaces the earlier one.
	EventEnvRun = "env.run"

	// Statuses of a setup step and of a run, in addition to EnvOK, EnvError and EnvPending.
	EnvRunning = "running"
	EnvSkipped = "skipped" // a step that had nothing to do (already done, its check passed)

	// Modes of a run.
	RunApply   = "apply"   // the machine took a new list: do what is not done yet
	RunUpdate  = "update"  // the person (or an editor) asked: the same as apply, now, and the checks of every script again
	RunCheck   = "check"   // only look: the checks, the clones, the logins; change nothing
	RunSandbox = "sandbox" // the whole list from scratch in a sandbox with an empty home; the machine itself is not touched

	// EnvSetupFile is the runtime's account of the setup in the link folder: the list's hash it worked on, each setup
	// item's status and the ledger of the scripts run once.
	EnvSetupFile = "environment-setup.json"
	// EnvAllowFile is the agent's word to the runtime on whether this machine may be set up by the list: {"setup": bool,
	// "at": unix ms}. The agent writes it from its policy (an own link: `setup`; an observe link: AGENT_HUB_SETUP=on) and
	// the runtime runs nothing while it is missing or says false.
	EnvAllowFile = "environment-allow.json"
	// EnvRunnerFile is the runtime's heartbeat in the link folder (EnvRunnerBeat), rewritten every EnvRunnerBeatEvery. The
	// agent offers FeatureEnvSetup only while it is fresh (EnvRunnerStale): no runtime, or one that cannot set up, no feature.
	EnvRunnerFile = "environment-runner.json"
	// EnvRunsDir holds a file per run (<runId>.json, EnvRun), kept 14 days.
	EnvRunsDir = "environment-runs"
	// EnvRequestsDir holds the runs the hub asked for and the runtime has not started (<runId>.json, EnvRunRequest).
	EnvRequestsDir = "environment-requests"

	// EnvRunKeep is how long runs are kept, on the machine and on the hub.
	EnvRunKeepDays = 14
	// EnvLogMax bounds the log of one step (its tail is kept).
	EnvLogMax = 16 << 10
)

const (
	EnvRunnerBeatEvery = 30 * time.Second
	EnvRunnerStale     = 2 * time.Minute
)

// EnvRunnerBeat is EnvRunnerFile.
type EnvRunnerBeat struct {
	PID     int    `json:"pid"`
	Version string `json:"version,omitempty"`
	At      int64  `json:"at"` // unix ms
}

// EnvAllow is EnvAllowFile.
type EnvAllow struct {
	Setup bool  `json:"setup"`
	At    int64 `json:"at"`
}

// EnvRunRequest is what the hub asks for. Items is only for a sandbox try of a list that is not saved (an editor's
// draft); empty, the run takes the list the machine holds.
type EnvRunRequest struct {
	RunID string    `json:"runId"`
	Mode  string    `json:"mode"`
	Items []EnvItem `json:"items,omitempty"`
	// By is who asked, for the log («Анна Петрова»); empty for a run the machine started itself.
	By string `json:"by,omitempty"`
	At int64  `json:"at"` // unix ms
}

// EnvRun is one run: what the machine did with the list, step by step.
type EnvRun struct {
	RunID      string    `json:"runId"`
	Mode       string    `json:"mode"`
	Hash       string    `json:"hash"` // the list the run worked on
	By         string    `json:"by,omitempty"`
	Status     string    `json:"status"` // running, ok, error, pending (something waits for the person: a token)
	StartedAt  int64     `json:"startedAt"`
	FinishedAt int64     `json:"finishedAt"` // 0 while running
	Steps      []EnvStep `json:"steps"`
}

// EnvStep is one item of the run.
type EnvStep struct {
	ItemID  string `json:"itemId"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"` // pending (not started yet, or waits for the person), running, ok, skipped, error
	Message string `json:"message,omitempty"`
	Exit    *int   `json:"exit,omitempty"`
	Ms      int64  `json:"ms"`
	// Log is the step's output, secrets masked, at most EnvLogMax (the tail).
	Log string `json:"log,omitempty"`
}

// EnvSetupState is the runtime's account (EnvSetupFile).
type EnvSetupState struct {
	Hash  string            `json:"hash"`
	Items []EnvApplied      `json:"items"`
	Once  map[string]string `json:"once"` // script item id → the hash of the source it ran once with, successfully
	At    int64             `json:"at"`
}

// IsSetupKind says whether the machine (the runtime) does something with an item of this kind besides the chats.
func IsSetupKind(kind string) bool {
	return kind == EnvKindScript || kind == EnvKindAccess || kind == EnvKindRepo
}

// ValidRunMode is one of the four modes.
func ValidRunMode(m string) bool {
	return m == RunApply || m == RunUpdate || m == RunCheck || m == RunSandbox
}

// EnvRunIDRe is what a run id looks like; it names a file.
var EnvRunIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// SafeNameRe is how a secret of the safe is named (internal/secrets).
var SafeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// SecretVar is the variable a script gets a secret of the safe in: npm-token → NPM_TOKEN.
func SecretVar(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	v := b.String()
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		v = "_" + v
	}
	return v
}

// AccessSecret is the safe's name of the token an access item logs in with when the item does not name one:
// github-token for GitHub, gitlab-token for any GitLab.
func AccessSecret(service string) string {
	if service == "github" {
		return "github-token"
	}
	return "gitlab-token"
}
