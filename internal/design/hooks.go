package design

// LaunchRequest is a new agent conversation in a project's folder, as the dialog starts it.
type LaunchRequest struct {
	Cwd         string
	CwdRoots    []string
	Prompt      string
	PromptFiles []string
}

// LaunchResult is the conversation started (OK) or the status and the error code or text to word for the person.
type LaunchResult struct {
	OK     bool
	TabID  string
	Status int
	Error  string
}

// Terminal is what a message to an agent needs of the tab keeper and tmux.
type Terminal interface {
	// AgentTab is a layout tab's tmux session and panel type; ok false when there is no such tab.
	AgentTab(tabID string) (session, panelType string, ok bool)
	// Front is whether only a shell runs in the pane, and the name of what does.
	Front(session string) (safe bool, process string)
	// Deliver types a line and Enter, or submits through the agent's editor (a long or multi-line text).
	Deliver(session, text string, viaEditor bool) error
	// NeedsEditor is whether a text must go through the editor.
	NeedsEditor(text string) bool
}

// PushRequest is a board sent to the person's other devices.
type PushRequest struct {
	Title, URL, Tag, DeviceID string
}
