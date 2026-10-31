package wirefake

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The scripted agent reads markers in a message's text: what the turn that message opens does instead of answering.
const (
	MarkWork       = "[fake:work]"        // the turn stays live until turn.finish or an interrupt
	MarkQuestion   = "[fake:question]"    // the turn asks a question
	MarkPermission = "[fake:permission]"  // the turn asks for a tool permission
	MarkPlan       = "[fake:plan]"        // the turn proposes a plan
	MarkFail       = "[fake:fail]"        // the turn fails on a rate limit
	MarkStuck      = "[fake:stuck]"       // the message stays in the input: failed{stuck-in-input}
	MarkLaunchFail = "[fake:launch-fail]" // as the first message of conversation.create: the launch fails
)

// Options are a fake's: Hub publishes its events; Step paces the scripted agent (300 ms when 0).
type Options struct {
	Hub     *wire.Hub
	Machine string
	Uploads *wire.Uploads
	Limits  wire.Limits
	Step    time.Duration
}

// Service is a convo.Service on the fixtures with a scripted agent behind every conversation.
type Service struct {
	opt Options

	mu         sync.Mutex
	gen        int // bumped by Reset: the timers of an older generation do nothing
	convs      map[string]*conv
	projects   map[string]wire.Project
	favs       wire.Favorites
	favN       int      // groups made, for their ids
	order      []string // project ids, oldest first
	defaultCwd string
	delays     map[wire.Op]time.Duration
	ids        int64
	tmpl       templates
	opened     map[string]string           // session id -> the conversation opened on it
	terms      map[string]map[string]int64 // conversation -> own terminal id -> created at
	designs    []fakeDesign                // the design library's projects, newest first; nil until first used
	edits      map[string]string           // conversation, path -> a saved file's text
}

var _ convo.Service = (*Service)(nil)

// templates are the gates the scripted agent asks, read from the fixtures.
type templates struct {
	question   wire.Question
	permission wire.Permission
	plan       wire.Permission
}

type conv struct {
	rec          wire.Conversation
	snap         wire.ConvSnapshot
	older        []wire.Turn  // turns before the snapshot's, oldest first
	olderEntries []wire.Entry // their entries
	deleted      bool
	typing       string // the clientId being typed, "" when none
	script       string // the text of the message that opened the live turn
}

// New loads the fixtures. The Hub must be the one the Server serves.
func New(opt Options) (*Service, error) {
	if opt.Step <= 0 {
		opt.Step = 300 * time.Millisecond
	}
	s := &Service{opt: opt, delays: map[wire.Op]time.Duration{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) now() int64 { return time.Now().UnixMilli() }

// load (re)reads the fixtures; under s.mu or before the fake serves.
func (s *Service) load() error {
	shift := s.now() - fixtureNow
	m := s.opt.Machine
	convs, err := snapshotOf("snapshot.convs", shift, m)
	if err != nil {
		return err
	}
	full, err := snapshotOf("snapshot.conv", shift, m)
	if err != nil {
		return err
	}
	q, err := eventOf("ev.question_asked", shift, m)
	if err != nil {
		return err
	}
	p, err := eventOf("ev.permission_requested", shift, m)
	if err != nil {
		return err
	}
	pl, err := eventOf("ev.plan_proposed", shift, m)
	if err != nil {
		return err
	}
	s.tmpl = templates{question: q.(wire.QuestionAskedEv).Question, permission: p.(wire.PermissionRequestedEv).Permission,
		plan: pl.(wire.PlanProposedEv).Permission}
	fullID, _ := full.Ch.ConvID()
	s.convs = map[string]*conv{}
	s.opened = nil
	s.terms, s.designs, s.edits = nil, nil, nil
	s.projects = map[string]wire.Project{}
	s.favs = wire.EmptyFavorites()
	s.defaultCwd = convs.Convs.DefaultCwd
	for _, p := range convs.Convs.Projects {
		p.CreatedAt += shift
		s.addProject(p)
	}
	for _, rec := range convs.Convs.Conversations {
		c := &conv{rec: rec}
		if _, ok := s.projects[ptrStr(rec.ProjectID)]; rec.ProjectID != nil && !ok {
			s.addProject(wire.Project{ID: *rec.ProjectID, MachineID: s.opt.Machine, Path: rec.Cwd, Name: filepath.Base(rec.Cwd), CreatedAt: rec.CreatedAt})
		}
		if rec.ID == fullID {
			// The conversation the fixtures play has a goal, so the right column of the page has one to show.
			c.rec.Goal = &wire.Goal{Text: "Довести контракт v2 до конца: схема, Go и страница сходятся, фикстуры зелёные",
				Status: wire.GoalActive, Since: s.now() - 26*3600*1000}
			c.rec.Recap = &wire.Recap{Text: "Свёл типы, схему и фикстуры контракта v2, всё зелёное. Дальше жду твоего решения, как назвать события правой колонки.",
				At: s.now() - 40*60*1000}
			c.snap = clone(*full.Conv)
			c.older, c.olderEntries = history(rec, c.snap.Turns, 40)
		} else {
			c.snap = s.minimal(rec)
		}
		s.convs[rec.ID] = c
	}
	return nil
}

// minimal is the snapshot of a fixture conversation the fixtures hold only the record of: its last turn, its gate
// and the messages its queue counts.
func (s *Service) minimal(rec wire.Conversation) wire.ConvSnapshot {
	snap := wire.ConvSnapshot{Turns: []wire.Turn{}, Entries: []wire.Entry{}, Messages: []wire.Message{},
		Questions: []wire.Question{}, Permissions: []wire.Permission{}, Background: []wire.BackgroundTask{},
		Queue: wire.Queue{ConversationID: rec.ID, Items: []string{}, Paused: rec.Queue.Paused, Rev: 1}}
	var turnID *string
	if lt := rec.LastTurn; lt != nil {
		id := lt.ID
		turnID = &id
		answer := strings.Replace(id, "#0", "#9", 1)
		snap.Turns = append(snap.Turns, wire.Turn{ID: id, Author: wire.TurnAuthor{Provider: rec.Provider}, StartedAt: lt.StartedAt,
			EndedAt: lt.EndedAt, DurationMs: lt.DurationMs, ToolCount: lt.ToolCount, StepCount: 2, Status: lt.Status,
			AnswerEntryID: &answer, Files: []wire.TurnFile{}})
		if lt.Status == wire.TurnFailed {
			snap.Turns[0].Failure = &wire.TurnFailure{Code: wire.TurnFailedUnknown}
		}
		snap.Entries = append(snap.Entries,
			wire.Entry{ID: id, TurnID: turnID, At: lt.StartedAt, Rev: 1, T: wire.EntryUser, Text: "Продолжай", Attachments: []wire.AttachmentRef{}},
			wire.Entry{ID: answer, TurnID: turnID, At: lt.StartedAt + 1000, Rev: 1, T: wire.EntrySay, MD: lt.Snippet})
	}
	if g := rec.Gate; g != nil {
		tid := ""
		if turnID != nil {
			tid = *turnID
		}
		switch g.Kind {
		case wire.GateQuestion:
			q := clone(s.tmpl.question)
			q.ID, q.ConversationID, q.TurnID = g.ID, rec.ID, tid
			snap.Questions = append(snap.Questions, q)
		default:
			p := clone(s.tmpl.permission)
			if g.Kind == wire.GatePlan {
				p = clone(s.tmpl.plan)
			}
			p.ID, p.ConversationID, p.TurnID = g.ID, rec.ID, turnID
			snap.Permissions = append(snap.Permissions, p)
		}
	}
	for i := 0; i < rec.Queue.Count; i++ {
		id := fmt.Sprintf("legacy-%s-%d", rec.ID, i)
		snap.Queue.Items = append(snap.Queue.Items, id)
		snap.Messages = append(snap.Messages, wire.Message{ClientID: id, ConversationID: rec.ID, Text: "Запусти тесты",
			Attachments: []wire.AttachmentRef{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt, CreatedAt: rec.State.Since,
			State: wire.MessageState{K: wire.MsgQueued, Position: i + 1, Reason: wire.QueuedPaused}, Rev: 1})
	}
	return snap
}

// history makes n turns before first, each a question and its answer, for the pages up the feed.
func history(rec wire.Conversation, first []wire.Turn, n int) ([]wire.Turn, []wire.Entry) {
	start := rec.CreatedAt
	if len(first) > 0 {
		start = first[0].StartedAt
	}
	var turns []wire.Turn
	var entries []wire.Entry
	for i := 0; i < n; i++ {
		at := start - int64(n-i)*10*60*1000
		id := fmt.Sprintf("c:00000000-0000-4000-8000-%012x#0", i+1)
		answer := fmt.Sprintf("c:00000000-0000-4000-9000-%012x#0", i+1)
		end, dur := at+60_000, int64(60_000)
		tid := id
		turns = append(turns, wire.Turn{ID: id, Author: wire.TurnAuthor{Provider: rec.Provider}, StartedAt: at, EndedAt: &end,
			DurationMs: &dur, StepCount: 2, Status: wire.TurnDone, AnswerEntryID: &answer, Files: []wire.TurnFile{}})
		entries = append(entries,
			wire.Entry{ID: id, TurnID: &tid, At: at, Rev: 1, T: wire.EntryUser, Text: fmt.Sprintf("Шаг %d", i+1), Attachments: []wire.AttachmentRef{}},
			wire.Entry{ID: answer, TurnID: &tid, At: end, Rev: 1, T: wire.EntrySay, MD: fmt.Sprintf("Готово: шаг %d.", i+1)})
	}
	return turns, entries
}

// --- reads -------------------------------------------------------------------------------------------------------

func (s *Service) Conversations(ctx context.Context) (wire.ConvsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := wire.ConvsSnapshot{Conversations: []wire.Conversation{}, Projects: []wire.Project{}, DefaultCwd: s.defaultCwd}
	for _, id := range s.order {
		out.Projects = append(out.Projects, s.projects[id])
	}
	for _, c := range s.convs {
		if !c.deleted {
			out.Conversations = append(out.Conversations, clone(c.rec))
		}
	}
	sort.SliceStable(out.Conversations, func(i, j int) bool {
		a, b := out.Conversations[i], out.Conversations[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.LastActivityAt != b.LastActivityAt {
			return a.LastActivityAt > b.LastActivityAt
		}
		return a.ID < b.ID
	})
	if len(s.favs.Groups) > 0 || len(s.favs.Loose) > 0 {
		favs := s.favs.Clone()
		out.Favorites = &favs
	}
	return out, nil
}

func (s *Service) get(id string) (*conv, error) {
	c := s.convs[id]
	if c == nil || c.deleted {
		return nil, convo.ErrNotFound
	}
	return c, nil
}

func (s *Service) Conversation(ctx context.Context, id string) (wire.ConvSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return wire.ConvSnapshot{}, err
	}
	return clone(c.snap), nil
}

func (s *Service) ConversationAfter(ctx context.Context, id, afterEntryID string) (wire.ConvSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return wire.ConvSnapshot{}, err
	}
	at := slices.IndexFunc(c.snap.Entries, func(e wire.Entry) bool { return e.ID == afterEntryID })
	if at < 0 {
		return wire.ConvSnapshot{}, convo.ErrUnknownCursor
	}
	snap := clone(c.snap)
	snap.Entries = snap.Entries[at+1:]
	touched := map[string]bool{}
	for _, e := range snap.Entries {
		if e.TurnID != nil {
			touched[*e.TurnID] = true
		}
	}
	snap.Turns = slices.DeleteFunc(snap.Turns, func(t wire.Turn) bool { return !touched[t.ID] })
	snap.BeforeTurnID = nil
	return snap, nil
}

// turns is every turn of c, oldest first, and every entry of them.
func (c *conv) turns() ([]wire.Turn, []wire.Entry) {
	return append(slices.Clone(c.older), c.snap.Turns...), append(slices.Clone(c.olderEntries), c.snap.Entries...)
}

func (s *Service) Turns(ctx context.Context, id, beforeTurnID string, limit int) (wire.TurnsPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return wire.TurnsPage{}, err
	}
	turns, entries := c.turns()
	end := len(turns)
	if beforeTurnID != "" {
		if end = slices.IndexFunc(turns, func(t wire.Turn) bool { return t.ID == beforeTurnID }); end < 0 {
			return wire.TurnsPage{}, convo.ErrNotFound
		}
	}
	start := max(0, end-limit)
	page := wire.TurnsPage{Turns: clone(turns[start:end]), Entries: []wire.Entry{}}
	if start > 0 {
		before := turns[start].ID
		page.BeforeTurnID = &before
	}
	answers := map[string]bool{}
	for _, t := range page.Turns {
		if t.AnswerEntryID != nil {
			answers[*t.AnswerEntryID] = true
		}
	}
	for _, e := range entries {
		if answers[e.ID] {
			page.Entries = append(page.Entries, clone(e))
		}
	}
	return page, nil
}

func (s *Service) TurnEntries(ctx context.Context, id, turnID string) (wire.EntriesPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return wire.EntriesPage{}, err
	}
	turns, entries := c.turns()
	if !slices.ContainsFunc(turns, func(t wire.Turn) bool { return t.ID == turnID }) {
		return wire.EntriesPage{}, convo.ErrNotFound
	}
	page := wire.EntriesPage{TurnID: turnID, Entries: []wire.Entry{}}
	for _, e := range entries {
		if e.TurnID != nil && *e.TurnID == turnID {
			page.Entries = append(page.Entries, clone(e))
		}
	}
	return page, nil
}

