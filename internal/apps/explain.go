package apps

import (
	"fmt"
	"strings"
)

// The layers that decide whether an app is on in a chat, in the order they apply:
//
//  1. machine — the app is installed here (installed.json): its version, trust, and whether this harnessly is one it
//     works with;
//  2. team — an owner put it on every member's machine (team.json) and it follows a channel; a team's app is never on
//     by itself;
//  3. setup — its required secrets and settings, without which it waits;
//  4. activation — always (every new chat of the person's own install), a command (/x turns it on in the field), or by
//     hand (the chat's switch);
//  5. chat — on or off in this chat (the conversation's apps), and what the chat's agent can run (Claude Code only);
//  6. wall — whether the agent of a chat with apps runs inside the operating system's wall.
//
// Two more lines stand beside the layers: consent — what the app waits for the person to agree to, and where they agree
// — and views — the errors its views reported lately (viewlog.go).
//
// Explain says what each layer says of one app, so «why is it not in this chat» has an answer line by line.

// ExplainChat is the chat an explanation is about; nil for none (run outside a chat).
type ExplainChat struct {
	Provider string
	Apps     []string
}

// ExplainInput is what Explain reads: the install (nil when the app is not on this machine), the chat, the required
// secrets still missing, the running harnessly's version, the wall's state and the chat's apps' weight.
type ExplainInput struct {
	ID          string
	App         *Installed
	Chat        *ExplainChat
	Waiting     []string
	HostVersion string
	// Wall says whether the wall applies here (Linux); WallErr is what keeps it from standing, nil when it stands.
	Wall      bool
	WallErr   error
	ChatBytes int
	// ViewLog are the last lines its views reported (viewlog.go), oldest first.
	ViewLog []ViewLogLine
}

// ExplainLine is one layer's word: Layer names it, Says is what it says, Off marks the layer that keeps the app off.
type ExplainLine struct {
	Layer string `json:"layer"`
	Says  string `json:"says"`
	Off   bool   `json:"off,omitempty"`
}

// Explain is the layers' words for one app, in their order.
func Explain(in ExplainInput) []ExplainLine {
	var out []ExplainLine
	add := func(layer string, off bool, format string, args ...any) {
		out = append(out, ExplainLine{Layer: layer, Says: fmt.Sprintf(format, args...), Off: off})
	}
	a := in.App
	if a == nil || a.Manifest == nil {
		add("machine", true, "%s is not installed on this machine: harnessly app install <folder>, or from the team's catalog", in.ID)
		return out
	}
	m := a.Manifest
	how := "from " + a.Source
	if a.Source == "" {
		how = "from a package"
	}
	if a.Dev {
		how = "as a dev install of " + a.Source
	}
	add("machine", false, "installed %s %s, trust %s", a.Version, how, m.Trust)
	if in.HostVersion != "" && m.Harnessly != "" {
		if rng, err := ParseRange(m.Harnessly); err == nil && !rng.Allows(in.HostVersion) {
			add("machine", true, "this harnessly is %s, the app wants %s: it is not turned on until the host is updated", in.HostVersion, m.Harnessly)
		}
	}
	if a.Team {
		ch := a.Channel
		if ch == "" {
			ch = "stable"
		}
		add("team", false, "the team's app, put on every member's machine by an owner, following %s; it is never on by itself, a chat turns it on", ch)
		if a.Muted {
			add("team", true, "the person turned it off for themselves (О приложении → включить снова)")
		}
	} else {
		add("team", false, "the person's own install, not the team's")
	}
	switch {
	case a.NeedsConsent():
		add("consent", true, "waits for the person's consent to %s; %s", waitingWords(a.WaitingAccess()), ConsentWhere(a.ID))
	case a.Pending != nil:
		add("consent", false, "version %s waits for the person's consent to %s, %s runs until then; %s", a.Pending.Version, waitingWords(a.WaitingAccess()), a.Version, ConsentWhere(a.ID))
	}
	if a.Off {
		add("machine", true, "the person turned it off (Приложения → %s → «Включено»)", a.ID)
	}
	if len(in.Waiting) > 0 {
		add("setup", true, "waits for secrets: %s (О приложении → настройки, or harnessly secret ask)", strings.Join(in.Waiting, ", "))
	} else if len(m.Secrets) > 0 || len(m.Settings) > 0 {
		add("setup", false, "secrets and settings in place")
	}
	chatTarget := m.HasTarget(TargetChat)
	switch {
	case !chatTarget:
		add("activation", true, "works in %s, not in a chat", strings.Join(m.Targets, ", "))
	case m.ActivatesOn(OnAlways) && a.Team:
		add("activation", false, "asks to be always on, but a team's app is not: turn it on in a chat")
	case m.ActivatesOn(OnAlways):
		add("activation", false, "always: on in every new chat of this machine; a chat that was open before keeps its own list")
	}
	if cmd := m.Command(); chatTarget && cmd != "" {
		add("activation", false, "%s in the chat's field turns it on", cmd)
	}
	if chatTarget && !m.ActivatesOn(OnAlways) && m.Command() == "" {
		add("activation", false, "by hand: the chat's switch (or harnessly app enable %s in the chat)", a.ID)
	}
	if c := in.Chat; c != nil && chatTarget {
		on := false
		for _, id := range c.Apps {
			if id == a.ID {
				on = true
			}
		}
		switch {
		case c.Provider != "" && c.Provider != "claude" && c.Provider != "codex":
			add("chat", true, "this chat runs %s: apps run in Claude Code and Codex chats only", c.Provider)
		case on:
			add("chat", false, "on in this chat; the agent took it at its last start")
		default:
			add("chat", true, "off in this chat")
		}
		if w := ChatBudgetWarning(in.ChatBytes); w != "" {
			add("chat", false, "%s", w)
		}
	}
	if n := len(in.ViewLog); n > 0 {
		// Only the count and the level: the text is the app's own words, read with app logs, marked as such there.
		add("views", false, "%d line(s) of its views' errors, the last a %s line (harnessly app logs %s)", n, in.ViewLog[n-1].Level, a.ID)
	}
	switch {
	case !in.Wall:
		add("wall", false, "no wall on this system: the guard hook alone keeps the agent to the apps' rules")
	case in.WallErr != nil:
		add("wall", false, "the agent runs without the wall, the guard hook alone: %v (Проверка машины → bubblewrap)", in.WallErr)
	default:
		add("wall", false, "the agent of a chat with apps runs inside the wall")
	}
	return out
}

// waitingWords names what waits, by the codes of the manifest.
func waitingWords(list []Access) string {
	if len(list) == 0 {
		return "what it reaches"
	}
	return strings.Join(AccessCodes(list), ", ")
}
