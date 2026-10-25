// Package delegation lets an agent hand work to another agent the person sees: a new conversation of the machine — any
// provider, any model — nested under the one that delegated it. The delegating agent reads it, writes to it and waits
// for it through the runtime's MCP server (tools.go); the conversation is an ordinary chat the person can open, watch
// and take over at any moment.
//
// Who calls is never taken from the call: the runtime finds the conversation whose agent holds the connection
// (cmd/runtime). What an agent may touch is the chats it delegated and theirs, unless the person let agents write to
// any chat. A delegated chat may delegate in turn, MaxDepth levels down, and each conversation at most the person's
// number of chats at a time.
package delegation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// MaxDepth is how deep delegation goes: a chat the person started (0) delegates (1), which delegates (2), no more.
	MaxDepth = 2
	// MaxWakes is how many times a delegated chat wakes its parent: a pair of agents that keep handing each other
	// work stops waking at last, and the person sees why.
	MaxWakes = 20
	// Device is the device the delegated chats' commands come from.
	Device = "agent"
	// PromptMax is the longest task or message, in characters.
	PromptMax = 100_000
)

// Conversations is what the service needs of the machine's conversations (convo.Runtime).
type Conversations interface {
	Record(id string) (wire.Conversation, bool)
	Conversations(ctx context.Context) (wire.ConvsSnapshot, error)
	Conversation(ctx context.Context, id string) (wire.ConvSnapshot, error)
	ConversationAfter(ctx context.Context, id, afterEntryID string) (wire.ConvSnapshot, error)
	Do(ctx context.Context, device string, cmd wire.Command) wire.CmdResult
	DeliverPeer(ctx context.Context, convID, clientID, text string, steer bool) wire.CmdResult
}

// Service is the delegation of one runtime.
type Service struct {
	Convs Conversations
	// Settings are the person's limits, read at every call.
	Settings func() wire.DelegationSettings
	// Models are the models an agent CLI takes here; nil trusts any id.
	Models func(p wire.Provider) []wire.AgentModel
	// ProfileModels are the ids of the models of a backend profile (Kimi, GLM, Grok); nil, or none for an unknown profile,
	// leaves a delegated chat on the CLI's own account.
	ProfileModels func(profile string) []string
	// Self is this machine and its owner, as the envelopes of the messages between the agents name them.
	Self peers.Sender
	Book *Book
	Now  func() time.Time
	// Poll is how often a wait looks at the chat; 0 is half a second.
	Poll time.Duration
	// Log hears the wake-ups that could not go; nil hears nothing.
	Log func(msg string, kv ...any)
}

// Refusal is a call the service did not do, in words for the agent.
type Refusal struct {
	Code string
	Msg  string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Msg }

func refuse(code, format string, a ...any) error {
	return &Refusal{Code: code, Msg: fmt.Sprintf(format, a...)}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) settings() wire.DelegationSettings {
	if s.Settings == nil {
		return wire.DefaultDelegation
	}
	return s.Settings()
}

func (s *Service) caller(id string) (wire.Conversation, error) {
	if id == "" {
		return wire.Conversation{}, refuse("unknown-caller", "this agent does not run in a chat of the machine, so it cannot delegate")
	}
	rec, ok := s.Convs.Record(id)
	if !ok {
		return wire.Conversation{}, refuse("unknown-caller", "the chat of this agent is not known to the machine")
	}
	return rec, nil
}

// depth is how many parents the chat has (0 for one the person started); a broken chain ends where the parent is gone.
func (s *Service) depth(rec wire.Conversation) int {
	d := 0
	for rec.ParentID != nil && d <= MaxDepth {
		parent, ok := s.Convs.Record(*rec.ParentID)
		if !ok {
			break
		}
		rec, d = parent, d+1
	}
	return d
}

// descends tells whether id is a chat the caller delegated, or one of theirs.
func (s *Service) descends(id, caller string) bool {
	for range MaxDepth + 1 {
		rec, ok := s.Convs.Record(id)
		if !ok || rec.ParentID == nil {
			return false
		}
		if *rec.ParentID == caller {
			return true
		}
		id = *rec.ParentID
	}
	return false
}

// target is a chat the caller may read, write to and wait for.
func (s *Service) target(caller wire.Conversation, id string) (wire.Conversation, error) {
	rec, ok := s.Convs.Record(id)
	if !ok {
		return wire.Conversation{}, refuse("not-found", "no chat %s on this machine", id)
	}
	if id == caller.ID {
		return wire.Conversation{}, refuse("not-allowed", "that is this agent's own chat")
	}
	if !s.descends(id, caller.ID) && !s.settings().AnyChat {
		return wire.Conversation{}, refuse("not-allowed", "chat %s was not delegated by this agent; the person has not let agents write to other chats", id)
	}
	return rec, nil
}

