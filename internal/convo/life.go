package convo

import (
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The life of a chat's terminal — its tmux session and the agent in it — has one owner: this package. A chat is in
// one of three phases, read off its agent state:
//
//   - cold: no agent process and no tmux session; the record and the transcript are all there is;
//   - warm: the session is there and the agent stands at its prompt;
//   - hot:  a turn runs, or the agent waits for the person's answer inside one.
//
// The owner alone moves a chat between them:
//
//	cold → warm   the launch of a new chat (Runtime.Create, freshLaunch) and the resume of a stopped one (resume):
//	              by its next message, a model change, or the start of the runtime for a chat that was hot when
//	              the runtime went down (cutAtRestart, restartDue);
//	warm ↔ hot    the agent's own turns (agentstate);
//	warm → cold   the stop of the terminal (stopTerminal): by the person, or by the sweep once the chat stood idle
//	              for one of its windows (Reap, idleWindows);
//	hot → cold    the person's forced stop, and the agent seen gone (seeing).
//
// Everything else that makes, ends or resumes tmux sessions asks the owner instead of deciding for a chat: the
// sleep of idle agent tabs and the resume of agent tabs at a start (both made for tabs that are no chats) leave a
// chat's session alone (OwnsSession), the layouts' restore of sessions makes none for it, and the sweep of bare
// sessions ends a chat's only when the owner calls it spare (SessionSpare).
//
// Deps.LegacyLife puts all of that back the way it was before the chats had an owner.

// Phase is where a chat's terminal stands.
type Phase string

const (
	PhaseCold Phase = "cold"
	PhaseWarm Phase = "warm"
	PhaseHot  Phase = "hot"
)

// PhaseOf reads the phase off an agent state. A state not looked at yet (unknown, after a start) and a launch on its
// way count as warm: a session may be there, and nothing ends it before a look says what it holds.
func PhaseOf(k wire.AgentStateKind) Phase {
	switch k {
	case wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		return PhaseHot
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed:
		return PhaseCold
	}
	return PhaseWarm
}

// Phase is the phase of a conversation as its actor last left it; false for one the runtime does not have.
func (r *Runtime) Phase(id string) (Phase, bool) {
	v := r.view(id)
	if v == nil || v.deleted {
		return "", false
	}
	return PhaseOf(v.rec.State.K), true
}

// owns is the live conversation that holds the session as its agent's, nil when the owner is off or there is none.
func (r *Runtime) owns(session string) *recView {
	if r.d.LegacyLife || session == "" {
		return nil
	}
	r.mu.Lock()
	id := r.bySession[session]
	r.mu.Unlock()
	if id == "" {
		return nil
	}
	if v := r.view(id); v != nil && !v.deleted {
		return v
	}
	return nil
}

// OwnsSession says the tmux session is a chat's, so its life is decided here and by nobody else. Always false with
// Deps.LegacyLife.
func (r *Runtime) OwnsSession(session string) bool { return r.owns(session) != nil }

// SessionSpare says whether a chat's session that holds nothing but its shell may be ended: the chat is cold with
// nothing waiting to be typed, so the session serves nobody and the resume makes it again. owned is false for a
// session that is no chat's (or with Deps.LegacyLife): the caller decides as it did.
func (r *Runtime) SessionSpare(session string) (spare, owned bool) {
	v := r.owns(session)
	if v == nil {
		return false, false
	}
	return v.rec.State.K == wire.StateStopped && v.rec.Queue.Count == 0, true
}

// --- the start of the runtime ---

// hotAtStop: the agent was in a turn when the record was last written.
func hotAtStop(k wire.AgentStateKind) bool { return PhaseOf(k) == PhaseHot }

// ownAtRestart is the owner's part of a start, called before the first look: a chat whose agent was up and can be
// resumed is cold when that look finds its session gone (the layouts make none for it; see seeing), and one that
// was hot is brought back by the chat itself once the look has said so.
func (c *conv) ownAtRestart(was wire.AgentState, running bool) {
	if c.d.LegacyLife || !agentUp(was.K) || c.rec.Session == "" || c.rec.AgentSessionID == "" && c.rec.TranscriptPath == "" {
		return
	}
	c.coldAtRestart = true
	if hotAtStop(was.K) && !running {
		c.startUp, c.startGate = true, true
	}
}

// startUpDue resumes the agent of a chat that was hot when the runtime went down and is cold now. The word to go on
// (restartDue) has resumed it already when there was one to send; this is the chat that gets none — the automatic
// continuation is off, or the agent was waiting for the person — and whose agent is owed all the same.
func (c *conv) startUpDue(now time.Time) {
	if !c.startUp {
		return
	}
	k := c.rec.State.K
	if k == wire.StateUnknown {
		return // not looked at yet
	}
	if k == wire.StateStopped && c.rec.State.Resumable && c.rec.TerminalStop == nil && c.deletedAt == 0 {
		c.resume(now)
	}
	c.startUp, c.startGate = false, false
}

// startResumes is how many chats resume their agents at once after a start: every one of them starts an agent CLI,
// and a machine that came back with twenty chats at work must not start twenty at the same moment.
const startResumes = 4

// --- the idle windows ---

// idleWindows are the two windows of the idle stop. setting is the runtime's: a chat nobody has open is stopped once
// it stood idle that long; 0 never. floor is the threshold the sleep of idle agent tabs kept for every chat before
// the chats had an owner, and keeps here: a Claude chat whose record shows no activity for that long is stopped
// whatever the setting says and whoever has it open — an agent nobody wrote to for hours is memory the machine's
// other chats need. While the machine is short of memory the floor is the shorter pressure window. 0 for no floor,
// always with Deps.LegacyLife (the tabs' sleep does it).
func (r *Runtime) idleWindows() (setting, floor time.Duration) {
	if r.d.IdleStop != nil {
		setting = r.d.IdleStop()
	}
	if r.d.LegacyLife {
		return setting, 0
	}
	if r.d.IdleFloor != nil {
		floor = max(r.d.IdleFloor(), 0)
	}
	if r.d.MemoryShort != nil && r.d.PressureStop != nil {
		if p := r.d.PressureStop(); p > 0 && (floor == 0 || p < floor) && r.d.MemoryShort() {
			floor = p
		}
	}
	return setting, floor
}