func (s *Service) EntryOutput(ctx context.Context, id, entryID string) (wire.OutputPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.get(id)
	if err != nil {
		return wire.OutputPage{}, err
	}
	_, entries := c.turns()
	at := slices.IndexFunc(entries, func(e wire.Entry) bool { return e.ID == entryID && e.T == wire.EntryTool && e.Output != nil })
	if at < 0 {
		return wire.OutputPage{}, convo.ErrNotFound
	}
	out := entries[at].Output
	text := out.Text
	if out.Truncated != nil {
		var b strings.Builder
		b.WriteString(text)
		for i := 1; int64(b.Len()) < out.Truncated.Total; i++ {
			fmt.Fprintf(&b, "\nстрока %d полного вывода", i)
		}
		text = b.String()[:out.Truncated.Total]
		text = strings.ToValidUTF8(text, "")
	}
	return wire.OutputPage{EntryID: entryID, Output: wire.ToolOutput{Text: text}}, nil
}

// --- events ------------------------------------------------------------------------------------------------------

func (s *Service) pub(ch wire.Channel, e wire.Event) int64 { return s.opt.Hub.Publish(ch, e) }

func (s *Service) pubConv(c *conv, e wire.Event) int64 { return s.pub(wire.ConvChannel(c.rec.ID), e) }

// upsert publishes c's whole record with the next rev.
func (s *Service) upsert(c *conv) int64 {
	c.rec.Rev++
	c.rec.Queue = wire.QueueSummary{Count: len(c.snap.Queue.Items), Paused: c.snap.Queue.Paused}
	return s.pub(wire.ChannelConvs, wire.ConversationUpsertEv{Conv: clone(c.rec)})
}

func (s *Service) setState(c *conv, st wire.AgentState) int64 {
	c.rec.State = st
	c.rec.Rev++
	if p := c.rec.PendingModel; p != nil && (st.K == wire.StateReady || st.K == wire.StateIdle) {
		// The agent is at its prompt: the model change that waited for it goes in, as the runtime does.
		to := wire.Model{ID: p.ID, Effort: p.Effort, Ultracode: p.Ultracode}
		c.rec.ModelLog = append(c.rec.ModelLog, wire.ModelSwitch{At: s.now(), From: c.rec.Model, To: to})
		c.rec.Model, c.rec.PendingModel = &to, nil
		return s.upsert(c)
	}
	return s.pub(wire.ChannelConvs, wire.ConversationStateEv{ID: c.rec.ID, State: st, Rev: c.rec.Rev})
}

