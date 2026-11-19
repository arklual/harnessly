package hubproto

import (
	"crypto/ed25519"
	"time"
)

// Heavy jobs and the pool of builder machines (docs/hub/builders.md). A person (or an agent) hands the hub a
// heavy command (a Blender render, a build) with the files it needs; the hub places it on an online builder machine
// whose labels cover what the job needs, the builder's agent runs it in a folder of its own and hands the result files
// back through the hub, and the submitter takes them from there. The names below are the wire of the two halves:
//
//   - the machine that submits (or lists the builders, or cancels) talks to the hub in signed HTTP, POST /agent/jobs/...,
//     signed under JobsDomain: the same shape as the messages between machines;
//   - the hub talks to a builder over its link: job.start, job.poll, job.cancel, job.caps.

// JobsDomain is the first line of the message signed for a request about jobs: a signature made for the dial, the
// messages or the sites is good for nothing here, and this one for nothing else.
const JobsDomain = "harnessly-agent-jobs-v1"

// JobsPath is the root of the signed requests of a machine about jobs.
const JobsPath = "/agent/jobs"

// FeatureJobRun is in `hello` when the agent runs heavy jobs of the hub. It is offered only when the operator of the
// machine switched it on there (AGENT_HUB_JOBS=on): a job is a command the hub's people start on the machine, so the
// machine's own person says yes first; the hub's role of the machine (builder) is the second key.
const FeatureJobRun = "job.run"

// Operations the hub asks of a builder's agent.
const (
	// OpJobCaps asks what the machine can run: {} in, JobCaps out. The hub uses it to fill in a new builder's labels
	// and slots, and to show the admin what is detected.
	OpJobCaps = "job.caps"
	// OpJobStart hands the agent one job: JobStart in, {ok} out. Asking again for a job the agent already has is not
	// an error. CodeBusy when no slot is free; CodePolicyDenied for a tool the machine does not run.
	OpJobStart = "job.start"
	// OpJobPoll asks for the state of jobs: {"jobIds": [...]} in (at most JobPollMax), {"jobs": {id: JobState}} out. A job
	// the agent does not know is missing from the answer; one that was going when the agent restarted is failed.
	OpJobPoll = "job.poll"
	// OpJobCancel ends a job: {"jobId"} in, {ok} out. A job the agent does not have is not an error.
	OpJobCancel = "job.cancel"

	// JobPollMax bounds the jobs one poll asks about.
	JobPollMax = 50
)

// The statuses of a job. Queued and running are active; the rest are final.
const (
	HeavyQueued    = "queued"
	HeavyRunning   = "running"
	HeavyDone      = "done"
	HeavyFailed    = "failed"
	HeavyCancelled = "cancelled"
)

// HeavyActive says the job is not over.
func HeavyActive(status string) bool { return status == HeavyQueued || status == HeavyRunning }

// JobKindHeavy is the only kind of job today: a heavy command that wants a builder.
const JobKindHeavy = "heavy"

// JobCaps is what a machine's agent says it can run.
type JobCaps struct {
	// Caps are the labels it detected (blender, ffmpeg, docker, go, node, gpu...).
	Caps []string `json:"caps"`
	// Slots is how many jobs it runs at once by default (the machine's heavy slots).
	Slots int `json:"slots"`
	// Tools are the programs a job may start on this machine, by name.
	Tools []string `json:"tools"`
}

// JobStart is the body of job.start.
type JobStart struct {
	JobID string `json:"jobId"`
	// Argv is the command, program first, no shell. The program's base name must be one of the machine's Tools.
	Argv []string `json:"argv"`
	// Env are extra variables for the command (names the agent checks, never a secret of the hub).
	Env map[string]string `json:"env,omitempty"`
	// TimeoutSec ends the command when it runs longer.
	TimeoutSec int `json:"timeoutSec"`
	// Input says the job has an input archive at JobsPath/<id>/input (tar.gz), unpacked into the job's folder first.
	Input bool `json:"input"`
	// Outputs are the globs, relative to the job's folder, of the files handed back when the command ends.
	Outputs []string `json:"outputs"`
}

// JobState is the agent's answer about one job in job.poll.
type JobState struct {
	Status   string `json:"status"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
	// Log is the tail of the command's output.
	Log string `json:"log,omitempty"`
	// Outputs are the files already handed to the hub (all of them when the job is final).
	Outputs []JobFile `json:"outputs,omitempty"`
}

// JobFile is one result file of a job.
type JobFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// SignJobs makes the Authorization header of a machine's request about jobs.
func SignJobs(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(JobsDomain, priv, machineID, now, host)
}

// VerifyJobs checks a header made by SignJobs.
func VerifyJobs(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(JobsDomain, pub, a, host, now)
}
