// Package wire is the v2 contract between a runtime and its pages: the data model, the socket frames and events, the
// commands of POST /api/v2/cmd/<op> and their results. schema/frames.json is the source of truth; these types and
// web/app/next/protocol/v2.ts mirror it, and the fixtures in testdata keep the three in step.
//
// Times in *At fields are unix milliseconds by the machine's clock. A union with a few fields (AgentState,
// MessageState, GateState, Entry) is one flat struct whose JSON carries exactly the fields of its kind; a union of
// frames, events or commands is an interface over one struct per kind.
package wire

import (
	"strconv"
	"strings"
	"time"
)

// Proto is the protocol version of this package.
const Proto = 2

// Ms is a unix time in milliseconds.
type Ms = int64

// Machine is one machine of the fleet; standalone there is one, "local".
type Machine struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Host       string         `json:"host"`
	Reach      Reach          `json:"reach"`
	LastSeenAt *Ms            `json:"lastSeenAt"`
	Runtime    *RuntimeInfo   `json:"runtime"` // nil: the runtime did not answer /api/v2/hello
	Agents     []MachineAgent `json:"agents"`
	Load       *MachineLoad   `json:"load,omitempty"` // only while a subscriber asked for {load: true}
	// Health is the machine's own judgement of its disk, memory and CPU; absent where the source does not judge it.
	Health *MachineHealth `json:"health,omitempty"`
}

// RuntimeInfo is what a machine's runtime says of itself.
type RuntimeInfo struct {
	Version  string    `json:"version"`
	Proto    int       `json:"proto"`
	Features []Feature `json:"features"`
}

// MachineAgent is one agent CLI on a machine: what it can be started with and whether a running one can change model.
type MachineAgent struct {
	Provider Provider           `json:"provider"`
	Version  *string            `json:"version"`
	Status   AgentInstallStatus `json:"status"`
	// Models are the models the CLI takes, the first being the one a new conversation offers; empty when the machine
	// does not know them.
	Models []AgentModel `json:"models"`
	// CanSwitchModel: a running conversation of this CLI takes the model command.
	CanSwitchModel bool `json:"canSwitchModel"`
	// Profiles are the vendor setups (Kimi, GLM, Grok) a conversation can start on through this CLI, each with its own
	// models. Only a page that names the `profiles` cap gets them; absent where there are none.
	Profiles []ProfileOffer `json:"profiles,omitempty"`
}

// AgentModel is one model of an agent CLI. Efforts are the reasoning efforts it takes, empty when it has no choice;
// DefaultEffort is the one it runs on when none is asked for.
type AgentModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Hint is one short line on what the model is for, "" when the CLI says nothing honest about it.
	Hint          string   `json:"hint"`
	Efforts       []Effort `json:"efforts"`
	DefaultEffort *Effort  `json:"defaultEffort"`
	// Ultracode: the model takes Claude Code's ultracode mode; absent when it does not.
	Ultracode bool `json:"ultracode,omitempty"`
}

// MachineLoad is a machine's load at one moment.
type MachineLoad struct {
	At         Ms      `json:"at"`
	CPU        float64 `json:"cpu"`
	MemUsed    int64   `json:"memUsed"`
	MemTotal   int64   `json:"memTotal"`
	DiskFree   int64   `json:"diskFree"`
	DiskTotal  int64   `json:"diskTotal"`
	BusyAgents int     `json:"busyAgents"`
}

// MachineHealth is how the machine judges its own load (internal/machines/health.go): ok, or what runs short and how
// badly. Level is the worst of Issues; Issues are worst first, then disk, mem, cpu, and empty when ok. Since is when the
// machine came to this level.
type MachineHealth struct {
	Level  HealthLevel   `json:"level"`
	Since  Ms            `json:"since"`
	Issues []HealthIssue `json:"issues"`
}

// HealthIssue is one metric past its line: warn or critical, never ok.
type HealthIssue struct {
	Metric HealthMetric `json:"metric"`
	Level  HealthLevel  `json:"level"`
}

