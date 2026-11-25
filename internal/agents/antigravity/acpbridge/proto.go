// Package acpbridge runs a native Antigravity chat over the CLI's Agent Client Protocol (`agy acp`) instead of its
// terminal UI. The bridge is the process in the chat's terminal: it owns the `agy acp` session, draws the
// conversation in the terminal (in the frame the runtime's screen reader knows: an input box and a model footer),
// takes what the person types there, and serves the runtime on a unix socket, so messages, steering, stops,
// approvals and model changes go over a structured channel, as Codex's do over its app-server.
package acpbridge

import "encoding/json"

// The socket speaks JSON lines. A request carries Req; its answer carries the same Req. Events carry no Req.

// Request is what the runtime asks of the bridge.
type Request struct {
	Req int    `json:"req"`
	Op  string `json:"op"` // hello, prompt, cancel, config, permission

	// prompt
	ID     string   `json:"id,omitempty"` // the client's id of the message: a message sent twice starts one turn
	Text   string   `json:"text,omitempty"`
	Images []string `json:"images,omitempty"` // paths of image files
	Files  []string `json:"files,omitempty"`  // paths of other attached files

	// config: "" leaves a setting as it is
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Mode   string `json:"mode,omitempty"` // ask, plan, bypass

	// permission
	Permission string `json:"permission,omitempty"` // the id of the ask
	Decision   string `json:"decision,omitempty"`   // allow, always, deny, cancel
}

// Reply answers one Request.
type Reply struct {
	Req   int    `json:"req"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// State comes with hello and config.
	State *State `json:"state,omitempty"`
}

// State is what the bridge's session is now.
type State struct {
	Session    string `json:"session"`
	Transcript string `json:"transcript,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Busy       bool   `json:"busy"`
	// Delivered are the client ids of the messages the session has taken.
	Delivered []string `json:"delivered,omitempty"`
}

// Event is what the bridge tells unasked.
type Event struct {
	Ev string `json:"ev"` // turn-started, turn-ended, permission, permission-gone, state

	// turn-ended
	Stop  string `json:"stop,omitempty"` // end_turn, cancelled, max_tokens, refusal, error
	Error string `json:"error,omitempty"`

	// permission, permission-gone
	Permission *Permission `json:"permission,omitempty"`

	// state
	State *State `json:"state,omitempty"`
}

// Permission is a tool call the agent asks the person to allow.
type Permission struct {
	ID    string          `json:"id"`
	Tool  string          `json:"tool"`            // the agent's tool name: run_command, write_to_file, …
	Kind  string          `json:"kind,omitempty"`  // ACP's kind: execute, edit, read, fetch, …
	Title string          `json:"title,omitempty"` // ACP's title: the command, the file
	Input json.RawMessage `json:"input,omitempty"` // the call's arguments
	// Always: an option that allows calls like this one from now on is offered.
	Always bool `json:"always,omitempty"`
}

// Socket line limit: a prompt with long text fits, a runaway line does not.
const maxLine = 8 << 20
