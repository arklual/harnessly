package convo

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/msgindex"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The runtime reaches the machine only through these, so a test plays tmux, the process table, the launcher and the
// clock without them.

// Machine is the conversations' panes and processes.
type Machine interface {
	// Pane is the agent pane of a tmux session, nil when the session has none.
	Pane(session string) typist.Pane
	// Probe is what runs in the session now.
	Probe(session string) Probe
}

// Probe is the process under a session's pane.
type Probe struct {
	// Session: the tmux session exists.
	Session bool
	// Foreground: the agent CLI, not a shell, is in the pane's foreground.
	Foreground bool
	// Shell: a bare shell is in the foreground (the agent exited).
	Shell bool
	// Resumable: the agent has a session to start again on.
	Resumable bool
	// HandOver: the running tool can be handed to the background (Claude: Bash, an MCP call).
	HandOver bool
}

// Profiles knows the backend profiles: another vendor's models behind an agent CLI. A conversation runs on one from its
// start to its end; the profile is on its record as Model.Profile.
type Profiles interface {
	// Lookup says which CLI a profile runs through, whether a key is saved for it and which models it has; ok is false
	// for a profile the machine does not know.
	Lookup(id string) (via wire.Provider, configured bool, models []string, ok bool)
	// OwnsModel says whether the id is a model of some profile: a conversation on the CLI's own account is not started on
	// it or switched to it.
	OwnsModel(model string) bool
	// Window is the context window of a model of the profile, in tokens; 0 when the profile or the model is not known.
	Window(profile, model string) int
}

// Launcher starts agents. Its calls return at once; the steps, the ready and the failure come back through
// Runtime.Launch*.
type Launcher interface {
	Launch(req LaunchRequest) error
	// Resume starts a stopped agent again on its session.
	Resume(conv wire.Conversation) error
}

// LaunchRequest is a conversation to start.
type LaunchRequest struct {
	ID       string
	Cwd      string
	Provider wire.Provider
	Model    *wire.Model
	// ResumeSessionID starts the agent on a session it kept (the model stays the session's own); TranscriptPath is that
	// session's transcript, which the conversation already shows.
	ResumeSessionID string
	TranscriptPath  string
	// Fork starts the agent on a fork of ResumeSessionID made by its CLI (Codex's `fork`): a session of its own with the
	// other's history, found as a fresh one's is. TranscriptPath is "" then.
	Fork bool
	// First is the text the agent starts with as its first prompt (a message the conversation carries by the launch,
	// never typed); "" for none.
	First string
}

// AgentSession is a session an agent CLI kept on the machine.
type AgentSession struct {
	Title          string
	Cwd            string
	TranscriptPath string
}

// Background reads and ends the work an agent runs beside its turn: shells, monitors, subagents.
type Background interface {
	// Tasks are the tasks running now. pane is the agent's screen as the last look saw it ("" when there was none):
	// the footer of an agent that counts its shells is the authority on how many run.
	Tasks(c wire.Conversation, pane string) []wire.BackgroundTask
	// Stop ends a task the agent can no longer be asked about, and returns once it is over; an error when it cannot be
	// found or does not end.
	Stop(c wire.Conversation, taskID string) error
}

// Waker is a Background that also knows the agent's own scheduled wakeup: the chat waits for it as for a task, but it
// is no task a person can open or stop.
type Waker interface {
	// Wakeup is when the pending wakeup comes (epoch ms); 0 when the agent has none.
	Wakeup(c wire.Conversation) wire.Ms
}

// Closer ends a conversation's pane and its tab: what a deleted conversation leaves behind.
type Closer interface {
	// Close kills the tmux session of the conversation and removes its tab from the layout; it returns once both are done.
	Close(c wire.Conversation) error
}

// Terminals end the conversations' terminals and say what runs in them: terminal.stop and the idle stop (reap.go).
// Every call may touch tmux and /proc, so none runs on an actor.
type Terminals interface {
	// Owned: the session is the agent pane of a chat of this runtime, which it may end — never the session the runtime
	// itself runs in, nor one of a Legacy tab that is no chat, nor one of another program.
	Owned(session string) bool
	// Sample is the process tree under the session's pane now; false when it cannot be read.
	Sample(session string) (PaneSample, bool)
	// Watched: someone has the chat or its terminal open now (a page subscribed to the conversation, a terminal
	// attached to the pane).
	Watched(convID, session string) bool
	// Stop ends the agent and everything under the pane, then the tmux session, and returns once they are gone, with
	// the resident memory they held (0 when unknown).
	Stop(session string) (freedBytes int64, err error)
}

// PaneSample is the process tree under a pane at one moment.
type PaneSample struct {
	// Root is the pane's own process (its shell).
	Root int
	// Procs are every process of the tree, Root included.
	Procs []Proc
}

// Proc is one process of a pane's tree as /proc tells it.
type Proc struct {
	PID, PPID int
	// Comm is the process's name (/proc/<pid>/comm), Argv its command line.
	Comm string
	Argv []string
	// CPU is the processor time it has used so far (user and system).
	CPU time.Duration
	// RSS is its resident memory in bytes.
	RSS int64
}

