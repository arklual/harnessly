package agent

import "encoding/json"

// Wire protocol between the cluster and the machine agent, version 1. One WebSocket per person × machine, JSON text
// frames. A request carries an id and gets exactly one response with the same id; an event has no id. Tunnel bytes
// travel as binary frames (see tunnels.go).

// Hello is the answer to `hello`.
type Hello struct {
	Protocol     int      `json:"protocol"`
	AgentVersion string   `json:"agentVersion"`
	User         string   `json:"user"`
	Home         string   `json:"home"`
	Hostname     string   `json:"hostname"`
	Features     []string `json:"features"`
}

// TranscriptFile is one transcript as the agent sees it.
type TranscriptFile struct {
	Provider string  `json:"provider"`
	Path     string  `json:"path"`
	Size     int64   `json:"size"`
	Inode    uint64  `json:"inode"`
	MtimeMs  float64 `json:"mtimeMs"`
	// SeenAt is when the agent noticed the change (events only), to measure delivery latency.
	SeenAt *int64 `json:"seenAt,omitempty"`
	// Appended carries the complete lines the change added, valid only if the reader's offset equals From.
	Appended *Appended `json:"appended,omitempty"`
}

type Appended struct {
	From int64  `json:"from"`
	Next int64  `json:"next"`
	Data string `json:"data"`
}

// ReadChunk answers `transcripts.read`: complete lines only, base64 of the raw bytes.
type ReadChunk struct {
	Offset      int64  `json:"offset"`
	Size        int64  `json:"size"`
	Inode       uint64 `json:"inode"`
	Data        string `json:"data"`
	Next        int64  `json:"next"`
	LineTooLong bool   `json:"lineTooLong"`
}

// StateFile is a file of the runtime's data directory, named relative to it.
type StateFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// StateResult is the cluster's answer to a state commit or patch.
type StateResult struct {
	OK      bool            `json:"ok"`
	Version *float64        `json:"version,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Error   *string         `json:"error,omitempty"`
}

// Events.

type evTranscripts struct {
	Event string           `json:"event"`
	Files []TranscriptFile `json:"files"`
}

type evState struct {
	Event   string  `json:"event"`
	Name    string  `json:"name"`
	Content *string `json:"content"`
}

type evMachine struct {
	Event        string          `json:"event"`
	Snapshot     json.RawMessage `json:"snapshot"`
	RuntimeError *string         `json:"runtimeError"`
}

type evAutomationResult struct {
	Event       string `json:"event"`
	RunID       string `json:"runId"`
	ResultToken string `json:"resultToken"`
	Summary     string `json:"summary"`
	BugIssueID  string `json:"bugIssueId,omitempty"`
	MrURL       string `json:"mrUrl,omitempty"`
}

type DocBinding struct {
	Slug           string  `json:"slug"`
	Kind           string  `json:"kind"`
	Key            *string `json:"key"`
	Title          string  `json:"title"`
	TabID          *string `json:"tabId"`
	SessionName    *string `json:"sessionName"`
	AgentSessionID *string `json:"agentSessionId"`
	Provider       string  `json:"provider"`
	SourcePath     *string `json:"sourcePath"`
	Cwd            *string `json:"cwd"`
}

type evDocBound struct {
	Event   string     `json:"event"`
	Binding DocBinding `json:"binding"`
}

type evDocReply struct {
	Event    string `json:"event"`
	ThreadID string `json:"threadId"`
	Body     string `json:"body"`
	Resolve  bool   `json:"resolve"`
}

type evRelay struct {
	Event   string          `json:"event"`
	Message json.RawMessage `json:"message"`
}

type evStateCommit struct {
	Event     string          `json:"event"`
	RequestID string          `json:"requestId"`
	Name      string          `json:"name"`
	Base      float64         `json:"base"`
	Content   json.RawMessage `json:"content"`
}

type evStatePatch struct {
	Event       string                     `json:"event"`
	RequestID   string                     `json:"requestId"`
	Name        string                     `json:"name"`
	SessionName string                     `json:"sessionName"`
	Set         map[string]json.RawMessage `json:"set"`
	Unset       []string                   `json:"unset"`
}
