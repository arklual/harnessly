package wire

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The checks here are the invariants the JSON shape cannot carry: closed lists, ids that must not be empty, the
// kind a channel takes. A decoder runs them on everything it reads; a sender may run them in tests.

func in[T comparable](name string, v T, all []T) error {
	if slices.Contains(all, v) {
		return nil
	}
	return fmt.Errorf("%s: unknown %v", name, v)
}

func allIn[T comparable](name string, vs []T, all []T) error {
	for _, v := range vs {
		if err := in(name, v, all); err != nil {
			return err
		}
	}
	return nil
}

func need(name, v string) error {
	if v == "" {
		return fmt.Errorf("%s: empty", name)
	}
	return nil
}

func needPtr(name string, v *string) error {
	if v == nil {
		return nil
	}
	return need(name, *v)
}

func turnID(name, id string) error {
	if !ValidTurnID(id) {
		return fmt.Errorf("%s: %q is not a turn id", name, id)
	}
	return nil
}

func turnIDPtr(name string, id *string) error {
	if id == nil {
		return nil
	}
	return turnID(name, *id)
}

func entryID(name, id string) error {
	if !ValidEntryID(id) {
		return fmt.Errorf("%s: %q is not an entry id", name, id)
	}
	return nil
}

func entryIDPtr(name string, id *string) error {
	if id == nil {
		return nil
	}
	return entryID(name, *id)
}

// wait checks the term of a waiting state; required says the state has one.
func wait(w *Wait, required bool) error {
	switch {
	case w == nil && required:
		return errors.New("wait: none")
	case w != nil && w.DeadlineAt < w.SlowAt:
		return errors.New("wait: deadlineAt before slowAt")
	}
	return nil
}

func questionID(name, id string) error {
	if !ValidQuestionID(id) {
		return fmt.Errorf("%s: %q is not a question id", name, id)
	}
	return nil
}

func permissionID(name, id string) error {
	if !ValidPermissionID(id) {
		return fmt.Errorf("%s: %q is not a permission id", name, id)
	}
	return nil
}

func gateID(name, id string) error {
	if !ValidQuestionID(id) && !ValidPermissionID(id) {
		return fmt.Errorf("%s: %q is not a question or permission id", name, id)
	}
	return nil
}

func convID(name, id string) error {
	if !ValidConvID(id) {
		return fmt.Errorf("%s: %q is not a conversation id", name, id)
	}
	return nil
}

func positive(name string, v int64) error {
	if v < 1 {
		return fmt.Errorf("%s: %d is below 1", name, v)
	}
	return nil
}

func nonNegative(name string, v int64) error {
	if v < 0 {
		return fmt.Errorf("%s: %d is below 0", name, v)
	}
	return nil
}

func under(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}

func each[T interface{ Validate() error }](name string, vs []T) error {
	for i, v := range vs {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%s[%d]: %w", name, i, err)
		}
	}
	return nil
}

func (m Machine) Validate() error {
	errs := []error{need("id", m.ID), in("reach", m.Reach, Reaches)}
	if m.Runtime != nil {
		errs = append(errs, under("runtime", m.Runtime.Validate()))
	}
	errs = append(errs, each("agents", m.Agents))
	if m.Health != nil {
		errs = append(errs, under("health", m.Health.Validate()))
	}
	return errors.Join(errs...)
}

func (h MachineHealth) Validate() error {
	errs := []error{in("level", h.Level, HealthLevels), nonNegative("since", h.Since), each("issues", h.Issues)}
	worst := HealthOK
	for _, i := range h.Issues {
		if slices.Index(HealthLevels, i.Level) > slices.Index(HealthLevels, worst) {
			worst = i.Level
		}
	}
	if worst != h.Level {
		errs = append(errs, fmt.Errorf("level: %s, the worst issue is %s", h.Level, worst))
	}
	return errors.Join(errs...)
}

func (i HealthIssue) Validate() error {
	errs := []error{in("metric", i.Metric, HealthMetrics), in("level", i.Level, HealthLevels)}
	if i.Level == HealthOK {
		errs = append(errs, errors.New("level: an issue is warn or critical"))
	}
	return errors.Join(errs...)
}

func (r RuntimeInfo) Validate() error { return allIn("features", r.Features, Features) }

func (a MachineAgent) Validate() error {
	errs := []error{in("provider", a.Provider, Providers), in("status", a.Status, AgentInstallStatuses)}
	for i, m := range a.Models {
		errs = append(errs, under("models["+strconv.Itoa(i)+"]", m.Validate()))
	}
	for i, p := range a.Profiles {
		errs = append(errs, under("profiles["+strconv.Itoa(i)+"]", p.Validate()))
	}
	return errors.Join(errs...)
}

func (m AgentModel) Validate() error {
	errs := []error{need("id", m.ID), need("label", m.Label), allIn("efforts", m.Efforts, Efforts)}
	if m.DefaultEffort != nil {
		errs = append(errs, in("defaultEffort", *m.DefaultEffort, Efforts))
	}
	return errors.Join(errs...)
}

func (p Project) Validate() error {
	return errors.Join(need("id", p.ID), need("machineId", p.MachineID), need("path", p.Path))
}

func (d Dir) Validate() error {
	errs := []error{need("path", d.Path), need("name", d.Name)}
	if d.Branch != nil {
		errs = append(errs, need("branch", *d.Branch))
		if !d.Git {
			errs = append(errs, errors.New("branch: only a git folder has one"))
		}
	}
	return errors.Join(errs...)
}