// Uploads finds the files the page uploaded before it sent.
type Uploads interface {
	// Resolve is the attachments of ids and the paths of their pictures; a refuse code when one is missing or wrong.
	Resolve(ids []string) ([]wire.AttachmentRef, []string, wire.RefuseCode)
}

// Refs describes the places a message points at that only the machine knows how to name (a design of the library), and
// notes that the conversation points at them (the design lists the chat among its own).
type Refs interface {
	// Line is the agent's line for r, said of conversation conv; a refuse code when r is not there (not-found).
	Line(conv string, r wire.MessageRef) (string, wire.RefuseCode)
}

// Clock is the actors' time: now, and a timer the runner waits on. Tests move it by hand.
type Clock interface {
	typist.Clock
	// After fires once at d from now.
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the wall clock.
type SystemClock struct{ typist.SystemClock }

func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Logger is where the actors write what a person reading the journal needs.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

// Deps is what a Runtime is built on.
type Deps struct {
	MachineID string
	Publisher Publisher
	Machine   Machine
	Launcher  Launcher
	Uploads   Uploads
	// Refs names the designs a message points at; nil refuses a design reference (unsupported), the other kinds need none.
	Refs  Refs
	Clock Clock
	Log   Logger
	// Store keeps the conversations and the op answers; nil keeps nothing (tests).
	Store *Store
	// Source opens a conversation's transcript; nil reads the files of internal/transcript.
	Source func(c wire.Conversation) feed.Source
	// Closer closes the pane and tab of a deleted conversation; nil leaves them.
	Closer Closer
	// Background is where the background tasks come from; nil knows none.
	Background Background
	// Meta reads the effort and the goal off a Claude transcript; nil knows none.
	Meta *sessionmeta.Reader
	// Titler names a conversation from its first message and the agent's first answer, for an agent that wrote no title
	// of its own (Codex, or Claude before it did); nil never names one, and the first message stays the title.
	Titler func(p wire.Provider, firstMessage, firstAnswer string) (string, error)
	// Worktree makes a git worktree for a conversation and returns its folder; nil: a git worktree off the HEAD of the
	// repository the folder is in (a folder that is none is used as it is).
	Worktree func(cwd, name string) (string, error)
	// Shots reads the pictures the agent and its subagents looked at off a Claude transcript; nil knows none.
	Shots *shots.Reader
	// PictureRoots are the folders, besides a conversation's own, a picture its messages name may be shown from (Claude's
	// scratchpads, the uploads); nil shows none, the conversation's folder included.
	PictureRoots []string
	// Models are the models an agent CLI takes; nil knows none, and the model command then trusts the id's shape.
	Models func(p wire.Provider) []wire.AgentModel
	// Profiles are the backend profiles (Kimi, GLM, Grok) a conversation may run on; nil knows none, and a conversation
	// asked to start on one is refused.
	Profiles Profiles
	// AgentSession finds a session the agent CLI kept; nil knows none, and a conversation cannot then be opened on one.
	AgentSession func(p wire.Provider, sessionID string) (AgentSession, bool)
	// DefaultCwd is the folder a new conversation starts in when the person names none: it goes to the page with the
	// projects. Empty offers none.
	DefaultCwd string
	Limits     wire.Limits
	// Terminals end the conversations' terminals; nil: terminal.stop is refused{unsupported} and nothing is stopped
	// for standing idle.
	Terminals Terminals
	// Index keeps the messages of the conversations for search; nil: nothing is searched.
	Index *msgindex.Index
	// Digest cuts another conversation to about budget characters for a message that points at it (wire.RefChat); nil:
	// such a reference is refused (unsupported).
	Digest func(ctx context.Context, conv string, budget int) (wire.ChatDigest, error)
	// ExportDir is where a conversation is written whole as Markdown for another agent to read; empty: no digest.
	ExportDir string
	// IdleStop is after how long without work a chat's terminal is stopped by itself, read at every sweep; nil or 0
	// never.
	IdleStop func() time.Duration
	// Rewinder binds a rewound chat to its new session and keeps the folder's snapshots; nil (or no Terminals): rewind
	// is refused{unsupported}.
	Rewinder Rewinder
	// AutoContinue says which of the runtime's own continuations are on, read each time one is due: after a usage
	// limit resets, after a restart cut a turn short. nil: both on.
	AutoContinue func() (limit, restart bool)
	// Window is the window the agent itself reports for a session — its statusline for Claude, the transcript's
	// model_context_window for Codex — and the tokens its last answer was asked with (0 where it says nothing). nil: the
	// window is guessed from the model id.
	Window func(p wire.Provider, sessionID, transcriptPath string) (used, limit int64)
	// LimitReset is when the provider's usage limit resets by the limits it last reported — the window it ran out of —
	// for a notice that names no time; 0 when they say nothing. nil: never known.
	LimitReset func(p wire.Provider, now time.Time) wire.Ms
}