// Project is a folder on a machine made a project; its id is the machine's own and is not to be parsed.
type Project struct {
	ID        string `json:"id"`
	MachineID string `json:"machineId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Git       bool   `json:"git"`
	CreatedAt Ms     `json:"createdAt"`
}

// Dir is a folder the person may make a project: one the machine offers under its default folder. Branch is the checked
// out branch of a git folder, nil for any other or one on a detached head; Empty: nothing in it.
type Dir struct {
	Path   string  `json:"path"`
	Name   string  `json:"name"`
	Git    bool    `json:"git"`
	Branch *string `json:"branch"`
	Empty  bool    `json:"empty"`
}

// AgentSession is a session an agent CLI kept on the machine, outside any conversation of the runtime: the person may
// continue it. Title is the agent's own name for it or the first thing asked; Turns counts the person's messages.
type AgentSession struct {
	Provider  Provider `json:"provider"`
	SessionID string   `json:"sessionId"`
	Title     string   `json:"title"`
	Cwd       string   `json:"cwd"`
	UpdatedAt Ms       `json:"updatedAt"`
	Turns     int      `json:"turns"`
}

// AgentSessions is the answer of GET /api/v2/sessions: the newest first.
type AgentSessions []AgentSession

// SessionsMax is how many sessions GET /api/v2/sessions offers.
const SessionsMax = 50

// Terminal is one of a conversation's own terminals: a shell in the conversation's folder, kept by the machine until it
// is closed. The id is a short lowercase token that carries no meaning to the page; the list is oldest first.
type Terminal struct {
	ID        string `json:"id"`
	CreatedAt Ms     `json:"createdAt"`
}

// Terminals is the answer of GET /api/v2/conversations/{id}/terminals.
type Terminals []Terminal

// TerminalsMax is how many own terminals a conversation may have at once.
const TerminalsMax = 8

// Dirs is the answer of GET /api/v2/dirs: the folders offered, git folders first, then by name.
type Dirs []Dir

// Model is a conversation's model; a nil Effort is the model's default. Ultracode is Claude Code's mode of working
// beside the effort: standing dynamic workflows with subagents on every task, at any effort level. Absent is off.
type Model struct {
	ID        string  `json:"id"`
	Effort    *Effort `json:"effort"`
	Ultracode bool    `json:"ultracode,omitempty"`
	// Profile is the backend profile the conversation runs on (ProfileOffer.ID); absent for the CLI's own account. It
	// is set when the conversation starts and does not change. Only a page that names the `profiles` cap gets it. A
	// command never carries it here: it names the profile beside the model (ConversationCreateCmd.Profile).
	Profile string `json:"profile,omitempty"`
}

// Conversation is one agent tab: its id is the runtime's tabId ("tab-…").
type Conversation struct {
	ID             string       `json:"id"`
	MachineID      string       `json:"machineId"`
	ProjectID      *string      `json:"projectId"`
	Cwd            string       `json:"cwd"`
	Provider       Provider     `json:"provider"`
	Model          *Model       `json:"model"`
	Title          string       `json:"title"`
	TitleSource    TitleSource  `json:"titleSource"`
	Pinned         bool         `json:"pinned"`
	CreatedAt      Ms           `json:"createdAt"`
	LastActivityAt Ms           `json:"lastActivityAt"`
	LastPromptAt   Ms           `json:"lastPromptAt"` // the person's last real message (createdAt until then); the sidebar orders by it
	State          AgentState   `json:"state"`
	SeenAt         Ms           `json:"seenAt"` // read up to; unread when LastTurn.EndedAt > SeenAt — not an agent state
	LastTurn       *TurnSummary `json:"lastTurn"`
	Queue          QueueSummary `json:"queue"`
	Gate           *GateRef     `json:"gate"`             // the open gate; its details are in the conv channel
	Goal           *Goal        `json:"goal,omitempty"`   // the agent's /goal; absent when it has none
	Launch         *Launch      `json:"launch,omitempty"` // set on a conversation started with others by one prompt
	// PendingModel is a change of model or effort the person asked for that the agent has not taken yet: the runtime
	// holds it and applies it at the first moment the agent can (see PendingModel). Absent when none waits.
	PendingModel *PendingModel `json:"pendingModel,omitempty"`
	// ModelLog is the changes of model the runtime applied, oldest first, the last ModelLogMax of them: the feed shows
	// each where it happened. Absent when there were none.
	ModelLog []ModelSwitch `json:"modelLog,omitempty"`
	Recap    *Recap        `json:"recap,omitempty"` // Claude's latest «while you were away» summary; absent when it has none
	// BgAgents is how many pieces of background work the agent waits for now — subagents, commands, monitors and a
	// scheduled wakeup together: a chat that waits for them is still at work although its prompt is free. Absent when
	// it waits for nothing. The name is from when only subagents counted; a client that knows nothing else still
	// draws the chat as at work. BgWait says what the number is made of.
	BgAgents int     `json:"bgAgents,omitempty"`
	BgWait   *BgWait `json:"bgWait,omitempty"`
	// TerminalStop says the conversation's terminal was stopped — by the person or for standing idle — while the agent
	// is not running again; the feed ends with a quiet row about it. Absent otherwise.
	TerminalStop *TerminalStop `json:"terminalStop,omitempty"`
	// TerminalFailure is a provider-neutral, non-retryable failure of the last turn. NeedsTarget means the runtime
	// will not retry or move the conversation itself: the person must choose where to continue.
	TerminalFailure *TerminalFailure `json:"terminalFailure,omitempty"`
	// Rewinds are the person's rewinds of the conversation to an earlier message of theirs, oldest first, the last
	// RewindsMax of them: the feed folds what each left behind where it was cut. Absent when there were none.
	Rewinds []RewindMark `json:"rewinds,omitempty"`
	// Origin is the conversation this one came out of: a fork of it, or its context handed to another agent or model.
	// Absent for a conversation started anew.
	Origin *Origin `json:"origin,omitempty"`
	// Limited says the agent's last turn ended on its plan's usage limit: the runtime holds the queue until the limit
	// resets and then goes on by itself (LimitHold). Absent otherwise.
	Limited *LimitHold `json:"limited,omitempty"`
	// AutoContinues are the times the runtime went on by itself — after a limit reset, after a restart cut a turn —
	// oldest first, the last AutoContinueMax of them: the feed draws each at the message it sent. Absent when none.
	AutoContinues []AutoContinue `json:"autoContinues,omitempty"`
	// ParentID is the conversation whose agent delegated this one (an agent's delegate_task): the sidebar nests it under
	// that one, the parent's feed shows it as a card. Absent for a chat the person started.
	ParentID *string `json:"parentId,omitempty"`
	Rev      int64   `json:"rev"`

	// Server only: the tmux session, the transcript and the agent's session id.
	Session        string `json:"-"`
	TranscriptPath string `json:"-"`
	AgentSessionID string `json:"-"`
}

// TerminalStop is when and why a conversation's terminal was stopped. IdleMs is how long it had stood idle when the
// runtime stopped it (By idle); absent for a stop the person asked for.
type TerminalStop struct {
	At     Ms             `json:"at"`
	By     TerminalStopBy `json:"by"`
	IdleMs int64          `json:"idleMs,omitempty"`
}

// TerminalFailure is a provider failure which ended a turn and requires an explicit target choice.
type TerminalFailure struct {
	Kind        TerminalFailureKind `json:"kind"`
	At          Ms                  `json:"at"`
	Detail      string              `json:"detail"`
	Terminal    bool                `json:"terminal"`
	NeedsTarget bool                `json:"needsTarget"`
}

// RewindMark is one rewind: the conversation went on from before one of the person's messages, with other words, and
// the turns from that message on were left behind (they stay readable: GET .../rewinds/{n}/turns).
type RewindMark struct {
	At Ms `json:"at"`
	// AfterTurnID is the last turn kept, after which the feed draws what was left; null when the cut kept none of the
	// turns the transcript showed (they were before the first one read).
	AfterTurnID *string `json:"afterTurnId"`
	// Turns is how many turns were left behind: the message's own and every one after it.
	Turns int `json:"turns"`
	// Files: the chat's folder was returned to how it was before the message too.
	Files bool `json:"files"`
	// UndoUntil is until when rewind.undo takes the conversation back to where it was.
	UndoUntil Ms `json:"undoUntil"`
}

// RewindsMax is how many rewinds a conversation keeps on its record.
const RewindsMax = 10

// RewindUndoMs is how long a rewind can be undone.
const RewindUndoMs = 15_000

// RewindPlan is what a rewind to one of the person's messages would do, before the person chooses: GET
// /api/v2/conversations/{id}/rewind?entryId=.
type RewindPlan struct {
	EntryID string `json:"entryId"`
	// Turns is how many turns the rewind leaves behind.
	Turns int `json:"turns"`
	// Files is the snapshot of the chat's folder from before the message and what returning to it changes; null when
	// there is none (the snapshots are taken when a turn ends).
	Files *RewindFiles `json:"files"`
}

// RewindFiles is the folder's snapshot a rewind can return to.
type RewindFiles struct {
	At Ms `json:"at"`
	// Changes are the files that would change, at most RewindChangesMax of them; More is how many more.
	Changes []RewindChange `json:"changes"`
	More    int            `json:"more"`
}

// RewindChangesMax is how many changed files a plan names.
const RewindChangesMax = 50

// RewindChange is one file a rewind of the folder changes.
type RewindChange struct {
	Path   string     `json:"path"`
	Change FileChange `json:"change"`
}

// FileChange is what returning to a snapshot does to a file.
type FileChange string

const (
	FileRestored FileChange = "restored" // it is not there now, the snapshot had it
	FileChanged  FileChange = "changed"
	FileRemoved  FileChange = "removed" // it is there now, the snapshot did not have it
)

// FileChanges is every FileChange, in the schema's order.
var FileChanges = []FileChange{FileRestored, FileChanged, FileRemoved}

// TerminalStopBy is who stopped a terminal: the person (terminal.stop) or the runtime, for standing idle.
type TerminalStopBy string

const (
	TerminalStopPerson TerminalStopBy = "person"
	TerminalStopIdle   TerminalStopBy = "idle"
)

// TerminalStopBys is every TerminalStopBy, in the schema's order.
var TerminalStopBys = []TerminalStopBy{TerminalStopPerson, TerminalStopIdle}

// Origin is the conversation another one came out of, as it was then. A fork (K fork) holds the other's history up to
// AtTurn (its last finished turn when absent): in an agent session of its own (Via session), or, where the agent CLI
// cannot fork there, as an excerpt of it in its first message (Via excerpt). A handoff (K handoff) runs on another
// agent or model and starts with the other's context in its first message: an excerpt of the transcript or a note the
// other's agent wrote (Via). At is when it was made.
type Origin struct {
	K            OriginKind `json:"k"`
	FromID       string     `json:"fromId"`
	FromTitle    string     `json:"fromTitle"`
	FromProvider Provider   `json:"fromProvider"`
	FromModel    *Model     `json:"fromModel"`
	AtTurn       *string    `json:"atTurn,omitempty"`
	Via          OriginVia  `json:"via"`
	At           Ms         `json:"at"`
}

// OriginKind is how a conversation came out of another one.
type OriginKind string

const (
	OriginFork    OriginKind = "fork"
	OriginHandoff OriginKind = "handoff"
)

// OriginKinds is every OriginKind, in the schema's order.
var OriginKinds = []OriginKind{OriginFork, OriginHandoff}

// OriginVia is what carried the other conversation's context: the agent's own forked session, an excerpt of the
// transcript, or a note the agent wrote.
type OriginVia string

const (
	ViaSession OriginVia = "session"
	ViaExcerpt OriginVia = "excerpt"
	ViaNote    OriginVia = "note"
	ViaHistory OriginVia = "history"
)

// OriginVias is every OriginVia, in the schema's order.
var OriginVias = []OriginVia{ViaSession, ViaExcerpt, ViaNote, ViaHistory}

// HandoffMode is how a conversation's context goes to a new one on another agent or model: an excerpt of its
// transcript, or a note its own agent writes for the next one.
type HandoffMode string

const (
	HandoffExcerpt HandoffMode = "excerpt"
	HandoffNote    HandoffMode = "note"
	HandoffHistory HandoffMode = "history"
)

// HandoffModes is every HandoffMode, in the schema's order.
var HandoffModes = []HandoffMode{HandoffExcerpt, HandoffNote, HandoffHistory}

// DraftMode is the text a handoff draft is: an excerpt or a note for a handoff, or the result note a fork sends back
// into the conversation it came from.
type DraftMode string

const (
	DraftExcerpt DraftMode = "excerpt"
	DraftNote    DraftMode = "note"
	DraftResult  DraftMode = "result"
	DraftHistory DraftMode = "history"
)

// DraftModes is every DraftMode, in the schema's order.
var DraftModes = []DraftMode{DraftExcerpt, DraftNote, DraftResult, DraftHistory}

// HandoffDraft answers POST /api/v2/conversations/{id}/handoff-draft {mode, provider?, model?}: the text a handoff
// or a merge would carry, for the page to show and the person to edit before it goes. Tokens is its size as the
// runtime estimates it (about four characters a token); Turns is how many of the conversation's turns an excerpt holds.
type HandoffDraft struct {
	Mode     DraftMode        `json:"mode"`
	Text     string           `json:"text,omitempty"`
	Tokens   int              `json:"tokens"`
	Turns    int              `json:"turns"`
	Artifact *HandoffArtifact `json:"artifact,omitempty"`
}

// HandoffArtifact is private handoff material held by the runtime. The page receives only its opaque ID and size,
// never the filesystem path.
type HandoffArtifact struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}

// HandoffTextMax is the longest text a handoff or a merge carries, in characters: the message it goes in, with its
// wrapping, stays under Limits.MaxText.
const HandoffTextMax = 90_000

// HandoffDraftRequest is the body of POST /api/v2/conversations/{id}/handoff-draft. Provider and Model name the agent
// the context goes to: an excerpt is cut to fit its window.
type HandoffDraftRequest struct {
	Mode     DraftMode `json:"mode"`
	Provider Provider  `json:"provider,omitempty"`
	Model    string    `json:"model,omitempty"`
	// Profile is the backend profile the context goes to (its models have windows of their own).
	Profile string `json:"profile,omitempty"`
}

// LimitHold is a conversation held on its agent's usage limit. At is when the turn ended on it; ResetsAt is when the
// limit resets, as the agent said or the limits it last reported (nil when neither names a time). ContinueAt is when the
// runtime goes on by itself — ResetsAt and a short jitter — nil when it will not: the person said no (limit cancel), the
// setting is off, or no time is known. Until then the queue is paused{limited}: what the person sends waits for it.
type LimitHold struct {
	Provider   Provider    `json:"provider"`
	Window     LimitWindow `json:"window"`
	At         Ms          `json:"at"`
	ResetsAt   *Ms         `json:"resetsAt"`
	ContinueAt *Ms         `json:"continueAt"`
}

// LimitWindow is which limit an agent hit: the five-hour session, the weekly one, or one it did not name.
type LimitWindow string

const (
	LimitSession LimitWindow = "session"
	LimitWeekly  LimitWindow = "weekly"
	LimitOther   LimitWindow = "other"
)

// LimitWindows is every LimitWindow, in the schema's order.
var LimitWindows = []LimitWindow{LimitSession, LimitWeekly, LimitOther}

// AutoContinue is one time the runtime went on by itself: why, when, and the message that went — its own (Own, the
// message's Auto set) or the one the person had queued meanwhile.
type AutoContinue struct {
	At       Ms              `json:"at"`
	Why      AutoContinueWhy `json:"why"`
	ClientID string          `json:"clientId"`
	Own      bool            `json:"own,omitempty"`
}

// AutoContinueWhy is why the runtime went on: the usage limit reset, or a restart of the runtime cut a turn short.
type AutoContinueWhy string

const (
	AutoAfterLimit   AutoContinueWhy = "limit"
	AutoAfterRestart AutoContinueWhy = "restart"
)

// AutoContinueWhys is every AutoContinueWhy, in the schema's order.
var AutoContinueWhys = []AutoContinueWhy{AutoAfterLimit, AutoAfterRestart}

// TerminalAside is a screen of the agent's terminal over its input box that Esc closes without changing anything.
type TerminalAside string

const (
	AsideLogin  TerminalAside = "login"
	AsideModel  TerminalAside = "model"
	AsideEffort TerminalAside = "effort"
)

// TerminalAsides is every TerminalAside, in the schema's order.
var TerminalAsides = []TerminalAside{AsideLogin, AsideModel, AsideEffort}

// AutoContinueMax is how many of its own continuations a conversation keeps.
const AutoContinueMax = 20

// LimitJitterMax is how far past ResetsAt the runtime goes on at most: chats held on one limit do not all start at once.
const LimitJitterMax = 90 * time.Second

// OnOff is a switch of the runtime's settings.
type OnOff string

const (
	On  OnOff = "on"
	Off OnOff = "off"
)

// OnOffs is every OnOff, in the schema's order.
var OnOffs = []OnOff{On, Off}

// IdleStop is after how long without work the runtime stops a conversation's terminal by itself; off never.
type IdleStop string

const (
	IdleStopOff IdleStop = "off"
	IdleStop1h  IdleStop = "1h"
	IdleStop2h  IdleStop = "2h"
	IdleStop6h  IdleStop = "6h"
)

// IdleStops is every IdleStop, in the schema's order; DefaultIdleStop is the one of a runtime nobody set.
var (
	IdleStops       = []IdleStop{IdleStopOff, IdleStop1h, IdleStop2h, IdleStop6h}
	DefaultIdleStop = IdleStop2h
)

// After is the time an IdleStop names; 0 for off or a value it does not know.
func (s IdleStop) After() time.Duration {
	switch s {
	case IdleStop1h:
		return time.Hour
	case IdleStop2h:
		return 2 * time.Hour
	case IdleStop6h:
		return 6 * time.Hour
	}
	return 0
}

// RuntimeSettings are the settings of the runtime itself, the machine's and not the person's: GET and PUT
// /api/v2/runtime-settings, kept in <data>/v2/runtime-settings.json.
//
// LimitContinue: a chat held on its usage limit goes on by itself when the limit resets. RestartContinue: a chat whose
// turn a restart of the runtime cut short goes on by itself once its agent is back. A PUT that leaves one out keeps it.
type RuntimeSettings struct {
	IdleStop        IdleStop `json:"idleStop"`
	LimitContinue   OnOff    `json:"limitContinue,omitempty"`
	RestartContinue OnOff    `json:"restartContinue,omitempty"`
	// Delegation is what agents may do with the chats they delegate; absent is DefaultDelegation, and a PUT without it
	// keeps what was set.
	Delegation *DelegationSettings `json:"delegation,omitempty"`
}

// DelegationSettings bound the agents' delegation: how many chats one conversation may have delegated (not deleted),
// and whether an agent may write to any chat of the machine rather than only to the ones it delegated.
type DelegationSettings struct {
	MaxChildren int  `json:"maxChildren"`
	AnyChat     bool `json:"anyChat"`
}

// DelegationMaxChildren is the most a person may let one conversation delegate; DefaultDelegation the setting nobody set.
const DelegationMaxChildren = 10

var DefaultDelegation = DelegationSettings{MaxChildren: 5}

// DelegationOf is the delegation settings in force: s.Delegation, else the defaults.
func (s RuntimeSettings) DelegationOf() DelegationSettings {
	if s.Delegation == nil {
		return DefaultDelegation
	}
	return *s.Delegation
}

// ModelLogMax is how many applied changes of model a conversation keeps.
const ModelLogMax = 20

// PendingModel is a model change accepted and not applied yet. It waits for the agent: not started (stopped, gone,
// never launched) it is applied right after the launch, before the first message; mid-turn, at the end of the turn,
// before the next queued message is typed. Applying is true while the runtime types it into the agent's box. The
// latest ask replaces the one waiting.
type PendingModel struct {
	ID        string  `json:"id"`
	Effort    *Effort `json:"effort"`
	Ultracode bool    `json:"ultracode,omitempty"`
	Since     Ms      `json:"since"`
	Applying  bool    `json:"applying"`
}

// ModelSwitch is a change of model the runtime applied: From is nil when the conversation had no model on record.
// Refused, when set, says the agent did not take the change To asked for (the record kept From): why, in the words of
// a command's refusal — conflict (the agent kept its model), internal (it never confirmed), option-unknown (the model
// does not take ultracode), agent-working (its screen was never at the prompt).
type ModelSwitch struct {
	At      Ms          `json:"at"`
	From    *Model      `json:"from"`
	To      Model       `json:"to"`
	Refused *RefuseCode `json:"refused,omitempty"`
}

// Goal is the /goal of an agent's session: the condition it keeps working toward. Since is when the goal was set.
type Goal struct {
	Text   string     `json:"text"`
	Status GoalStatus `json:"status"`
	Since  Ms         `json:"since"`
}

// Recap is the summary Claude Code writes for the person who comes back after being away. At is when it was written.
type Recap struct {
	Text string `json:"text"`
	At   Ms     `json:"at"`
}

// GoalStatus is where a goal is: active, paused (the agent stopped checking until the person writes), or met.
type GoalStatus string

const (
	GoalActive GoalStatus = "active"
	GoalPaused GoalStatus = "paused"
	GoalMet    GoalStatus = "met"
)

// GoalStatuses is every GoalStatus, in the schema's order.
var GoalStatuses = []GoalStatus{GoalActive, GoalPaused, GoalMet}

// QueueSummary is a conversation's queue as the sidebar needs it.
type QueueSummary struct {
	Count  int         `json:"count"`
	Paused *QueuePause `json:"paused"`
}

// GateRef points at a conversation's open question, permission or plan.
type GateRef struct {
	Kind GateKind `json:"kind"`
	ID   string   `json:"id"`
}

// AgentState is where a conversation's agent is. Only the fields of K are on the wire:
//
//	starting{since, step, wait} · working{since, turnId|null, compacting} · waiting-question{since, questionId} ·
//	waiting-permission{since, permissionId} · ready{since} · idle{since} · stopped{since, resumable} ·
//	launch-failed{since, reason: LaunchFailure} · gone{since, reason: GoneReason} · unknown{since}
//
// unknown is "not checked yet": for delivery it is not idle.
type AgentState struct {
	K            AgentStateKind `json:"k"`
	Since        Ms             `json:"since"`
	Step         LaunchStep     `json:"step"`
	Wait         *Wait          `json:"wait"` // starting: when the launch is slow and when it fails
	TurnID       *string        `json:"turnId"`
	Compacting   bool           `json:"compacting"`
	QuestionID   string         `json:"questionId"`
	PermissionID string         `json:"permissionId"` // a plan waits here too, its kind is in the Permission
	Resumable    bool           `json:"resumable"`
	Reason       string         `json:"reason"` // LaunchFailure for launch-failed, GoneReason for gone
}

// Turn is one turn: the user entry that opened it and the steps up to its end.
type Turn struct {
	ID            string       `json:"id"` // the id of the user entry that opened the turn
	Author        TurnAuthor   `json:"author"`
	StartedAt     Ms           `json:"startedAt"`
	EndedAt       *Ms          `json:"endedAt"`
	DurationMs    *int64       `json:"durationMs"`
	ToolCount     int          `json:"toolCount"`
	StepCount     int          `json:"stepCount"`
	Status        TurnStatus   `json:"status"`
	Failure       *TurnFailure `json:"failure"`
	AnswerEntryID *string      `json:"answerEntryId"` // the last reply, shown in the folded turn
	Files         []TurnFile   `json:"files"`
	// Outputs are the things the turn made that the person opens: a published document, a design, a document of the
	// repository, a merge request, a stand. Absent when there are none.
	Outputs []TurnOutput `json:"outputs,omitempty"`
}

// TurnAuthor is who answered in a turn.
type TurnAuthor struct {
	Provider Provider `json:"provider"`
	Role     *string  `json:"role"`
}

// TurnFailure is why a turn failed; Detail is the agent's raw text (data, not a translation).
type TurnFailure struct {
	Code    TurnFailureCode `json:"code"`
	Detail  *string         `json:"detail"`
	RetryAt *Ms             `json:"retryAt"`
}

// TurnFile is a file a turn touched.
type TurnFile struct {
	Path string `json:"path"`
	Op   FileOp `json:"op"`
}

// TurnOutput is a thing a turn made: what it is, whether it is new, what it is called and what opens it — a link (an
// artifact, a merge request, a stand), a design's id, or a file's path (a document).
type TurnOutput struct {
	Kind  OutputKind `json:"kind"`
	Op    OutputOp   `json:"op"`
	Title string     `json:"title"`
	Ref   string     `json:"ref"`
	// A delivered file (kind delivery): Ref is its path; its name, type, size in bytes and sha256 as the platform read them.
	Name   string `json:"name,omitempty"`
	Mime   string `json:"mime,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// TurnSummary is a turn as the sidebar row and the notifications need it.
type TurnSummary struct {
	ID         string     `json:"id"`
	Status     TurnStatus `json:"status"`
	StartedAt  Ms         `json:"startedAt"`
	EndedAt    *Ms        `json:"endedAt"`
	DurationMs *int64     `json:"durationMs"`
	ToolCount  int        `json:"toolCount"`
	Snippet    string     `json:"snippet"` // at most SnippetMax characters
	// Failure is why a failed turn failed, so a notification can tell a limit from an error; absent otherwise.
	// Not filled yet: a page built before this field rejects a record with a field it does not know, and open tabs keep
	// their build for days (the update banner asks, it does not reload). Pages already read it; a later release fills it
	// (FailureCode) once those builds are gone.
	Failure TurnFailureCode `json:"failure,omitempty"`
}

// FailureCode is the code of t's failure, or "" when it did not fail.
func FailureCode(t Turn) TurnFailureCode {
	if t.Failure == nil {
		return ""
	}
	return t.Failure.Code
}

// SnippetMax is the longest TurnSummary.Snippet, in characters.
const SnippetMax = 160

// ProjectNameMax is the longest name of a project, in characters.
const ProjectNameMax = 120

// TurnCounts are a turn's counters; they ride inside step events.
type TurnCounts struct {
	ToolCount int `json:"toolCount"`
	StepCount int `json:"stepCount"`
}

// Entry is one record of the feed. ID, TurnID, At and Rev are on every kind; the rest only on its T:
//
//	user{text, attachments, clientId|null} · say{md} · think{text} ·
//	tool{toolUseId, tool, summary, ok|null, output|null} · subagent{agentId, agentType, description, status} ·
//	answers{questionId, pairs} · gate{permissionId} · compaction{tokensBefore|null, tokensAfter|null, keep} ·
//	notice{level, code, detail|null} · interrupt{}
//
// The id is stable: "c:<jsonl uuid>#<k>" for Claude, "x:<byte offset>#<k>" for Codex and lines without a uuid,
// "live:<turnId>" for the live reply off the screen. Rev grows as the entry is appended to.
type Entry struct {
	ID     string    `json:"id"`
	TurnID *string   `json:"turnId"`
	At     Ms        `json:"at"`
	Rev    int64     `json:"rev"`
	T      EntryKind `json:"t"`

	Text         string          `json:"text"` // user, think
	Attachments  []AttachmentRef `json:"attachments"`
	ClientID     *string         `json:"clientId"`
	MD           string          `json:"md"`
	ToolUseID    string          `json:"toolUseId"`
	Tool         string          `json:"tool"`
	Summary      string          `json:"summary"`
	OK           *bool           `json:"ok"`
	Output       *ToolOutput     `json:"output"`
	AgentID      string          `json:"agentId"`
	AgentType    string          `json:"agentType"`
	Description  string          `json:"description"`
	Status       SubagentStatus  `json:"status"`
	Model        *string         `json:"model"`      // subagent: the model it was asked to run on
	DurationMs   *int64          `json:"durationMs"` // subagent: how long it ran, once it has finished
	QuestionID   string          `json:"questionId"`
	Pairs        []AnswerPair    `json:"pairs"`
	PermissionID string          `json:"permissionId"`
	TokensBefore *int64          `json:"tokensBefore"`
	TokensAfter  *int64          `json:"tokensAfter"`
	Keep         []string        `json:"keep"`
	Level        NoticeLevel     `json:"level"`
	Code         string          `json:"code"`
	Detail       *string         `json:"detail"`
	Peers        []PeerNote      `json:"peers"` // peer: what other agents' sessions wrote to this one
}

// QuestionID is the id of the question asked by a tool use.
func QuestionID(toolUseID string) string { return "q:" + toolUseID }

// PermissionID is the id of the permission asked by a tool use, or "p:<tabId>:<eventSeq>" for one read off the screen.
func PermissionID(toolUseID string) string { return "p:" + toolUseID }

// ValidQuestionID and ValidPermissionID report whether an id carries its kind's prefix.
func ValidQuestionID(id string) bool   { return len(id) > 2 && strings.HasPrefix(id, "q:") }
func ValidPermissionID(id string) bool { return len(id) > 2 && strings.HasPrefix(id, "p:") }

// ClaudeEntryID is the id of entry k of a Claude transcript line.
func ClaudeEntryID(lineUUID string, k int) string {
	return "c:" + strings.ToLower(lineUUID) + "#" + strconv.Itoa(k)
}

// OffsetEntryID is the id of entry k of a transcript line without a uuid (Codex), by the line's byte offset.
func OffsetEntryID(offset int64, k int) string {
	return "x:" + strconv.FormatInt(offset, 10) + "#" + strconv.Itoa(k)
}

// LiveEntryID is the id of a turn's live reply read off the screen, until its transcript line replaces it.
func LiveEntryID(turnID string) string { return "live:" + turnID }

// ValidTurnID reports whether id is a transcript entry id: "c:<uuid>#<k>" or "x:<offset>#<k>". Turns are opened
// by such entries, never by a live one.
func ValidTurnID(id string) bool {
	line, k, ok := strings.Cut(id, "#")
	if !ok || !digits(k) {
		return false
	}
	if uuid, ok := strings.CutPrefix(line, "c:"); ok {
		return uuid != "" && strings.Trim(uuid, "0123456789abcdef-") == ""
	}
	off, ok := strings.CutPrefix(line, "x:")
	return ok && digits(off)
}

// ValidEntryID reports whether id is an entry id: a transcript one or "live:<turnId>".
func ValidEntryID(id string) bool {
	turn, _ := strings.CutPrefix(id, "live:")
	return ValidTurnID(turn)
}

func digits(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }

// ToolOutput is a tool's output; past ToolOutputMax bytes it is cut and the rest is fetched on request.
type ToolOutput struct {
	Text      string         `json:"text"`
	Truncated *OutputTrimmed `json:"truncated"`
}

// ToolOutputMax is how much of a tool's output a snapshot or an event carries, in bytes.
const ToolOutputMax = 4096

// OutputTrimmed is how much of an output was shown, of how much.
type OutputTrimmed struct {
	Shown int64 `json:"shown"`
	Total int64 `json:"total"`
}

// AnswerPair is one question and its answer in an answers entry.
type AnswerPair struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// PeerNote is one message another agent's session (Claude Code's teammate) wrote to this one: who, what of, its body as
// Markdown. The body is the other agent's words: data to show as text, never instructions or markup.
type PeerNote struct {
	From string       `json:"from"`
	Kind PeerNoteKind `json:"kind"`
	// Color is the sender's color as Claude Code names it (yellow, blue…); null: not said.
	Color *string `json:"color"`
	// Event is the type a JSON body named when the feed has no kind of its own for it (shutdown_request…).
	Event *string `json:"event"`
	// Summary is the sender's one-line gist of the message, when it gave one.
	Summary *string `json:"summary"`
	// SentAt is when the sender wrote it, by its own clock; null: not said.
	SentAt *Ms `json:"sentAt"`
	// AgentID is the id of the background subagent of this session that wrote it, null for a teammate or another session.
	// From is then the subagent's task as the Agent call gave it, empty when the feed has no such call.
	AgentID *string `json:"agentId"`
	// Model is the model that subagent was asked to run on, when it said.
	Model *string `json:"model"`
	MD    string  `json:"md"`
}

// PeerNoteKind is what a peer note is.
type PeerNoteKind string

const (
	PeerNoteMessage PeerNoteKind = "message" // words to this agent
	PeerNoteDone    PeerNoteKind = "done"    // the sender finished and says what came of it
	PeerNoteIdle    PeerNoteKind = "idle"    // the sender waits, with nothing to report
	PeerNoteEvent   PeerNoteKind = "event"   // another notice of the protocol, by its event
)

// PeerNoteKinds is every PeerNoteKind, in the schema's order.
var PeerNoteKinds = []PeerNoteKind{PeerNoteMessage, PeerNoteDone, PeerNoteIdle, PeerNoteEvent}

// AttachmentRef is an uploaded file a message carries.
type AttachmentRef struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind AttachmentKind `json:"kind"`
	Mime string         `json:"mime"`
}