func (s *Service) message(c *conv, clientID string) *wire.Message {
	for i := range c.snap.Messages {
		if c.snap.Messages[i].ClientID == clientID {
			return &c.snap.Messages[i]
		}
	}
	return nil
}

func (s *Service) setMsg(c *conv, m *wire.Message, st wire.MessageState) int64 {
	m.State = st
	m.Rev++
	return s.pubConv(c, wire.PromptStateEv{ClientID: m.ClientID, State: st, Rev: m.Rev})
}

func (s *Service) wait(slowMs, deadlineMs int64) *wire.Wait {
	now := s.now()
	return &wire.Wait{SlowAt: now + slowMs, DeadlineAt: now + deadlineMs}
}

// queueChanged publishes c's queue and the states of its messages that moved, and the record's count.
func (s *Service) queueChanged(c *conv) int64 {
	c.snap.Queue.Rev++
	seq := s.pubConv(c, wire.QueueChangedEv{Queue: clone(c.snap.Queue)})
	s.positions(c)
	s.upsert(c)
	return seq
}

// headReason is why the head of c's queue waits, "" when it can be typed now.
func headReason(c *conv) wire.QueueReason {
	if c.snap.Queue.Paused != nil {
		return wire.QueuedPaused
	}
	switch c.rec.State.K {
	case wire.StateWorking:
		return wire.QueuedAgentBusy
	case wire.StateWaitingPermission, wire.StateWaitingQuestion:
		return wire.QueuedBlockedMenu
	case wire.StateStarting:
		return wire.QueuedLaunching
	case wire.StateStopped:
		return wire.QueuedResuming
	case wire.StateGone, wire.StateLaunchFailed:
		return wire.QueuedPaused
	}
	return ""
}

// queuedAt is the state of the message at index i of c's queue.
func (s *Service) queuedAt(c *conv, i int) wire.MessageState {
	reason := wire.QueuedBehind
	if i == 0 {
		if reason = headReason(c); reason == "" {
			reason = wire.QueuedAgentBusy // about to be typed
		}
	}
	var w *wire.Wait
	if reason != wire.QueuedBehind && reason != wire.QueuedAgentBusy {
		w = s.wait(wire.WaitSlowMs, 15*60*1000)
	}
	return wire.MessageState{K: wire.MsgQueued, Position: i + 1, Reason: reason, Wait: w}
}

// positions gives every queued message its position and reason.
func (s *Service) positions(c *conv) {
	for i, id := range c.snap.Queue.Items {
		m := s.message(c, id)
		if m == nil {
			continue
		}
		st := s.queuedAt(c, i)
		if m.State.K == st.K && m.State.Position == st.Position && m.State.Reason == st.Reason {
			continue
		}
		s.setMsg(c, m, st)
	}
}

// later runs fn under s.mu after d, unless the fake was reset meanwhile.
func (s *Service) later(d time.Duration, fn func()) {
	gen := s.gen
	time.AfterFunc(d, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gen == gen {
			fn()
		}
	})
}

func (s *Service) newID() string {
	s.ids++
	return fmt.Sprintf("c:fa4e0000-0000-4000-8000-%012x#0", s.ids)
}

// --- the scripted agent --------------------------------------------------------------------------------------------

// pump types the head of c's queue when the agent can take it.
func (s *Service) pump(c *conv) {
	if c.typing != "" || len(c.snap.Queue.Items) == 0 || headReason(c) != "" {
		return
	}
	id := c.snap.Queue.Items[0]
	m := s.message(c, id)
	if m == nil {
		return
	}
	c.snap.Queue.Items = c.snap.Queue.Items[1:]
	s.typeMessage(c, m)
	s.queueChanged(c)
}

// typeMessage plays typing → in-input → accepted and opens a turn, or joins the live one (steer).
func (s *Service) typeMessage(c *conv, m *wire.Message) {
	c.typing = m.ClientID
	s.setMsg(c, m, wire.MessageState{K: wire.MsgTyping, Wait: s.wait(wire.WaitSlowMs, wire.TypingDeadline)})
	id := m.ClientID
	step := s.opt.Step
	s.later(step, func() {
		m := s.message(c, id)
		if m == nil {
			return
		}
		s.setMsg(c, m, wire.MessageState{K: wire.MsgInInput, Wait: s.wait(wire.WaitSlowMs, wire.ConfirmDeadline)})
		s.later(step, func() {
			m := s.message(c, id)
			if m == nil {
				return
			}
			if strings.Contains(m.Text, MarkStuck) {
				c.typing = ""
				s.setMsg(c, m, wire.MessageState{K: wire.MsgFailed, Code: wire.MsgFailedStuckInInput, Retryable: true, At: s.now()})
				m.Text = strings.ReplaceAll(m.Text, MarkStuck, "") // a retry gets through
				s.pump(c)
				return
			}
			s.accept(c, m)
		})
	})
}

// accept puts m's user entry in the feed: a new turn, or the live one when the agent works (steer).
func (s *Service) accept(c *conv, m *wire.Message) {
	c.typing = ""
	entryID := s.newID()
	s.setMsg(c, m, wire.MessageState{K: wire.MsgAccepted, EntryID: &entryID})
	clientID := m.ClientID
	c.rec.LastPromptAt = s.now()
	if live := c.live(); live != nil && c.rec.State.K == wire.StateWorking {
		tid := live.ID
		s.step(c, wire.Entry{ID: entryID, TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntryUser, Text: m.Text,
			Attachments: m.Attachments, ClientID: &clientID})
		s.setMsg(c, m, wire.MessageState{K: wire.MsgInTurn, TurnID: tid})
		return
	}
	tid := entryID
	turn := wire.Turn{ID: tid, Author: wire.TurnAuthor{Provider: c.rec.Provider}, StartedAt: s.now(), Status: wire.TurnLive,
		Files: []wire.TurnFile{}}
	c.snap.Turns = append(c.snap.Turns, turn)
	c.script = m.Text
	s.pubConv(c, wire.TurnStartedEv{Turn: clone(turn)})
	s.step(c, wire.Entry{ID: entryID, TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntryUser, Text: m.Text,
		Attachments: m.Attachments, ClientID: &clientID})
	s.setState(c, wire.AgentState{K: wire.StateWorking, Since: s.now(), TurnID: &tid})
	s.setMsg(c, m, wire.MessageState{K: wire.MsgInTurn, TurnID: tid})
	s.play(c, tid)
}

// live is c's live turn, nil when none.
func (c *conv) live() *wire.Turn {
	for i := len(c.snap.Turns) - 1; i >= 0; i-- {
		if c.snap.Turns[i].Status == wire.TurnLive {
			return &c.snap.Turns[i]
		}
	}
	return nil
}

// step appends an entry to the feed and to its turn's counters.
func (s *Service) step(c *conv, e wire.Entry) int64 {
	c.snap.Entries = append(c.snap.Entries, e)
	var counts *wire.TurnCounts
	if e.TurnID != nil {
		for i := range c.snap.Turns {
			if t := &c.snap.Turns[i]; t.ID == *e.TurnID {
				t.StepCount++
				if e.T == wire.EntryTool {
					t.ToolCount++
				}
				counts = &wire.TurnCounts{ToolCount: t.ToolCount, StepCount: t.StepCount}
			}
		}
	}
	c.rec.LastActivityAt = e.At
	return s.pubConv(c, wire.StepEv{Entry: e, Counts: counts})
}

