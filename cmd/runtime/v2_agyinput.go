package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity/acpbridge"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// agyInput is the structured channel of native Antigravity chats: each runs as `harnessly antigravity-acp`, a bridge
// to the CLI's Agent Client Protocol, which listens on a socket of its conversation. Messages (pictures and steering
// included), stops, model changes and approvals go over it; the bridge tells the session it runs and every turn's end.
type agyInput struct {
	mgr *acpbridge.Manager
	rt  atomic.Pointer[convo.Runtime]
	// bind names the session and transcript a tab's bridge runs.
	bind func(tab string, st acpbridge.State)
	// tabOf is the tab a conversation runs in (its bridge's socket is named by it); convOf the conversation of a tab.
	tabOf  func(conv wire.Conversation) string
	convOf func(tab string) string

	mu    sync.Mutex
	bound map[string]string // conversation → the session told last
}

func newAgyInput(dataDir string) *agyInput {
	a := &agyInput{bound: map[string]string{}}
	dir := filepath.Join(dataDir, "antigravity-input")
	if len(dir) > 60 {
		// A socket's path must fit a unix address: a deep data folder keeps its sockets in a short one of the user's.
		dir = filepath.Join(os.TempDir(), "harnessly-"+strconv.Itoa(os.Getuid())+"-agy")
	}
	a.mgr = &acpbridge.Manager{Dir: dir, Sink: a}
	return a
}

// socket is where a new chat's bridge is to listen.
func (a *agyInput) socket(conv string) string {
	if a == nil {
		return ""
	}
	return a.mgr.Socket(conv)
}

func (a *agyInput) Send(ctx context.Context, conv wire.Conversation, m convo.InputMessage) error {
	err := a.mgr.Send(ctx, a.tab(conv), acpbridge.Message{ID: m.ClientID, Text: m.Text, Images: m.Images})
	if errors.Is(err, acpbridge.ErrUncertain) {
		return errors.Join(convo.ErrInputUncertain, err)
	}
	return err
}

func (a *agyInput) Interrupt(ctx context.Context, conv wire.Conversation) error {
	return a.mgr.Cancel(ctx, a.tab(conv))
}

// bridgeMode is the bridge's name of a permission mode.
var bridgeMode = map[wire.PermissionMode]string{wire.PermissionAsk: acpbridge.ModeAsk, wire.PermissionPlan: acpbridge.ModePlan,
	wire.PermissionBypass: acpbridge.ModeBypass}

func (a *agyInput) Configure(ctx context.Context, conv wire.Conversation, model string, effort *wire.Effort, permission wire.PermissionMode) error {
	e, mode := "", ""
	if effort != nil {
		e = string(*effort)
	}
	if permission != "" {
		m, ok := bridgeMode[permission]
		if !ok {
			return errors.New("Antigravity has no such permission mode")
		}
		mode = m
	}
	_, err := a.mgr.Configure(ctx, a.tab(conv), model, e, mode)
	return err
}

func (a *agyInput) tab(conv wire.Conversation) string {
	if a.tabOf != nil {
		if t := a.tabOf(conv); t != "" {
			return t
		}
	}
	return conv.ID
}

// conv is the conversation of a tab the bridges name, "" when none is known.
func (a *agyInput) conv(tab string) string {
	if a.convOf != nil {
		return a.convOf(tab)
	}
	return tab
}

// --- what the bridges tell (by tab) ---

func (a *agyInput) State(conv string, st acpbridge.State) {
	if st.Session == "" || a.bind == nil {
		return
	}
	a.mu.Lock()
	same := a.bound[conv] == st.Session
	a.bound[conv] = st.Session
	a.mu.Unlock()
	if !same {
		a.bind(conv, st)
	}
}

func (a *agyInput) TurnStarted(string) {}

func (a *agyInput) TurnEnded(tab, stop, detail string) {
	rt, conv := a.rt.Load(), a.conv(tab)
	if rt == nil || conv == "" {
		return
	}
	// A stopped turn ends here too: Antigravity has no Stop hook, and its transcript does not mark a cancelled turn.
	failed := stop == "error" || stop == "refusal"
	if stop == "max_tokens" || stop == "max_turn_requests" {
		failed, detail = true, "Antigravity stopped: "+stop
	}
	_ = rt.InputTurnEnded(conv, failed, detail)
}