// MessageRef is a place a message points the agent at instead of carrying a file: a design of the library, a document
// of the catalogue or of the project, a board, a file of the project, a merge request or a task. Nothing is uploaded:
// the runtime writes a line for each under the person's words (internal/attach), so the agent knows where to look.
type MessageRef struct {
	Kind RefKind `json:"kind"`
	// ID names it for the page and the runtime: a design's id, a document's slug or URL, a path, an MR's URL, a task key.
	ID string `json:"id"`
	// Title is what the chip says.
	Title string `json:"title"`
	// Target is where it is for the agent: a URL or a path; "" for a design, which the runtime finds by its id.
	Target string `json:"target"`
}

// RefsMax is how many references one message or draft carries.
const RefsMax = 10

// Message is one message to an agent; ClientID, set by the page, is its idempotency key.
type Message struct {
	ClientID       string          `json:"clientId"`
	ConversationID string          `json:"conversationId"`
	Text           string          `json:"text"`
	Attachments    []AttachmentRef `json:"attachments"`
	// Refs are the places it points at; Text already ends with their block. Absent when none.
	Refs []MessageRef `json:"refs,omitempty"`
	Mode MessageMode  `json:"mode"`
	Kind MessageKind  `json:"kind"`
	// Auto is set on a message the runtime wrote itself to go on (AutoContinue): the feed draws it as a quiet line,
	// not as the person's words. Absent on the person's messages.
	Auto AutoContinueWhy `json:"auto,omitempty"`
	// Cleared is the screen the person left open in the agent's terminal (their /login, a picker) that was closed
	// before the message went in: the chat says so under it. Absent when nothing was in the way.
	Cleared   TerminalAside `json:"cleared,omitempty"`
	CreatedAt Ms            `json:"createdAt"`
	Attempts  int           `json:"attempts"`
	State     MessageState  `json:"state"`
	Rev       int64         `json:"rev"`

	// Server only: when the text was typed and how long the transcript was then, to find its user entry.
	TypedAt     Ms    `json:"-"`
	TypedOffset int64 `json:"-"`
}

// MessageState is where a message is. Only the fields of K are on the wire:
//
//	queued{position, reason, wait|null} · typing{wait} · in-input{wait} · accepted{entryId|null} · in-turn{turnId} · done{turnId} ·
//	failed{code, retryable, at} · cancelled{by: "user"}
//
// draft, uploading and submitting live only on the page and never cross the wire.
type MessageState struct {
	K         MessageStateKind `json:"k"`
	Position  int              `json:"position"` // 1 is the head of the queue
	Reason    QueueReason      `json:"reason"`
	Wait      *Wait            `json:"wait"`    // queued (nil: agent-busy and behind have no term), typing, in-input
	EntryID   *string          `json:"entryId"` // nil: the hook confirmed it, the transcript line is not read yet
	TurnID    string           `json:"turnId"`
	Code      MessageFailure   `json:"code"`
	Retryable bool             `json:"retryable"`
	At        Ms               `json:"at"`
	By        string           `json:"by"`
}

// CancelledByUser is the only author of a cancel.
const CancelledByUser = "user"

// Queue is a conversation's queue: client ids, the head first.
type Queue struct {
	ConversationID string      `json:"conversationId"`
	Items          []string    `json:"items"`
	Paused         *QueuePause `json:"paused"`
	Rev            int64       `json:"rev"`
	// Stalled are the items that wait stalled, for the older form of the queue (a page without CapMsgStatus does not
	// list them: it holds them as failed). Server only.
	Stalled []string `json:"-"`
}

// QueuePause is why and since when a queue is paused.
type QueuePause struct {
	Reason PauseReason `json:"reason"`
	At     Ms          `json:"at"`
}

// Question is an AskUserQuestion; its id is "q:" and the tool use id. Question and permission ids share one space
// within a conversation, told apart by their prefix: the runtime guarantees it.
type Question struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversationId"`
	TurnID         string         `json:"turnId"`
	AskedAt        Ms             `json:"askedAt"`
	Items          []QuestionItem `json:"items"`
	State          GateState      `json:"state"`
}

// QuestionItem is one question of an AskUserQuestion.
type QuestionItem struct {
	Header   string           `json:"header"`
	Question string           `json:"question"`
	Multi    bool             `json:"multi"`
	Options  []QuestionOption `json:"options"`
}

// QuestionOption is one choice of a question.
type QuestionOption struct {
	Label       string  `json:"label"`
	Description *string `json:"description"`
	Preview     *string `json:"preview"`
}

// Permission is a tool permission or a plan to approve; its id is "p:" and the tool use id, or "p:<tabId>:<eventSeq>"
// when the agent gave none (Claude: a Notification and the menu on the screen). TurnID is nil outside a turn.
type Permission struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversationId"`
	TurnID         *string            `json:"turnId"`
	Kind           PermissionKind     `json:"kind"`
	AskedAt        Ms                 `json:"askedAt"`
	Tool           *string            `json:"tool"`
	Command        *string            `json:"command"`
	Cwd            *string            `json:"cwd"`
	PlanMD         *string            `json:"planMd"`
	Options        []PermissionOption `json:"options"` // read off the screen
	State          GateState          `json:"state"`
}

// PermissionOption is one choice of a permission menu.
type PermissionOption struct {
	ID    string          `json:"id"`
	Label string          `json:"label"`
	Scope PermissionScope `json:"scope"`
}

// GateState is where a question or a permission is. Only the fields of K are on the wire:
//
//	open{} · answering{opId, wait} · answered{at, byDevice} · closed{reason}
type GateState struct {
	K        GateStateKind   `json:"k"`
	OpID     string          `json:"opId"`
	Wait     *Wait           `json:"wait"`
	At       Ms              `json:"at"`
	ByDevice string          `json:"byDevice"`
	Reason   GateCloseReason `json:"reason"`
	// Refused: an open gate the page decided on whose menu the machine could not match on the agent's screen; no key
	// went in, the person decides in the terminal. Absent otherwise.
	Refused *GateRefusal `json:"refused"`
	// AutoDenyAt: an open permission whose prompt says Claude denies it by itself (an unattended session) — when it
	// does (epoch ms), read off the screen. Absent otherwise.
	AutoDenyAt *Ms `json:"autoDenyAt"`
}

// BgWait is what an agent whose prompt is free still waits for, by kind; the counts that are 0 are absent. Its parts
// add up to Conversation.BgAgents.
type BgWait struct {
	Agents    int `json:"agents,omitempty"`    // background subagents running
	Teammates int `json:"teammates,omitempty"` // named teammates at work (one idle waits for a message, it is not waited for)
	Commands  int `json:"commands,omitempty"`  // background shell commands running
	Monitors  int `json:"monitors,omitempty"`  // monitors watching
	Workflows int `json:"workflows,omitempty"` // dynamic workflows running
	// Command is the title of the command when it is the only one running; absent otherwise.
	Command string `json:"command,omitempty"`
	// WakeupAt is when the agent's own scheduled wakeup comes (epoch ms); absent when none is pending.
	WakeupAt *Ms `json:"wakeupAt,omitempty"`
}