// children are the chats the caller delegated that are still there, oldest first.
func (s *Service) children(ctx context.Context, caller string) ([]wire.Conversation, error) {
	snap, err := s.Convs.Conversations(ctx)
	if err != nil {
		return nil, err
	}
	var out []wire.Conversation
	for _, c := range snap.Conversations {
		if c.ParentID != nil && *c.ParentID == caller {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b wire.Conversation) int { return int(a.CreatedAt - b.CreatedAt) })
	return out, nil
}

// Task is a piece of work to delegate.
type Task struct {
	Provider wire.Provider
	// Model is a model id of the provider; "" is the machine's default. Effort is "" for the model's own.
	Model  string
	Effort wire.Effort
	Prompt string
	// Cwd is the folder the agent starts in; "" is the caller's.
	Cwd   string
	Title string
	// Notify wakes the caller with the result when the chat finishes a turn.
	Notify bool
}

// Delegate starts a chat with the task, under the caller.
func (s *Service) Delegate(ctx context.Context, callerID string, t Task) (wire.Conversation, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return wire.Conversation{}, err
	}
	if !slices.Contains(wire.Providers, t.Provider) {
		return wire.Conversation{}, refuse("bad-request", "provider is claude or codex")
	}
	prompt := strings.TrimSpace(t.Prompt)
	if prompt == "" || len([]rune(prompt)) > PromptMax {
		return wire.Conversation{}, refuse("bad-request", "prompt: 1 to %d characters", PromptMax)
	}
	if s.depth(caller)+1 > MaxDepth {
		return wire.Conversation{}, refuse("too-deep", "this chat was itself delegated %d levels down; a delegated chat cannot delegate further", MaxDepth)
	}
	kids, err := s.children(ctx, caller.ID)
	if err != nil {
		return wire.Conversation{}, refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	if limit := s.settings().MaxChildren; len(kids) >= limit {
		return wire.Conversation{}, refuse("too-many", "this chat already has %d delegated chats (the person's limit is %d); finish with one and delete it, or ask the person", len(kids), limit)
	}
	model, profile, err := s.model(t, caller)
	if err != nil {
		return wire.Conversation{}, err
	}
	key := peers.NewID()
	cmd := wire.ConversationCreateCmd{OpID: "delegate-" + key, Provider: t.Provider, Model: model, Profile: profile, ParentID: caller.ID,
		Title: strings.TrimSpace(t.Title), First: &wire.FirstMessage{ClientID: "delegate-" + key, Text: prompt, AttachmentIDs: []string{}}}
	if title := []rune(cmd.Title); len(title) > wire.RefTitleMax {
		cmd.Title = string(title[:wire.RefTitleMax])
	}
	cwd := strings.TrimSpace(t.Cwd)
	if cwd != "" && cwd != caller.Cwd {
		if cwd, err = s.folder(ctx, caller, cwd); err != nil {
			return wire.Conversation{}, err
		}
	}
	switch {
	case cwd != "" && cwd != caller.Cwd:
		cmd.Cwd = cwd
	case caller.ProjectID != nil:
		cmd.ProjectID = *caller.ProjectID
	default:
		cmd.Cwd = caller.Cwd
	}
	asked := s.now().UnixMilli()
	res := s.Convs.Do(ctx, Device, cmd)
	if res.R != wire.ResultAccepted || res.ID == nil {
		return wire.Conversation{}, refuse(string(res.Code), "the chat was not started")
	}
	id := *res.ID
	s.Book.Update(id, func(p *Page) { p.Notify, p.AskedAt = t.Notify, asked })
	rec, ok := s.Convs.Record(id)
	if !ok {
		return wire.Conversation{}, refuse("not-found", "the chat went right after it started")
	}
	return rec, nil
}

// folder is the folder a task names for its chat, as it is on disk: an absolute path to a directory inside the
// caller's folder or project, or inside a project the machine knows. Anywhere else only when the person let agents
// write to any chat.
func (s *Service) folder(ctx context.Context, caller wire.Conversation, cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", refuse("bad-cwd", "cwd must be an absolute path")
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return "", refuse("bad-cwd", "cwd %s does not exist", cwd)
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return "", refuse("bad-cwd", "cwd %s is not a folder", cwd)
	}
	if s.settings().AnyChat {
		return real, nil
	}
	roots := []string{caller.Cwd}
	snap, err := s.Convs.Conversations(ctx)
	if err != nil {
		return "", refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	for _, p := range snap.Projects {
		roots = append(roots, p.Path)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(root); err == nil && inside(r, real) {
			return real, nil
		}
	}
	return "", refuse("bad-cwd", "cwd %s is outside this chat's folder and the machine's projects; the person can allow it in the settings", cwd)
}

