package convo

import (
	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// NoticeKind is what a Notice tells.
type NoticeKind string

const (
	// NoticeDone: the agent ended its turn and waits for the person (its Stop).
	NoticeDone NoticeKind = "done"
	// NoticeFailed: the turn ended with an error (StopFailure).
	NoticeFailed NoticeKind = "failed"
	// NoticeQuestion: the agent waits on a question, a permission or a plan.
	NoticeQuestion NoticeKind = "question"
	// NoticeTurnEnded: a turn ended however it ended (done, failed, stopped by the person, gone quiet); the folder's
	// snapshot for the rewind is taken on it.
	NoticeTurnEnded NoticeKind = "turn-ended"
	// NoticeState: the agent's state changed (setState); the tab keeper's view of the chat follows it.
	NoticeState NoticeKind = "state"
)

// Notice is one thing a chat's actor tells Deps.Notify, with the record as it stands right after it.
type Notice struct {
	Kind NoticeKind
	Conv wire.Conversation
}

// Push says the notice is one the person gets on their devices.
func (n Notice) Push() bool {
	return n.Kind == NoticeDone || n.Kind == NoticeFailed || n.Kind == NoticeQuestion
}

func (c *conv) tell(k NoticeKind) {
	if c.d.Notify == nil || c.deletedAt != 0 {
		return
	}
	c.d.Notify(Notice{Kind: k, Conv: c.rec})
}

// notify: the rule that ended the turn tells the person (only Stop and StopFailure do; silence and the person's own
// stop do not).
func (c *conv) notify(failed bool) {
	if failed {
		c.tell(NoticeFailed)
		return
	}
	c.tell(NoticeDone)
}

func (c *conv) notifyQuestion() { c.tell(NoticeQuestion) }

func (c *conv) notifyTurnEnded() { c.tell(NoticeTurnEnded) }

func failedIn(effects []agentstate.Effect) bool {
	for _, e := range effects {
		if e.K == agentstate.TurnFailed {
			return true
		}
	}
	return false
}

// opensGate: the rule put the agent in front of a question, a permission or a plan.
func opensGate(effects []agentstate.Effect) bool {
	for _, e := range effects {
		switch e.K {
		case agentstate.AskQuestion, agentstate.RequestPermission, agentstate.ProposePlan:
			return true
		}
	}
	return false
}

func waitingKind(k wire.AgentStateKind) bool {
	return k == wire.StateWaitingQuestion || k == wire.StateWaitingPermission
}
