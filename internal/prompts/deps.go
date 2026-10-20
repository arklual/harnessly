package prompts

import (
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// Panes is the tmux layer the package types through. *tmux.Tmux has every method but CaptureAtWidth, which needs the
// page's terminal: TmuxPanes adds it.
type Panes interface {
	HasSession(name string) bool
	IsAppSession(name string) bool
	SocketOf(name string) string
	Capture(name string) (string, bool)
	CaptureStyled(name string) (string, bool)
	// CaptureAtWidth is the screen as a program drawing at least cols×rows shows it (for parsing a TUI's options).
	CaptureAtWidth(name string, cols, rows int) (string, bool)
	SendRawKeys(name, keys string) error
	SendKeySequence(name string, keys ...string) error
	SendLiteral(name, text string) error
	SendLiteralThenEnter(name, text string) error
	SendBracketedPasteOnly(name, text string) error
	ExitCopyMode(name string)
	PaneID(name string) string
	PanePID(name string) int
	PaneCurrentCommand(name string) string
	PaneDetail(name string) tmux.PaneDetail
}

// TmuxPanes is *tmux.Tmux with the page's terminal for captures at a width; Term may be nil.
type TmuxPanes struct {
	*tmux.Tmux
	Term tmux.Terminal
}

// CaptureAtWidth captures as tmux.CaptureAtWidth does with the page's terminal.
func (p TmuxPanes) CaptureAtWidth(name string, cols, rows int) (string, bool) {
	return p.Tmux.CaptureAtWidth(p.Term, name, cols, rows)
}

// Tab is what the package reads of a tab; the status machine owns it.
type Tab struct {
	ID string
	// Session is the tab's tmux session ("" none).
	Session string
	// Provider is the agent CLI of the tab (ProviderClaude, ProviderCodex, "" a plain terminal).
	Provider string
	CliState string
	// TranscriptPath is the agent's JSONL transcript ("" not known yet).
	TranscriptPath string
	// Launching: a launch is under way; LaunchFailed: the last launch gave up.
	Launching, LaunchFailed bool
	// HasAgentSession: the tab knows its agent's session id (so a gone agent can be resumed).
	HasAgentSession bool
}

// Tabs looks tabs up.
type Tabs interface {
	Tab(tabID string) (Tab, bool)
	// TabOfSession is the tab whose tmux session this is.
	TabOfSession(session string) (Tab, bool)
}

// Resume results of bringing a tab's agent back for a queued message.
const (
	ResumeResumed   = "resumed"
	ResumeRunning   = "running"
	ResumeNoSession = "no-session"
	// Anything else (a shell still starting, a busy shell) is tried again later.
)

// Agents is what the queue needs from the agents of tabs.
type Agents interface {
	// Running says whether the tab's agent process runs under its pane.
	Running(tab Tab) bool
	// RecoverSession finds a Claude tab's lost session id in its transcript or history and stores it; false when none.
	RecoverSession(tab Tab) bool
	// Resume brings a gone agent back with its session (`--resume`); see the Resume* results.
	Resume(tab Tab) string
	// GiveUpLaunch marks the tab's launch failed (launching since now, failed) and tells the pages; the message stays
	// queued and the page shows the launch failed.
	GiveUpLaunch(tabID string)
}

// Events are what the package reports to the status machine.
type Events interface {
	// Interrupted: Stop was pressed (Escape sent); the status machine applies an interrupt hook (→ idle, which flushes
	// the queue 700 ms later).
	Interrupted(tabID, session string)
	// MenuOnScreen: a command left a menu open in the tab; true when the tab, still in a state that can show one, moved
	// to needs-input.
	MenuOnScreen(tabID string) bool
	// RecheckStuck asks for the stuck check of a Claude tab now, ignoring its quiet limit (after a command ran).
	RecheckStuck(tabID string)
	// LaunchChosen: /model or /effort was confirmed in the session; the conversation's launch keeps it (the other half
	// as it was).
	LaunchChosen(session string, applied Applied)
	// CommandsReloaded: the agent CLI reloaded its plugins or skills; its command catalog is stale.
	CommandsReloaded(provider string)
}

// Broadcaster sends a status-socket message to every page.
type Broadcaster interface {
	Broadcast(msg any)
}

// QueueUpdate is the status-socket message of a queue change.
type QueueUpdate struct {
	Type  string `json:"type"`
	TabID string `json:"tabId"`
	Items []Item `json:"items"`
}

// QueueSync is the status-socket message with every queue, sent to a page that connects or asks for a sync.
type QueueSync struct {
	Type   string            `json:"type"`
	Queues map[string][]Item `json:"queues"`
}
