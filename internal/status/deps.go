package status

import (
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// Tab is a layout tab as the keeper reads it.
type Tab struct {
	ID              string
	SessionName     string
	Name            string
	PanelType       Opt[string]
	CliState        string
	LastEventName   string
	DismissedAt     Opt[float64]
	LastUserMessage Opt[string]
	Agent           agents.TabAgent
}

// TabFromLayout reads a tab object of a layout file.
func TabFromLayout(t *jsvalue.Object) Tab {
	str := func(k string) string { s, _ := t.Get(k); v, _ := s.(string); return v }
	optStr := func(k string) Opt[string] {
		v, has := t.Get(k)
		if !has {
			return Opt[string]{}
		}
		if s, ok := v.(string); ok {
			return Some(s)
		}
		return Null[string]()
	}
	tab := Tab{ID: str("id"), SessionName: str("sessionName"), Name: str("name"), PanelType: optStr("panelType"),
		CliState: str("cliState"), LastEventName: str("lastEventName"), LastUserMessage: optStr("lastUserMessage")}
	if v, has := t.Get("dismissedAt"); has {
		if f, ok := v.(float64); ok {
			tab.DismissedAt = Some(f)
		} else {
			tab.DismissedAt = Null[float64]()
		}
	}
	ptr := func(o *jsvalue.Object, k string) *string {
		if o == nil {
			return nil
		}
		if v, _ := o.Get(k); v != nil {
			if s, ok := v.(string); ok {
				return &s
			}
		}
		return nil
	}
	tab.Agent = agents.TabAgent{ID: tab.ID, ClaudeSessionID: ptr(t, "claudeSessionId"), ClaudeJSONLPath: ptr(t, "claudeJsonlPath"), ClaudeSummary: ptr(t, "claudeSummary")}
	if st := t.Object("agentState"); st != nil {
		id, _ := st.Get("providerId")
		pid, _ := id.(string)
		tab.Agent.AgentState = &agents.AgentState{ProviderID: pid, SessionID: ptr(st, "sessionId"), JSONLPath: ptr(st, "jsonlPath"), Summary: ptr(st, "summary")}
	}
	return tab
}

// Workspace is a workspace as the keeper needs it.
type Workspace struct {
	ID, Name    string
	Directories []string
}

// Layouts is the layout and workspace store.
type Layouts interface {
	Workspaces() []Workspace
	// Tabs are a workspace's tabs in tree order; ok false when it has no readable layout.
	Tabs(wsID string) ([]Tab, bool)
	UpdateTabCliStatus(session, cliState string, dismissedAt Opt[float64], lastEventName string) error
	// UpdateTabAgentState stores what is set; a set Opt with a nil value stores null.
	UpdateTabAgentState(session, provider string, sessionID, jsonlPath, summary, lastUserMessage Opt[string]) error
	// RecoverSession gives a Claude tab that lost its session id the one its transcript or history shows, stored.
	RecoverSession(session string) (sessionID string, ok bool)
}

// Tmux is what the keeper asks tmux.
type Tmux interface {
	ReadAllPanes() (map[string]tmux.PaneInfo, bool)
	PaneTitle(session string) string
	PaneDetail(session string) tmux.PaneDetail
	PanePID(session string) int
	Capture(session string) (string, bool)
	// CaptureAtWidth may pause and resize the attached terminal (Codex readiness, recovery of an unknown tab).
	CaptureAtWidth(session string, cols, rows int) (string, bool)
	SessionCwd(session string) string
	SendRawKeys(session, keys string) error
	ParseSessionName(name string) (wsID, paneID, tabID string, ok bool)
}

// Procs reads the process table.
type Procs interface {
	Children(pid int) []int
	// ListeningPorts are the TCP ports the pane's descendants listen on, sorted.
	ListeningPorts(pid int) []int
}

// Provider is an agent CLI: Claude Code or Codex.
type Provider interface {
	ID() string
	Running(panePID int, children []int) bool
	Detect(panePID int, children []int) agents.SessionInfo
	Snapshot(transcript string, force bool) (agents.Snapshot, error)
	HistoryStats(transcript string) (*agents.HistoryStats, error)
	// LastConversationAt is unused here; sleep reads it.
	ParsePaneTitle(title string) *string
	SessionIDFromPath(transcript string) string
	ReadSessionID(t agents.TabAgent) string
	ReadJSONLPath(t agents.TabAgent) string
	ReadSummary(t agents.TabAgent) string
	// FindSession is a session's transcript by its id (Codex); "" when not found or not supported.
	FindSession(sessionID string) string
	// TranscriptIn is a session's transcript in a folder's project directory (Claude); "" when missing.
	TranscriptIn(cwd, sessionID string) string
	// RunsWithHooks: whether the agent among children runs with the platform's hooks; found false when none runs yet.
	RunsWithHooks(children []int) (hooked, found bool)
}

// Queue is the prompt queue as the keeper drives it.
type Queue interface {
	// Schedule looks at the tab's queue after delay (a turn just ended, a launch ended, a restart).
	Schedule(tabID string, delay time.Duration)
	// Tabs are the tabs with something queued.
	Tabs() []string
	Pending(tabID string) bool
	Snapshot() any
}

// Pusher sends a web push: kind "done" or "question".
type Pusher interface {
	Push(tabID string, e Entry, kind string)
}

// Mismatch is a state the keeper corrected, for the team's telemetry.
type Mismatch struct {
	Kind, Shown, Actual, Provider string
	DurationMs                    *float64
	Unhooked                      bool
}

// Deps are everything the keeper reads and drives.
type Deps struct {
	Layouts   Layouts
	Tmux      Tmux
	Procs     Procs
	Providers map[string]Provider
	Queue     Queue
	History   *history.Store
	Push      Pusher
	// TurnEnd snapshots the session folder for rewinding once a turn ended.
	TurnEnd func(tabID, cwd string)
	// AgentLaunched hears of every agent the platform starts or resumes in a tab, before the keeper marks it.
	AgentLaunched func(tabID string)
	// Mismatch reports a corrected state.
	Mismatch func(Mismatch)
	// TabRemoved: the keeper forgot a closed tab (its prompt queue goes with it).
	TabRemoved func(tabID string)
	// PromptEnter takes the time since the runtime's last Enter in a pane (ms), once.
	PromptEnter func(session string, now float64) (float64, bool)
	// CodexRateLimits caches the limits a Codex transcript carries.
	CodexRateLimits func(transcript string)
	// BackgroundTasks reads a Claude transcript's background work; ForgetTranscript drops what it remembers of one.
	BackgroundTasks  func(transcript string) []background.Task
	ForgetTranscript func(transcript string)
	// BackgroundTimings are the feed's; zero for production.
	BackgroundTimings background.Timings
	Log               func(level, msg string, kv ...any)
	Now               func() time.Time
	NewID             func() string
}

func providerOfPanel(panelType Opt[string]) string {
	return agents.ProviderOfPanel(panelType.Value())
}