func (d Dirs) Validate() error { return each("dirs", d) }

var terminalIDPattern = regexp.MustCompile(`^[a-z0-9]{6,32}$`)

// ValidTerminalID says whether s names an own terminal.
func ValidTerminalID(s string) bool { return terminalIDPattern.MatchString(s) }

func (t Terminal) Validate() error {
	errs := []error{nonNegative("createdAt", int64(t.CreatedAt))}
	if !ValidTerminalID(t.ID) {
		errs = append(errs, errors.New("id: not a terminal id"))
	}
	return errors.Join(errs...)
}

func (t Terminals) Validate() error {
	if len(t) > TerminalsMax {
		return errors.New("terminals: too many")
	}
	return each("terminals", t)
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func sessionID(name, v string) error {
	if !sessionIDPattern.MatchString(v) {
		return errors.New(name + ": not a session id (a UUID)")
	}
	return nil
}

func (s AgentSession) Validate() error {
	errs := []error{in("provider", s.Provider, Providers), sessionID("sessionId", s.SessionID), need("title", s.Title), need("cwd", s.Cwd),
		nonNegative("updatedAt", int64(s.UpdatedAt)), nonNegative("turns", int64(s.Turns))}
	if s.Cwd != "" && !strings.HasPrefix(s.Cwd, "/") {
		errs = append(errs, errors.New("cwd: must be absolute"))
	}
	return errors.Join(errs...)
}

func (s AgentSessions) Validate() error {
	if len(s) > SessionsMax {
		return errors.New("sessions: too many")
	}
	return each("sessions", s)
}

func (m Model) Validate() error {
	errs := []error{need("id", m.ID)}
	if m.Profile != "" {
		errs = append(errs, profileIDErr(m.Profile))
	}
	if m.Effort != nil {
		errs = append(errs, in("effort", *m.Effort, Efforts))
	}
	return errors.Join(errs...)
}

func (c Conversation) Validate() error {
	errs := []error{
		convID("id", c.ID), need("machineId", c.MachineID), needPtr("projectId", c.ProjectID),
		in("provider", c.Provider, Providers), in("titleSource", c.TitleSource, TitleSources),
		under("state", c.State.Validate()), under("queue", c.Queue.Validate()), positive("rev", c.Rev),
	}
	if c.Model != nil {
		errs = append(errs, under("model", c.Model.Validate()))
	}
	if len(c.Rewinds) > RewindsMax {
		errs = append(errs, fmt.Errorf("rewinds: more than %d", RewindsMax))
	}
	for i, m := range c.Rewinds {
		errs = append(errs, under(fmt.Sprintf("rewinds[%d]", i), m.Validate()))
	}
	if c.LastTurn != nil {
		errs = append(errs, under("lastTurn", c.LastTurn.Validate()))
	}
	if c.Gate != nil {
		errs = append(errs, in("gate.kind", c.Gate.Kind, GateKinds), gateID("gate.id", c.Gate.ID))
	}
	if c.Goal != nil {
		errs = append(errs, need("goal.text", c.Goal.Text), in("goal.status", c.Goal.Status, GoalStatuses))
	}
	if c.Recap != nil {
		errs = append(errs, need("recap.text", c.Recap.Text))
	}
	if o := c.Origin; o != nil {
		errs = append(errs, in("origin.k", o.K, OriginKinds), convID("origin.fromId", o.FromID),
			in("origin.fromProvider", o.FromProvider, Providers), in("origin.via", o.Via, OriginVias))
		if o.FromModel != nil {
			errs = append(errs, under("origin.fromModel", o.FromModel.Validate()))
		}
		if o.AtTurn != nil && !ValidTurnID(*o.AtTurn) {
			errs = append(errs, fmt.Errorf("origin.atTurn: %q is not a turn id", *o.AtTurn))
		}
		if o.K == OriginHandoff && o.Via == ViaSession {
			errs = append(errs, errors.New("origin.via: a handoff carries an excerpt or a note"))
		}
	}
	if l := c.Limited; l != nil {
		errs = append(errs, in("limited.provider", l.Provider, Providers), in("limited.window", l.Window, LimitWindows))
		if l.ContinueAt != nil && l.ResetsAt == nil {
			errs = append(errs, errors.New("limited.continueAt: without resetsAt"))
		}
	}
	if len(c.AutoContinues) > AutoContinueMax {
		errs = append(errs, errors.New("autoContinues: too many"))
	}
	for _, a := range c.AutoContinues {
		errs = append(errs, in("autoContinues.why", a.Why, AutoContinueWhys), need("autoContinues.clientId", a.ClientID))
	}
	if c.ParentID != nil {
		errs = append(errs, convID("parentId", *c.ParentID))
	}
	if ts := c.TerminalStop; ts != nil {
		errs = append(errs, in("terminalStop.by", ts.By, TerminalStopBys))
		if ts.IdleMs < 0 {
			errs = append(errs, errors.New("terminalStop.idleMs: negative"))
		}
	}
	if f := c.TerminalFailure; f != nil {
		errs = append(errs, under("terminalFailure", f.Validate()))
	}
	if p := c.PendingModel; p != nil {
		errs = append(errs, need("pendingModel.id", p.ID))
		if p.Effort != nil {
			errs = append(errs, in("pendingModel.effort", *p.Effort, Efforts))
		}
	}
	if len(c.ModelLog) > ModelLogMax {
		errs = append(errs, errors.New("modelLog: too many"))
	}
	for _, sw := range c.ModelLog {
		errs = append(errs, under("modelLog.to", sw.To.Validate()))
		if sw.From != nil {
			errs = append(errs, under("modelLog.from", sw.From.Validate()))
		}
		if sw.Refused != nil {
			errs = append(errs, in("modelLog.refused", *sw.Refused, RefuseCodes))
		}
	}
	return errors.Join(errs...)
}

func (q QueueSummary) Validate() error {
	if q.Paused == nil {
		return nil
	}
	return under("paused", q.Paused.Validate())
}

func (p QueuePause) Validate() error { return in("reason", p.Reason, PauseReasons) }

func (s AgentState) Validate() error {
	if err := in("k", s.K, AgentStateKinds); err != nil {
		return err
	}
	switch s.K {
	case StateStarting:
		return errors.Join(in("step", s.Step, LaunchSteps), wait(s.Wait, true))
	case StateWorking:
		return turnIDPtr("turnId", s.TurnID)
	case StateWaitingQuestion:
		return questionID("questionId", s.QuestionID)
	case StateWaitingPermission:
		return permissionID("permissionId", s.PermissionID)
	case StateLaunchFailed:
		return in("reason", LaunchFailure(s.Reason), LaunchFailures)
	case StateGone:
		return in("reason", GoneReason(s.Reason), GoneReasons)
	}
	return nil
}

func (t Turn) Validate() error {
	errs := []error{
		turnID("id", t.ID), in("author.provider", t.Author.Provider, Providers), in("status", t.Status, TurnStatuses),
		entryIDPtr("answerEntryId", t.AnswerEntryID),
	}
	switch {
	case t.Status == TurnFailed && t.Failure == nil:
		errs = append(errs, errors.New("failure: a failed turn says why"))
	case t.Status != TurnFailed && t.Failure != nil:
		errs = append(errs, fmt.Errorf("failure: on a %s turn", t.Status))
	case t.Failure != nil:
		errs = append(errs, in("failure.code", t.Failure.Code, TurnFailureCodes))
	}
	if t.Status == TurnLive && t.EndedAt != nil {
		errs = append(errs, errors.New("endedAt: on a live turn"))
	}
	for i, f := range t.Files {
		errs = append(errs, in(fmt.Sprintf("files[%d].op", i), f.Op, FileOps))
	}
	for i, o := range t.Outputs {
		errs = append(errs, in(fmt.Sprintf("outputs[%d].kind", i), o.Kind, OutputKinds),
			in(fmt.Sprintf("outputs[%d].op", i), o.Op, OutputOps), need(fmt.Sprintf("outputs[%d].title", i), o.Title),
			need(fmt.Sprintf("outputs[%d].ref", i), o.Ref))
	}
	return errors.Join(errs...)
}

func (t TurnSummary) Validate() error {
	errs := []error{turnID("id", t.ID), in("status", t.Status, TurnStatuses)}
	if n := utf8.RuneCountInString(t.Snippet); n > SnippetMax {
		errs = append(errs, fmt.Errorf("snippet: %d characters, over %d", n, SnippetMax))
	}
	return errors.Join(errs...)
}

func (e Entry) Validate() error {
	errs := []error{entryID("id", e.ID), turnIDPtr("turnId", e.TurnID), positive("rev", e.Rev), in("t", e.T, EntryKinds)}
	switch e.T {
	case EntryUser:
		errs = append(errs, needPtr("clientId", e.ClientID), each("attachments", e.Attachments))
	case EntryTool:
		errs = append(errs, need("toolUseId", e.ToolUseID))
	case EntrySubagent:
		errs = append(errs, need("agentId", e.AgentID), in("status", e.Status, SubagentStatuses))
	case EntryAnswers:
		errs = append(errs, questionID("questionId", e.QuestionID))
	case EntryGate:
		errs = append(errs, permissionID("permissionId", e.PermissionID))
	case EntryNotice:
		errs = append(errs, in("level", e.Level, NoticeLevels), need("code", e.Code))
	case EntryPeer:
		if len(e.Peers) == 0 {
			errs = append(errs, errors.New("peers: none"))
		}
		for i, n := range e.Peers {
			errs = append(errs, in(fmt.Sprintf("peers[%d].kind", i), n.Kind, PeerNoteKinds))
		}
	}
	return errors.Join(errs...)
}

func (a AttachmentRef) Validate() error {
	return errors.Join(need("id", a.ID), in("kind", a.Kind, AttachmentKinds))
}

// RefTitleMax and RefTargetMax bound a reference's words, in characters.
const (
	RefTitleMax  = 200
	RefTargetMax = 4096
)

func (r MessageRef) Validate() error {
	errs := []error{in("kind", r.Kind, RefKinds), need("id", r.ID)}
	if utf8.RuneCountInString(r.ID) > RefTargetMax {
		errs = append(errs, fmt.Errorf("id: longer than %d characters", RefTargetMax))
	}
	if utf8.RuneCountInString(r.Title) > RefTitleMax {
		errs = append(errs, fmt.Errorf("title: longer than %d characters", RefTitleMax))
	}
	if utf8.RuneCountInString(r.Target) > RefTargetMax {
		errs = append(errs, fmt.Errorf("target: longer than %d characters", RefTargetMax))
	}
	return errors.Join(errs...)
}

// refsValid checks a message's or a draft's references: at most RefsMax, each well formed.
func refsValid(refs []MessageRef) error {
	if len(refs) > RefsMax {
		return fmt.Errorf("refs: more than %d", RefsMax)
	}
	return each("refs", refs)
}

func (m Message) Validate() error {
	errs := []error{
		need("clientId", m.ClientID), convID("conversationId", m.ConversationID), each("attachments", m.Attachments),
		refsValid(m.Refs),
		in("mode", m.Mode, MessageModes), in("kind", m.Kind, MessageKinds), under("state", m.State.Validate()),
		positive("rev", m.Rev),
	}
	if m.Auto != "" {
		errs = append(errs, in("auto", m.Auto, AutoContinueWhys))
	}
	return errors.Join(errs...)
}

func (s MessageState) Validate() error {
	if err := in("k", s.K, MessageStateKinds); err != nil {
		return err
	}
	switch s.K {
	case MsgQueued:
		return errors.Join(positive("position", int64(s.Position)), in("reason", s.Reason, QueueReasons),
			wait(s.Wait, false))
	case MsgTyping, MsgInInput:
		return wait(s.Wait, true)
	case MsgAccepted:
		return entryIDPtr("entryId", s.EntryID)
	case MsgInTurn, MsgDone:
		return turnID("turnId", s.TurnID)
	case MsgFailed:
		return in("code", s.Code, MessageFailures)
	case MsgCancelled:
		return in("by", s.By, []string{CancelledByUser})
	}
	return nil
}

func (q Queue) Validate() error {
	errs := []error{convID("conversationId", q.ConversationID), positive("rev", q.Rev)}
	for i, id := range q.Items {
		errs = append(errs, need(fmt.Sprintf("items[%d]", i), id))
	}
	if q.Paused != nil {
		errs = append(errs, under("paused", q.Paused.Validate()))
	}
	return errors.Join(errs...)
}

func (q Question) Validate() error {
	return errors.Join(
		questionID("id", q.ID), convID("conversationId", q.ConversationID), turnID("turnId", q.TurnID),
		under("state", q.State.Validate()),
	)
}

func (p Permission) Validate() error {
	errs := []error{
		permissionID("id", p.ID), convID("conversationId", p.ConversationID), turnIDPtr("turnId", p.TurnID),
		in("kind", p.Kind, PermissionKinds),
		under("state", p.State.Validate()),
	}
	for i, o := range p.Options {
		errs = append(errs, need(fmt.Sprintf("options[%d].id", i), o.ID),
			in(fmt.Sprintf("options[%d].scope", i), o.Scope, PermissionScopes))
	}
	return errors.Join(errs...)
}

func (s GateState) Validate() error {
	if err := in("k", s.K, GateStateKinds); err != nil {
		return err
	}
	switch s.K {
	case GateStateOpen:
		if s.Refused != nil {
			return in("refused", *s.Refused, GateRefusals)
		}
	case GateStateAnswering:
		return errors.Join(need("opId", s.OpID), wait(s.Wait, true))
	case GateStateAnswered:
		return need("byDevice", s.ByDevice)
	case GateStateClosed:
		return in("reason", s.Reason, GateCloseReasons)
	}
	return nil
}

func (s Shot) Validate() error {
	return errors.Join(need("id", s.ID), need("name", s.Name))
}

func (p Picture) Validate() error {
	errs := []error{need("id", p.ID), need("text", p.Text)}
	if !strings.HasPrefix(p.Text, "/") {
		errs = append(errs, errors.New("text: not an absolute path"))
	}
	return errors.Join(errs...)
}

func picturesValid(pictures []Picture) error {
	errs := []error{each("pictures", pictures)}
	if len(pictures) > PicturesMax {
		errs = append(errs, errors.New("pictures: too many"))
	}
	return errors.Join(errs...)
}

func shotsValid(shots []Shot) error {
	errs := []error{each("shots", shots)}
	if len(shots) > ShotsMax {
		errs = append(errs, errors.New("shots: too many"))
	}
	return errors.Join(errs...)
}

func (b BackgroundTask) Validate() error {
	errs := []error{
		need("id", b.ID), convID("conversationId", b.ConversationID), in("kind", b.Kind, BackgroundKinds),
		in("status", b.Status, BackgroundStatuses),
	}
	if b.Effort != nil {
		errs = append(errs, in("effort", *b.Effort, Efforts))
	}
	if b.Teammate && b.Kind != BgSubagent {
		errs = append(errs, errors.New("teammate: only a subagent is one"))
	}
	if b.Status == BgStatusIdle && !b.Teammate {
		errs = append(errs, errors.New("status: only a teammate is idle"))
	}
	if (b.Workflow != nil) != (b.Kind == BgWorkflow) {
		errs = append(errs, errors.New("workflow: a workflow and only it has one"))
	}
	if b.Workflow != nil {
		errs = append(errs, under("workflow", b.Workflow.Validate()))
	}
	return errors.Join(errs...)
}

func (w WorkflowRun) Validate() error {
	var errs []error
	for i, p := range w.Phases {
		for k, a := range p.Agents {
			path := fmt.Sprintf("phases[%d].agents[%d]", i, k)
			errs = append(errs, under(path, errors.Join(need("id", a.ID), in("status", a.Status, BackgroundStatuses))))
			if a.Status == BgStatusIdle {
				errs = append(errs, errors.New(path+".status: a workflow's agent is never idle"))
			}
		}
	}
	return errors.Join(errs...)
}

func (n Now) Validate() error { return turnID("turnId", n.TurnID) }

func (s MachineSnapshot) Validate() error { return under("machine", s.Machine.Validate()) }

func (s ConvsSnapshot) Validate() error {
	errs := []error{each("conversations", s.Conversations), each("projects", s.Projects)}
	if s.Favorites != nil {
		errs = append(errs, under("favorites", s.Favorites.Validate()))
	}
	errs = append(errs, each("drafts", s.Drafts))
	return errors.Join(errs...)
}

func (s ConvSnapshot) Validate() error {
	errs := []error{
		each("turns", s.Turns), each("entries", s.Entries), turnIDPtr("beforeTurnId", s.BeforeTurnID),
		under("queue", s.Queue.Validate()), each("messages", s.Messages), each("questions", s.Questions),
		each("permissions", s.Permissions), each("background", s.Background), shotsValid(s.Shots), picturesValid(s.Pictures),
	}
	if s.Now != nil {
		errs = append(errs, under("now", s.Now.Validate()))
	}
	return errors.Join(errs...)
}

func (h Hello) Validate() error { return allIn("features", h.Features, Features) }

func (m RewindMark) Validate() error {
	errs := []error{turnIDPtr("afterTurnId", m.AfterTurnID)}
	if m.Turns < 1 {
		errs = append(errs, errors.New("turns: less than 1"))
	}
	if m.UndoUntil < m.At {
		errs = append(errs, errors.New("undoUntil: before at"))
	}
	return errors.Join(errs...)
}

func (p RewindPlan) Validate() error {
	errs := []error{turnID("entryId", p.EntryID)}
	if p.Turns < 1 {
		errs = append(errs, errors.New("turns: less than 1"))
	}
	if f := p.Files; f != nil {
		if len(f.Changes) > RewindChangesMax || f.More < 0 {
			errs = append(errs, errors.New("files: changes out of bounds"))
		}
		for i, c := range f.Changes {
			errs = append(errs, need(fmt.Sprintf("files.changes[%d].path", i), c.Path), in(fmt.Sprintf("files.changes[%d].change", i), c.Change, FileChanges))
		}
	}
	return errors.Join(errs...)
}

func (p TurnsPage) Validate() error {
	return errors.Join(each("turns", p.Turns), each("entries", p.Entries), turnIDPtr("beforeTurnId", p.BeforeTurnID))
}

func (p EntriesPage) Validate() error {
	return errors.Join(turnID("turnId", p.TurnID), each("entries", p.Entries))
}

func (p OutputPage) Validate() error { return entryID("entryId", p.EntryID) }

func channel(name string, c Channel) error {
	if !c.Valid() {
		return fmt.Errorf("%s: unknown channel %q", name, c)
	}
	return nil
}

func proto(v int) error {
	if v != Proto {
		return fmt.Errorf("proto: %d, not %d", v, Proto)
	}
	return nil
}

func (f HelloFrame) Validate() error { return errors.Join(proto(f.Proto), need("device", f.Device)) }

func (f SubFrame) Validate() error {
	errs := []error{channel("ch", f.Ch)}
	if f.AfterSeq != nil && *f.AfterSeq < 0 {
		errs = append(errs, errors.New("afterSeq: below 0"))
	}
	if f.AfterEntryID != "" {
		errs = append(errs, entryID("afterEntryId", f.AfterEntryID))
	}
	if f.AfterSeq != nil && f.Epoch == "" {
		errs = append(errs, errors.New("afterSeq: without an epoch"))
	}
	return errors.Join(errs...)
}

func (f UnsubFrame) Validate() error { return channel("ch", f.Ch) }

func (f PingFrame) Validate() error { return nil }

func (f WelcomeFrame) Validate() error {
	return errors.Join(
		proto(f.Proto), need("epoch", f.Epoch), under("machine", f.Machine.Validate()),
		allIn("features", f.Features, Features),
	)
}

func (f SnapshotFrame) Validate() error {
	if err := errors.Join(channel("ch", f.Ch), in("mode", f.Mode, SnapshotModes)); err != nil {
		return err
	}
	if f.Seq < 0 {
		return errors.New("seq: below 0")
	}
	set := 0
	for _, has := range []bool{f.Machine != nil, f.Convs != nil, f.Conv != nil} {
		if has {
			set++
		}
	}
	data, ok := f.data().(interface{ Validate() error })
	if set != 1 || !ok {
		return fmt.Errorf("data: not the data of %q", f.Ch)
	}
	return under("data", data.Validate())
}

// ChannelEvents is the event types a channel carries.
func ChannelEvents(c Channel) []EventType {
	if _, conv := c.ConvID(); conv {
		return ConvEventTypes
	}
	switch c {
	case ChannelMachine:
		return MachineEventTypes
	case ChannelConvs:
		return ConvsEventTypes
	}
	return nil
}

func (f EvFrame) Validate() error {
	if err := errors.Join(channel("ch", f.Ch), positive("seq", f.Seq)); err != nil {
		return err
	}
	if f.E == nil {
		return errors.New("e: no event")
	}
	if !slices.Contains(ChannelEvents(f.Ch), f.E.EventType()) {
		return fmt.Errorf("e: %s does not go on %q", f.E.EventType(), f.Ch)
	}
	return under("e", f.E.Validate())
}

func (f PongFrame) Validate() error { return nil }

func (f ErrorFrame) Validate() error {
	errs := []error{in("code", f.Code, FrameErrorCodes)}
	if f.Ch != "" {
		errs = append(errs, channel("ch", f.Ch))
	}
	return errors.Join(errs...)
}

func (e MachineStatusEv) Validate() error {
	errs := []error{in("reach", e.Reach, Reaches), each("agents", e.Agents)}
	if e.Runtime != nil {
		errs = append(errs, under("runtime", e.Runtime.Validate()))
	}
	if e.Health != nil {
		errs = append(errs, under("health", e.Health.Validate()))
	}
	return errors.Join(errs...)
}

func (e MachineLoadEv) Validate() error        { return nil }
func (e ConversationUpsertEv) Validate() error { return under("conv", e.Conv.Validate()) }

func (e ConversationStateEv) Validate() error {
	return errors.Join(convID("id", e.ID), under("state", e.State.Validate()), positive("rev", e.Rev))
}

func (e ConversationSeenEv) Validate() error {
	return errors.Join(convID("id", e.ID), positive("rev", e.Rev))
}

func (e ProjectUpsertEv) Validate() error { return under("project", e.Project.Validate()) }

func (e ConversationRemovedEv) Validate() error { return convID("id", e.ID) }
func (e TurnStartedEv) Validate() error         { return under("turn", e.Turn.Validate()) }
func (e StepEv) Validate() error                { return under("entry", e.Entry.Validate()) }
func (e EntryUpdateEv) Validate() error         { return under("entry", e.Entry.Validate()) }

func (e EntryReplaceEv) Validate() error {
	return errors.Join(entryID("oldId", e.OldID), under("entry", e.Entry.Validate()))
}

func (e NowEv) Validate() error           { return e.Now.Validate() }
func (e QuestionAskedEv) Validate() error { return under("question", e.Question.Validate()) }

func (e PermissionRequestedEv) Validate() error {
	return errors.Join(under("permission", e.Permission.Validate()), kindOf(e.Permission, PermTool))
}

func (e PlanProposedEv) Validate() error {
	return errors.Join(under("permission", e.Permission.Validate()), kindOf(e.Permission, PermPlan))
}

func kindOf(p Permission, k PermissionKind) error {
	if p.Kind != k {
		return fmt.Errorf("permission.kind: %s, not %s", p.Kind, k)
	}
	return nil
}

func (e GateUpdatedEv) Validate() error {
	return errors.Join(gateID("id", e.ID), under("state", e.State.Validate()))
}

func (e CompactionEv) Validate() error {
	errs := []error{in("phase", e.Phase, CompactionPhases), under("entry", e.Entry.Validate())}
	if e.Entry.T != EntryCompaction {
		errs = append(errs, fmt.Errorf("entry: %s, not compaction", e.Entry.T))
	}
	return errors.Join(errs...)
}

func (e TurnFinishedEv) Validate() error {
	errs := []error{under("turn", e.Turn.Validate())}
	if e.Turn.Status != TurnDone && e.Turn.Status != TurnInterrupted {
		errs = append(errs, fmt.Errorf("turn.status: %s in turn_finished", e.Turn.Status))
	}
	return errors.Join(errs...)
}

func (e TurnFailedEv) Validate() error {
	errs := []error{under("turn", e.Turn.Validate())}
	if e.Turn.Status != TurnFailed {
		errs = append(errs, fmt.Errorf("turn.status: %s in turn_failed", e.Turn.Status))
	}
	return errors.Join(errs...)
}

func (f TerminalFailure) Validate() error {
	errs := []error{in("kind", f.Kind, TerminalFailureKinds)}
	if !f.Terminal {
		errs = append(errs, errors.New("terminal: false"))
	}
	if !f.NeedsTarget {
		errs = append(errs, errors.New("needsTarget: false"))
	}
	return errors.Join(errs...)
}

func (e QuotaExhaustedEv) Validate() error {
	err := under("failure", e.Failure.Validate())
	if e.Failure.Kind != FailureQuotaExhausted {
		return errors.Join(err, fmt.Errorf("failure.kind: %s, not quota_exhausted", e.Failure.Kind))
	}
	return err
}

func (e QueueChangedEv) Validate() error { return under("queue", e.Queue.Validate()) }

func (e PromptStateEv) Validate() error {
	return errors.Join(need("clientId", e.ClientID), under("state", e.State.Validate()), positive("rev", e.Rev))
}

func (e MessageUpsertEv) Validate() error     { return under("message", e.Message.Validate()) }
func (e BackgroundUpsertEv) Validate() error  { return under("task", e.Task.Validate()) }
func (e BackgroundRemovedEv) Validate() error { return need("id", e.ID) }
func (e ShotsSetEv) Validate() error          { return shotsValid(e.Shots) }
func (e PicturesSetEv) Validate() error       { return picturesValid(e.Pictures) }
func (e FeedResetEv) Validate() error {
	return TurnsPage(e).Validate()
}

func (c SendCmd) Validate() error {
	errs := []error{
		need("opId", c.OpID), need("clientId", c.ClientID), convID("conversationId", c.ConversationID),
		in("mode", c.Mode, MessageModes), in("kind", c.Kind, MessageKinds),
	}
	if c.OpID != c.ClientID {
		errs = append(errs, errors.New("opId: not the clientId"))
	}
	for i, id := range c.AttachmentIDs {
		errs = append(errs, need(fmt.Sprintf("attachmentIds[%d]", i), id))
	}
	errs = append(errs, refsValid(c.Refs))
	return errors.Join(errs...)
}

func scoped(opID, conv string) error {
	return errors.Join(need("opId", opID), convID("conversationId", conv))
}

func (c CancelCmd) Validate() error {
	return errors.Join(scoped(c.OpID, c.ConversationID), need("clientId", c.ClientID))
}

func (c RetryCmd) Validate() error {
	return errors.Join(scoped(c.OpID, c.ConversationID), need("clientId", c.ClientID))
}

func (c PromoteCmd) Validate() error {
	return errors.Join(scoped(c.OpID, c.ConversationID), need("clientId", c.ClientID), in("to", c.To, PromoteTos))
}

func (c QueueMoveCmd) Validate() error {
	errs := []error{scoped(c.OpID, c.ConversationID), need("clientId", c.ClientID)}
	if c.BeforeClientID != nil {
		errs = append(errs, need("beforeClientId", *c.BeforeClientID))
		if *c.BeforeClientID == c.ClientID {
			errs = append(errs, errors.New("beforeClientId: a message is not placed before itself"))
		}
	}
	return errors.Join(errs...)
}

func (c AnswerCmd) Validate() error {
	errs := []error{scoped(c.OpID, c.ConversationID), questionID("questionId", c.QuestionID)}
	if c.ChatAbout == (len(c.Answers) > 0) {
		errs = append(errs, errors.New("answers: either answers or chatAbout"))
	}
	for i, a := range c.Answers {
		if a.I < 0 || slices.ContainsFunc(a.Choices, func(n int) bool { return n < 0 }) {
			errs = append(errs, fmt.Errorf("answers[%d]: a negative index", i))
		}
		if (a.Text != nil) == (len(a.Choices) > 0) || a.Text != nil && *a.Text == "" {
			errs = append(errs, fmt.Errorf("answers[%d]: either choices or text", i))
		}
	}
	return errors.Join(errs...)
}

func (c DecideCmd) Validate() error {
	return errors.Join(scoped(c.OpID, c.ConversationID), permissionID("permissionId", c.PermissionID),
		need("optionId", c.OptionID))
}

func (c InterruptCmd) Validate() error   { return scoped(c.OpID, c.ConversationID) }
func (c QueueResumeCmd) Validate() error { return scoped(c.OpID, c.ConversationID) }

func (c SeenCmd) Validate() error {
	if c.ConversationID == SeenAll {
		return need("opId", c.OpID)
	}
	return scoped(c.OpID, c.ConversationID)
}

// commandModel checks the model of a command that starts a conversation: the profile it runs on is named beside it.
func commandModel(m *Model, profile string) []error {
	var errs []error
	if m != nil {
		errs = append(errs, under("model", m.Validate()))
		if m.Profile != "" {
			errs = append(errs, errors.New("model.profile: name the profile in profile"))
		}
	}
	if profile != "" {
		errs = append(errs, under("profile", profileIDErr(profile)))
	}
	return errs
}

func (c ConversationCreateCmd) Validate() error {
	errs := []error{need("opId", c.OpID), in("provider", c.Provider, Providers)}
	errs = append(errs, commandModel(c.Model, c.Profile)...)
	if (c.ProjectID == "") == (c.Cwd == "") {
		errs = append(errs, errors.New("projectId: either projectId or cwd"))
	}
	if c.First != nil {
		errs = append(errs, need("first.clientId", c.First.ClientID))
	}
	if c.Launch != nil {
		errs = append(errs, need("launch.id", c.Launch.ID))
		if utf8.RuneCountInString(c.Launch.Prompt) > LaunchPromptMax {
			errs = append(errs, errors.New("launch.prompt: at most 200 characters"))
		}
	}
	if c.ParentID != "" {
		errs = append(errs, convID("parentId", c.ParentID))
	}
	if utf8.RuneCountInString(c.Title) > RefTitleMax {
		errs = append(errs, fmt.Errorf("title: longer than %d characters", RefTitleMax))
	}
	return errors.Join(errs...)
}

func byID(opID, id string) error { return errors.Join(need("opId", opID), convID("id", id)) }

func (c ConversationRenameCmd) Validate() error { return byID(c.OpID, c.ID) }

func (c ConversationRetitleCmd) Validate() error { return byID(c.OpID, c.ID) }
func (c ConversationPinCmd) Validate() error     { return byID(c.OpID, c.ID) }

func (c ConversationMoveCmd) Validate() error {
	return errors.Join(byID(c.OpID, c.ID), needPtr("projectId", c.ProjectID))
}

func (c ConversationDeleteCmd) Validate() error { return byID(c.OpID, c.ID) }
func (c TerminalStopCmd) Validate() error       { return byID(c.OpID, c.ID) }
func (c RewindUndoCmd) Validate() error         { return byID(c.OpID, c.ID) }

func (c RewindCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID), turnID("entryId", c.EntryID)}
	if strings.TrimSpace(c.Text) == "" {
		errs = append(errs, errors.New("text: empty"))
	}
	return errors.Join(errs...)
}