// play is the scripted turn: a thought, a command, then what the opening text asks for, an answer by default.
func (s *Service) play(c *conv, tid string) {
	step := s.opt.Step
	alive := func() bool { t := c.live(); return t != nil && t.ID == tid && c.rec.State.K == wire.StateWorking }
	s.later(step, func() {
		if !alive() {
			return
		}
		s.step(c, wire.Entry{ID: s.newID(), TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntryThink, Text: "Смотрю, что нужно сделать."})
		thought, command := "Запускаю проверку", "go test ./..."
		now := wire.Now{TurnID: tid, Thought: &thought, Action: &wire.NowAction{Tool: "Bash", Summary: "go test", Command: &command}, At: s.now()}
		c.snap.Now = &now
		s.pubConv(c, wire.NowEv{Now: now})
		s.later(step, func() {
			if !alive() {
				return
			}
			ok := true
			s.step(c, wire.Entry{ID: s.newID(), TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntryTool, ToolUseID: fmt.Sprintf("toolu_fake%d", s.ids),
				Tool: "Bash", Summary: "go test ./...", OK: &ok, Output: &wire.ToolOutput{Text: "ok  \tgithub.com/acme/app\t0.4s"}})
			s.later(step, func() {
				if !alive() {
					return
				}
				switch {
				case strings.Contains(c.script, MarkWork):
				case strings.Contains(c.script, MarkQuestion):
					s.ask(c, wire.GateQuestion)
				case strings.Contains(c.script, MarkPermission):
					s.ask(c, wire.GatePermission)
				case strings.Contains(c.script, MarkPlan):
					s.ask(c, wire.GatePlan)
				case strings.Contains(c.script, MarkFail):
					detail := "429 rate_limit_error"
					retry := s.now() + 60_000
					s.finish(c, wire.TurnFailed, &wire.TurnFailure{Code: wire.TurnFailedRateLimit, Detail: &detail, RetryAt: &retry})
				default:
					s.answer(c, tid)
				}
			})
		})
	})
}

// answer ends the live turn with a reply.
func (s *Service) answer(c *conv, tid string) {
	s.step(c, wire.Entry{ID: s.newID(), TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntrySay, MD: "Готово: проверка прошла."})
	s.finish(c, wire.TurnDone, nil)
}

// finish ends the live turn: done, interrupted or failed.
func (s *Service) finish(c *conv, status wire.TurnStatus, failure *wire.TurnFailure) {
	t := c.live()
	if t == nil {
		return
	}
	now := s.now()
	dur := now - t.StartedAt
	t.EndedAt, t.DurationMs, t.Status, t.Failure = &now, &dur, status, failure
	for i := len(c.snap.Entries) - 1; i >= 0; i-- {
		if e := c.snap.Entries[i]; e.TurnID != nil && *e.TurnID == t.ID && e.T == wire.EntrySay && !strings.HasPrefix(e.ID, "live:") {
			id := e.ID
			t.AnswerEntryID = &id
			break
		}
	}
	c.snap.Entries = slices.DeleteFunc(c.snap.Entries, func(e wire.Entry) bool { return e.ID == wire.LiveEntryID(t.ID) })
	c.snap.Now = nil
	turn := clone(*t)
	if status == wire.TurnFailed {
		s.pubConv(c, wire.TurnFailedEv{Turn: turn})
	} else {
		s.pubConv(c, wire.TurnFinishedEv{Turn: turn})
	}
	s.closeGates(c, wire.ClosedTurnEnded)
	for i := range c.snap.Messages {
		if m := &c.snap.Messages[i]; m.State.K == wire.MsgInTurn && m.State.TurnID == t.ID {
			s.setMsg(c, m, wire.MessageState{K: wire.MsgDone, TurnID: t.ID})
		}
	}
	snippet := "Ход прерван"
	if t.AnswerEntryID != nil {
		for _, e := range c.snap.Entries {
			if e.ID == *t.AnswerEntryID {
				snippet = e.MD
			}
		}
	}
	if len([]rune(snippet)) > wire.SnippetMax {
		snippet = string([]rune(snippet)[:wire.SnippetMax])
	}
	c.rec.LastTurn = &wire.TurnSummary{ID: t.ID, Status: t.Status, StartedAt: t.StartedAt, EndedAt: t.EndedAt, DurationMs: t.DurationMs,
		ToolCount: t.ToolCount, Snippet: snippet}
	c.rec.LastActivityAt = now
	c.rec.Gate = nil
	st := wire.AgentState{K: wire.StateReady, Since: now}
	if status == wire.TurnInterrupted {
		st.K = wire.StateIdle
	}
	s.setState(c, st)
	s.upsert(c)
	s.positions(c)
	s.pump(c)
}

// closeGates closes c's open gates.
func (s *Service) closeGates(c *conv, reason wire.GateCloseReason) {
	closed := wire.GateState{K: wire.GateStateClosed, Reason: reason}
	for i := range c.snap.Questions {
		if q := &c.snap.Questions[i]; q.State.K == wire.GateStateOpen || q.State.K == wire.GateStateAnswering {
			q.State = closed
			s.pubConv(c, wire.GateUpdatedEv{ID: q.ID, State: closed})
		}
	}
	for i := range c.snap.Permissions {
		if p := &c.snap.Permissions[i]; p.State.K == wire.GateStateOpen || p.State.K == wire.GateStateAnswering {
			p.State = closed
			s.pubConv(c, wire.GateUpdatedEv{ID: p.ID, State: closed})
		}
	}
}

// ask opens a gate of kind in c's live turn (or outside a turn).
func (s *Service) ask(c *conv, kind wire.GateKind) {
	now := s.now()
	var tid *string
	if t := c.live(); t != nil {
		id := t.ID
		tid = &id
	}
	s.ids++
	switch kind {
	case wire.GateQuestion:
		q := clone(s.tmpl.question)
		q.ID, q.ConversationID, q.AskedAt, q.State = wire.QuestionID(fmt.Sprintf("toolu_fake%d", s.ids)), c.rec.ID, now, wire.GateState{K: wire.GateStateOpen}
		q.TurnID = c.rec.ID
		if tid != nil {
			q.TurnID = *tid
		} else if len(c.snap.Turns) > 0 {
			q.TurnID = c.snap.Turns[len(c.snap.Turns)-1].ID
		}
		c.snap.Questions = append(c.snap.Questions, q)
		s.pubConv(c, wire.QuestionAskedEv{Question: q})
		c.rec.Gate = &wire.GateRef{Kind: kind, ID: q.ID}
		s.setState(c, wire.AgentState{K: wire.StateWaitingQuestion, Since: now, QuestionID: q.ID})
	default:
		p := clone(s.tmpl.permission)
		if kind == wire.GatePlan {
			p = clone(s.tmpl.plan)
		}
		p.ID, p.ConversationID, p.TurnID, p.AskedAt, p.State = wire.PermissionID(fmt.Sprintf("toolu_fake%d", s.ids)), c.rec.ID, tid, now, wire.GateState{K: wire.GateStateOpen}
		c.snap.Permissions = append(c.snap.Permissions, p)
		if kind == wire.GatePlan {
			s.pubConv(c, wire.PlanProposedEv{Permission: p})
		} else {
			s.pubConv(c, wire.PermissionRequestedEv{Permission: p})
		}
		c.rec.Gate = &wire.GateRef{Kind: kind, ID: p.ID}
		s.setState(c, wire.AgentState{K: wire.StateWaitingPermission, Since: now, PermissionID: p.ID})
	}
	s.upsert(c)
	s.positions(c)
}

// gate is the state of c's question or permission id, nil when it has none: looked up each time, the slices grow.
func (c *conv) gate(id string) *wire.GateState {
	for i := range c.snap.Questions {
		if c.snap.Questions[i].ID == id {
			return &c.snap.Questions[i].State
		}
	}
	for i := range c.snap.Permissions {
		if c.snap.Permissions[i].ID == id {
			return &c.snap.Permissions[i].State
		}
	}
	return nil
}

// resolved: the gate is being answered; a step later it is answered, the agent works on and answers.
func (s *Service) resolved(c *conv, id, opID, device string, entry wire.Entry) int64 {
	*c.gate(id) = wire.GateState{K: wire.GateStateAnswering, OpID: opID, Wait: s.wait(wire.WaitSlowMs, wire.AnswerDeadline)}
	seq := s.pubConv(c, wire.GateUpdatedEv{ID: id, State: *c.gate(id)})
	s.later(s.opt.Step, func() {
		st := c.gate(id)
		if st == nil || st.K != wire.GateStateAnswering {
			return
		}
		*st = wire.GateState{K: wire.GateStateAnswered, At: s.now(), ByDevice: device}
		s.pubConv(c, wire.GateUpdatedEv{ID: id, State: *st})
		entry.At = s.now()
		var tid *string
		if t := c.live(); t != nil {
			id := t.ID
			tid = &id
		}
		entry.TurnID = tid
		s.step(c, entry)
		c.rec.Gate = nil
		s.setState(c, wire.AgentState{K: wire.StateWorking, Since: s.now(), TurnID: tid})
		s.upsert(c)
		s.later(s.opt.Step, func() {
			switch t := c.live(); {
			case t != nil && c.rec.State.K == wire.StateWorking:
				s.answer(c, t.ID)
			case t == nil && c.rec.State.K == wire.StateWorking:
				s.setState(c, wire.AgentState{K: wire.StateReady, Since: s.now()})
				s.pump(c)
			}
		})
	})
	return seq
}

