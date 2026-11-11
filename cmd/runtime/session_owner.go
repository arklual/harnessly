package main

import (
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sleep"
)

// sessionOwnerEnv set to 0 gives the chats' tmux sessions back to the keepers of the agent tabs, as before the
// conversations owned them (internal/convo/life.go): the tabs' idle sleep and startup resume act on chats too, the
// layouts restore their sessions at a start, and the session sweep judges a chat's session by its tab. Anything else,
// or nothing, leaves a chat's session to the conversations alone.
const sessionOwnerEnv = "RUNTIME_SESSION_OWNER"

func sessionOwnerOn(getenv func(string) string) bool { return getenv(sessionOwnerEnv) != "0" }

// chatOwns says the tmux session is a chat's and its life is the conversations' to decide; false while v2 is not up
// (its saved conversations are loaded before anything here asks) and with the owner switched off.
func (c *core) chatOwns(session string) bool {
	v := c.v2.Load()
	return v != nil && v.rt.OwnsSession(session)
}

// memoryShort: less than sleep.PressureRatio of the machine's memory is available; unreadable is no pressure.
func memoryShort() bool {
	r, ok := proc.MemAvailableRatio()
	return ok && r < sleep.PressureRatio
}

// sleepFloors read, at every sweep, the thresholds the tabs' sleep is set with, which hold for the chats too: after
// how long without activity an agent is stopped whatever the idle-stop setting says, and the shorter threshold of a
// machine short of memory.
func sleepFloors(names sleep.EnvNames) (idle, pressure func() time.Duration) {
	ms := func(v float64) time.Duration { return time.Duration(v) * time.Millisecond }
	return func() time.Duration { return ms(sleep.ReadConfig(os.Getenv, names).IdleMs) },
		func() time.Duration { return ms(sleep.ReadConfig(os.Getenv, names).PressureIdleMs) }
}