func (c LimitCmd) Validate() error {
	return errors.Join(byID(c.OpID, c.ID), in("action", c.Action, LimitActions))
}
func (c ConversationRestoreCmd) Validate() error { return byID(c.OpID, c.ID) }
func (c ConversationResumeCmd) Validate() error {
	if c.ID != "" {
		if c.Provider != "" || c.SessionID != "" || c.Cwd != "" {
			return errors.New("id: a conversation's own resume names nothing else")
		}
		return byID(c.OpID, c.ID)
	}
	errs := []error{need("opId", c.OpID), in("provider", c.Provider, Providers), sessionID("sessionId", c.SessionID), need("cwd", c.Cwd)}
	if c.Cwd != "" && !strings.HasPrefix(c.Cwd, "/") {
		errs = append(errs, errors.New("cwd: must be absolute"))
	}
	return errors.Join(errs...)
}

func (c ConversationForkCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID)}
	if c.AtTurn != nil && !ValidTurnID(*c.AtTurn) {
		errs = append(errs, fmt.Errorf("atTurn: %q is not a turn id", *c.AtTurn))
	}
	return errors.Join(errs...)
}

// handoffText: a handoff's or a merge's text is not empty and fits its message.
func handoffText(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("text: required")
	}
	if utf8.RuneCountInString(text) > HandoffTextMax {
		return fmt.Errorf("text: longer than %d characters", HandoffTextMax)
	}
	return nil
}