// --- commands ----------------------------------------------------------------------------------------------------

// Do runs one command on the fake. A delay set through the control waits first, within ctx.
func (s *Service) Do(ctx context.Context, device string, cmd wire.Command) wire.CmdResult {
	s.mu.Lock()
	d := s.delays[cmd.Op()]
	delete(s.delays, cmd.Op())
	s.mu.Unlock()
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return wire.Refused(wire.RefuseTimeout)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch c := cmd.(type) {
	case wire.SendCmd:
		return s.send(c)
	case wire.CancelCmd:
		return s.cancel(c)
	case wire.RetryCmd:
		return s.retry(c)
	case wire.PromoteCmd:
		return s.promote(c)
	case wire.AnswerCmd:
		return s.answerCmd(c, device)
	case wire.DecideCmd:
		return s.decide(c, device)
	case wire.InterruptCmd:
		return s.interrupt(c)
	case wire.QueueMoveCmd:
		return s.queueMove(c)
	case wire.QueueResumeCmd:
		cv, err := s.get(c.ConversationID)
		if err != nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		seq := s.opt.Hub.Seq(wire.ConvChannel(cv.rec.ID))
		if cv.snap.Queue.Paused != nil {
			cv.snap.Queue.Paused = nil
			seq = s.queueChanged(cv)
			s.pump(cv)
		}
		return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
	case wire.SeenCmd:
		return s.seen(c)
	case wire.ConversationCreateCmd:
		return s.create(c)
	case wire.ConversationForkCmd:
		return s.fork(c)
	case wire.ConversationHandoffCmd:
		return s.handoffTo(c)
	case wire.ConversationMergeCmd:
		return s.merge(c)
	case wire.ConversationRenameCmd:
		return s.edit(c.ID, func(cv *conv) { cv.rec.Title, cv.rec.TitleSource = c.Title, wire.TitleFromUser })
	case wire.ConversationRetitleCmd:
		return s.edit(c.ID, func(cv *conv) { cv.rec.Title, cv.rec.TitleSource = "Новое название", wire.TitleFromAgent })
	case wire.ConversationPinCmd:
		return s.edit(c.ID, func(cv *conv) { cv.rec.Pinned = c.Pinned })
	case wire.ConversationMoveCmd:
		if c.ProjectID != nil && s.projects[*c.ProjectID].ID == "" {
			return wire.Refused(wire.RefuseNotFound)
		}
		return s.edit(c.ID, func(cv *conv) { cv.rec.ProjectID = c.ProjectID })
	case wire.ConversationDeleteCmd:
		cv, err := s.get(c.ID)
		if err != nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		if working(cv) && (c.Force == nil || !*c.Force) {
			return wire.Refused(wire.RefuseAgentWorking)
		}
		cv.deleted = true
		return wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.ConversationRemovedEv{ID: cv.rec.ID}))
	case wire.TerminalStopCmd:
		return s.terminalStop(c)
	case wire.ConversationRestoreCmd:
		cv := s.convs[c.ID]
		if cv == nil || !cv.deleted {
			return wire.Refused(wire.RefuseNotFound)
		}
		cv.deleted = false
		return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
	case wire.ConversationResumeCmd:
		if c.ID == "" {
			return s.resumeSession(c)
		}
		return s.resume(c)
	case wire.ModelCmd:
		cv, err := s.get(c.ID)
		if err != nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		if working(cv) || cv.rec.State.K == wire.StateStopped || cv.rec.State.K == wire.StateStarting {
			// The agent is away or at work: the change waits for its prompt (setState applies it).
			cv.rec.PendingModel = &wire.PendingModel{ID: c.Model, Effort: c.Effort, Ultracode: c.UltracodeAfter(cv.rec.Model), Since: s.now()}
			return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
		}
		from := cv.rec.Model
		cv.rec.Model, cv.rec.PendingModel = &wire.Model{ID: c.Model, Effort: c.Effort, Ultracode: c.UltracodeAfter(from)}, nil
		cv.rec.ModelLog = append(cv.rec.ModelLog, wire.ModelSwitch{At: s.now(), From: from, To: *cv.rec.Model})
		return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
	case wire.BgStopCmd:
		return s.bgStop(c)
	case wire.FavGroupCreateCmd:
		s.favN++
		id := fmt.Sprintf("fg-fake%03d", s.favN)
		res := s.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) {
			return f.WithGroup(wire.FavGroup{ID: id, Name: strings.TrimSpace(c.Name)}), true
		})
		if res.R != wire.ResultRefused {
			res.ID = &id
		}
		return res
	case wire.FavGroupRenameCmd:
		return s.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.Renamed(c.ID, strings.TrimSpace(c.Name)) })
	case wire.FavGroupDeleteCmd:
		return s.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.Without(c.ID) })
	case wire.FavGroupMoveCmd:
		return s.favEdit("", func(f wire.Favorites) (wire.Favorites, bool) { return f.GroupMoved(c.ID, c.BeforeID) })
	case wire.FavPlaceCmd:
		cv, err := s.get(c.ID)
		if err != nil || c.GroupID != nil && !s.favs.HasGroup(*c.GroupID) {
			return wire.Refused(wire.RefuseNotFound)
		}
		if !cv.rec.Pinned {
			s.edit(c.ID, func(cv *conv) { cv.rec.Pinned = true })
		}
		return s.favEdit(c.ID, func(f wire.Favorites) (wire.Favorites, bool) { return f.Placed(c.ID, c.GroupID, c.BeforeID) })
	case wire.GoalCmd:
		cv, err := s.get(c.ID)
		if err != nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		if cv.rec.Provider != wire.ProviderClaude {
			return wire.Refused(wire.RefuseUnsupported)
		}
		if text := strings.TrimSpace(c.Text); text == "" {
			cv.rec.Goal = nil
		} else {
			cv.rec.Goal = &wire.Goal{Text: text, Status: wire.GoalActive, Since: s.now()}
		}
		return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
	case wire.LimitCmd:
		cv, err := s.get(c.ID)
		if err != nil || cv.rec.Limited == nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		held := *cv.rec.Limited
		switch c.Action {
		case wire.LimitNow:
			cv.rec.Limited = nil
		case wire.LimitCancel:
			held.ContinueAt = nil
			cv.rec.Limited = &held
		case wire.LimitAuto:
			if held.ResetsAt != nil {
				at := *held.ResetsAt + wire.LimitJitterMax.Milliseconds()/2
				held.ContinueAt = &at
			}
			cv.rec.Limited = &held
		}
		return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
	case wire.ProjectCreateCmd:
		if c.Path == "" && s.defaultCwd != "" {
			p := wire.Project{ID: projectID(s.opt.Machine, "name:"+strings.TrimSpace(c.Name)), MachineID: s.opt.Machine,
				Path: s.defaultCwd, Name: strings.TrimSpace(c.Name), CreatedAt: s.now()}
			if _, ok := s.projects[p.ID]; !ok {
				s.addProject(p)
			}
			res := wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.ProjectUpsertEv{Project: p}))
			res.ID = &p.ID
			return res
		}
		if !filepath.IsAbs(c.Path) {
			return wire.Refused(wire.RefuseNotFound)
		}
		path := filepath.Clean(c.Path)
		p, ok := s.projects[projectID(s.opt.Machine, path)]
		if !ok {
			p = wire.Project{ID: projectID(s.opt.Machine, path), MachineID: s.opt.Machine, Path: path, Name: filepath.Base(path),
				CreatedAt: s.now()}
			s.addProject(p)
		}
		res := wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.ProjectUpsertEv{Project: p}))
		res.ID = &p.ID
		return res
	case wire.ProjectUpdateCmd:
		p, ok := s.projects[c.ID]
		if !ok || c.Path != "" && !filepath.IsAbs(c.Path) {
			return wire.Refused(wire.RefuseNotFound)
		}
		if c.Path != "" {
			p.Path = filepath.Clean(c.Path)
		}
		if name := strings.TrimSpace(c.Name); name != "" {
			p.Name = name
		}
		s.projects[p.ID] = p
		return wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.ProjectUpsertEv{Project: p}))
	case wire.ProjectDeleteCmd:
		if _, ok := s.projects[c.ID]; !ok {
			return wire.Refused(wire.RefuseNotFound)
		}
		for _, cv := range s.convs {
			if ptrStr(cv.rec.ProjectID) != c.ID {
				continue
			}
			cv.rec.ProjectID = nil
			if cv.deleted {
				continue
			}
			if c.Chats == wire.ProjectChatsDelete {
				cv.deleted = true
				s.pub(wire.ChannelConvs, wire.ConversationRemovedEv{ID: cv.rec.ID})
			} else {
				s.upsert(cv)
			}
		}
		delete(s.projects, c.ID)
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == c.ID })
		return wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.ProjectRemovedEv{ID: c.ID}))
	}
	return wire.Refused(wire.RefuseUnsupported)
}