// Total is how many pieces of work the wait holds.
func (w BgWait) Total() int {
	n := w.Agents + w.Teammates + w.Commands + w.Monitors + w.Workflows
	if w.WakeupAt != nil {
		n++
	}
	return n
}

// BackgroundTask is a subagent or a command running beside a conversation's turn.
type BackgroundTask struct {
	ID             string           `json:"id"`
	ConversationID string           `json:"conversationId"`
	Kind           BackgroundKind   `json:"kind"`
	Title          string           `json:"title"`
	StartedAt      Ms               `json:"startedAt"`
	EndedAt        *Ms              `json:"endedAt"`
	Status         BackgroundStatus `json:"status"`
	DevServer      bool             `json:"devServer"`
	Stoppable      bool             `json:"stoppable"`
	OutputPath     *string          `json:"outputPath"`
	// Monitor: a command kind that is the agent's Monitor, not a shell. Absent for the rest.
	Monitor bool `json:"monitor,omitempty"`
	// Teammate: a subagent that is a named teammate of the agent (its title is the name, its role what it was started
	// for). Only a teammate is ever idle. Absent for the rest.
	Teammate bool `json:"teammate,omitempty"`
	// Subagents only: the model it runs on (the alias or the full id), the effort when the agent says it, its kind
	// («Explore», «general-purpose») and the last thing it did, one line. Absent when unknown.
	Model  *string `json:"model,omitempty"`
	Effort *Effort `json:"effort,omitempty"`
	Role   *string `json:"role,omitempty"`
	Last   *string `json:"last,omitempty"`
	// Workflow: what a workflow's run says — its phases and the agents it started in each. Only a workflow has it.
	Workflow *WorkflowRun `json:"workflow,omitempty"`
}

// WorkflowRun is a dynamic workflow as its files tell it: the name and description of its script, the Workflow call
// that started it (its tool entry in the feed) and its phases in the script's order, each with the agents started in
// it so far.
type WorkflowRun struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ToolUseID   *string         `json:"toolUseId"`
	Phases      []WorkflowPhase `json:"phases"`
}

// WorkflowPhase is a stage of a workflow; a phase no agent has started in yet has none.
type WorkflowPhase struct {
	Title  string          `json:"title"`
	Agents []WorkflowAgent `json:"agents"`
}

// WorkflowAgent is an agent a workflow started: its label in the script, the model it runs on, where it is, its last
// step and the first words of what it returned. Its id names its transcript, so it opens in the pane as a subagent.
type WorkflowAgent struct {
	ID        string           `json:"id"`
	Label     string           `json:"label"`
	Status    BackgroundStatus `json:"status"`
	StartedAt *Ms              `json:"startedAt"`
	EndedAt   *Ms              `json:"endedAt"`
	Model     *string          `json:"model"`
	Last      *string          `json:"last"`
	Result    *string          `json:"result"`
}

// ShotsMax is how many pictures a conversation lists.
const ShotsMax = 30

// Shot is a picture an agent looked at: a file it read, or one a tool (a browser's screenshot) saved. Its bytes are
// GET /api/v2/conversations/{id}/shot?id=<id>, served only for a picture listed here. Agent is the subagent that
// looked at it (its description); absent when the agent itself did. At is when it did.
type Shot struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	At    Ms      `json:"at"`
	Agent *string `json:"agent,omitempty"`
}

// PicturesMax is how many pictures named in messages a conversation lists.
const PicturesMax = 60

// Picture is a picture a message of the conversation names by its path (the agent's «Скриншот: /tmp/…/home.png», a
// pasted upload's path in the person's words): Text is the path exactly as the message writes it, so the page finds it
// in the text. Listed only while the file is there, inside the conversation's folder, a Claude scratchpad or the
// uploads, and a picture by its first bytes. Its bytes are GET /api/v2/conversations/{id}/shot?id=<id>, as a shot's.
type Picture struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Now is what a working agent is doing this moment; the page ticks the timer itself.
type Now struct {
	TurnID  string     `json:"turnId"`
	Thought *string    `json:"thought"`
	Action  *NowAction `json:"action"`
	Word    *string    `json:"word"` // the live reply off the screen
	At      Ms         `json:"at"`
}

// NowAction is the tool call in progress.
type NowAction struct {
	Tool    string  `json:"tool"`
	Summary string  `json:"summary"`
	Command *string `json:"command"`
}

// Limits are what a runtime accepts; the page checks them before it sends.
type Limits struct {
	MaxQueue       int   `json:"maxQueue"`
	MaxText        int   `json:"maxText"` // UTF-16 units
	MaxAttachments int   `json:"maxAttachments"`
	MaxImageBytes  int64 `json:"maxImageBytes"`
	MaxFileBytes   int64 `json:"maxFileBytes"`
	SlowMs         int64 `json:"slowMs"`       // a command unanswered this long shows "дольше обычного"
	CmdBudgetMs    int64 `json:"cmdBudgetMs"`  // the runtime answers every command within this, refused{timeout} at worst
	CmdTimeoutMs   int64 `json:"cmdTimeoutMs"` // the page gives up on a command after this and offers retry or cancel
}

// DefaultLimits are the limits of prompts.MaxQueuedPerTab, prompts.MaxQueuedText and the mockup's file sizes.
var DefaultLimits = Limits{
	MaxQueue: 20, MaxText: 100_000, MaxAttachments: 20, MaxImageBytes: 10 << 20, MaxFileBytes: 50 << 20,
	SlowMs: 3000, CmdBudgetMs: 10_000, CmdTimeoutMs: 12_000,
}

// Wait is the term of a state that waits on the runtime: past SlowAt the page shows "дольше обычного", and by
// DeadlineAt the runtime has moved the state on (failed, launch-failed, back to open) — it never waits longer.
type Wait struct {
	SlowAt     Ms `json:"slowAt"`
	DeadlineAt Ms `json:"deadlineAt"`
}

// The terms of the waits of the runtime: §12 of the spec asks for "дольше обычного" at 3 s and an error by 10–15 s.
const (
	WaitSlowMs     = 3000
	TypingDeadline = 12_000 // typing → in-input, else queued again or failed{type-failed}
	// ConfirmDeadline: in-input → accepted, else the message goes back to the head of the queue, stalled (a steered
	// one, which may sit in Claude's own queue, fails unconfirmed instead).
	ConfirmDeadline = 30_000
	AnswerDeadline  = 10_000 // answering → answered or back to open
	LaunchSlowMs    = 15_000
	LaunchDeadline  = 150_000 // starting → idle, else launch-failed (prompts/rules.go: 150 s without a prompt box)
)

// MachineSnapshot is the data of a machine snapshot.
type MachineSnapshot struct {
	Machine Machine `json:"machine"`
}

// ConvsSnapshot is the data of a convs snapshot. Projects are the folders of the machine made projects, oldest first;
// DefaultCwd is the folder a conversation starts in when the person names none (the machine's home), empty when the
// machine has none to offer.
type ConvsSnapshot struct {
	Conversations []Conversation `json:"conversations"`
	Projects      []Project      `json:"projects"`
	DefaultCwd    string         `json:"defaultCwd"`
	Favorites     *Favorites     `json:"favorites,omitempty"` // absent while nothing is grouped or placed
	Drafts        []Draft        `json:"drafts,omitempty"`    // the composers' drafts kept on the machine; absent while none
}

// ConvSnapshot is the data of a conv:<id> snapshot. Turns are the last turns, oldest first; Entries are the steps
// of the last three and the answer entries of the rest. BeforeTurnID is the cursor for older turns, nil at the
// start. With mode "after" Entries are those after the cursor and Turns the turns they touch.
type ConvSnapshot struct {
	Turns        []Turn           `json:"turns"`
	Entries      []Entry          `json:"entries"`
	BeforeTurnID *string          `json:"beforeTurnId"`
	Queue        Queue            `json:"queue"`
	Messages     []Message        `json:"messages"` // not final, and final ones of the last 10 minutes
	Questions    []Question       `json:"questions"`
	Permissions  []Permission     `json:"permissions"`
	Background   []BackgroundTask `json:"background"`
	// Shots are the pictures the agent and its subagents looked at lately, newest first (see Shot); absent when none.
	Shots []Shot `json:"shots,omitempty"`
	// Pictures are the pictures the conversation's messages name, the latest named last (see Picture); absent when none.
	Pictures []Picture `json:"pictures,omitempty"`
	Now      *Now      `json:"now"`
}

// DeviceHeader carries the device id on every HTTP request of the page, as hello does on the socket: the page makes
// the id once and keeps it in localStorage. It names the device in answered{byDevice}.
const DeviceHeader = "X-Harnessly-Device"

// The turns page: limit defaults to TurnsPageDefault; below 1 or above TurnsPageMax it is refused{bad-request}.
// A page that cannot answer sends a refusal, {r: "refused", code}, as a command does.
const (
	TurnsPageDefault = 30
	TurnsPageMax     = 100
)

// Hello is the answer of GET /api/v2/hello; a 404 there is a runtime without v2.
type Hello struct {
	Proto    int       `json:"proto"`
	Version  string    `json:"version"`
	Features []Feature `json:"features"`
}

// TurnsPage is the answer of GET /api/v2/conversations/{id}/turns?before=&limit=: turns oldest first and their
// answer entries.
type TurnsPage struct {
	Turns        []Turn  `json:"turns"`
	Entries      []Entry `json:"entries"`
	BeforeTurnID *string `json:"beforeTurnId"`
}

// EntriesPage is the answer of GET .../turns/{turnId}/entries.
type EntriesPage struct {
	TurnID  string  `json:"turnId"`
	Entries []Entry `json:"entries"`
}

// OutputPage is the answer of GET .../entries/{entryId}/output: the whole output.
type OutputPage struct {
	EntryID string     `json:"entryId"`
	Output  ToolOutput `json:"output"`
}

// Channel is a subscription of the socket: "machine", "convs" or "conv:<convId>".
type Channel string

const (
	ChannelMachine Channel = "machine"
	ChannelConvs   Channel = "convs"
	convPrefix             = "conv:"
)

// ConvChannel is the channel of one conversation.
func ConvChannel(id string) Channel { return Channel(convPrefix + id) }

// ConvID is the conversation of a conv channel; ok is false for the other channels.
func (c Channel) ConvID() (id string, ok bool) {
	id, ok = strings.CutPrefix(string(c), convPrefix)
	return id, ok && ValidConvID(id)
}

// Valid reports whether c is a channel of this protocol.
func (c Channel) Valid() bool {
	_, conv := c.ConvID()
	return c == ChannelMachine || c == ChannelConvs || conv
}