var handoffArtifactID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func artifactID(name, id string) error {
	if !handoffArtifactID.MatchString(id) {
		return fmt.Errorf("%s: not an artifact id", name)
	}
	return nil
}

func (a HandoffArtifact) Validate() error {
	return errors.Join(artifactID("artifact.id", a.ID), positive("artifact.bytes", a.Bytes))
}

func (c ConversationHandoffCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID), in("provider", c.Provider, Providers), in("mode", c.Mode, HandoffModes)}
	if c.Mode == HandoffHistory {
		errs = append(errs, artifactID("artifactId", c.ArtifactID))
		if c.Text != "" {
			errs = append(errs, errors.New("text: forbidden for history"))
		}
	} else {
		errs = append(errs, handoffText(c.Text))
		if c.ArtifactID != "" {
			errs = append(errs, errors.New("artifactId: forbidden outside history"))
		}
	}
	errs = append(errs, commandModel(c.Model, c.Profile)...)
	return errors.Join(errs...)
}

func (c ConversationMergeCmd) Validate() error {
	return errors.Join(byID(c.OpID, c.ID), handoffText(c.Text))
}

func (d HandoffDraft) Validate() error {
	errs := []error{in("mode", d.Mode, DraftModes)}
	if d.Mode == DraftHistory {
		if d.Artifact == nil {
			errs = append(errs, errors.New("artifact: required for history"))
		} else {
			errs = append(errs, d.Artifact.Validate())
		}
		if d.Text != "" {
			errs = append(errs, errors.New("text: forbidden for history"))
		}
	} else {
		errs = append(errs, need("text", d.Text))
		if d.Artifact != nil {
			errs = append(errs, errors.New("artifact: forbidden outside history"))
		}
	}
	if d.Tokens < 0 || d.Turns < 0 {
		errs = append(errs, errors.New("tokens: negative"))
	}
	return errors.Join(errs...)
}