func working(c *conv) bool {
	switch c.rec.State.K {
	case wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		return true
	}
	return false
}

// projectID is base32(sha1(machineId + "\x00" + path))[:12], lower case.
func (s *Service) addProject(p wire.Project) {
	s.projects[p.ID] = p
	s.order = append(s.order, p.ID)
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func projectID(machine, path string) string {
	sum := sha1.Sum([]byte(machine + "\x00" + path))
	return strings.ToLower(base32.StdEncoding.EncodeToString(sum[:]))[:12]
}

// favEdit is what the runtime does to the layout: kept down to the pinned chats (and `extra`, just pinned), changed,
// published whole.
func (s *Service) favEdit(extra string, edit func(wire.Favorites) (wire.Favorites, bool)) wire.CmdResult {
	var pinned []wire.Conversation
	for _, cv := range s.convs {
		if !cv.deleted && cv.rec.Pinned && cv.rec.ID != extra {
			pinned = append(pinned, cv.rec)
		}
	}
	sort.Slice(pinned, func(i, j int) bool {
		if pinned[i].LastActivityAt != pinned[j].LastActivityAt {
			return pinned[i].LastActivityAt > pinned[j].LastActivityAt
		}
		return pinned[i].ID < pinned[j].ID
	})
	ids := []string{}
	if extra != "" {
		ids = append(ids, extra)
	}
	for _, c := range pinned {
		ids = append(ids, c.ID)
	}
	next, ok := edit(s.favs.Normalized(ids))
	if !ok {
		return wire.Refused(wire.RefuseNotFound)
	}
	s.favs = next.Normalized(ids)
	return wire.Accepted(wire.ChannelConvs, s.pub(wire.ChannelConvs, wire.FavoritesUpsertEv{Favorites: s.favs.Clone()}))
}

func (s *Service) edit(id string, fn func(*conv)) wire.CmdResult {
	cv, err := s.get(id)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	fn(cv)
	res := wire.Accepted(wire.ChannelConvs, s.upsert(cv))
	rev := cv.rec.Rev
	res.Rev = &rev
	return res
}

func (s *Service) send(c wire.SendCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	switch {
	case err != nil:
		return wire.Refused(wire.RefuseNotFound)
	case len(utf16.Encode([]rune(c.Text))) > s.opt.Limits.MaxText:
		return wire.Refused(wire.RefuseTextTooLong)
	case len(c.AttachmentIDs) > s.opt.Limits.MaxAttachments:
		return wire.Refused(wire.RefuseTooManyAttachments)
	case cv.rec.State.K == wire.StateGone:
		return wire.Refused(wire.RefuseAgentGone)
	case len(cv.snap.Queue.Items) >= s.opt.Limits.MaxQueue:
		return wire.Refused(wire.RefuseQueueFull)
	}
	if m := s.message(cv, c.ClientID); m != nil {
		return wire.Refused(wire.RefuseConflict)
	}
	atts := []wire.AttachmentRef{}
	for _, id := range c.AttachmentIDs {
		ref, _, ok := s.opt.Uploads.Get(id)
		if !ok {
			return wire.Refused(wire.RefuseAttachmentMissing)
		}
		atts = append(atts, ref)
	}
	text := c.Text
	if len(c.Refs) > 0 {
		// The fake names a design by its id only: it has no library.
		items := make([]attach.Item, 0, len(c.Refs))
		for _, r := range c.Refs {
			items = append(items, attach.Item{Ref: r, Line: attach.LineOf(r)})
		}
		text = attach.With(text, attach.Block(items))
	}
	m := wire.Message{ClientID: c.ClientID, ConversationID: cv.rec.ID, Text: text, Attachments: atts, Refs: slices.Clone(c.Refs), Mode: c.Mode, Kind: c.Kind,
		CreatedAt: s.now(), State: wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedBehind}, Rev: 1}
	cv.snap.Messages = append(cv.snap.Messages, m)
	if c.Mode == wire.ModeSteer && cv.rec.State.K == wire.StateWorking && cv.typing == "" {
		seq := s.pubConv(cv, wire.MessageUpsertEv{Message: clone(m)})
		s.typeMessage(cv, s.message(cv, c.ClientID))
		return wire.Queued(wire.ConvChannel(cv.rec.ID), seq)
	}
	at := len(cv.snap.Queue.Items)
	if c.Mode != wire.ModeQueue {
		at = 0 // now and steer go to the head
	}
	cv.snap.Queue.Items = slices.Insert(cv.snap.Queue.Items, at, c.ClientID)
	s.message(cv, c.ClientID).State = s.queuedAt(cv, at)
	seq := s.pubConv(cv, wire.MessageUpsertEv{Message: clone(*s.message(cv, c.ClientID))})
	s.queueChanged(cv)
	s.pump(cv)
	return wire.Queued(wire.ConvChannel(cv.rec.ID), seq)
}

func (s *Service) cancel(c wire.CancelCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	m := s.message(cv, c.ClientID)
	if m == nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	if m.State.K != wire.MsgQueued {
		return wire.Refused(wire.RefuseTooLate)
	}
	cv.snap.Queue.Items = slices.DeleteFunc(cv.snap.Queue.Items, func(id string) bool { return id == c.ClientID })
	seq := s.setMsg(cv, m, wire.MessageState{K: wire.MsgCancelled, By: wire.CancelledByUser})
	s.queueChanged(cv)
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
}

func (s *Service) retry(c wire.RetryCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	m := s.message(cv, c.ClientID)
	if m == nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	if m.State.K != wire.MsgFailed {
		return wire.Refused(wire.RefuseNotFailed)
	}
	m.Attempts++
	cv.snap.Queue.Items = slices.Insert(cv.snap.Queue.Items, 0, c.ClientID)
	seq := s.setMsg(cv, m, s.queuedAt(cv, 0))
	s.queueChanged(cv)
	s.pump(cv)
	return wire.Queued(wire.ConvChannel(cv.rec.ID), seq)
}

func (s *Service) promote(c wire.PromoteCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	m := s.message(cv, c.ClientID)
	switch {
	case m == nil:
		return wire.Refused(wire.RefuseNotFound)
	case m.State.K != wire.MsgQueued:
		return wire.Refused(wire.RefuseTooLate)
	case c.To == wire.PromoteToSteer && cv.rec.State.K != wire.StateWorking:
		return wire.Refused(wire.RefuseNotWorking)
	}
	m.Mode = wire.MessageMode(c.To)
	cv.snap.Queue.Items = slices.DeleteFunc(cv.snap.Queue.Items, func(id string) bool { return id == c.ClientID })
	if c.To == wire.PromoteToSteer && cv.typing == "" {
		seq := s.queueChanged(cv)
		s.typeMessage(cv, m)
		return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
	}
	cv.snap.Queue.Items = slices.Insert(cv.snap.Queue.Items, 0, c.ClientID)
	seq := s.queueChanged(cv)
	s.pump(cv)
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
}

