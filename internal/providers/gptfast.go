package providers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The fast mode of a GPT chat. Claude Code's /fast is Anthropic's alone, so a GPT chat's fast mode lives in
// the relay instead: each request of the chat says which tmux pane it comes from, and while that pane's chat is in fast
// mode the relay asks the ChatGPT backend for the fast tier, as `service_tier = "fast"` makes the Codex CLI do. The
// mode is read on every request, so turning it needs no restart of the agent.
//
// The pane comes in a header Claude Code sends (ANTHROPIC_CUSTOM_HEADERS on the chat's launch line, ChatHeaderEnv)
// and CLIProxyAPI passes on to its Codex upstream untouched: Version, which Claude Code itself never sends and which
// CLIProxyAPI only copies (its codex executor, applyCodexHeaders). The relay takes the header off before the request
// goes up.

// chatHeader is the header that carries a GPT chat's pane through the proxy, chatMarker the start of its value.
const (
	chatHeader = "Version"
	chatMarker = "harnessly-pane/"
)

// fastTier is the service tier the Codex CLI sends for `service_tier = "fast"`: its model cache names the «Fast» tier
// "priority" (catalog.hasFastTier).
const fastTier = "priority"

// ChatHeaderEnv is the environment word of a GPT chat's launch line, written for sh: the pane's shell fills in the
// pane ($TMUX_PANE) and its tmux server ($TMUX) when the line runs, so every launch and resume of the chat says its
// pane whichever way it was started.
const ChatHeaderEnv = `ANTHROPIC_CUSTOM_HEADERS="` + chatHeader + `: ` + chatMarker + `$TMUX_PANE/$TMUX"`

// ClaudeLineEnv is what a Claude Code chat on the profile adds to its launch line: the pane header for gpt (whose
// proxy has a fast tier to offer), nothing for another profile.
func (l Launcher) ClaudeLineEnv(profile string) string {
	if p, ok := Find(profile); ok && p.Switched && l.Store != nil {
		if _, ok := l.Store.SwitchOf(profile).(FastTiers); ok {
			return ChatHeaderEnv
		}
	}
	return ""
}

// FastTiers is a switch whose vendor has a fast tier for some of its models.
type FastTiers interface {
	HasFast(model string) bool
}

// HasFast says whether the ChatGPT backend has the fast tier for the model, by Codex's own model cache.
func (g *GPTProxy) HasFast(model string) bool { return g.FastTier != nil && g.FastTier(model) }

// prepare takes the pane header off a request of the proxy and, on a turn of a chat in fast mode, asks for the fast tier.
func (g *GPTProxy) prepare(r *http.Request, rest string, body []byte) []byte {
	pane, socket, ok := chatPane(r.Header.Get(chatHeader))
	if !ok {
		return body
	}
	r.Header.Del(chatHeader)
	if body == nil || !strings.HasSuffix(rest, "/responses") || r.Header.Get("Content-Encoding") != "" || g.FastPane == nil {
		return body
	}
	if !g.FastPane(pane, socket) {
		return body
	}
	return WithFastTier(body, g.HasFast)
}

// chatPane reads the pane header: the pane's id and the name of its tmux server's socket (the last part of the path
// $TMUX starts with); ok is false for a value that is not the header's.
func chatPane(v string) (pane, socket string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(v), chatMarker)
	if !ok {
		return "", "", false
	}
	pane, tmux, _ := strings.Cut(rest, "/")
	path, _, _ := strings.Cut("/"+tmux, ",")
	if socket = filepath.Base(path); socket == "/" || socket == "." {
		socket = ""
	}
	return pane, socket, true
}

// WithFastTier is a Responses request asking for the fast tier when its model has one; any other body is given back
// as it came. A body without a service tier gets one put first, so the rest of its bytes stay as they were.
func WithFastTier(body []byte, hasFast func(model string) bool) []byte {
	var head map[string]json.RawMessage
	if err := json.Unmarshal(body, &head); err != nil {
		return body
	}
	var model string
	if err := json.Unmarshal(head["model"], &model); err != nil || !hasFast(model) {
		return body
	}
	tier, _ := json.Marshal(fastTier)
	if _, has := head["service_tier"]; has {
		head["service_tier"] = tier
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(head); err != nil {
			return body
		}
		return bytes.TrimRight(out.Bytes(), "\n")
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	inner := bytes.TrimLeft(trimmed[1:], " \t\r\n")
	sep := ","
	if len(inner) > 0 && inner[0] == '}' {
		sep = ""
	}
	out := make([]byte, 0, len(trimmed)+len(tier)+20)
	out = append(out, `{"service_tier":`...)
	out = append(out, tier...)
	out = append(out, sep...)
	return append(out, trimmed[1:]...)
}

// AgentModels are the wire records of the profile's models, each with the fast mode its switch says the vendor has.
func (s *Store) AgentModels(p Preset) []wire.AgentModel {
	out := p.AgentModels()
	if s == nil || !p.Switched {
		return out
	}
	if ft, ok := s.SwitchOf(p.ID).(FastTiers); ok {
		for i := range out {
			out[i].Fast = ft.HasFast(out[i].ID)
		}
	}
	return out
}