// inside tells whether path is root or below it; both are clean absolute paths.
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// model is the task's model as the wire has it: nil for the default with the default effort. A chat that runs on a
// backend profile delegates to the same profile when the task is for the same CLI and names no model, or one of the
// profile's: the profile is the second result. A task that names a model of the CLI's own account gets a chat on it.
func (s *Service) model(t Task, caller wire.Conversation) (*wire.Model, string, error) {
	if t.Effort != "" && !slices.Contains(wire.Efforts, t.Effort) {
		return nil, "", refuse("bad-request", "effort is one of %s", joinEfforts(wire.Efforts))
	}
	if cur := caller.Model; cur != nil && cur.Profile != "" && caller.Provider == t.Provider && s.ProfileModels != nil {
		id := t.Model
		if id == "" {
			id = cur.ID
		}
		if slices.Contains(s.ProfileModels(cur.Profile), id) {
			if t.Effort != "" {
				return nil, "", refuse("bad-request", "%s takes no effort", id)
			}
			return &wire.Model{ID: id}, cur.Profile, nil
		}
	}
	m, err := s.plainModel(t)
	return m, "", err
}

// plainModel is the task's model on the CLI's own account.
func (s *Service) plainModel(t Task) (*wire.Model, error) {
	if t.Model == "" && t.Effort == "" {
		return nil, nil
	}
	var models []wire.AgentModel
	if s.Models != nil {
		models = s.Models(t.Provider)
	}
	id := t.Model
	if id == "" {
		if len(models) == 0 {
			return nil, refuse("bad-request", "name a model to give an effort")
		}
		id = models[0].ID
	}
	m := &wire.Model{ID: id}
	if t.Effort != "" {
		e := t.Effort
		m.Effort = &e
	}
	if s.Models == nil {
		return m, nil
	}
	i := slices.IndexFunc(models, func(am wire.AgentModel) bool { return am.ID == id })
	if i < 0 {
		ids := make([]string, len(models))
		for j, am := range models {
			ids[j] = am.ID
		}
		return nil, refuse("bad-request", "%s has no model %q here; it has: %s", t.Provider, id, strings.Join(ids, ", "))
	}
	if t.Effort != "" && len(models[i].Efforts) > 0 && !slices.Contains(models[i].Efforts, t.Effort) {
		return nil, refuse("bad-request", "%s takes the efforts %s", id, joinEfforts(models[i].Efforts))
	}
	return m, nil
}

func joinEfforts(es []wire.Effort) string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e)
	}
	return strings.Join(out, ", ")
}

// Read is the chat's feed as text: the last limit entries, or the ones after the entry since.
func (s *Service) Read(ctx context.Context, callerID, id, since string, limit int) (string, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return "", err
	}
	rec, err := s.target(caller, id)
	if err != nil {
		return "", err
	}
	var snap wire.ConvSnapshot
	if since == "" {
		snap, err = s.Convs.Conversation(ctx, id)
	} else {
		snap, err = s.Convs.ConversationAfter(ctx, id, since)
	}
	if err != nil {
		return "", refuse("unavailable", "the chat did not answer: %v", err)
	}
	return transcript(rec, snap, limit), nil
}