// queueMove is queue.move by the runtime's rules: only a queued message moves, and not across a message of another
// urgency (steered, then «now», then the rest) or before one that has left the line.
func (s *Service) queueMove(c wire.QueueMoveCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	m := s.message(cv, c.ClientID)
	if m == nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	items := cv.snap.Queue.Items
	from := slices.Index(items, c.ClientID)
	if m.State.K != wire.MsgQueued || from < 0 {
		return wire.Refused(wire.RefuseTooLate)
	}
	rest := slices.Delete(slices.Clone(items), from, from+1)
	at := len(rest)
	if c.BeforeClientID != nil {
		if at = slices.Index(rest, *c.BeforeClientID); at < 0 {
			return wire.Refused(wire.RefuseConflict)
		}
	}
	rank := func(id string) int {
		switch o := s.message(cv, id); {
		case o == nil:
			return 2
		case o.Mode == wire.ModeSteer:
			return 0
		case o.Mode == wire.ModeNow:
			return 1
		}
		return 2
	}
	if (at > 0 && rank(rest[at-1]) > rank(c.ClientID)) || (at < len(rest) && rank(rest[at]) < rank(c.ClientID)) {
		return wire.Refused(wire.RefuseConflict)
	}
	if at == from {
		return wire.Accepted(wire.ConvChannel(cv.rec.ID), s.opt.Hub.Seq(wire.ConvChannel(cv.rec.ID)))
	}
	cv.snap.Queue.Items = slices.Insert(rest, at, c.ClientID)
	seq := s.queueChanged(cv)
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
}

func (s *Service) answerCmd(c wire.AnswerCmd, device string) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	i := slices.IndexFunc(cv.snap.Questions, func(q wire.Question) bool { return q.ID == c.QuestionID })
	if i < 0 {
		return wire.Refused(wire.RefuseNotFound)
	}
	q := &cv.snap.Questions[i]
	if code := gateRefusal(q.State); code != "" {
		return wire.Refused(code)
	}
	if c.ChatAbout {
		q.State = wire.GateState{K: wire.GateStateClosed, Reason: wire.ClosedChatAbout}
		seq := s.pubConv(cv, wire.GateUpdatedEv{ID: q.ID, State: q.State})
		cv.rec.Gate = nil
		s.setState(cv, wire.AgentState{K: wire.StateReady, Since: s.now()})
		s.upsert(cv)
		s.pump(cv)
		return wire.Accepted(wire.ConvChannel(cv.rec.ID), seq)
	}
	var pairs []wire.AnswerPair
	for _, a := range c.Answers {
		if a.I < 0 || a.I >= len(q.Items) {
			return wire.Refused(wire.RefuseOptionUnknown)
		}
		item := q.Items[a.I]
		var chosen []string
		for _, ch := range a.Choices {
			if ch < 0 || ch >= len(item.Options) {
				return wire.Refused(wire.RefuseOptionUnknown)
			}
			chosen = append(chosen, item.Options[ch].Label)
		}
		if a.Text != nil {
			chosen = append(chosen, *a.Text)
		}
		pairs = append(pairs, wire.AnswerPair{Q: item.Question, A: strings.Join(chosen, ", ")})
	}
	entry := wire.Entry{ID: s.newID(), Rev: 1, T: wire.EntryAnswers, QuestionID: q.ID, Pairs: pairs}
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), s.resolved(cv, q.ID, c.OpID, device, entry))
}

func gateRefusal(st wire.GateState) wire.RefuseCode {
	switch st.K {
	case wire.GateStateClosed:
		return wire.RefuseGateClosed
	case wire.GateStateAnswered, wire.GateStateAnswering:
		return wire.RefuseGateAnswered
	}
	return ""
}

func (s *Service) decide(c wire.DecideCmd, device string) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	i := slices.IndexFunc(cv.snap.Permissions, func(p wire.Permission) bool { return p.ID == c.PermissionID })
	if i < 0 {
		return wire.Refused(wire.RefuseNotFound)
	}
	p := &cv.snap.Permissions[i]
	if code := gateRefusal(p.State); code != "" {
		return wire.Refused(code)
	}
	if !slices.ContainsFunc(p.Options, func(o wire.PermissionOption) bool { return o.ID == c.OptionID }) {
		return wire.Refused(wire.RefuseOptionUnknown)
	}
	entry := wire.Entry{ID: s.newID(), Rev: 1, T: wire.EntryGate, PermissionID: p.ID}
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), s.resolved(cv, p.ID, c.OpID, device, entry))
}

func (s *Service) interrupt(c wire.InterruptCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	if !working(cv) {
		return wire.Refused(wire.RefuseNotWorking)
	}
	ch := wire.ConvChannel(cv.rec.ID)
	var seq int64
	if t := cv.live(); t != nil {
		tid := t.ID
		seq = s.step(cv, wire.Entry{ID: s.newID(), TurnID: &tid, At: s.now(), Rev: 1, T: wire.EntryInterrupt})
	}
	if c.Pauses() {
		cv.snap.Queue.Paused = &wire.QueuePause{Reason: wire.PausedInterrupted, At: s.now()}
		s.queueChanged(cv)
	}
	if cv.live() != nil {
		s.finish(cv, wire.TurnInterrupted, nil)
	} else {
		s.closeGates(cv, wire.ClosedAgentMovedOn)
		cv.rec.Gate = nil
		seq = s.setState(cv, wire.AgentState{K: wire.StateIdle, Since: s.now()})
		s.upsert(cv)
		return wire.Accepted(wire.ChannelConvs, seq)
	}
	return wire.Accepted(ch, seq)
}

func (s *Service) seen(c wire.SeenCmd) wire.CmdResult {
	seq := s.opt.Hub.Seq(wire.ChannelConvs)
	var targets []*conv
	if c.ConversationID == wire.SeenAll {
		for _, cv := range s.convs {
			if !cv.deleted {
				targets = append(targets, cv)
			}
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].rec.ID < targets[j].rec.ID })
	} else {
		cv, err := s.get(c.ConversationID)
		if err != nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		targets = []*conv{cv}
	}
	for _, cv := range targets {
		if c.UpTo > cv.rec.SeenAt {
			cv.rec.SeenAt = c.UpTo
			cv.rec.Rev++
			seq = s.pub(wire.ChannelConvs, wire.ConversationSeenEv{ID: cv.rec.ID, SeenAt: cv.rec.SeenAt, Rev: cv.rec.Rev})
		}
	}
	return wire.Accepted(wire.ChannelConvs, seq)
}

