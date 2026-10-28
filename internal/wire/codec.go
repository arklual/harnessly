package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// fieldSet lists the JSON fields one kind of a flat union carries; "name?" is optional and left out when nil.
type fieldSet []string

// marshalFlat writes plain (a union struct without its methods) with the discriminator and the fields of kind only.
func marshalFlat(disc, kind string, plain any, table map[string]fieldSet) ([]byte, error) {
	fields, ok := table[kind]
	if !ok {
		return nil, fmt.Errorf("wire: unknown %s %q", disc, kind)
	}
	b, err := json.Marshal(plain)
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(fields)+1)
	out[disc] = all[disc]
	for _, f := range fields {
		name, optional := strings.CutSuffix(f, "?")
		if optional && string(all[name]) == "null" {
			continue
		}
		out[name] = all[name]
	}
	return json.Marshal(out)
}

// unmarshalFlat reads a flat union into plain: the discriminator must be a known kind, and the object must carry
// every required field of that kind and no field of another.
func unmarshalFlat(b []byte, disc string, plain any, table map[string]fieldSet) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("wire: %s union is null", disc)
	}
	var kind string
	if err := json.Unmarshal(raw[disc], &kind); err != nil {
		return fmt.Errorf("wire: %s: %w", disc, err)
	}
	fields, ok := table[kind]
	if !ok {
		return fmt.Errorf("wire: unknown %s %q", disc, kind)
	}
	allowed := map[string]bool{disc: true}
	for _, f := range fields {
		name, optional := strings.CutSuffix(f, "?")
		allowed[name] = true
		if _, has := raw[name]; !has && !optional {
			return fmt.Errorf("wire: %s %q without %q", disc, kind, name)
		}
	}
	for name := range raw {
		if !allowed[name] {
			return fmt.Errorf("wire: %s %q with unknown field %q", disc, kind, name)
		}
	}
	return decodeStrict(b, plain)
}

// tagged writes plain (a struct without its methods) with its tag as the first field.
func tagged(key, tag string, plain any) ([]byte, error) {
	b, err := json.Marshal(plain)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != '{' {
		return nil, fmt.Errorf("wire: %s %q is not an object", key, tag)
	}
	t, _ := json.Marshal(tag)
	out := append([]byte(`{"`+key+`":`), t...)
	if len(b) > 2 {
		out = append(out, ',')
	}
	return append(out, b[1:]...), nil
}

// untag splits an object into its tag and the rest of its fields.
func untag(b []byte, key string) (tag string, rest []byte, err error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", nil, err
	}
	if raw == nil {
		return "", nil, errors.New("wire: null instead of an object")
	}
	t, ok := raw[key]
	if !ok {
		return "", nil, fmt.Errorf("wire: object without %q", key)
	}
	if err := json.Unmarshal(t, &tag); err != nil {
		return "", nil, fmt.Errorf("wire: %s: %w", key, err)
	}
	delete(raw, key)
	rest, err = json.Marshal(raw)
	return tag, rest, err
}

// decodeStrict reads exactly one JSON value into v and refuses fields v does not have.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("wire: data after the value")
	}
	return nil
}

var agentStateFields = map[string]fieldSet{
	string(StateStarting):          {"since", "step", "wait"},
	string(StateWorking):           {"since", "turnId", "compacting"},
	string(StateWaitingQuestion):   {"since", "questionId"},
	string(StateWaitingPermission): {"since", "permissionId"},
	string(StateReady):             {"since"},
	string(StateIdle):              {"since"},
	string(StateStopped):           {"since", "resumable"},
	string(StateLaunchFailed):      {"since", "reason"},
	string(StateGone):              {"since", "reason"},
	string(StateUnknown):           {"since"},
}

func (s AgentState) MarshalJSON() ([]byte, error) {
	type plain AgentState
	return marshalFlat("k", string(s.K), plain(s), agentStateFields)
}