// ValidConvID reports whether id is a conversation id: a runtime tabId, "tab-" and [A-Za-z0-9_-].
func ValidConvID(id string) bool {
	rest, ok := strings.CutPrefix(id, "tab-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// ClientFrame is a frame the page sends on the socket: HelloFrame, SubFrame, UnsubFrame or PingFrame.
type ClientFrame interface {
	ClientFrameKind() FrameKind
	Validate() error
}

// ServerFrame is a frame the runtime sends on the socket: WelcomeFrame, SnapshotFrame, EvFrame, PongFrame or
// ErrorFrame.
type ServerFrame interface {
	ServerFrameKind() FrameKind
	Validate() error
}

// HelloFrame opens the socket; Device is the device's stable id.
type HelloFrame struct {
	Proto  int    `json:"proto"`
	Device string `json:"device"`
	App    string `json:"app"`
}

// SubFrame subscribes to a channel. Epoch and AfterSeq replay the ring; AfterEntryID asks for what follows that
// entry when the ring cannot.
type SubFrame struct {
	Ch           Channel  `json:"ch"`
	Epoch        string   `json:"epoch,omitempty"`
	AfterSeq     *int64   `json:"afterSeq,omitempty"`
	AfterEntryID string   `json:"afterEntryId,omitempty"`
	Opts         *SubOpts `json:"opts,omitempty"`
}

// SubOpts are a subscription's options; Load asks the machine channel for machine_load.
type SubOpts struct {
	Load *bool `json:"load,omitempty"`
}

// UnsubFrame ends a subscription.
type UnsubFrame struct {
	Ch Channel `json:"ch"`
}

// PingFrame is the page's heartbeat; T comes back in the pong.
type PingFrame struct {
	T int64 `json:"t"`
}

// WelcomeFrame answers hello.
type WelcomeFrame struct {
	Proto      int       `json:"proto"`
	Epoch      string    `json:"epoch"` // new on every start of the runtime
	ServerTime Ms        `json:"serverTime"`
	Machine    Machine   `json:"machine"`
	Features   []Feature `json:"features"`
	Limits     Limits    `json:"limits"`
}

// SnapshotFrame is a channel's state up to Seq. The hub subscribes the channel before it takes the snapshot, so
// no event is lost between them; the page drops the events with seq ≤ Seq that arrive after it, and applies every
// event idempotently by id and rev, so an event the snapshot already holds changes nothing. Its data is the one of the channel: Machine, Convs or Conv.
type SnapshotFrame struct {
	Ch      Channel          `json:"ch"`
	Seq     int64            `json:"seq"` // the last seq the snapshot includes; 0 before any event
	Mode    SnapshotMode     `json:"mode"`
	Machine *MachineSnapshot `json:"-"`
	Convs   *ConvsSnapshot   `json:"-"`
	Conv    *ConvSnapshot    `json:"-"`
}

// EvFrame is one event of a channel; a gap in Seq means a new sub with the cursor.
type EvFrame struct {
	Ch  Channel `json:"ch"`
	Seq int64   `json:"seq"`
	E   Event   `json:"e"`
}

// PongFrame answers ping.
type PongFrame struct {
	T int64 `json:"t"`
}

// ErrorFrame refuses a frame; Ch is the channel it was about, if any.
type ErrorFrame struct {
	Ch   Channel        `json:"ch,omitempty"`
	Code FrameErrorCode `json:"code"`
}

func (HelloFrame) ClientFrameKind() FrameKind    { return FrameHello }
func (SubFrame) ClientFrameKind() FrameKind      { return FrameSub }
func (UnsubFrame) ClientFrameKind() FrameKind    { return FrameUnsub }
func (PingFrame) ClientFrameKind() FrameKind     { return FramePing }
func (WelcomeFrame) ServerFrameKind() FrameKind  { return FrameWelcome }
func (SnapshotFrame) ServerFrameKind() FrameKind { return FrameSnapshot }
func (EvFrame) ServerFrameKind() FrameKind       { return FrameEv }
func (PongFrame) ServerFrameKind() FrameKind     { return FramePong }
func (ErrorFrame) ServerFrameKind() FrameKind    { return FrameError }

// Event is one change on a channel. The events of the machine channel are MachineEventTypes, of convs
// ConvsEventTypes and of conv:<id> ConvEventTypes.
type Event interface {
	EventType() EventType
	Validate() error
}

// MachineStatusEv: the machine's reach, runtime, agents or health changed. An event without health leaves the health
// the page holds as it was.
type MachineStatusEv struct {
	Reach   Reach          `json:"reach"`
	Runtime *RuntimeInfo   `json:"runtime"`
	Agents  []MachineAgent `json:"agents"`
	Health  *MachineHealth `json:"health,omitempty"`
}

// MachineLoadEv is the machine's load, every 2 s while a subscriber asked for it.
type MachineLoadEv struct {
	Load MachineLoad `json:"load"`
}

// ConversationUpsertEv is a conversation's whole record.
type ConversationUpsertEv struct {
	Conv Conversation `json:"conv"`
}

// ConversationStateEv is a conversation's new agent state; Rev is the record's revision after it.
type ConversationStateEv struct {
	ID    string     `json:"id"`
	State AgentState `json:"state"`
	Rev   int64      `json:"rev"`
}

// ConversationSeenEv is a conversation's new seenAt.
type ConversationSeenEv struct {
	ID     string `json:"id"`
	SeenAt Ms     `json:"seenAt"`
	Rev    int64  `json:"rev"`
}

// ConversationRemovedEv: the conversation is deleted or gone from the list.
type ConversationRemovedEv struct {
	ID string `json:"id"`
}

// ProjectUpsertEv is a project's whole record, new or the same again.
type ProjectUpsertEv struct {
	Project Project `json:"project"`
}

// FavoritesUpsertEv is the favorites' whole layout, after any change of it.
type FavoritesUpsertEv struct {
	Favorites Favorites `json:"favorites"`
}

// TurnStartedEv: a turn opened.
type TurnStartedEv struct {
	Turn Turn `json:"turn"`
}

// StepEv is a new entry; Counts are its turn's counters after it, nil outside a turn.
type StepEv struct {
	Entry  Entry       `json:"entry"`
	Counts *TurnCounts `json:"counts"`
}

// EntryUpdateEv is an entry with the same id and a higher rev.
type EntryUpdateEv struct {
	Entry Entry `json:"entry"`
}

// EntryReplaceEv swaps an entry for another id, as the live reply for its transcript line.
type EntryReplaceEv struct {
	OldID string `json:"oldId"`
	Entry Entry  `json:"entry"`
}

// NowEv is what the agent does this moment, at most twice a second.
type NowEv struct {
	Now
}

// QuestionAskedEv: the agent asks a question.
type QuestionAskedEv struct {
	Question Question `json:"question"`
}

// PermissionRequestedEv: the agent asks for a tool permission.
type PermissionRequestedEv struct {
	Permission Permission `json:"permission"`
}

// PlanProposedEv: the agent proposes a plan; Permission.Kind is plan.
type PlanProposedEv struct {
	Permission Permission `json:"permission"`
}

// GateUpdatedEv is a question's or a permission's new state.
type GateUpdatedEv struct {
	ID    string    `json:"id"`
	State GateState `json:"state"`
}

// CompactionEv: a compaction started or finished; Entry is its compaction entry.
type CompactionEv struct {
	Phase CompactionPhase `json:"phase"`
	Entry Entry           `json:"entry"`
}

// TurnFinishedEv: a turn ended, done or interrupted.
type TurnFinishedEv struct {
	Turn Turn `json:"turn"`
}

// TurnFailedEv: a turn failed; Turn.Failure says why.
type TurnFailedEv struct {
	Turn Turn `json:"turn"`
}

// QuotaExhaustedEv says the provider cannot accept another request on the current target. It is emitted once for the
// failed turn; the conversation record carries the durable marker.
type QuotaExhaustedEv struct {
	Failure TerminalFailure `json:"failure"`
}

// QueueChangedEv is the conversation's whole queue.
type QueueChangedEv struct {
	Queue Queue `json:"queue"`
}

// PromptStateEv is a message's new state, on every server-side step of it.
type PromptStateEv struct {
	ClientID string       `json:"clientId"`
	State    MessageState `json:"state"`
	Rev      int64        `json:"rev"`
}

// MessageUpsertEv is a message's whole record.
type MessageUpsertEv struct {
	Message Message `json:"message"`
}

// BackgroundUpsertEv is a background task's whole record.
type BackgroundUpsertEv struct {
	Task BackgroundTask `json:"task"`
}

// BackgroundRemovedEv: a background task is gone from the list.
type BackgroundRemovedEv struct {
	ID string `json:"id"`
}

func (MachineStatusEv) EventType() EventType       { return EvMachineStatus }
func (MachineLoadEv) EventType() EventType         { return EvMachineLoad }
func (ConversationUpsertEv) EventType() EventType  { return EvConversationUpsert }
func (ConversationStateEv) EventType() EventType   { return EvConversationState }
func (ConversationSeenEv) EventType() EventType    { return EvConversationSeen }
func (ConversationRemovedEv) EventType() EventType { return EvConversationRemoved }
func (ProjectUpsertEv) EventType() EventType       { return EvProjectUpsert }
func (FavoritesUpsertEv) EventType() EventType     { return EvFavoritesUpsert }
func (DraftUpsertEv) EventType() EventType         { return EvDraftUpsert }
func (TurnStartedEv) EventType() EventType         { return EvTurnStarted }
func (StepEv) EventType() EventType                { return EvStep }
func (EntryUpdateEv) EventType() EventType         { return EvEntryUpdate }
func (EntryReplaceEv) EventType() EventType        { return EvEntryReplace }
func (NowEv) EventType() EventType                 { return EvNow }
func (QuestionAskedEv) EventType() EventType       { return EvQuestionAsked }
func (PermissionRequestedEv) EventType() EventType { return EvPermissionRequested }
func (PlanProposedEv) EventType() EventType        { return EvPlanProposed }
func (GateUpdatedEv) EventType() EventType         { return EvGateUpdated }
func (CompactionEv) EventType() EventType          { return EvCompaction }
func (TurnFinishedEv) EventType() EventType        { return EvTurnFinished }
func (TurnFailedEv) EventType() EventType          { return EvTurnFailed }
func (QuotaExhaustedEv) EventType() EventType      { return EvQuotaExhausted }
func (QueueChangedEv) EventType() EventType        { return EvQueueChanged }
func (PromptStateEv) EventType() EventType         { return EvPromptState }
func (MessageUpsertEv) EventType() EventType       { return EvMessageUpsert }

// ShotsSetEv is the whole list of pictures, newest first, when it changed.
type ShotsSetEv struct {
	Shots []Shot `json:"shots"`
}

func (ShotsSetEv) EventType() EventType { return EvShotsSet }

// FeedResetEv is the feed as it is now, after a rewind or its undo replaced the transcript under it: the page drops the
// turns and entries it holds and takes these (the newest turns, as a snapshot's), with BeforeTurnID the cursor for
// older ones.
type FeedResetEv struct {
	Turns        []Turn  `json:"turns"`
	Entries      []Entry `json:"entries"`
	BeforeTurnID *string `json:"beforeTurnId"`
}

func (FeedResetEv) EventType() EventType { return EvFeedReset }

// PicturesSetEv is the whole list of pictures the messages name, when it changed.
type PicturesSetEv struct {
	Pictures []Picture `json:"pictures"`
}

func (PicturesSetEv) EventType() EventType       { return EvPicturesSet }
func (BackgroundUpsertEv) EventType() EventType  { return EvBackgroundUpsert }
func (BackgroundRemovedEv) EventType() EventType { return EvBackgroundRemoved }

// Command is the body of POST /api/v2/cmd/<op>. OpKey is its opId: the same opId with the same body gets the same
// result, with another body refused{conflict}.
type Command interface {
	Op() Op
	OpKey() string
	Validate() error
}

// SendCmd puts a message in the conversation's queue; its opId is its clientId.
type SendCmd struct {
	OpID           string      `json:"opId"`
	ClientID       string      `json:"clientId"`
	ConversationID string      `json:"conversationId"`
	Text           string      `json:"text"`
	AttachmentIDs  []string    `json:"attachmentIds"`
	Mode           MessageMode `json:"mode"`
	Kind           MessageKind `json:"kind"`
	// Refs are the places the message points at; the runtime appends their block to Text. Absent when none.
	Refs []MessageRef `json:"refs,omitempty"`
}

// CancelCmd takes a queued message out of the queue.
type CancelCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	ClientID       string `json:"clientId"`
}

// RetryCmd queues a failed message again, attempts+1.
type RetryCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	ClientID       string `json:"clientId"`
}

// PromoteCmd moves a queued message to now or to steer.
type PromoteCmd struct {
	OpID           string    `json:"opId"`
	ConversationID string    `json:"conversationId"`
	ClientID       string    `json:"clientId"`
	To             PromoteTo `json:"to"`
}

// QueueMoveCmd puts a queued message before another queued one of the same conversation (BeforeClientID null: at the
// end of the line). Only a message that is queued moves; one being typed, taken or past that stays where it is.
type QueueMoveCmd struct {
	OpID           string  `json:"opId"`
	ConversationID string  `json:"conversationId"`
	ClientID       string  `json:"clientId"`
	BeforeClientID *string `json:"beforeClientId"`
}

// AnswerCmd answers a question: Answers, one per item, or ChatAbout to talk it over in words.
type AnswerCmd struct {
	OpID           string           `json:"opId"`
	ConversationID string           `json:"conversationId"`
	QuestionID     string           `json:"questionId"`
	Answers        []QuestionAnswer `json:"answers,omitempty"`
	ChatAbout      bool             `json:"chatAbout,omitempty"`
}

// QuestionAnswer is the answer to item I: the indexes of the chosen options, or the person's own text instead of
// them — never both, and never neither.
type QuestionAnswer struct {
	I       int     `json:"i"`
	Choices []int   `json:"choices"`
	Text    *string `json:"text,omitempty"`
}

// DecideCmd picks an option of a permission or a plan.
type DecideCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	PermissionID   string `json:"permissionId"`
	OptionID       string `json:"optionId"`
}

// InterruptCmd presses Escape once; the queue pauses unless PauseQueue is false.
type InterruptCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	PauseQueue     *bool  `json:"pauseQueue,omitempty"`
}

// Pauses reports whether the interrupt pauses the queue; it does unless the page said false.
func (c InterruptCmd) Pauses() bool { return c.PauseQueue == nil || *c.PauseQueue }

// QueueResumeCmd lifts a queue's pause.
type QueueResumeCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
}

// SeenCmd moves seenAt to max(seenAt, UpTo) of one conversation or of all (ConversationID = SeenAll).
type SeenCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	UpTo           Ms     `json:"upTo"`
}

// SeenAll is the conversation id of a seen for every conversation.
const SeenAll = "all"

// ConversationCreateCmd opens a conversation in a project or a folder; First is queued as soon as it launches. The
// accepted result's ID is the new conversation's id: the page subscribes to its conv channel with afterSeq 0,
// and the ring holds that channel from its first event, so the first message's states are not lost.
type ConversationCreateCmd struct {
	OpID      string   `json:"opId"`
	ProjectID string   `json:"projectId,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Provider  Provider `json:"provider"`
	Model     *Model   `json:"model"` // nil: the agent's default
	// Profile starts the conversation on a backend profile (ProfileOffer.ID) of the Provider's CLI, with one of its
	// models; empty: the CLI's own account. The profile needs a saved key and cannot be changed later.
	Profile string        `json:"profile,omitempty"`
	First   *FirstMessage `json:"first,omitempty"`
	// Worktree starts the conversation in a git worktree of its own, on a new branch off the folder's HEAD; a folder
	// that is no git repository is used as it is.
	Worktree bool `json:"worktree,omitempty"`
	// Launch marks the conversation as one of several started by the same prompt on different models.
	Launch *Launch `json:"launch,omitempty"`
	// ParentID makes the new conversation a delegated one of that conversation (see Conversation.ParentID).
	ParentID string `json:"parentId,omitempty"`
	// Title names the conversation from the start (as a rename would); empty takes the first message's line.
	Title string `json:"title,omitempty"`
}

// Launch is what the conversations of one prompt started on several models share: the id of the launch and the prompt.
type Launch struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

// LaunchPromptMax is the longest prompt a launch keeps, in characters.
const LaunchPromptMax = 200

// FirstMessage is the message a new conversation starts with.
type FirstMessage struct {
	ClientID      string   `json:"clientId"`
	Text          string   `json:"text"`
	AttachmentIDs []string `json:"attachmentIds"`
}

// ConversationRenameCmd sets a conversation's title.
type ConversationRenameCmd struct {
	OpID  string `json:"opId"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ConversationRetitleCmd asks the agent's cheap model to name the conversation anew from what it holds. The title
// comes back on the record (source «agent»), a name the person typed gives way to it: they asked.
type ConversationRetitleCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
}

// ConversationPinCmd pins or unpins a conversation.
type ConversationPinCmd struct {
	OpID   string `json:"opId"`
	ID     string `json:"id"`
	Pinned bool   `json:"pinned"`
}

// ConversationMoveCmd moves a conversation to a project, or out of any (nil).
type ConversationMoveCmd struct {
	OpID      string  `json:"opId"`
	ID        string  `json:"id"`
	ProjectID *string `json:"projectId"`
}

// ConversationDeleteCmd hides a conversation for 7 days; Force deletes a working one too.
type ConversationDeleteCmd struct {
	OpID  string `json:"opId"`
	ID    string `json:"id"`
	Force *bool  `json:"force,omitempty"`
}

// ConversationForkCmd branches a conversation off another one after a finished turn (AtTurn; the last finished one
// when absent). The new conversation keeps the other's folder, project, agent and model, and holds its history up to
// there; the other stays as it is. The accepted result's ID is the new conversation's id, on convs.
type ConversationForkCmd struct {
	OpID   string  `json:"opId"`
	ID     string  `json:"id"`
	AtTurn *string `json:"atTurn,omitempty"`
}

// ConversationHandoffCmd opens a conversation on Provider and Model, in the folder and project of conversation ID, with
// that conversation's context: Text is the excerpt or the note (the draft the page showed, as the person left it),
// which the runtime wraps into the new conversation's first message. The conversation it came from stays as it is.
// The accepted result's ID is the new conversation's id, on convs.
type ConversationHandoffCmd struct {
	OpID     string   `json:"opId"`
	ID       string   `json:"id"`
	Provider Provider `json:"provider"`
	Model    *Model   `json:"model"`
	// Profile starts the new conversation on a backend profile (see ConversationCreateCmd.Profile); empty: the CLI's
	// own account. A handoff never inherits the profile of the conversation it comes from: it is how a chat moves to
	// another one.
	Profile string      `json:"profile,omitempty"`
	Mode    HandoffMode `json:"mode"`
	Text    string      `json:"text,omitempty"`
	// ArtifactID is the opaque server-owned history artifact. It is present only in history mode.
	ArtifactID string `json:"artifactId,omitempty"`
}

