package selfupdate

// The window between updates. Every update restarts the runtime, and a runtime's start is heavy: the chats are away
// for a moment and the machine works hard to bring them back. A hub that releases several times a day would restart
// every machine as often, so after an update Run leaves the next ones alone for MinGap and only checks: the page still
// shows that an update is there, and a person who asks for it (the button, the command) gets it at once.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultMinGap is the window of a runtime that was not told another one (RUNTIME_UPDATE_MIN_GAP).
const DefaultMinGap = 24 * time.Hour

type windowState struct {
	AppliedAt string `json:"appliedAt"`
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// markApplied remembers that the machine was updated now.
func (u *Updater) markApplied() {
	if u.StateFile == "" {
		return
	}
	raw, _ := json.Marshal(windowState{AppliedAt: u.now().UTC().Format(time.RFC3339)})
	tmp := u.StateFile + ".new"
	if err := os.MkdirAll(filepath.Dir(u.StateFile), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, u.StateFile)
}

// heldUntil is the moment the window ends; zero when the machine may update by itself now.
func (u *Updater) heldUntil() time.Time {
	if u.MinGap <= 0 || u.StateFile == "" {
		return time.Time{}
	}
	raw, err := os.ReadFile(u.StateFile)
	if err != nil {
		return time.Time{}
	}
	var st windowState
	if json.Unmarshal(raw, &st) != nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, st.AppliedAt)
	if err != nil {
		return time.Time{}
	}
	now := u.now()
	// A clock that went back must not hold the updates for longer than the window.
	if until := at.Add(u.MinGap); until.After(now) && !at.After(now.Add(time.Minute)) {
		return until
	}
	return time.Time{}
}