func (s *Service) create(c wire.ConversationCreateCmd) wire.CmdResult {
	if c.Provider == wire.ProviderCodex {
		return wire.Refused(wire.RefuseUnsupported) // the fake machine has no Codex
	}
	cwd, pid := c.Cwd, (*string)(nil)
	if c.ProjectID != "" {
		if cwd = s.projects[c.ProjectID].Path; cwd == "" {
			return wire.Refused(wire.RefuseNotFound)
		}
		id := c.ProjectID
		pid = &id
	}
	if c.First != nil {
		for _, id := range c.First.AttachmentIDs {
			if _, _, ok := s.opt.Uploads.Get(id); !ok {
				return wire.Refused(wire.RefuseAttachmentMissing)
			}
		}
	}
	var parent *string
	if c.ParentID != "" {
		if s.convs[c.ParentID] == nil {
			return wire.Refused(wire.RefuseNotFound)
		}
		parent = &c.ParentID
	}
	now := s.now()
	id := newConvID()
	cv := &conv{rec: wire.Conversation{ID: id, ParentID: parent, MachineID: s.opt.Machine, ProjectID: pid, Cwd: cwd, Provider: c.Provider, Model: c.Model, Launch: c.Launch,
		TitleSource: wire.TitleFromFirstMessage, CreatedAt: now, LastActivityAt: now, LastPromptAt: now, SeenAt: now,
		State: wire.AgentState{K: wire.StateStarting, Since: now, Step: wire.StepMachine, Wait: s.wait(wire.LaunchSlowMs, wire.LaunchDeadline)}},
		snap: wire.ConvSnapshot{Turns: []wire.Turn{}, Entries: []wire.Entry{}, Messages: []wire.Message{}, Questions: []wire.Question{},
			Permissions: []wire.Permission{}, Background: []wire.BackgroundTask{},
			Queue: wire.Queue{ConversationID: id, Items: []string{}, Rev: 1}}}
	fail := false
	if c.First != nil {
		cv.rec.Title = strings.TrimSpace(strings.ReplaceAll(c.First.Text, MarkLaunchFail, ""))
		if r := []rune(cv.rec.Title); len(r) > 60 {
			cv.rec.Title = string(r[:60])
		}
		fail = strings.Contains(c.First.Text, MarkLaunchFail)
	}
	s.convs[id] = cv
	seq := s.upsert(cv)
	if c.First != nil {
		atts := []wire.AttachmentRef{}
		for _, aid := range c.First.AttachmentIDs {
			ref, _, _ := s.opt.Uploads.Get(aid)
			atts = append(atts, ref)
		}
		m := wire.Message{ClientID: c.First.ClientID, ConversationID: id, Text: c.First.Text, Attachments: atts, Mode: wire.ModeQueue,
			Kind: wire.KindPrompt, CreatedAt: now, State: wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedLaunching,
				Wait: s.wait(wire.WaitSlowMs, 15*60*1000)}, Rev: 1}
		cv.snap.Messages = append(cv.snap.Messages, m)
		cv.snap.Queue.Items = append(cv.snap.Queue.Items, m.ClientID)
		s.pubConv(cv, wire.MessageUpsertEv{Message: clone(m)})
		s.queueChanged(cv)
	}
	s.launch(cv, []wire.LaunchStep{wire.StepCli, wire.StepProject, wire.StepPrompt}, fail)
	res := wire.Accepted(wire.ChannelConvs, seq)
	res.ID = &id
	return res
}

// launch walks the launch steps, then the agent is idle (or the launch failed) and the queue moves.
func (s *Service) launch(cv *conv, steps []wire.LaunchStep, fail bool) {
	s.later(s.opt.Step, func() {
		if cv.rec.State.K != wire.StateStarting {
			return
		}
		if len(steps) > 0 {
			st := cv.rec.State
			st.Step = steps[0]
			s.setState(cv, st)
			s.launch(cv, steps[1:], fail)
			return
		}
		if fail {
			s.setState(cv, wire.AgentState{K: wire.StateLaunchFailed, Since: s.now(), Reason: string(wire.LaunchFailedNoPromptBox)})
			cv.snap.Queue.Paused = &wire.QueuePause{Reason: wire.PausedLaunchFailed, At: s.now()}
			s.queueChanged(cv)
			return
		}
		s.setState(cv, wire.AgentState{K: wire.StateIdle, Since: s.now()})
		s.positions(cv)
		s.pump(cv)
	})
}

// fakeSessions are the sessions the fake machine's agent kept outside any conversation.
var fakeSessions = []wire.AgentSession{
	{Provider: wire.ProviderClaude, SessionID: "d4618428-ad72-4227-8e97-f77e8c0f8c17", Title: "Починить гонку в очереди",
		Cwd: "/home/coder/workspace/harnessly", Turns: 14},
	{Provider: wire.ProviderClaude, SessionID: "5c0a3f1e-8b2d-4e6a-9f47-2d1b7c8e9a03", Title: "Разобрать падение сборки",
		Cwd: "/home/coder/workspace/acme", Turns: 3},
}

// Sessions is what GET /api/v2/sessions answers: the scripted sessions no conversation was opened on, the newest first.
func (s *Service) Sessions() []wire.AgentSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []wire.AgentSession{}
	for i, f := range fakeSessions {
		if _, taken := s.opened[f.SessionID]; !taken {
			f.UpdatedAt = wire.Ms(s.now()) - wire.Ms(i+1)*3600_000
			out = append(out, f)
		}
	}
	return out
}

// resumeSession opens a conversation on a scripted session.
func (s *Service) resumeSession(c wire.ConversationResumeCmd) wire.CmdResult {
	var found *wire.AgentSession
	for i := range fakeSessions {
		if f := &fakeSessions[i]; f.Provider == c.Provider && f.SessionID == c.SessionID {
			found = f
		}
	}
	switch {
	case found == nil:
		return wire.Refused(wire.RefuseNotFound)
	case found.Cwd != c.Cwd:
		return wire.Refused(wire.RefuseBadRequest)
	}
	if held, ok := s.opened[found.SessionID]; ok {
		cv := s.convs[held]
		res := wire.Accepted(wire.ChannelConvs, s.upsert(cv))
		res.ID = &held
		return res
	}
	res := s.create(wire.ConversationCreateCmd{OpID: c.OpID, Provider: c.Provider, Cwd: c.Cwd})
	if res.R != wire.ResultAccepted {
		return res
	}
	cv := s.convs[*res.ID]
	cv.rec.Title = found.Title
	if s.opened == nil {
		s.opened = map[string]string{}
	}
	s.opened[found.SessionID] = cv.rec.ID
	res.Seq = s.upsert(cv)
	return res
}

func (s *Service) resume(c wire.ConversationResumeCmd) wire.CmdResult {
	cv, err := s.get(c.ID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	switch {
	case cv.rec.State.K == wire.StateGone, cv.rec.State.K == wire.StateStopped && !cv.rec.State.Resumable:
		return wire.Refused(wire.RefuseAgentGone)
	case cv.rec.State.K != wire.StateStopped && cv.rec.State.K != wire.StateLaunchFailed:
		return wire.Accepted(wire.ChannelConvs, s.opt.Hub.Seq(wire.ChannelConvs))
	}
	cv.rec.TerminalStop = nil
	seq := s.setState(cv, wire.AgentState{K: wire.StateStarting, Since: s.now(), Step: wire.StepPrompt, Wait: s.wait(wire.LaunchSlowMs, wire.LaunchDeadline)})
	if p := cv.snap.Queue.Paused; p != nil && (p.Reason == wire.PausedLaunchFailed || p.Reason == wire.PausedAgentGone) {
		cv.snap.Queue.Paused = nil
		s.queueChanged(cv)
	}
	s.launch(cv, nil, false)
	return wire.Accepted(wire.ChannelConvs, seq)
}

// terminalStop is terminal.stop by the runtime's rules: a running agent's terminal ends (a working one only with
// force), the conversation stays stopped and resumable, and its record says who stopped it.
func (s *Service) terminalStop(c wire.TerminalStopCmd) wire.CmdResult {
	cv, err := s.get(c.ID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	switch cv.rec.State.K {
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed, wire.StateStarting:
		return wire.Refused(wire.RefuseAgentGone)
	}
	if working(cv) && (c.Force == nil || !*c.Force) {
		return wire.Refused(wire.RefuseAgentWorking)
	}
	now := s.now()
	cv.rec.State = wire.AgentState{K: wire.StateStopped, Since: now, Resumable: true}
	cv.rec.TerminalStop = &wire.TerminalStop{At: now, By: wire.TerminalStopPerson}
	return wire.Accepted(wire.ChannelConvs, s.upsert(cv))
}

func (s *Service) bgStop(c wire.BgStopCmd) wire.CmdResult {
	cv, err := s.get(c.ConversationID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	i := slices.IndexFunc(cv.snap.Background, func(t wire.BackgroundTask) bool { return t.ID == c.TaskID })
	switch {
	case i < 0:
		return wire.Refused(wire.RefuseNotFound)
	case cv.snap.Background[i].Status != wire.BgStatusRunning:
		return wire.Refused(wire.RefuseTooLate)
	case !cv.snap.Background[i].Stoppable:
		return wire.Refused(wire.RefuseUnsupported)
	}
	t := &cv.snap.Background[i]
	now := s.now()
	t.Status, t.EndedAt = wire.BgStatusStopped, &now
	return wire.Accepted(wire.ConvChannel(cv.rec.ID), s.pubConv(cv, wire.BackgroundUpsertEv{Task: clone(*t)}))
}

func newConvID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "tab-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