// ConversationMergeCmd sends a fork's result back into the conversation it came from: Text (the note the fork's agent
// wrote, as the person left it), wrapped with where it comes from, goes into that conversation's queue as a message
// whose client id is OpID. The answer is that message's, on the other conversation's channel, with its id.
type ConversationMergeCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

// TerminalStopCmd ends the terminal of a conversation: its tmux session and everything that runs in it (the agent and
// what the agent started). The conversation and its history stay; the next message starts the agent again on its
// session. Force stops a working agent too.
type TerminalStopCmd struct {
	OpID  string `json:"opId"`
	ID    string `json:"id"`
	Force *bool  `json:"force,omitempty"`
}

// RewindCmd goes on from before one of the person's messages (EntryID, a turn's opening user entry) with Text in its
// place: the agent starts again on the conversation as it was before that message, and the turns from it on are left
// behind. Files returns the chat's folder to its snapshot from before the message too. Only while the agent is idle.
type RewindCmd struct {
	OpID    string `json:"opId"`
	ID      string `json:"id"`
	EntryID string `json:"entryId"`
	Text    string `json:"text"`
	Files   bool   `json:"files"`
}

// RewindUndoCmd takes the conversation back to where the last rewind took it from (and the folder, when that rewind
// returned it), while the rewind's UndoUntil has not passed.
type RewindUndoCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
}

// LimitCmd decides what a conversation held on its usage limit does: now goes on at once (the person's queued message,
// or the runtime's own word to go on), cancel leaves the chat to wait without going on by itself, auto goes on by
// itself at the reset again. Refused{not-found} when the conversation is not held.
type LimitCmd struct {
	OpID   string      `json:"opId"`
	ID     string      `json:"id"`
	Action LimitAction `json:"action"`
}

// LimitAction is what limit asks.
type LimitAction string

const (
	LimitNow    LimitAction = "now"
	LimitCancel LimitAction = "cancel"
	LimitAuto   LimitAction = "auto"
)

// LimitActions is every LimitAction, in the schema's order.
var LimitActions = []LimitAction{LimitNow, LimitCancel, LimitAuto}

// ConversationRestoreCmd brings a deleted conversation back within its 7 days.
type ConversationRestoreCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
}

// ConversationResumeCmd starts an agent again on a session: a stopped conversation's own (ID), or one the agent CLI
// kept on the machine that no conversation holds (Provider, SessionID, Cwd), which opens a new conversation. Exactly one
// of the two forms.
type ConversationResumeCmd struct {
	OpID      string   `json:"opId"`
	ID        string   `json:"id,omitempty"`
	Provider  Provider `json:"provider,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
}

// ModelCmd switches a conversation's model: at once, or queued after the turn. Ultracode turns the mode on or off
// with it; absent keeps the mode the conversation runs.
type ModelCmd struct {
	OpID      string  `json:"opId"`
	ID        string  `json:"id"`
	Model     string  `json:"model"`
	Effort    *Effort `json:"effort"`
	Ultracode *bool   `json:"ultracode,omitempty"`
}

// BgStopCmd stops a background task.
type BgStopCmd struct {
	OpID           string `json:"opId"`
	ConversationID string `json:"conversationId"`
	TaskID         string `json:"taskId"`
}

// GoalCmd sets the session's /goal (Claude only): «/goal text» is typed into the agent, and an empty text clears
// it («/goal clear»). It queues as a command message; the goal on the record follows what the transcript says.
type GoalCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

// GoalTextMax is the longest goal a page may set, in characters.
const GoalTextMax = 2000

// ProjectCreateCmd makes a folder of the machine a project. Name is what the person calls it, the folder's own name
// when empty. Without a path the project is a named group of chats that start in the machine's default folder.
type ProjectCreateCmd struct {
	OpID string `json:"opId"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

// ProjectUpdateCmd changes a project: Path, the folder its new chats start in (chats already made stay where they
// are); Name, what the person calls it. An empty field is left as it is, one of them is needed.
type ProjectUpdateCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

// FavGroupCreateCmd makes an empty group in the favorites; the accepted result's ID is the new group's id.
type FavGroupCreateCmd struct {
	OpID string `json:"opId"`
	Name string `json:"name"`
}

// FavGroupRenameCmd renames a group of the favorites.
type FavGroupRenameCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FavGroupDeleteCmd removes a group; its chats stay in the favorites, ungrouped.
type FavGroupDeleteCmd struct {
	OpID string `json:"opId"`
	ID   string `json:"id"`
}

// FavGroupMoveCmd puts a group before another one (BeforeID), or last when BeforeID is nil.
type FavGroupMoveCmd struct {
	OpID     string  `json:"opId"`
	ID       string  `json:"id"`
	BeforeID *string `json:"beforeId"`
}

// FavPlaceCmd puts a conversation into the favorites — pinning it when it is not — in a group (GroupID) or ungrouped
// (nil), before another conversation of that place (BeforeID) or last when BeforeID is nil. A conversation already in
// the favorites is moved.
type FavPlaceCmd struct {
	OpID     string  `json:"opId"`
	ID       string  `json:"id"`
	GroupID  *string `json:"groupId"`
	BeforeID *string `json:"beforeId"`
}

func (SendCmd) Op() Op                { return OpSend }
func (CancelCmd) Op() Op              { return OpCancel }
func (RetryCmd) Op() Op               { return OpRetry }
func (PromoteCmd) Op() Op             { return OpPromote }
func (QueueMoveCmd) Op() Op           { return OpQueueMove }
func (AnswerCmd) Op() Op              { return OpAnswer }
func (DecideCmd) Op() Op              { return OpDecide }
func (InterruptCmd) Op() Op           { return OpInterrupt }
func (QueueResumeCmd) Op() Op         { return OpQueueResume }
func (SeenCmd) Op() Op                { return OpSeen }
func (ConversationCreateCmd) Op() Op  { return OpConversationCreate }
func (ConversationRenameCmd) Op() Op  { return OpConversationRename }
func (ConversationPinCmd) Op() Op     { return OpConversationPin }
func (ConversationRetitleCmd) Op() Op { return OpConversationRetitle }
func (ConversationMoveCmd) Op() Op    { return OpConversationMove }
func (ConversationDeleteCmd) Op() Op  { return OpConversationDelete }
func (TerminalStopCmd) Op() Op        { return OpTerminalStop }
func (RewindCmd) Op() Op              { return OpRewind }
func (RewindUndoCmd) Op() Op          { return OpRewindUndo }
func (LimitCmd) Op() Op               { return OpLimit }
func (ConversationRestoreCmd) Op() Op { return OpConversationRestore }
func (ConversationResumeCmd) Op() Op  { return OpConversationResume }
func (ModelCmd) Op() Op               { return OpModel }
func (ConversationForkCmd) Op() Op    { return OpConversationFork }
func (ConversationHandoffCmd) Op() Op { return OpConversationHandoff }
func (ConversationMergeCmd) Op() Op   { return OpConversationMerge }
func (BgStopCmd) Op() Op              { return OpBgStop }
func (ProjectCreateCmd) Op() Op       { return OpProjectCreate }
func (ProjectUpdateCmd) Op() Op       { return OpProjectUpdate }
func (FavGroupCreateCmd) Op() Op      { return OpFavGroupCreate }
func (FavGroupRenameCmd) Op() Op      { return OpFavGroupRename }
func (FavGroupDeleteCmd) Op() Op      { return OpFavGroupDelete }
func (FavGroupMoveCmd) Op() Op        { return OpFavGroupMove }
func (FavPlaceCmd) Op() Op            { return OpFavPlace }
func (GoalCmd) Op() Op                { return OpGoal }
func (DraftSetCmd) Op() Op            { return OpDraftSet }

func (c SendCmd) OpKey() string                { return c.OpID }
func (c CancelCmd) OpKey() string              { return c.OpID }
func (c RetryCmd) OpKey() string               { return c.OpID }
func (c PromoteCmd) OpKey() string             { return c.OpID }
func (c QueueMoveCmd) OpKey() string           { return c.OpID }
func (c AnswerCmd) OpKey() string              { return c.OpID }
func (c DecideCmd) OpKey() string              { return c.OpID }
func (c InterruptCmd) OpKey() string           { return c.OpID }
func (c QueueResumeCmd) OpKey() string         { return c.OpID }
func (c SeenCmd) OpKey() string                { return c.OpID }
func (c ConversationCreateCmd) OpKey() string  { return c.OpID }
func (c ConversationRenameCmd) OpKey() string  { return c.OpID }
func (c ConversationPinCmd) OpKey() string     { return c.OpID }
func (c ConversationRetitleCmd) OpKey() string { return c.OpID }
func (c ConversationMoveCmd) OpKey() string    { return c.OpID }
func (c ConversationDeleteCmd) OpKey() string  { return c.OpID }
func (c TerminalStopCmd) OpKey() string        { return c.OpID }
func (c RewindCmd) OpKey() string              { return c.OpID }
func (c RewindUndoCmd) OpKey() string          { return c.OpID }
func (c LimitCmd) OpKey() string               { return c.OpID }
func (c ConversationRestoreCmd) OpKey() string { return c.OpID }
func (c ConversationResumeCmd) OpKey() string  { return c.OpID }
func (c ModelCmd) OpKey() string               { return c.OpID }
func (c ConversationForkCmd) OpKey() string    { return c.OpID }
func (c ConversationHandoffCmd) OpKey() string { return c.OpID }
func (c ConversationMergeCmd) OpKey() string   { return c.OpID }
func (c BgStopCmd) OpKey() string              { return c.OpID }
func (c ProjectCreateCmd) OpKey() string       { return c.OpID }
func (c ProjectUpdateCmd) OpKey() string       { return c.OpID }
func (c FavGroupCreateCmd) OpKey() string      { return c.OpID }
func (c FavGroupRenameCmd) OpKey() string      { return c.OpID }
func (c FavGroupDeleteCmd) OpKey() string      { return c.OpID }
func (c FavGroupMoveCmd) OpKey() string        { return c.OpID }
func (c FavPlaceCmd) OpKey() string            { return c.OpID }
func (c GoalCmd) OpKey() string                { return c.OpID }
func (c DraftSetCmd) OpKey() string            { return c.OpID }

// CmdResult is the answer of a command. Accepted and queued carry the channel and seq from which the effect shows
// on the socket — the page waits for that seq, not for a timer — and Rev or ID when the command made one;
// refused carries only Code.
type CmdResult struct {
	R    ResultKind `json:"r"`
	Ch   Channel    `json:"ch"`
	Seq  int64      `json:"seq"`
	Rev  *int64     `json:"rev"`
	ID   *string    `json:"id"`
	Code RefuseCode `json:"code"`
}

// Accepted is an accepted result whose effect shows on ch from seq on.
func Accepted(ch Channel, seq int64) CmdResult { return CmdResult{R: ResultAccepted, Ch: ch, Seq: seq} }

// Queued is a queued result whose effect shows on ch from seq on.
func Queued(ch Channel, seq int64) CmdResult { return CmdResult{R: ResultQueued, Ch: ch, Seq: seq} }

// Refused is a refused result.
func Refused(code RefuseCode) CmdResult { return CmdResult{R: ResultRefused, Code: code} }

// The closed lists of the protocol, in the order of schema/frames.json.

// Provider is the agent CLI behind a conversation.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// Providers is every Provider, in the schema's order.
var Providers = []Provider{ProviderClaude, ProviderCodex}

// Feature is a capability a runtime announces in hello and welcome.
type Feature string

const (
	FeatureQueue       Feature = "queue"
	FeatureSteer       Feature = "steer"
	FeatureQuestions   Feature = "questions"
	FeaturePermissions Feature = "permissions"
	FeatureBackground  Feature = "background"
	FeatureModel       Feature = "model"
	FeatureVoice       Feature = "voice"
	FeatureFiles       Feature = "files"
	FeatureCheckpoints Feature = "checkpoints"
	FeatureDesignly    Feature = "designly"
	FeatureSearch      Feature = "search"
	FeatureVault       Feature = "vault"
	FeaturePush        Feature = "push"
	FeatureTerminal    Feature = "terminal"
	FeatureMachine     Feature = "machine"
	FeatureApps        Feature = "apps"
	FeatureSettings    Feature = "settings"
	FeatureDrafts      Feature = "drafts"
	FeatureQueueMove   Feature = "queue-move"
	// FeatureTerminalStop: terminal.stop ends a conversation's terminal, the runtime stops idle ones by itself, and
	// GET/PUT /api/v2/runtime-settings say after how long.
	FeatureTerminalStop Feature = "terminal-stop"
	FeatureAppsHub      Feature = "apps-hub"
	// FeatureRewind: rewind and rewind.undo go on from before one of the person's messages, GET .../rewind?entryId=
	// says what that would do and GET .../rewinds/{n}/turns shows what a rewind left behind.
	FeatureRewind Feature = "rewind"
	// FeatureFork: conversation.fork branches a conversation off another one, and conversation.merge sends a fork's
	// result back (POST …/handoff-draft {mode: result} writes it).
	FeatureFork Feature = "fork"
	// FeatureHandoff: conversation.handoff opens a conversation on another agent with this one's context, drafted by
	// POST …/handoff-draft.
	FeatureHandoff Feature = "handoff"
	// FeatureAutoContinue: a chat held on its usage limit (Conversation.Limited) goes on by itself at the reset, limit
	// moves or cancels that, and a turn a restart cut short goes on by itself (runtime settings limitContinue,
	// restartContinue).
	FeatureAutoContinue Feature = "auto-continue"
)

// Features is every Feature, in the schema's order.
var Features = []Feature{FeatureQueue, FeatureSteer, FeatureQuestions, FeaturePermissions, FeatureBackground, FeatureModel, FeatureVoice, FeatureFiles, FeatureCheckpoints, FeatureDesignly, FeatureSearch, FeatureVault, FeaturePush, FeatureTerminal, FeatureMachine, FeatureApps, FeatureSettings, FeatureDrafts, FeatureQueueMove, FeatureAppsHub, FeatureTerminalStop, FeatureRewind, FeatureFork, FeatureHandoff, FeatureAutoContinue}

// Reach is whether the cluster hears a machine; standalone it is always online.
type Reach string

const (
	ReachOnline      Reach = "online"
	ReachUnreachable Reach = "unreachable"
	ReachUnknown     Reach = "unknown"
)

// Reaches is every Reach, in the schema's order.
var Reaches = []Reach{ReachOnline, ReachUnreachable, ReachUnknown}

// HealthLevel is how a machine stands: ok, warn (worth a look) or critical (work is about to fail).
type HealthLevel string

const (
	HealthOK       HealthLevel = "ok"
	HealthWarn     HealthLevel = "warn"
	HealthCritical HealthLevel = "critical"
)

// HealthLevels is every HealthLevel, in the schema's order (worse later).
var HealthLevels = []HealthLevel{HealthOK, HealthWarn, HealthCritical}

// HealthMetric is what a health issue is about: the data folder's disk, memory (with swap-in), sustained CPU.
type HealthMetric string

const (
	HealthDisk HealthMetric = "disk"
	HealthMem  HealthMetric = "mem"
	HealthCPU  HealthMetric = "cpu"
)

// HealthMetrics is every HealthMetric, in the schema's order.
var HealthMetrics = []HealthMetric{HealthDisk, HealthMem, HealthCPU}

// AgentInstallStatus is whether an agent CLI can run on a machine.
type AgentInstallStatus string

const (
	InstallReady         AgentInstallStatus = "ready"
	InstallMissing       AgentInstallStatus = "missing"
	InstallLoginRequired AgentInstallStatus = "login-required"
)

// AgentInstallStatuses is every AgentInstallStatus, in the schema's order.
var AgentInstallStatuses = []AgentInstallStatus{InstallReady, InstallMissing, InstallLoginRequired}

// Effort is a model's reasoning effort.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
	// EffortUltra is above max: the deepest level of the Codex models that have it (gpt-5.6-sol, gpt-5.6-terra).
	EffortUltra Effort = "ultra"
	// EffortUltracode is no effort of the wire any more: records and asks from before ultracode became a mode of its
	// own (Model.Ultracode) said it as one. They are read as xhigh with ultracode on: what `--effort ultracode` runs.
	EffortUltracode Effort = "ultracode"
)

