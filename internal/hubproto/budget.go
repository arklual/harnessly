package hubproto

// What the hub tells a machine about limits (docs/hub/limits.md, «On the machine»): for each person, account and model
// that is near or past a limit, its status. A machine applies it to new chats (and, in stop mode, to running ones);
// whatever is not listed is ok. Without a link the machine keeps the last list it got.

const (
	OpBudgetSet = "budget.set"
	// FeatureBudget is in `hello` when the agent applies budget.set.
	FeatureBudget = "budget"

	BudgetOK   = "ok"
	BudgetWarn = "warn"
	BudgetStop = "stop"

	// Enforcement modes (HUB_LIMITS_ENFORCE): off sends nothing; warn makes a stop only a notice; stop stops.
	EnforceOff  = "off"
	EnforceWarn = "warn"
	EnforceStop = "stop"
)

// BudgetSet replaces the machine's whole list. Version grows with every change on the hub; the agent ignores a version
// not newer than the one it holds, and reports the one it holds in hello (Hello.BudgetVersion) so that the hub sends a
// new list only when the machine's is old.
type BudgetSet struct {
	Version  int64         `json:"version"`
	Mode     string        `json:"mode"`
	IssuedAt int64         `json:"issuedAt"`
	Entries  []BudgetEntry `json:"entries"`
}

// BudgetEntry is the tightest rule for one person × account × model. Account "" is any account; ModelPattern "" any
// model, else a family or model name as the price list keys it (a forbidden model is an entry with Forbid).
type BudgetEntry struct {
	User         string  `json:"user"` // the person's email, as runtime.session names them
	Account      string  `json:"account"`
	ModelPattern string  `json:"modelPattern"`
	Status       string  `json:"status"`
	Forbid       bool    `json:"forbid"`
	Pct          float64 `json:"pct"`      // used ÷ limit of the binding rule
	ResetsAt     int64   `json:"resetsAt"` // epoch ms when the binding rule's period ends; 0 for a forbidden model
	Reason       string  `json:"reason"`   // Russian, shown as is: «Лимит команды «Платформа» на октябрь исчерпан, сброс 1 ноября»
	RuleID       string  `json:"ruleId"`
}

// BudgetSetResult is the agent's answer: the version it now holds.
type BudgetSetResult struct {
	Version int64 `json:"version"`
}
