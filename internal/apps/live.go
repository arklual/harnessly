package apps

import (
	"fmt"
	"slices"
	"strings"
)

// An app turned on in a chat whose agent already runs. Every Claude Code chat has the host's server
// (harnessly-app) from its start, quiet while no app is on: the server's tools are read at every call from the apps on
// in the chat now (HostTools.Enabled), and the client is told when they change (notifications/tools/list_changed). What
// the server cannot carry, the agent gets on its next turn from the live hook (`harnessly hook app-context --live`):
// the host tools' words, each new app's instructions and the files of its skills.
//
// An app goes live only when it needs nothing that only a launch gives: rules for the agent's own tools and the hooks
// that hold them (the app-guard reads a snapshot written at the launch), checks of the turn, context on activation, its
// own MCP servers (their config and secrets go in at a launch), or parts of a Claude Code plugin. Such an app waits for
// the agent to start again (the chat stops an agent at rest and the next message brings it back with the app), and
// until then its tools refuse with the reason. So does every app of a deployment that requires the wall
// (RUNTIME_APPS_REQUIRE_WALL) when the agent started without it: the wall is made only at a launch.

// LaunchNeeds names what of the app only a launch of the agent gives; "" when a running agent can take it as it is.
func LaunchNeeds(pkg *Package) string {
	if pkg == nil || pkg.Manifest == nil {
		return "its manifest"
	}
	m := pkg.Manifest
	var needs []string
	if ag := m.Agent; ag != nil {
		// The rules hold only with the apps' permissions on (policy.go Decide).
		if t := ag.Tools; t != nil && len(t.Allow)+len(t.Deny)+len(t.Confirm) > 0 && PermissionsOn() {
			needs = append(needs, "rules for the agent's tools")
		}
		if len(ag.Validators) > 0 {
			needs = append(needs, "checks of the agent's turn")
		}
		if len(ag.Context) > 0 {
			needs = append(needs, "context on activation")
		}
		if len(ag.MCP) > 0 {
			needs = append(needs, "MCP servers")
		}
		if ag.Flow != "" || ag.Exclusive {
			needs = append(needs, "a flow of its own")
		}
	}
	if len(pkg.MCP) > 0 && !slices.Contains(needs, "MCP servers") {
		needs = append(needs, "MCP servers")
	}
	if m.Allows("hooks.agent") {
		needs = append(needs, "hooks")
	}
	if parts := pkg.pluginParts(); len(parts) > 0 {
		needs = append(needs, "plugin parts ("+strings.Join(parts, ", ")+")")
	}
	return strings.Join(needs, ", ")
}

// LiveRefusal is what the app's tools answer while it waits for a new launch of the agent.
func LiveRefusal(name, needs string) string {
	return name + " was turned on after this chat's agent started and needs " + needs +
		", which only a new start of the agent gives. Tell the person: the app works from the agent's next start " +
		"(stop the agent and send a message: the chat goes on with the app)."
}

// WallRefusal is what the tools of an app answer in a deployment that requires the wall when the agent started without it.
func WallRefusal(name string) string {
	return name + " was turned on after this chat's agent started without the wall around the apps, and this " +
		"deployment requires the wall (RUNTIME_APPS_REQUIRE_WALL). Tell the person: the app works from the agent's next " +
		"start (stop the agent and send a message: the chat goes on with the app inside the wall)."
}

// LiveBrief is what the agent of a running chat is told of an app turned on in it: its instructions and where its
// skills are. Its slash commands come with the next start only.
func LiveBrief(a Installed) (string, error) {
	text, err := instructions(a)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## App: %s (%s)\n\n", a.Manifest.Name, a.ID)
	if text != "" {
		b.WriteString(text + "\n\n")
	}
	b.WriteString(skillList(a))
	return strings.TrimSpace(b.String()), nil
}

// LiveNotice is the additionalContext of the agent's next turn after apps were turned on or off in its chat; "" when
// nothing changed. on are the briefs of the apps turned on (LiveBrief), waiting the refusals of the ones that wait for a
// new start, off the names of the apps turned off.
func LiveNotice(on, waiting, off []string) string {
	var parts []string
	if len(on) > 0 {
		parts = append(parts, "# Apps turned on in this chat\n\nThe person turned these apps on while you work. Their tools are "+
			"the mcp__harnessly-app__* tools (load them with ToolSearch if they are deferred). Keep to what each app says.\n\n"+
			HostInstructions()+"\n\n"+strings.Join(on, "\n\n"))
	}
	if len(waiting) > 0 {
		parts = append(parts, "# Apps that wait for the agent's next start\n\n"+strings.Join(waiting, "\n\n"))
	}
	if len(off) > 0 {
		parts = append(parts, "# Apps turned off in this chat\n\n"+strings.Join(off, ", ")+
			": the person turned them off. Their tools are gone; do not follow their instructions any more.")
	}
	return strings.Join(parts, "\n\n")
}