// Efforts is every Effort, in the schema's order.
var Efforts = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax, EffortUltra}

// TitleSource is who named a conversation.
type TitleSource string

const (
	TitleFromUser         TitleSource = "user"
	TitleFromAgent        TitleSource = "agent"
	TitleFromFirstMessage TitleSource = "first-message"
)

// TitleSources is every TitleSource, in the schema's order.
var TitleSources = []TitleSource{TitleFromUser, TitleFromAgent, TitleFromFirstMessage}

// AgentStateKind is the kind of an AgentState.
type AgentStateKind string

const (
	StateStarting          AgentStateKind = "starting"
	StateWorking           AgentStateKind = "working"
	StateWaitingQuestion   AgentStateKind = "waiting-question"
	StateWaitingPermission AgentStateKind = "waiting-permission"
	StateReady             AgentStateKind = "ready"
	StateIdle              AgentStateKind = "idle"
	StateStopped           AgentStateKind = "stopped"
	StateLaunchFailed      AgentStateKind = "launch-failed"
	StateGone              AgentStateKind = "gone"
	StateUnknown           AgentStateKind = "unknown"
)

// AgentStateKinds is every AgentStateKind, in the schema's order.
var AgentStateKinds = []AgentStateKind{StateStarting, StateWorking, StateWaitingQuestion, StateWaitingPermission, StateReady, StateIdle, StateStopped, StateLaunchFailed, StateGone, StateUnknown}

// LaunchStep is a step of the launch screen.
type LaunchStep string

const (
	StepMachine LaunchStep = "machine"
	StepCli     LaunchStep = "cli"
	StepProject LaunchStep = "project"
	StepPrompt  LaunchStep = "prompt"
)

// LaunchSteps is every LaunchStep, in the schema's order.
var LaunchSteps = []LaunchStep{StepMachine, StepCli, StepProject, StepPrompt}

// LaunchFailure is why an agent did not come up.
type LaunchFailure string

const (
	LaunchFailedNoProcess     LaunchFailure = "no-process"
	LaunchFailedExited        LaunchFailure = "exited"
	LaunchFailedNoPromptBox   LaunchFailure = "no-prompt-box"
	LaunchFailedNoSession     LaunchFailure = "no-session"
	LaunchFailedCliMissing    LaunchFailure = "cli-missing"
	LaunchFailedLoginRequired LaunchFailure = "login-required"
	// LaunchFailedMachineBusy: tmux did not answer in time, the machine is overloaded; the same launch may go later.
	LaunchFailedMachineBusy LaunchFailure = "machine-busy"
)

// LaunchFailures is every LaunchFailure, in the schema's order.
var LaunchFailures = []LaunchFailure{LaunchFailedNoProcess, LaunchFailedExited, LaunchFailedNoPromptBox, LaunchFailedNoSession, LaunchFailedCliMissing, LaunchFailedLoginRequired, LaunchFailedMachineBusy}

// GoneReason is why a conversation's tab is gone.
type GoneReason string

const (
	GoneTabClosed GoneReason = "tab-closed"
	GoneTmuxLost  GoneReason = "tmux-lost"
)

// GoneReasons is every GoneReason, in the schema's order.
var GoneReasons = []GoneReason{GoneTabClosed, GoneTmuxLost}

// TurnStatus is where a turn is.
type TurnStatus string

const (
	TurnLive        TurnStatus = "live"
	TurnDone        TurnStatus = "done"
	TurnInterrupted TurnStatus = "interrupted"
	TurnFailed      TurnStatus = "failed"
)

// TurnStatuses is every TurnStatus, in the schema's order.
var TurnStatuses = []TurnStatus{TurnLive, TurnDone, TurnInterrupted, TurnFailed}

// TurnFailureCode is why a turn failed.
type TurnFailureCode string

const (
	TurnFailedApiError        TurnFailureCode = "api-error"
	TurnFailedRateLimit       TurnFailureCode = "rate-limit"
	TurnFailedContextOverflow TurnFailureCode = "context-overflow"
	TurnFailedAgentExited     TurnFailureCode = "agent-exited"
	TurnFailedUnknown         TurnFailureCode = "unknown"
)

// TurnFailureCodes is every TurnFailureCode, in the schema's order.
var TurnFailureCodes = []TurnFailureCode{TurnFailedApiError, TurnFailedRateLimit, TurnFailedContextOverflow, TurnFailedAgentExited, TurnFailedUnknown}

// FileOp is what a turn did to a file.
type FileOp string

const (
	FileCreated FileOp = "created"
	FileEdited  FileOp = "edited"
	FileRead    FileOp = "read"
)

// FileOps is every FileOp, in the schema's order.
var FileOps = []FileOp{FileCreated, FileEdited, FileRead}

// OutputKind is what a turn made: a published document or board, a design, a document of the repository, a merge
// request, a stand (a preview or a dev server).
type OutputKind string

const (
	OutputArtifact OutputKind = "artifact"
	OutputDesign   OutputKind = "design"
	OutputDoc      OutputKind = "doc"
	OutputMR       OutputKind = "mr"
	OutputStand    OutputKind = "stand"
	// OutputDelivery is a file the agent handed to the person on purpose (harnessly deliver).
	OutputDelivery OutputKind = "delivery"
)

// OutputKinds is every OutputKind, in the schema's order.
var OutputKinds = []OutputKind{OutputArtifact, OutputDesign, OutputDoc, OutputMR, OutputStand, OutputDelivery}

// OutputOp is whether the turn made the thing or changed one there was.
type OutputOp string

const (
	OutputCreated OutputOp = "created"
	OutputChanged OutputOp = "changed"
)

// OutputOps is every OutputOp, in the schema's order.
var OutputOps = []OutputOp{OutputCreated, OutputChanged}

// EntryKind is the kind of a feed Entry.
type EntryKind string

const (
	EntryUser       EntryKind = "user"
	EntrySay        EntryKind = "say"
	EntryThink      EntryKind = "think"
	EntryTool       EntryKind = "tool"
	EntrySubagent   EntryKind = "subagent"
	EntryAnswers    EntryKind = "answers"
	EntryGate       EntryKind = "gate"
	EntryCompaction EntryKind = "compaction"
	EntryNotice     EntryKind = "notice"
	EntryInterrupt  EntryKind = "interrupt"
	EntryPeer       EntryKind = "peer"
)

// EntryKinds is every EntryKind, in the schema's order.
var EntryKinds = []EntryKind{EntryUser, EntrySay, EntryThink, EntryTool, EntrySubagent, EntryAnswers, EntryGate, EntryCompaction, EntryNotice, EntryInterrupt, EntryPeer}

// SubagentStatus is where a subagent entry is.
type SubagentStatus string

const (
	SubagentRunning SubagentStatus = "running"
	SubagentDone    SubagentStatus = "done"
	SubagentFailed  SubagentStatus = "failed"
)

// SubagentStatuses is every SubagentStatus, in the schema's order.
var SubagentStatuses = []SubagentStatus{SubagentRunning, SubagentDone, SubagentFailed}

// NoticeLevel is how loud a notice entry is.
type NoticeLevel string

const (
	NoticeInfo  NoticeLevel = "info"
	NoticeWarn  NoticeLevel = "warn"
	NoticeError NoticeLevel = "error"
)

// NoticeLevels is every NoticeLevel, in the schema's order.
var NoticeLevels = []NoticeLevel{NoticeInfo, NoticeWarn, NoticeError}

// AttachmentKind is an image or another file.
type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentFile  AttachmentKind = "file"
)

// AttachmentKinds is every AttachmentKind, in the schema's order.
var AttachmentKinds = []AttachmentKind{AttachmentImage, AttachmentFile}

// RefKind is what a MessageRef points at.
type RefKind string

const (
	RefDesign RefKind = "design"
	RefDoc    RefKind = "doc"
	RefBoard  RefKind = "board"
	RefFile   RefKind = "file"
	RefMR     RefKind = "mr"
	RefTask   RefKind = "task"
	// RefChat is another conversation: ID is its id; the runtime hands the agent an excerpt and the path of the whole.
	RefChat RefKind = "chat"
)

// RefKinds is every RefKind, in the schema's order.
var RefKinds = []RefKind{RefDesign, RefDoc, RefBoard, RefFile, RefMR, RefTask, RefChat}

// MessageMode is when a message should reach the agent.
type MessageMode string

const (
	ModeQueue MessageMode = "queue"
	ModeNow   MessageMode = "now"
	ModeSteer MessageMode = "steer"
)

// MessageModes is every MessageMode, in the schema's order.
var MessageModes = []MessageMode{ModeQueue, ModeNow, ModeSteer}

// MessageKind tells a prompt from a command ('/model', '/compact', '!cmd').
type MessageKind string

const (
	KindPrompt  MessageKind = "prompt"
	KindCommand MessageKind = "command"
)

// MessageKinds is every MessageKind, in the schema's order.
var MessageKinds = []MessageKind{KindPrompt, KindCommand}

// MessageStateKind is the kind of a MessageState on the wire.
type MessageStateKind string

const (
	MsgQueued    MessageStateKind = "queued"
	MsgTyping    MessageStateKind = "typing"
	MsgInInput   MessageStateKind = "in-input"
	MsgAccepted  MessageStateKind = "accepted"
	MsgInTurn    MessageStateKind = "in-turn"
	MsgDone      MessageStateKind = "done"
	MsgFailed    MessageStateKind = "failed"
	MsgCancelled MessageStateKind = "cancelled"
)

// MessageStateKinds is every MessageStateKind, in the schema's order.
var MessageStateKinds = []MessageStateKind{MsgQueued, MsgTyping, MsgInInput, MsgAccepted, MsgInTurn, MsgDone, MsgFailed, MsgCancelled}

// QueueReason is why a queued message waits.
type QueueReason string

const (
	QueuedBehind      QueueReason = "behind"
	QueuedAgentBusy   QueueReason = "agent-busy"
	QueuedLaunching   QueueReason = "launching"
	QueuedResuming    QueueReason = "resuming"
	QueuedPaused      QueueReason = "paused"
	QueuedBlockedMenu QueueReason = "blocked-menu"
	QueuedBlockedText QueueReason = "blocked-text"
	// QueuedStalled: typed, and the agent did not take it within ConfirmDeadline — something is wrong beyond this
	// message. It is back at the head of the line, its text out of the agent's input box, and waits for the person
	// (retry or promote) instead of being typed again by itself: it is never typed twice. Only for a page with
	// CapMsgStatus; any other page gets it as failed{unconfirmed}.
	QueuedStalled QueueReason = "stalled"
)

// QueueReasons is every QueueReason, in the schema's order.
var QueueReasons = []QueueReason{QueuedBehind, QueuedAgentBusy, QueuedLaunching, QueuedResuming, QueuedPaused, QueuedBlockedMenu, QueuedBlockedText, QueuedStalled}

// MessageFailure is why a message failed.
type MessageFailure string

const (
	MsgFailedUploadFailed MessageFailure = "upload-failed"
	MsgFailedRefused      MessageFailure = "refused"
	MsgFailedLaunchFailed MessageFailure = "launch-failed"
	MsgFailedAgentGone    MessageFailure = "agent-gone"
	MsgFailedTypeFailed   MessageFailure = "type-failed"
	MsgFailedStuckInInput MessageFailure = "stuck-in-input"
	MsgFailedUnconfirmed  MessageFailure = "unconfirmed"
	MsgFailedExpired      MessageFailure = "expired"
	// MsgFailedReturned: the turn was interrupted while the message waited in Claude's queue, and Claude put it back
	// into the input box for the person to edit; it is not Entered again by itself.
	MsgFailedReturned MessageFailure = "returned-to-input"
)

// MessageFailures is every MessageFailure, in the schema's order.
var MessageFailures = []MessageFailure{MsgFailedUploadFailed, MsgFailedRefused, MsgFailedLaunchFailed, MsgFailedAgentGone, MsgFailedTypeFailed, MsgFailedStuckInInput, MsgFailedUnconfirmed, MsgFailedExpired, MsgFailedReturned}

// PauseReason is why a queue is paused.
type PauseReason string

const (
	PausedInterrupted  PauseReason = "interrupted"
	PausedUser         PauseReason = "user"
	PausedLaunchFailed PauseReason = "launch-failed"
	PausedAgentGone    PauseReason = "agent-gone"
	PausedLimited      PauseReason = "limited" // the agent hit its usage limit: the queue waits for the reset
	PausedNeedsTarget  PauseReason = "needs-target"
)

// PauseReasons is every PauseReason, in the schema's order.
var PauseReasons = []PauseReason{PausedInterrupted, PausedUser, PausedLaunchFailed, PausedAgentGone, PausedLimited, PausedNeedsTarget}

// TerminalFailureKind is a stable, provider-neutral terminal outcome.
type TerminalFailureKind string

const (
	FailureQuotaExhausted TerminalFailureKind = "quota_exhausted"
)

// TerminalFailureKinds is every TerminalFailureKind, in the schema's order.
var TerminalFailureKinds = []TerminalFailureKind{FailureQuotaExhausted}

// GateKind is the kind of an open gate on a conversation.
type GateKind string