func (r HandoffDraftRequest) Validate() error {
	errs := []error{in("mode", r.Mode, DraftModes)}
	if r.Provider != "" {
		errs = append(errs, in("provider", r.Provider, Providers))
	}
	if r.Profile != "" && !ValidProfileID(r.Profile) {
		errs = append(errs, errors.New("profile: not a profile id"))
	}
	return errors.Join(errs...)
}

func (c ModelCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID), need("model", c.Model)}
	if c.Effort != nil {
		errs = append(errs, in("effort", *c.Effort, Efforts))
	}
	return errors.Join(errs...)
}

func (c BgStopCmd) Validate() error {
	return errors.Join(scoped(c.OpID, c.ConversationID), need("taskId", c.TaskID))
}

func (c GoalCmd) Validate() error {
	errs := []error{byID(c.OpID, c.ID)}
	if utf8.RuneCountInString(c.Text) > GoalTextMax || strings.ContainsAny(c.Text, "\r\n") {
		errs = append(errs, errors.New("text: one line of at most 2000 characters"))
	}
	return errors.Join(errs...)
}

func (c ProjectCreateCmd) Validate() error {
	errs := []error{need("opId", c.OpID)}
	if c.Path == "" && strings.TrimSpace(c.Name) == "" {
		errs = append(errs, errors.New("name: required without a path"))
	}
	if c.Path != "" && !strings.HasPrefix(c.Path, "/") {
		errs = append(errs, errors.New("path: must be absolute"))
	}
	if utf8.RuneCountInString(c.Name) > ProjectNameMax || strings.ContainsAny(c.Name, "\r\n") {
		errs = append(errs, errors.New("name: one line of at most 120 characters"))
	}
	return errors.Join(errs...)
}

func (r CmdResult) Validate() error {
	if err := in("r", r.R, ResultKinds); err != nil {
		return err
	}
	if r.R == ResultRefused {
		return in("code", r.Code, RefuseCodes)
	}
	errs := []error{channel("ch", r.Ch), positive("seq", r.Seq), needPtr("id", r.ID)}
	if r.Rev != nil {
		errs = append(errs, positive("rev", *r.Rev))
	}
	return errors.Join(errs...)
}

func (s RuntimeSettings) Validate() error {
	errs := []error{in("idleStop", s.IdleStop, IdleStops)}
	if s.LimitContinue != "" {
		errs = append(errs, in("limitContinue", s.LimitContinue, OnOffs))
	}
	if s.RestartContinue != "" {
		errs = append(errs, in("restartContinue", s.RestartContinue, OnOffs))
	}
	if d := s.Delegation; d != nil && (d.MaxChildren < 1 || d.MaxChildren > DelegationMaxChildren) {
		errs = append(errs, errors.New("delegation.maxChildren: 1 to "+strconv.Itoa(DelegationMaxChildren)))
	}
	return errors.Join(errs...)
}
