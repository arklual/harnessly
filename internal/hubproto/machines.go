package hubproto

// What a machine tells the hub about itself beyond the card of its agents: its hardware once per link, its load every
// minute, and the progress of a restart of its runner. All of it is optional on both sides: an agent without the feature
// is not asked, a hub that does not subscribe is not sent.

const (
	// FeatureMetrics is in `hello` when the agent samples its load and sends `metrics.sample` to a link that
	// subscribed to it by name (a link without a subscription list gets nothing: old hubs never see the event).
	FeatureMetrics = "metrics"
	// FeatureDrain is in `hello` when the agent serves runtime.restart with when = now, after_chats or drain, the
	// runtime.restart.cancel operation and the `restart` event.
	FeatureDrain = "drain"

	// EventMetricsSample is one reading of the machine's load, every MetricsEvery seconds.
	EventMetricsSample = "metrics.sample"
	// EventRestart is the progress of a runtime.restart job.
	EventRestart = "restart"

	// OpRuntimeRestartCancel takes back a restart that waits for the chats: {jobId}.
	OpRuntimeRestartCancel = "runtime.restart.cancel"

	// MetricsEvery is the seconds between two samples.
	MetricsEvery = 60
)

// When a runtime.restart happens.
const (
	RestartNow        = "now"
	RestartAfterChats = "after_chats" // wait for the chats at work to finish, new ones may start meanwhile
	RestartDrain      = "drain"       // refuse new chats, wait for the chats at work to finish, then restart
)

// Progress of a restart job, the `state` of a RestartEvent and of the answer to runtime.restart.
const (
	RestartWaiting    = "waiting"
	RestartRestarting = "restarting"
	RestartDone       = "done"
	RestartFailed     = "failed"
	RestartCancelled  = "cancelled"
)

// Hardware is what the machine is made of. Zero values mean unknown.
type Hardware struct {
	Cores     int    `json:"cores,omitempty"`
	RAMBytes  int64  `json:"ramBytes,omitempty"`
	DiskBytes int64  `json:"diskBytes,omitempty"` // the filesystem of the work folder
	Model     string `json:"model,omitempty"`     // CPU or machine model, as the system names it
	Laptop    bool   `json:"laptop,omitempty"`
	OS        string `json:"os,omitempty"`
	Arch      string `json:"arch,omitempty"`
}

// MetricsSample is the event `metrics.sample` {"event":"metrics.sample","sample":{…}}. A percent the machine could not
// read is left out (null), and so are ChatsNow when the runtime did not answer.
type MetricsSample struct {
	CPUPct   *float64 `json:"cpuPct"`
	RAMPct   *float64 `json:"ramPct"`
	DiskPct  *float64 `json:"diskPct"`
	ChatsNow *int     `json:"chatsNow"`
	At       int64    `json:"at"` // epoch ms of the machine's clock
}

// RestartParams are the parameters of runtime.restart.
type RestartParams struct {
	JobID string `json:"jobId"`
	When  string `json:"when"`
}

// RestartEvent is the event `restart` {"event":"restart","restart":{…}}.
type RestartEvent struct {
	JobID    string `json:"jobId"`
	State    string `json:"state"`
	ChatsNow *int   `json:"chatsNow,omitempty"`
	Error    string `json:"error,omitempty"`
}