// Send puts a message into the chat's queue, as another agent's message (never as the person's words).
func (s *Service) Send(ctx context.Context, callerID, id, text string) (wire.Conversation, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return wire.Conversation{}, err
	}
	rec, err := s.target(caller, id)
	if err != nil {
		return wire.Conversation{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > PromptMax {
		return wire.Conversation{}, refuse("bad-request", "text: 1 to %d characters", PromptMax)
	}
	asked := s.now().UnixMilli()
	if res := s.deliver(ctx, caller, id, text); res.R != wire.ResultAccepted {
		return wire.Conversation{}, refuse(string(res.Code), "the message was not queued")
	}
	if _, ok := s.Book.Get(id); ok {
		s.Book.Update(id, func(p *Page) { p.AskedAt = asked })
	}
	return rec, nil
}

// deliver is a message of from's agent in to's queue, in the envelope another agent's message has (internal/peers).
func (s *Service) deliver(ctx context.Context, from wire.Conversation, to, text string) wire.CmdResult {
	id := peers.NewID()
	env := peers.Envelope{ID: id, Person: s.Self.Name(), Machine: s.Self.MachineLabel(), Chat: from.Title, Via: "agent", Text: text}
	if s.Self.Machine != "" {
		env.Ref = s.Self.Machine + "/" + from.ID
	}
	return s.Convs.DeliverPeer(ctx, to, convo.PeerClientPrefix+id, peers.Wrap(env), false)
}

// Outcome is how a wait ended.
type Outcome string

const (
	// Done: the agent finished its turn and nothing waits in its queue.
	Done Outcome = "done"
	// NeedsPerson: the agent asks the person a question or for a permission; it goes on once they answer.
	NeedsPerson Outcome = "needs-person"
	// Ended: the agent is not running (stopped, gone, never started).
	Ended Outcome = "ended"
	// Timeout: the agent still works.
	Timeout Outcome = "timeout"
)

// outcome is where the chat stands for the one waiting on it: "" while it works on what it was given at asked.
func outcome(rec wire.Conversation, asked int64) Outcome {
	switch rec.State.K {
	case wire.StateWaitingQuestion, wire.StateWaitingPermission:
		return NeedsPerson
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed:
		return Ended
	case wire.StateIdle, wire.StateReady:
		t := rec.LastTurn
		if rec.Queue.Count == 0 && rec.Queue.Paused == nil && t != nil && t.EndedAt != nil && int64(*t.EndedAt) >= asked {
			return Done
		}
		if rec.Queue.Paused != nil {
			return NeedsPerson
		}
	}
	return ""
}

// Result is a chat as a wait or a wake-up reports it.
type Result struct {
	Conv    wire.Conversation
	Outcome Outcome
	// Answer is the agent's last answer, "" when it gave none.
	Answer string
}

// Wait blocks until the chat finishes what it was last given, needs the person, ends, or the timeout passes.
func (s *Service) Wait(ctx context.Context, callerID, id string, timeout time.Duration) (Result, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return Result{}, err
	}
	rec, err := s.target(caller, id)
	if err != nil {
		return Result{}, err
	}
	var asked int64
	if p, ok := s.Book.Get(id); ok {
		asked = p.AskedAt
	}
	poll := s.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		if o := outcome(rec, asked); o != "" {
			return s.result(ctx, rec, o), nil
		}
		select {
		case <-ctx.Done():
			return Result{Conv: rec, Outcome: Timeout}, nil
		case <-deadline.C:
			return Result{Conv: rec, Outcome: Timeout}, nil
		case <-tick.C:
		}
		next, ok := s.Convs.Record(id)
		if !ok {
			return Result{}, refuse("not-found", "chat %s was deleted", id)
		}
		rec = next
	}
}

func (s *Service) result(ctx context.Context, rec wire.Conversation, o Outcome) Result {
	r := Result{Conv: rec, Outcome: o}
	if snap, err := s.Convs.Conversation(ctx, rec.ID); err == nil {
		r.Answer = lastAnswer(snap)
	}
	return r
}

// Children are the chats the caller delegated.
func (s *Service) Children(ctx context.Context, callerID string) ([]wire.Conversation, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return nil, err
	}
	kids, err := s.children(ctx, caller.ID)
	if err != nil {
		return nil, refuse("unavailable", "the machine's chats did not answer: %v", err)
	}
	return kids, nil
}

// Sweep looks at every delegated chat whose parent waits to be woken (see Observe): what the runtime runs every few
// seconds.
func (s *Service) Sweep() {
	for _, id := range s.Book.notifying() {
		if rec, ok := s.Convs.Record(id); ok {
			s.Observe(rec)
		}
	}
}

// Observe hears a conversation's record as it changes. A delegated chat whose parent asked to be woken and that has
// finished a turn (or ended) wakes the parent with its answer, once per turn and at most MaxWakes times. It may block
// on the conversations' actors: the caller runs it off them.
func (s *Service) Observe(rec wire.Conversation) {
	if rec.ParentID == nil {
		return
	}
	page, ok := s.Book.Get(rec.ID)
	if !ok || !page.Notify {
		return
	}
	o := outcome(rec, page.AskedAt)
	var key string
	switch o {
	case Done:
		key = "turn:" + rec.LastTurn.ID
	case Ended:
		// A terminal stopped (by the person, or for standing idle) is no news; an agent that died or never started is,
		// unless its last turn was already reported.
		if rec.State.K == wire.StateStopped || rec.LastTurn != nil && page.Woken == "turn:"+rec.LastTurn.ID {
			return
		}
		key = "end:" + string(rec.State.K)
	default:
		return
	}
	parent, ok := s.Convs.Record(*rec.ParentID)
	if !ok || !s.Book.claim(rec.ID, key, MaxWakes) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := s.result(ctx, rec, o)
	text := wakeText(r)
	if p, _ := s.Book.Get(rec.ID); p.Wakes >= MaxWakes {
		text += fmt.Sprintf("\n\nThis chat has woken you %d times, the most it may: it will not wake you again. Use wait_conversation from now on.", MaxWakes)
	}
	if res := s.deliver(ctx, rec, parent.ID, text); res.R != wire.ResultAccepted && s.Log != nil {
		s.Log("delegation: the parent was not woken", "parent", parent.ID, "child", rec.ID, "code", string(res.Code))
	}
}