func (s *AgentState) UnmarshalJSON(b []byte) error {
	type plain AgentState
	var p plain
	err := unmarshalFlat(b, "k", &p, agentStateFields)
	*s = AgentState(p)
	return err
}

var messageStateFields = map[string]fieldSet{
	string(MsgQueued):    {"position", "reason", "wait"},
	string(MsgTyping):    {"wait"},
	string(MsgInInput):   {"wait"},
	string(MsgAccepted):  {"entryId"},
	string(MsgInTurn):    {"turnId"},
	string(MsgDone):      {"turnId"},
	string(MsgFailed):    {"code", "retryable", "at"},
	string(MsgCancelled): {"by"},
}

func (s MessageState) MarshalJSON() ([]byte, error) {
	type plain MessageState
	return marshalFlat("k", string(s.K), plain(s), messageStateFields)
}

func (s *MessageState) UnmarshalJSON(b []byte) error {
	type plain MessageState
	var p plain
	err := unmarshalFlat(b, "k", &p, messageStateFields)
	*s = MessageState(p)
	return err
}

var gateStateFields = map[string]fieldSet{
	string(GateStateOpen):      {"refused?", "autoDenyAt?"},
	string(GateStateAnswering): {"opId", "wait"},
	string(GateStateAnswered):  {"at", "byDevice"},
	string(GateStateClosed):    {"reason"},
}

func (s GateState) MarshalJSON() ([]byte, error) {
	type plain GateState
	return marshalFlat("k", string(s.K), plain(s), gateStateFields)
}

func (s *GateState) UnmarshalJSON(b []byte) error {
	type plain GateState
	var p plain
	err := unmarshalFlat(b, "k", &p, gateStateFields)
	*s = GateState(p)
	return err
}

var entryFields = func() map[string]fieldSet {
	base := fieldSet{"id", "turnId", "at", "rev"}
	kinds := map[EntryKind]fieldSet{
		EntryUser:       {"text", "attachments", "clientId"},
		EntrySay:        {"md"},
		EntryThink:      {"text"},
		EntryTool:       {"toolUseId", "tool", "summary", "ok", "output"},
		EntrySubagent:   {"agentId", "agentType", "description", "status", "model", "durationMs", "output"},
		EntryAnswers:    {"questionId", "pairs"},
		EntryGate:       {"permissionId"},
		EntryCompaction: {"tokensBefore", "tokensAfter", "keep"},
		EntryNotice:     {"level", "code", "detail"},
		EntryInterrupt:  {},
		EntryPeer:       {"peers"},
	}
	out := make(map[string]fieldSet, len(kinds))
	for k, f := range kinds {
		out[string(k)] = append(append(fieldSet{}, base...), f...)
	}
	return out
}()

func (e Entry) MarshalJSON() ([]byte, error) {
	type plain Entry
	return marshalFlat("t", string(e.T), plain(e), entryFields)
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	type plain Entry
	var p plain
	err := unmarshalFlat(b, "t", &p, entryFields)
	*e = Entry(p)
	return err
}

var cmdResultFields = map[string]fieldSet{
	string(ResultAccepted): {"ch", "seq", "rev?", "id?"},
	string(ResultQueued):   {"ch", "seq", "rev?", "id?"},
	string(ResultRefused):  {"code"},
}

func (r CmdResult) MarshalJSON() ([]byte, error) {
	type plain CmdResult
	return marshalFlat("r", string(r.R), plain(r), cmdResultFields)
}

func (r *CmdResult) UnmarshalJSON(b []byte) error {
	type plain CmdResult
	var p plain
	err := unmarshalFlat(b, "r", &p, cmdResultFields)
	*r = CmdResult(p)
	return err
}

func (f HelloFrame) MarshalJSON() ([]byte, error) {
	type plain HelloFrame
	return tagged("f", string(FrameHello), plain(f))
}

