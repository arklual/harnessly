package consent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// LiveDir is where, under the runtime's data folder, a chat with a voice call on is marked: one empty file named by
// the tmux session its agent runs in.
const LiveDir = "voice-live/live"

// staleAfter: a mark older than the longest call is a crash's leftover, not a call.
const staleAfter = 35 * time.Minute

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Mark puts or takes away a chat's mark.
func Mark(dir, session string, on bool) error {
	if !sessionRe.MatchString(session) {
		return errors.New("bad session name")
	}
	path := filepath.Join(dir, session)
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0o600)
}

// Live says whether the chat of a session has a voice call on now.
func Live(dir, session string, now time.Time) bool {
	if !sessionRe.MatchString(session) {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, session))
	return err == nil && now.Sub(st.ModTime()) < staleAfter
}

// HookInput is what Claude Code's and Codex's PreToolUse hook gets on stdin, the part read here.
type HookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command any `json:"command"`
	} `json:"tool_input"`
}

// Command is a shell call's command line ("" for any other tool); Codex gives it as a list of words.
func (in HookInput) Command() string {
	switch c := in.ToolInput.Command.(type) {
	case string:
		return c
	case []any:
		var out string
		for i, w := range c {
			if s, ok := w.(string); ok {
				if i > 0 {
					out += " "
				}
				out += s
			}
		}
		return out
	}
	return ""
}

// Answer is the hook's answer to a call while the chat has a voice call on: nil lets it go; an outside one is asked
// about (Claude Code shows its permission request, which the person answers with «да» or the button) or, for Codex,
// whose hook cannot ask, refused with words that tell the agent to ask the person first.
func Answer(raw []byte, codex bool) []byte {
	var in HookInput
	// While a call is on, a call the hook could not read (no input, cut short, not JSON) is asked about, not let go.
	v := Verdict{Class: External, What: "вызов, который не разобрать"}
	if json.Unmarshal(raw, &in) == nil && in.ToolName != "" {
		v = Classify(in.ToolName, in.Command())
	}
	if v.Class != External {
		return nil
	}
	decision, reason := "ask", "Разговор голосом: это действие уходит наружу ("+v.What+"), нужно «да» человека."
	if codex {
		decision = "deny"
		reason = "The person is talking to this chat by voice, and this call reaches outside (" + v.What + "). " +
			"Do not run it yet: say in one sentence what you are about to do and ask «Делаем?»; run it after they say «да»."
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason,
	}})
	return out
}
