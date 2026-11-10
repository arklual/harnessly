package budget

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Gate is the runtime's side: for a chat it reads the list and says what to tell the person or refuse.
type Gate struct {
	Reader *Reader
	// UserOf is the person of a chat's tmux session; nil or "" is unknown, and then the person is the owner of the
	// machine (RUNTIME_OWNER_EMAIL), else the only person the list names.
	UserOf func(session string) string
	// Owner is the machine's owner email, "" for none.
	Owner string
	// Account is the accountRef a provider's CLI runs as on this machine; nil or "" is unknown.
	Account func(provider string) string

	mu   sync.Mutex
	seen map[string]bool
}

// noticesMax bounds the notices remembered; past it they are forgotten and a person may see one again.
const noticesMax = 4096

func (g *Gate) subject(set hubproto.BudgetSet, provider, session, model string) Subject {
	s := Subject{Model: model}
	if g.UserOf != nil {
		s.User = g.UserOf(session)
	}
	if s.User == "" {
		s.User = g.Owner
	}
	if s.User == "" {
		s.User = OnlyUser(set)
	}
	if g.Account != nil {
		s.Account = g.Account(provider)
	}
	return s
}

// Admit is the reason a new chat of the provider on the model ("" for unknown) may not start, "" when it may.
func (g *Gate) Admit(provider, model string) string {
	if g == nil {
		return ""
	}
	set := g.Reader.Get()
	d := Decide(set, g.subject(set, provider, "", model))
	if !d.Block {
		return ""
	}
	return Text(d.Entry)
}

// Reply is what a hook of a running chat answers, as the JSON the agent CLI reads from the hook's output; nil for
// nothing to say. event is the CLI's hook name: UserPromptSubmit and PreToolUse can stop a chat, any other only warns.
func (g *Gate) Reply(provider, event, session, model string) []byte {
	if g == nil {
		return nil
	}
	set := g.Reader.Get()
	d := Decide(set, g.subject(set, provider, session, model))
	if d.None() {
		return nil
	}
	text := Text(d.Entry)
	if d.Block && (event == "UserPromptSubmit" || event == "PreToolUse") {
		return stopReply(provider, event, text)
	}
	if g.firstTime(session, d) {
		raw, _ := json.Marshal(map[string]string{"systemMessage": text})
		return raw
	}
	return nil
}

// firstTime is true once per chat for an entry.
func (g *Gate) firstTime(session string, d Decision) bool {
	key := session + "|" + d.Key()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen[key] {
		return false
	}
	if g.seen == nil || len(g.seen) >= noticesMax {
		g.seen = map[string]bool{}
	}
	g.seen[key] = true
	return true
}

// stopReply refuses a prompt, or ends the turn at a tool call. The reason is shown to the person as it is. Codex's
// PreToolUse takes only a deny with a reason that is not empty: continue:false is rejected by its CLI and the call
// would run.
func stopReply(provider, event, text string) []byte {
	var v any
	switch {
	case event == "UserPromptSubmit":
		v = map[string]string{"decision": "block", "reason": text}
	case provider == "codex":
		v = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": text,
		}}
	default:
		v = map[string]any{"continue": false, "stopReason": text}
	}
	raw, _ := json.Marshal(v)
	return raw
}

// Text is what a person is told about an entry: the hub's reason and, unless it names it, when the limit resets.
func Text(e hubproto.BudgetEntry) string {
	text := strings.TrimRight(strings.TrimSpace(e.Reason), ".")
	if text == "" {
		text = locale.Pick("A usage limit is reached", "Достигнут лимит расхода")
	}
	low := strings.ToLower(text)
	if e.ResetsAt > 0 && !strings.Contains(low, "сброс") && !strings.Contains(low, "reset") {
		text += ". " + locale.Pick("Resets ", "Сброс ") + time.UnixMilli(e.ResetsAt).Local().Format("02.01 15:04")
	}
	return text + "."
}