func (f SubFrame) MarshalJSON() ([]byte, error) {
	type plain SubFrame
	return tagged("f", string(FrameSub), plain(f))
}

func (f UnsubFrame) MarshalJSON() ([]byte, error) {
	type plain UnsubFrame
	return tagged("f", string(FrameUnsub), plain(f))
}

func (f PingFrame) MarshalJSON() ([]byte, error) {
	type plain PingFrame
	return tagged("f", string(FramePing), plain(f))
}

func (f WelcomeFrame) MarshalJSON() ([]byte, error) {
	type plain WelcomeFrame
	return tagged("f", string(FrameWelcome), plain(f))
}

func (f PongFrame) MarshalJSON() ([]byte, error) {
	type plain PongFrame
	return tagged("f", string(FramePong), plain(f))
}

func (f ErrorFrame) MarshalJSON() ([]byte, error) {
	type plain ErrorFrame
	return tagged("f", string(FrameError), plain(f))
}

// data is the snapshot's data for its channel; nil when it holds none or the wrong one.
func (f SnapshotFrame) data() any {
	switch _, conv := f.Ch.ConvID(); {
	case f.Ch == ChannelMachine && f.Machine != nil:
		return f.Machine
	case f.Ch == ChannelConvs && f.Convs != nil:
		return f.Convs
	case conv && f.Conv != nil:
		return f.Conv
	}
	return nil
}

type snapshotWire struct {
	Ch   Channel         `json:"ch"`
	Seq  int64           `json:"seq"`
	Mode SnapshotMode    `json:"mode"`
	Data json.RawMessage `json:"data"`
}