const (
	GateQuestion   GateKind = "question"
	GatePermission GateKind = "permission"
	GatePlan       GateKind = "plan"
)

// GateKinds is every GateKind, in the schema's order.
var GateKinds = []GateKind{GateQuestion, GatePermission, GatePlan}

// PermissionKind tells a tool permission from a plan.
type PermissionKind string

const (
	PermTool PermissionKind = "tool"
	PermPlan PermissionKind = "plan"
)

// PermissionKinds is every PermissionKind, in the schema's order.
var PermissionKinds = []PermissionKind{PermTool, PermPlan}

// PermissionScope is what a permission option grants.
type PermissionScope string

const (
	ScopeOnce     PermissionScope = "once"
	ScopeAlways   PermissionScope = "always"
	ScopeDeny     PermissionScope = "deny"
	ScopePlanAuto PermissionScope = "plan-auto"
	ScopePlanAsk  PermissionScope = "plan-ask"
	ScopeRevise   PermissionScope = "revise"
)

// PermissionScopes is every PermissionScope, in the schema's order.
var PermissionScopes = []PermissionScope{ScopeOnce, ScopeAlways, ScopeDeny, ScopePlanAuto, ScopePlanAsk, ScopeRevise}

// GateStateKind is the kind of a GateState.
type GateStateKind string

const (
	GateStateOpen      GateStateKind = "open"
	GateStateAnswering GateStateKind = "answering"
	GateStateAnswered  GateStateKind = "answered"
	GateStateClosed    GateStateKind = "closed"
)

// GateStateKinds is every GateStateKind, in the schema's order.
var GateStateKinds = []GateStateKind{GateStateOpen, GateStateAnswering, GateStateAnswered, GateStateClosed}

// GateCloseReason is why a gate closed without an answer.
type GateCloseReason string

const (
	ClosedAgentMovedOn GateCloseReason = "agent-moved-on"
	ClosedChatAbout    GateCloseReason = "chat-about"
	ClosedTurnEnded    GateCloseReason = "turn-ended"
	ClosedAgentGone    GateCloseReason = "agent-gone"
)

// GateRefusal is why a decision taken on the page did not reach the agent's menu.
type GateRefusal string

const (
	// GateRefusedMenuUnmatched: the menu on the screen could not be matched with the one decided on.
	GateRefusedMenuUnmatched GateRefusal = "menu-unmatched"
)

// GateRefusals is every GateRefusal, in the schema's order.
var GateRefusals = []GateRefusal{GateRefusedMenuUnmatched}

// GateCloseReasons is every GateCloseReason, in the schema's order.
var GateCloseReasons = []GateCloseReason{ClosedAgentMovedOn, ClosedChatAbout, ClosedTurnEnded, ClosedAgentGone}

// BackgroundKind is a subagent, a command or a dynamic workflow.
type BackgroundKind string

const (
	BgSubagent BackgroundKind = "subagent"
	BgCommand  BackgroundKind = "command"
	// BgWorkflow: a dynamic workflow, a script of the agent that starts subagents in the background phase by phase.
	BgWorkflow BackgroundKind = "workflow"
)

// BackgroundKinds is every BackgroundKind, in the schema's order.
var BackgroundKinds = []BackgroundKind{BgSubagent, BgCommand, BgWorkflow}

// BackgroundStatus is where a background task is.
type BackgroundStatus string

const (
	BgStatusRunning BackgroundStatus = "running"
	BgStatusDone    BackgroundStatus = "done"
	BgStatusStopped BackgroundStatus = "stopped"
	BgStatusFailed  BackgroundStatus = "failed"
	// BgStatusIdle: a teammate that finished what it was sent and waits for the next message; no work running.
	BgStatusIdle BackgroundStatus = "idle"
)

// BackgroundStatuses is every BackgroundStatus, in the schema's order.
var BackgroundStatuses = []BackgroundStatus{BgStatusRunning, BgStatusDone, BgStatusStopped, BgStatusFailed, BgStatusIdle}

// SnapshotMode is a whole snapshot or what follows a cursor.
type SnapshotMode string

const (
	SnapshotReplace SnapshotMode = "replace"
	SnapshotAfter   SnapshotMode = "after"
)

// SnapshotModes is every SnapshotMode, in the schema's order.
var SnapshotModes = []SnapshotMode{SnapshotReplace, SnapshotAfter}

// FrameErrorCode is why the socket refused a frame.
type FrameErrorCode string

const (
	FrameErrProto        FrameErrorCode = "proto"
	FrameErrBadChannel   FrameErrorCode = "bad-channel"
	FrameErrNotFound     FrameErrorCode = "not-found"
	FrameErrTooManySubs  FrameErrorCode = "too-many-subs"
	FrameErrUnauthorized FrameErrorCode = "unauthorized"
)

// FrameErrorCodes is every FrameErrorCode, in the schema's order.
var FrameErrorCodes = []FrameErrorCode{FrameErrProto, FrameErrBadChannel, FrameErrNotFound, FrameErrTooManySubs, FrameErrUnauthorized}

// FrameKind is the kind of a socket frame, in its "f" field.
type FrameKind string

const (
	FrameHello FrameKind = "hello"
	FrameSub   FrameKind = "sub"
	FrameUnsub FrameKind = "unsub"
	FramePing  FrameKind = "ping"
)

// ClientFrameKinds is every ClientFrameKind, in the schema's order.
var ClientFrameKinds = []FrameKind{FrameHello, FrameSub, FrameUnsub, FramePing}

const (
	FrameWelcome  FrameKind = "welcome"
	FrameSnapshot FrameKind = "snapshot"
	FrameEv       FrameKind = "ev"
	FramePong     FrameKind = "pong"
	FrameError    FrameKind = "error"
)

// ServerFrameKinds is every ServerFrameKind, in the schema's order.
var ServerFrameKinds = []FrameKind{FrameWelcome, FrameSnapshot, FrameEv, FramePong, FrameError}

// EventType is the kind of an Event, in its "type" field.
type EventType string

const (
	EvMachineStatus EventType = "machine_status"
	EvMachineLoad   EventType = "machine_load"
)

// MachineEventTypes is every MachineEventType, in the schema's order.
var MachineEventTypes = []EventType{EvMachineStatus, EvMachineLoad}

const (
	EvConversationUpsert  EventType = "conversation_upsert"
	EvConversationState   EventType = "conversation_state"
	EvConversationSeen    EventType = "conversation_seen"
	EvConversationRemoved EventType = "conversation_removed"
	EvProjectUpsert       EventType = "project_upsert"
	EvFavoritesUpsert     EventType = "favorites_upsert"
	EvDraftUpsert         EventType = "draft_upsert"
)

// ConvsEventTypes is every ConvsEventType, in the schema's order.
var ConvsEventTypes = []EventType{EvConversationUpsert, EvConversationState, EvConversationSeen, EvConversationRemoved, EvProjectUpsert, EvFavoritesUpsert, EvDraftUpsert}

const (
	EvTurnStarted         EventType = "turn_started"
	EvStep                EventType = "step"
	EvEntryUpdate         EventType = "entry_update"
	EvEntryReplace        EventType = "entry_replace"
	EvNow                 EventType = "now"
	EvQuestionAsked       EventType = "question_asked"
	EvPermissionRequested EventType = "permission_requested"
	EvPlanProposed        EventType = "plan_proposed"
	EvGateUpdated         EventType = "gate_updated"
	EvCompaction          EventType = "compaction"
	EvTurnFinished        EventType = "turn_finished"
	EvTurnFailed          EventType = "turn_failed"
	EvQuotaExhausted      EventType = "quota_exhausted"
	EvQueueChanged        EventType = "queue_changed"
	EvPromptState         EventType = "prompt_state"
	EvMessageUpsert       EventType = "message_upsert"
	EvBackgroundUpsert    EventType = "background_upsert"
	EvBackgroundRemoved   EventType = "background_removed"
	EvShotsSet            EventType = "shots_set"
	EvPicturesSet         EventType = "pictures_set"
	EvFeedReset           EventType = "feed_reset"
)

// ConvEventTypes is every ConvEventType, in the schema's order.
var ConvEventTypes = []EventType{EvTurnStarted, EvStep, EvEntryUpdate, EvEntryReplace, EvNow, EvQuestionAsked, EvPermissionRequested, EvPlanProposed, EvGateUpdated, EvCompaction, EvTurnFinished, EvTurnFailed, EvQuotaExhausted, EvQueueChanged, EvPromptState, EvMessageUpsert, EvBackgroundUpsert, EvBackgroundRemoved, EvShotsSet, EvPicturesSet, EvFeedReset}

// CompactionPhase is the start or the end of a compaction.
type CompactionPhase string

const (
	CompactionStarted  CompactionPhase = "started"
	CompactionFinished CompactionPhase = "finished"
)

// CompactionPhases is every CompactionPhase, in the schema's order.
var CompactionPhases = []CompactionPhase{CompactionStarted, CompactionFinished}

// Op is a command of POST /api/v2/cmd/<op>.
type Op string

const (
	OpSend                Op = "send"
	OpCancel              Op = "cancel"
	OpRetry               Op = "retry"
	OpPromote             Op = "promote"
	OpAnswer              Op = "answer"
	OpDecide              Op = "decide"
	OpInterrupt           Op = "interrupt"
	OpQueueResume         Op = "queue.resume"
	OpSeen                Op = "seen"
	OpConversationCreate  Op = "conversation.create"
	OpConversationRename  Op = "conversation.rename"
	OpConversationPin     Op = "conversation.pin"
	OpConversationRetitle Op = "conversation.retitle"
	OpConversationMove    Op = "conversation.move"
	OpConversationDelete  Op = "conversation.delete"
	OpConversationRestore Op = "conversation.restore"
	OpConversationResume  Op = "conversation.resume"
	OpModel               Op = "model"
	OpBgStop              Op = "bg.stop"
	OpProjectCreate       Op = "project.create"
	OpProjectUpdate       Op = "project.update"
	OpFavGroupCreate      Op = "favorites.group.create"
	OpFavGroupRename      Op = "favorites.group.rename"
	OpFavGroupDelete      Op = "favorites.group.delete"
	OpFavGroupMove        Op = "favorites.group.move"
	OpFavPlace            Op = "favorites.place"
	OpGoal                Op = "goal"
	OpDraftSet            Op = "draft.set"
	OpQueueMove           Op = "queue.move"
	OpTerminalStop        Op = "terminal.stop"
	OpRewind              Op = "rewind"
	OpRewindUndo          Op = "rewind.undo"
	OpConversationFork    Op = "conversation.fork"
	OpConversationHandoff Op = "conversation.handoff"
	OpConversationMerge   Op = "conversation.merge"
	OpLimit               Op = "limit"
)

// Ops is every Op, in the schema's order.
var Ops = []Op{OpSend, OpCancel, OpRetry, OpPromote, OpAnswer, OpDecide, OpInterrupt, OpQueueResume, OpSeen, OpConversationCreate, OpConversationRename, OpConversationPin, OpConversationMove, OpConversationDelete, OpConversationRestore, OpConversationResume, OpModel, OpBgStop, OpProjectCreate, OpProjectUpdate, OpFavGroupCreate, OpFavGroupRename, OpFavGroupDelete, OpFavGroupMove, OpFavPlace, OpGoal, OpConversationRetitle, OpDraftSet, OpQueueMove, OpTerminalStop, OpRewind, OpRewindUndo, OpConversationFork, OpConversationHandoff, OpConversationMerge, OpLimit}

// PromoteTo is where promote moves a queued message.
type PromoteTo string

const (
	PromoteToNow   PromoteTo = "now"
	PromoteToSteer PromoteTo = "steer"
)

// PromoteTos is every PromoteTo, in the schema's order.
var PromoteTos = []PromoteTo{PromoteToNow, PromoteToSteer}

// ResultKind is the outcome of a command.
type ResultKind string

const (
	ResultAccepted ResultKind = "accepted"
	ResultQueued   ResultKind = "queued"
	ResultRefused  ResultKind = "refused"
)

// ResultKinds is every ResultKind, in the schema's order.
var ResultKinds = []ResultKind{ResultAccepted, ResultQueued, ResultRefused}

// RefuseCode is why a command was refused.
type RefuseCode string

const (
	RefuseBadRequest         RefuseCode = "bad-request"
	RefuseConflict           RefuseCode = "conflict"
	RefuseUnsupported        RefuseCode = "unsupported"
	RefuseForbidden          RefuseCode = "forbidden"
	RefuseNotFound           RefuseCode = "not-found"
	RefuseMachineRequired    RefuseCode = "machine-required"
	RefuseMachineOffline     RefuseCode = "machine-offline"
	RefuseAgentGone          RefuseCode = "agent-gone"
	RefuseAgentWorking       RefuseCode = "agent-working"
	RefuseNotWorking         RefuseCode = "not-working"
	RefuseQueueFull          RefuseCode = "queue-full"
	RefuseTextTooLong        RefuseCode = "text-too-long"
	RefuseAttachmentMissing  RefuseCode = "attachment-missing"
	RefuseAttachmentTooLarge RefuseCode = "attachment-too-large"
	RefuseAttachmentType     RefuseCode = "attachment-type"
	RefuseTooManyAttachments RefuseCode = "too-many-attachments"
	RefuseTooLate            RefuseCode = "too-late"
	RefuseNotFailed          RefuseCode = "not-failed"
	RefuseGateClosed         RefuseCode = "gate-closed"
	RefuseGateAnswered       RefuseCode = "gate-answered"
	RefuseOptionUnknown      RefuseCode = "option-unknown"
	RefuseRateLimited        RefuseCode = "rate-limited"
	RefuseInternal           RefuseCode = "internal"
	RefuseTimeout            RefuseCode = "timeout"
)

// RefuseCodes is every RefuseCode, in the schema's order.
var RefuseCodes = []RefuseCode{RefuseBadRequest, RefuseConflict, RefuseUnsupported, RefuseForbidden, RefuseNotFound, RefuseMachineRequired, RefuseMachineOffline, RefuseAgentGone, RefuseAgentWorking, RefuseNotWorking, RefuseQueueFull, RefuseTextTooLong, RefuseAttachmentMissing, RefuseAttachmentTooLarge, RefuseAttachmentType, RefuseTooManyAttachments, RefuseTooLate, RefuseNotFailed, RefuseGateClosed, RefuseGateAnswered, RefuseOptionUnknown, RefuseRateLimited, RefuseInternal, RefuseTimeout}

// RetryableRefuseCodes are the refusals a retry of the same command can turn into a success: the page offers
// "Повторить" for them and only "Отменить" for the rest.
var RetryableRefuseCodes = []RefuseCode{RefuseMachineOffline, RefuseRateLimited, RefuseInternal, RefuseTimeout}
