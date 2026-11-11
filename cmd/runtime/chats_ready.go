package main

// What /api/health says about the chats after a start, so a rollout can restart the runtimes of a machine one at a
// time and wait for each: a listening port says nothing, the routes are served only once the conversations are loaded,
// and the minutes after that are the heaviest of a runtime's life.
const (
	// chatsStarting: the saved conversations are not loaded yet; the page has nothing to talk to.
	chatsStarting = "starting"
	// chatsRestoring: the conversations are listed and answer, while the tabs are taken over, their tmux sessions come
	// back and the agents that ran before the restart are resumed.
	chatsRestoring = "restoring"
	// chatsReady: the start's work is done; the next runtime of the machine may start.
	chatsReady = "ready"
)

// chatsState is the state above; "" from a runtime that keeps no conversations (v2 off), which has nothing to wait for.
func (c *core) chatsState() string {
	if c == nil || c.v2d == nil || !v2Enabled() {
		return ""
	}
	v := c.v2.Load()
	switch {
	case v == nil || !v.ready.Load():
		return chatsStarting
	case !v.settled.Load() || !c.started.Load():
		return chatsRestoring
	}
	return chatsReady
}