func (f SnapshotFrame) MarshalJSON() ([]byte, error) {
	data := f.data()
	if data == nil {
		return nil, fmt.Errorf("wire: snapshot of %q without its data", f.Ch)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return tagged("f", string(FrameSnapshot), snapshotWire{f.Ch, f.Seq, f.Mode, b})
}

func (f *SnapshotFrame) UnmarshalJSON(b []byte) error {
	var w snapshotWire
	if err := decodeStrict(b, &w); err != nil {
		return err
	}
	*f = SnapshotFrame{Ch: w.Ch, Seq: w.Seq, Mode: w.Mode}
	switch _, conv := w.Ch.ConvID(); {
	case w.Ch == ChannelMachine:
		f.Machine = &MachineSnapshot{}
		return decodeStrict(w.Data, f.Machine)
	case w.Ch == ChannelConvs:
		f.Convs = &ConvsSnapshot{}
		return decodeStrict(w.Data, f.Convs)
	case conv:
		f.Conv = &ConvSnapshot{}
		return decodeStrict(w.Data, f.Conv)
	}
	return fmt.Errorf("wire: snapshot of unknown channel %q", w.Ch)
}

func (f EvFrame) MarshalJSON() ([]byte, error) {
	if f.E == nil {
		return nil, errors.New("wire: ev without an event")
	}
	type plain EvFrame
	return tagged("f", string(FrameEv), plain(f))
}

func (f *EvFrame) UnmarshalJSON(b []byte) error {
	var w struct {
		Ch  Channel         `json:"ch"`
		Seq int64           `json:"seq"`
		E   json.RawMessage `json:"e"`
	}
	if err := decodeStrict(b, &w); err != nil {
		return err
	}
	e, err := decodeEvent(w.E)
	*f = EvFrame{Ch: w.Ch, Seq: w.Seq, E: e}
	return err
}

var clientFrames = map[FrameKind]func([]byte) (ClientFrame, error){
	FrameHello: as[HelloFrame, ClientFrame],
	FrameSub:   as[SubFrame, ClientFrame],
	FrameUnsub: as[UnsubFrame, ClientFrame],
	FramePing:  as[PingFrame, ClientFrame],
}

var serverFrames = map[FrameKind]func([]byte) (ServerFrame, error){
	FrameWelcome:  as[WelcomeFrame, ServerFrame],
	FrameSnapshot: as[SnapshotFrame, ServerFrame],
	FrameEv:       as[EvFrame, ServerFrame],
	FramePong:     as[PongFrame, ServerFrame],
	FrameError:    as[ErrorFrame, ServerFrame],
}

var events = map[EventType]func([]byte) (Event, error){
	EvMachineStatus:       as[MachineStatusEv, Event],
	EvMachineLoad:         as[MachineLoadEv, Event],
	EvConversationUpsert:  as[ConversationUpsertEv, Event],
	EvConversationState:   as[ConversationStateEv, Event],
	EvConversationSeen:    as[ConversationSeenEv, Event],
	EvConversationRemoved: as[ConversationRemovedEv, Event],
	EvProjectUpsert:       as[ProjectUpsertEv, Event],
	EvFavoritesUpsert:     as[FavoritesUpsertEv, Event],
	EvDraftUpsert:         as[DraftUpsertEv, Event],
	EvTurnStarted:         as[TurnStartedEv, Event],
	EvStep:                as[StepEv, Event],
	EvEntryUpdate:         as[EntryUpdateEv, Event],
	EvEntryReplace:        as[EntryReplaceEv, Event],
	EvNow:                 as[NowEv, Event],
	EvQuestionAsked:       as[QuestionAskedEv, Event],
	EvPermissionRequested: as[PermissionRequestedEv, Event],
	EvPlanProposed:        as[PlanProposedEv, Event],
	EvGateUpdated:         as[GateUpdatedEv, Event],
	EvCompaction:          as[CompactionEv, Event],
	EvTurnFinished:        as[TurnFinishedEv, Event],
	EvTurnFailed:          as[TurnFailedEv, Event],
	EvQuotaExhausted:      as[QuotaExhaustedEv, Event],
	EvQueueChanged:        as[QueueChangedEv, Event],
	EvPromptState:         as[PromptStateEv, Event],
	EvMessageUpsert:       as[MessageUpsertEv, Event],
	EvBackgroundUpsert:    as[BackgroundUpsertEv, Event],
	EvBackgroundRemoved:   as[BackgroundRemovedEv, Event],
	EvShotsSet:            as[ShotsSetEv, Event],
	EvPicturesSet:         as[PicturesSetEv, Event],
	EvFeedReset:           as[FeedResetEv, Event],
}

// as decodes a T strictly and hands it over as its union U.
func as[T any, U any](b []byte) (U, error) {
	var v T
	err := decodeStrict(b, &v)
	return any(v).(U), err
}

// decodeTagged reads the tag of an object and decodes the rest as the kind the table names; nothing is checked.
func decodeTagged[K ~string, U any](b []byte, key, what string, table map[K]func([]byte) (U, error)) (U, error) {
	var zero U
	tag, rest, err := untag(b, key)
	if err != nil {
		return zero, err
	}
	decode, ok := table[K(tag)]
	if !ok {
		return zero, fmt.Errorf("wire: unknown %s %q", what, tag)
	}
	v, err := decode(rest)
	if err != nil {
		return zero, fmt.Errorf("wire: %s %q: %w", what, tag, err)
	}
	return v, nil
}

// DecodeClientFrame reads and checks one frame from the page; an unknown kind or field is an error.
func DecodeClientFrame(b []byte) (ClientFrame, error) {
	f, err := decodeTagged(b, "f", "client frame", clientFrames)
	if err != nil {
		return nil, err
	}
	return f, f.Validate()
}

// DecodeServerFrame reads and checks one frame from the runtime; an unknown kind or field is an error.
func DecodeServerFrame(b []byte) (ServerFrame, error) {
	f, err := decodeTagged(b, "f", "server frame", serverFrames)
	if err != nil {
		return nil, err
	}
	return f, f.Validate()
}

func decodeEvent(b []byte) (Event, error) { return decodeTagged(b, "type", "event", events) }

// DecodeEvent reads and checks one event; an unknown type or field is an error.
func DecodeEvent(b []byte) (Event, error) {
	e, err := decodeEvent(b)
	if err != nil {
		return nil, err
	}
	return e, e.Validate()
}

func eventJSON(e Event, plain any) ([]byte, error) {
	return tagged("type", string(e.EventType()), plain)
}

func (e MachineStatusEv) MarshalJSON() ([]byte, error) {
	type plain MachineStatusEv
	return eventJSON(e, plain(e))
}

func (e MachineLoadEv) MarshalJSON() ([]byte, error) {
	type plain MachineLoadEv
	return eventJSON(e, plain(e))
}

func (e ConversationUpsertEv) MarshalJSON() ([]byte, error) {
	type plain ConversationUpsertEv
	return eventJSON(e, plain(e))
}

func (e ConversationStateEv) MarshalJSON() ([]byte, error) {
	type plain ConversationStateEv
	return eventJSON(e, plain(e))
}

func (e ConversationSeenEv) MarshalJSON() ([]byte, error) {
	type plain ConversationSeenEv
	return eventJSON(e, plain(e))
}

func (e ConversationRemovedEv) MarshalJSON() ([]byte, error) {
	type plain ConversationRemovedEv
	return eventJSON(e, plain(e))
}

func (e ProjectUpsertEv) MarshalJSON() ([]byte, error) {
	type plain ProjectUpsertEv
	return eventJSON(e, plain(e))
}

func (e FavoritesUpsertEv) MarshalJSON() ([]byte, error) {
	type plain FavoritesUpsertEv
	return eventJSON(e, plain(e))
}

func (e DraftUpsertEv) MarshalJSON() ([]byte, error) {
	type plain DraftUpsertEv
	return eventJSON(e, plain(e))
}

func (e TurnStartedEv) MarshalJSON() ([]byte, error) {
	type plain TurnStartedEv
	return eventJSON(e, plain(e))
}

func (e StepEv) MarshalJSON() ([]byte, error) {
	type plain StepEv
	return eventJSON(e, plain(e))
}

func (e EntryUpdateEv) MarshalJSON() ([]byte, error) {
	type plain EntryUpdateEv
	return eventJSON(e, plain(e))
}

func (e EntryReplaceEv) MarshalJSON() ([]byte, error) {
	type plain EntryReplaceEv
	return eventJSON(e, plain(e))
}

func (e NowEv) MarshalJSON() ([]byte, error) {
	type plain NowEv
	return eventJSON(e, plain(e))
}

func (e QuestionAskedEv) MarshalJSON() ([]byte, error) {
	type plain QuestionAskedEv
	return eventJSON(e, plain(e))
}

func (e PermissionRequestedEv) MarshalJSON() ([]byte, error) {
	type plain PermissionRequestedEv
	return eventJSON(e, plain(e))
}

func (e PlanProposedEv) MarshalJSON() ([]byte, error) {
	type plain PlanProposedEv
	return eventJSON(e, plain(e))
}

func (e GateUpdatedEv) MarshalJSON() ([]byte, error) {
	type plain GateUpdatedEv
	return eventJSON(e, plain(e))
}

func (e CompactionEv) MarshalJSON() ([]byte, error) {
	type plain CompactionEv
	return eventJSON(e, plain(e))
}

func (e TurnFinishedEv) MarshalJSON() ([]byte, error) {
	type plain TurnFinishedEv
	return eventJSON(e, plain(e))
}

func (e TurnFailedEv) MarshalJSON() ([]byte, error) {
	type plain TurnFailedEv
	return eventJSON(e, plain(e))
}

func (e QuotaExhaustedEv) MarshalJSON() ([]byte, error) {
	type plain QuotaExhaustedEv
	return eventJSON(e, plain(e))
}

func (e QueueChangedEv) MarshalJSON() ([]byte, error) {
	type plain QueueChangedEv
	return eventJSON(e, plain(e))
}

func (e PromptStateEv) MarshalJSON() ([]byte, error) {
	type plain PromptStateEv
	return eventJSON(e, plain(e))
}

func (e MessageUpsertEv) MarshalJSON() ([]byte, error) {
	type plain MessageUpsertEv
	return eventJSON(e, plain(e))
}

func (e BackgroundUpsertEv) MarshalJSON() ([]byte, error) {
	type plain BackgroundUpsertEv
	return eventJSON(e, plain(e))
}

func (e BackgroundRemovedEv) MarshalJSON() ([]byte, error) {
	type plain BackgroundRemovedEv
	return eventJSON(e, plain(e))
}

func (e ShotsSetEv) MarshalJSON() ([]byte, error) {
	type plain ShotsSetEv
	return eventJSON(e, plain(e))
}

func (e FeedResetEv) MarshalJSON() ([]byte, error) {
	type plain FeedResetEv
	return eventJSON(e, plain(e))
}

func (e PicturesSetEv) MarshalJSON() ([]byte, error) {
	type plain PicturesSetEv
	return eventJSON(e, plain(e))
}

var commands = map[Op]func([]byte) (Command, error){
	OpSend:                as[SendCmd, Command],
	OpCancel:              as[CancelCmd, Command],
	OpRetry:               as[RetryCmd, Command],
	OpPromote:             as[PromoteCmd, Command],
	OpAnswer:              as[AnswerCmd, Command],
	OpDecide:              as[DecideCmd, Command],
	OpInterrupt:           as[InterruptCmd, Command],
	OpQueueResume:         as[QueueResumeCmd, Command],
	OpSeen:                as[SeenCmd, Command],
	OpConversationCreate:  as[ConversationCreateCmd, Command],
	OpConversationRename:  as[ConversationRenameCmd, Command],
	OpConversationRetitle: as[ConversationRetitleCmd, Command],
	OpConversationPin:     as[ConversationPinCmd, Command],
	OpConversationMove:    as[ConversationMoveCmd, Command],
	OpConversationDelete:  as[ConversationDeleteCmd, Command],
	OpConversationRestore: as[ConversationRestoreCmd, Command],
	OpConversationResume:  as[ConversationResumeCmd, Command],
	OpModel:               as[ModelCmd, Command],
	OpBgStop:              as[BgStopCmd, Command],
	OpProjectCreate:       as[ProjectCreateCmd, Command],
	OpFavGroupCreate:      as[FavGroupCreateCmd, Command],
	OpFavGroupRename:      as[FavGroupRenameCmd, Command],
	OpFavGroupDelete:      as[FavGroupDeleteCmd, Command],
	OpFavGroupMove:        as[FavGroupMoveCmd, Command],
	OpFavPlace:            as[FavPlaceCmd, Command],
	OpGoal:                as[GoalCmd, Command],
	OpDraftSet:            as[DraftSetCmd, Command],
	OpQueueMove:           as[QueueMoveCmd, Command],
	OpTerminalStop:        as[TerminalStopCmd, Command],
	OpRewind:              as[RewindCmd, Command],
	OpRewindUndo:          as[RewindUndoCmd, Command],
	OpConversationFork:    as[ConversationForkCmd, Command],
	OpConversationHandoff: as[ConversationHandoffCmd, Command],
	OpConversationMerge:   as[ConversationMergeCmd, Command],
	OpLimit:               as[LimitCmd, Command],
}

// DecodeCommand reads and checks the body of POST /api/v2/cmd/<op>; an unknown op or field is an error.
func DecodeCommand(op Op, body []byte) (Command, error) {
	decode, ok := commands[op]
	if !ok {
		return nil, fmt.Errorf("wire: unknown op %q", op)
	}
	c, err := decode(body)
	if err != nil {
		return nil, fmt.Errorf("wire: %s: %w", op, err)
	}
	return c, c.Validate()
}
