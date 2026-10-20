package claude

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// TranslateHook turns a hook report (`<event>` as the status hook script names it, and Notification's type) into a
// work-state event; false for a name that is not a hook event (the report then means "look again").
func TranslateHook(event, notificationType string) (agents.Event, bool) {
	kind := agents.EventKind(event)
	if !slices.Contains(agents.HookEventKinds, kind) {
		return agents.Event{}, false
	}
	if kind == agents.Notification {
		return agents.Event{Kind: kind, NotificationType: notificationType}, true
	}
	return agents.Event{Kind: kind}, true
}

// HookEvents pairs each Claude Code hook with the event name the status hook script is called with.
var HookEvents = []struct{ Hook, Event string }{
	{"SessionStart", "session-start"},
	{"UserPromptSubmit", "prompt-submit"},
	{"Notification", "notification"},
	{"Stop", "stop"},
	{"StopFailure", "stop-failure"},
	{"PreCompact", "pre-compact"},
	{"PostCompact", "post-compact"},
}

// HookSettingsConfig is what the `--settings` file of a launch points at.
type HookSettingsConfig struct {
	// StatusHook and Statusline are the absolute paths of the two scripts the runtime writes.
	StatusHook string
	Statusline string
	// Allow are permissions granted without asking (the runtime's own MCP servers, current and former names).
	Allow []string
	// HookTimeout is each hook's timeout in seconds (3 when 0); the script returns at once, so it hardly matters.
	HookTimeout int
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type hookEntry struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

// orderedHooks writes the hooks object in HookEvents order.
type orderedHooks []struct {
	name  string
	entry []hookEntry
}

func (h orderedHooks) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range h {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.name)
		v, err := marshalNoEscape(e.entry)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// HookSettings is the `--settings` file Claude is launched with, byte for byte as the previous runtime wrote it
// (JSON indented by two spaces, a final newline): the lifecycle hooks calling the status hook script, the status line
// script, the allowed tools, and peer messages accepted across permission modes.
func HookSettings(c HookSettingsConfig) []byte {
	timeout := c.HookTimeout
	if timeout == 0 {
		timeout = 3
	}
	hooks := orderedHooks{}
	for _, e := range HookEvents {
		cmd := `sh "` + c.StatusHook + `" ` + e.Event
		hooks = append(hooks, struct {
			name  string
			entry []hookEntry
		}{e.Hook, []hookEntry{{Matcher: "", Hooks: []hookCommand{{Type: "command", Command: cmd, Timeout: timeout}}}}})
	}
	allow := c.Allow
	if allow == nil {
		allow = []string{}
	}
	settings := struct {
		Hooks      orderedHooks `json:"hooks"`
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
		CrossSessionInbound string `json:"crossSessionInbound"`
	}{Hooks: hooks, CrossSessionInbound: "accept"}
	settings.StatusLine.Type = "command"
	settings.StatusLine.Command = `sh "` + c.Statusline + `"`
	settings.Permissions.Allow = allow
	compact, err := marshalNoEscape(settings)
	if err != nil {
		return nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil
	}
	out.WriteByte('\n')
	return out.Bytes()
}
