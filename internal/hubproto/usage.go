package hubproto

// What a machine reports to the hub about spend (docs/hub/usage.md): facts without the text of any conversation. The hub
// pulls them by a cursor: the agent says `usage.available`, the hub asks `usage.read` from its cursor, writes the batch
// in one transaction and says `usage.ack`. A record is never changed once it has a seq; a fact that changes (a session
// that goes on, a window measured again) is a new record, and the newest one by seq wins.

const (
	OpUsageRead         = "usage.read"
	OpUsageAck          = "usage.ack"
	EventUsageAvailable = "usage.available"
	// FeatureUsage is in `hello` when the agent keeps the outbox and answers usage.read and usage.ack.
	FeatureUsage = "usage"

	// UsageReadMax is the most records one usage.read returns; a larger max is cut to it.
	UsageReadMax = 1000
)

// Record kinds.
const (
	UsageCall    = "call"
	UsageSession = "session"
	UsageWindow  = "window"
	UsageBalance = "balance"
	// UsageTool counts the calls of one tool of a session on a day (docs/hub/environment.md); an old hub skips it.
	UsageTool = "tool"
)

// UsageAvailableEvent is sent after new records were written (at most every 10 s) and once after each connect.
type UsageAvailableEvent struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

// UsageReadParams asks for the records after seq After, at most Max (1..UsageReadMax).
type UsageReadParams struct {
	Epoch string `json:"epoch"` // the epoch the hub's cursor belongs to; "" for a first read
	After int64  `json:"after"`
	Max   int    `json:"max"`
}

// UsageReadResult is a batch in seq order. Last is the seq of its last record (After when it is empty); More says there
// are records after Last. When Epoch differs from the asked one the outbox was made anew (the machine's data folder
// wiped): the batch starts from the beginning of the new epoch whatever After was, and the hub moves its cursor there.
type UsageReadResult struct {
	Epoch   string        `json:"epoch"`
	Records []UsageRecord `json:"records"`
	Last    int64         `json:"last"`
	More    bool          `json:"more"`
}

// UsageAckParams tells the agent the hub has stored everything up to Seq of Epoch; it may drop those records.
type UsageAckParams struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

// UsageRecord is one fact; exactly the field named by Kind is set.
type UsageRecord struct {
	Seq     int64             `json:"seq"`
	Kind    string            `json:"kind"`
	At      int64             `json:"at"` // when the machine wrote it, epoch ms of the machine's clock
	Call    *UsageCallFact    `json:"call,omitempty"`
	Session *UsageSessionFact `json:"session,omitempty"`
	Window  *UsageWindowFact  `json:"window,omitempty"`
	Balance *UsageBalanceFact `json:"balance,omitempty"`
	Tool    *UsageToolFact    `json:"tool,omitempty"`
}

// UsageCallFact is one model call. MsgKey is the provider's own id of the message (Claude's message id / request id,
// Codex's response id): global, so the same call seen in a resumed, forked or moved conversation is one call.
type UsageCallFact struct {
	Provider     string `json:"provider"` // the agent CLI: claude, codex
	MsgKey       string `json:"msgKey"`
	SessionKey   string `json:"sessionKey"`
	Ts           int64  `json:"ts"` // epoch ms UTC
	Model        string `json:"model"`
	BaseURLHost  string `json:"baseUrlHost"` // where the call went: api.anthropic.com, api.kimi.com, …; "" for the default
	Input        int64  `json:"input"`
	Output       int64  `json:"output"`
	CacheRead    int64  `json:"cacheRead"`
	CacheWrite5m int64  `json:"cacheWrite5m"`
	CacheWrite1h int64  `json:"cacheWrite1h"`
}

// Session origins: a person's chat, an evaluation run, an automation (counted in money, not in KPI).
const (
	OriginLive       = "live"
	OriginEval       = "eval"
	OriginAutomation = "automation"

	AttributionExact   = "exact"   // the SessionStart hook recorded who started it and with which account
	AttributionGuessed = "guessed" // no record: the machine's creator and the machine's login
)

// UsageSessionFact is a session as it stands. It is sent again when it changes (new calls, an end, metrics); the
// newest by seq replaces the older on the hub.
type UsageSessionFact struct {
	Provider   string `json:"provider"`
	SessionKey string `json:"sessionKey"`
	// ParentKey is the parent session of a subagent: its money goes to the parent, its prompts and KPI do not.
	ParentKey   string `json:"parentKey"`
	StartedAt   int64  `json:"startedAt"`
	EndedAt     *int64 `json:"endedAt"` // null while it may go on
	Origin      string `json:"origin"`
	Subject     string `json:"subject"`    // the email of the person who started it (runtime.session), "" unknown
	AccountRef  string `json:"accountRef"` // see AccountRef rules in usage.md; "" unknown
	AuthMode    string `json:"authMode"`   // subscription, api_key, ""
	BaseURLHost string `json:"baseUrlHost"`
	Attribution string `json:"attribution"`
	// Metrics are the session's numbers from the machine's scanner (analytics.md, «KPI»); absent until it ran.
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// UsageWindowFact is one measurement of a rate-limit window of an account (provider-limits §3.1).
type UsageWindowFact struct {
	Provider   string   `json:"provider"`
	AccountRef string   `json:"accountRef"`
	Name       string   `json:"name"`  // five_hour, seven_day, seven_day_opus, …
	Scope      string   `json:"scope"` // all, or model:<family>
	LengthSec  int64    `json:"lengthSec"`
	Unit       string   `json:"unit"` // pct, tokens, requests, credits
	UsedPct    *float64 `json:"usedPct"`
	Used       *float64 `json:"used"`
	Limit      *float64 `json:"limit"`
	ResetsAt   *int64   `json:"resetsAt"`
	MeasuredAt int64    `json:"measuredAt"`
	Source     string   `json:"source"`     // statusline, oauth-usage, wham-usage, codexbar
	Confidence string   `json:"confidence"` // official, undocumented, estimated
}

// UsageBalanceFact is one measurement of a prepaid balance or a spend cap of an account.
type UsageBalanceFact struct {
	Provider      string   `json:"provider"`
	AccountRef    string   `json:"accountRef"`
	Currency      string   `json:"currency"` // USD, CNY, RUB, credits
	Available     *float64 `json:"available"`
	SpendCap      *float64 `json:"spendCap"`
	SpentInPeriod *float64 `json:"spentInPeriod"`
	MeasuredAt    int64    `json:"measuredAt"`
	Source        string   `json:"source"`
}