// Approval puts what Antigravity asks on the person's card and gives back the decision; false leaves it to the
// bridge's terminal.
func (a *agyInput) Approval(ctx context.Context, tab string, p acpbridge.Permission) (string, bool) {
	rt, conv := a.rt.Load(), a.conv(tab)
	if rt == nil || conv == "" {
		return "", false
	}
	ask := convo.PermissionAsk{Tool: p.Tool}
	var args map[string]any
	_ = json.Unmarshal(p.Input, &args)
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := args[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	fields := map[string]any{}
	switch {
	case p.Kind == "execute" || str("CommandLine") != "":
		ask.Tool = "Bash"
		fields["command"] = firstNonEmpty(str("CommandLine", "command"), p.Title)
		if cwd := str("Cwd", "cwd"); cwd != "" {
			ask.Cwd = cwd
		}
	case p.Kind == "edit" || p.Kind == "delete" || p.Kind == "move":
		ask.Tool = "Edit"
		fields["file_path"] = firstNonEmpty(str("TargetFile", "AbsolutePath", "FilePath", "path"), p.Title)
	default:
		if ask.Tool == "" {
			ask.Tool = firstNonEmpty(p.Kind, "tool")
		}
		for k, v := range args {
			fields[k] = v
		}
		if url := str("Url", "url"); url != "" {
			fields["url"] = url
		} else if path := str("AbsolutePath", "TargetFile", "path"); path != "" {
			fields["path"] = path
		} else if p.Title != "" {
			fields["query"] = p.Title
		}
	}
	ask.Input, _ = json.Marshal(fields)
	if p.Always {
		// The card offers «don't ask again»; the bridge picks Antigravity's own always-option for it.
		ask.Suggestions = json.RawMessage(`[{"type":"antigravity-always"}]`)
	}
	reply, ok := rt.PermissionRequest(ctx, conv, ask)
	switch {
	case !ok:
		return "", false
	case reply.Behavior == "allow" && len(reply.UpdatedPermissions) > 0:
		return "always", true
	case reply.Behavior == "allow":
		return "allow", true
	case reply.Interrupt:
		return "cancel", true
	}
	return "deny", true
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func (a *agyInput) Lost(tab string) {
	a.mu.Lock()
	delete(a.bound, tab)
	a.mu.Unlock()
	if rt, conv := a.rt.Load(), a.conv(tab); rt != nil && conv != "" {
		rt.InputLost(conv)
	}
}

// bindAntigravity takes the session a bridge runs as the tab's: the transcript the chat follows and the session it
// resumes. Only a path inside the CLI's own folder for that very session is taken.
func (c *core) bindAntigravity(tab string, st acpbridge.State) {
	if c.agents == nil || !agents.IsUUID(st.Session) {
		return
	}
	found, ok := c.FindTab(tab)
	if !ok || found.Tab.PanelType.Value() != agents.AntigravityPanel {
		return
	}
	path := st.Transcript
	if path == "" || !antigravity.ValidateTranscriptPath(c.cfg.Home, path, st.Session) {
		path = antigravity.TranscriptPath(c.cfg.Home, st.Session)
	}
	session := found.Tab.SessionName
	id := st.Session
	some := func(s string) *status.Opt[string] { o := status.Some(s); return &o }
	meta := status.HookMeta{SessionID: some(id), JSONLPath: some(path)}
	k := keeperRef{c}
	if _, _, ok := k.ApplyHookMeta(antigravity.ProviderID, session, meta); !ok {
		return
	}
	c.agents.nativeStarts.bindBridge(tab, id)
	k.HandleProviderEvent(antigravity.ProviderID, session, agents.Event{Kind: agents.SessionStart, AgentSession: id}, nil)
}

// bindBridge records the session a bridge told: the bridge is the runtime's own launch, so its word binds the
// conversation without a hook.
func (s *nativeAntigravityStarts) bindBridge(conversation, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byConversation == nil {
		s.byConversation = map[string]nativeAntigravityStart{}
	}
	cur := s.byConversation[conversation]
	cur.bound, cur.expected = id, id
	s.byConversation[conversation] = cur
}

// providerInputs is the structured channel of the conversations, by their provider.
type providerInputs struct {
	codex *codexInput
	agy   *agyInput
}

func (p providerInputs) Send(ctx context.Context, conv wire.Conversation, m convo.InputMessage) error {
	switch {
	case conv.Provider == wire.ProviderAntigravity && p.agy != nil:
		return p.agy.Send(ctx, conv, m)
	case conv.Provider == wire.ProviderCodex && p.codex != nil:
		return p.codex.Send(ctx, conv, m)
	}
	return acpbridge.ErrNoChannel
}

func (p providerInputs) Interrupt(ctx context.Context, conv wire.Conversation) error {
	if conv.Provider == wire.ProviderAntigravity && p.agy != nil {
		return p.agy.Interrupt(ctx, conv)
	}
	return acpbridge.ErrNoChannel
}

func (p providerInputs) Configure(ctx context.Context, conv wire.Conversation, model string, effort *wire.Effort, permission wire.PermissionMode) error {
	if conv.Provider == wire.ProviderAntigravity && p.agy != nil {
		return p.agy.Configure(ctx, conv, model, effort, permission)
	}
	return acpbridge.ErrNoChannel
}

// agyInputOf is the channel of native Antigravity chats, nil when they are off or no CLI can run their bridge.
func agyInputOf(dataDir string) *agyInput {
	if !nativeAntigravityEnabled() || appCLIBin() == "" {
		return nil
	}
	return newAgyInput(dataDir)
}

// inputsOf is the structured channel the core's conversations take, nil when no provider has one.
func inputsOf(c *core) convo.Input {
	if c == nil || c.agents == nil || c.agents.codexInput == nil && c.agents.agyInput == nil {
		return nil
	}
	return providerInputs{codex: c.agents.codexInput, agy: c.agents.agyInput}
}

// wireAgyInput gives the channel the conversations it reports to, and keeps it connected to every bridge.
func (v *v2Live) wireAgyInput(a *agyInput) {
	parse := v.c.cfg.Tmux.ParseSessionName
	a.tabOf = func(conv wire.Conversation) string {
		if _, _, tab, ok := parse(conv.Session); ok {
			return tab
		}
		return ""
	}
	a.convOf = func(tab string) string {
		if found, ok := v.c.FindTab(tab); ok && found.Tab.SessionName != "" {
			if id := v.rt.ConvOfSession(found.Tab.SessionName); id != "" {
				return id
			}
		}
		if v.rt.Has(tab) {
			return tab
		}
		return ""
	}
	a.bind = v.c.bindAntigravity
	a.rt.Store(v.rt)
	safego.Go("antigravity bridges", func() { a.mgr.Watch(context.Background()) })
}

// agyBridged: new native Antigravity chats run beside their bridge, so their model changes as they run.
func agyBridged() bool { return nativeAntigravityEnabled() && appCLIBin() != "" }
