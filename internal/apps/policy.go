package apps

import (
	"path/filepath"
	"strings"
)

// The tool policy of the apps on in a chat: what the app-guard hook answers for each tool call the agent makes.
//
// A rule names a tool as the manifest writes it: a built-in tool (Bash, Edit), a built-in tool with a rule on its input
// (Bash(git push*)), a tool of an MCP server (bank-cashback.create, bank-cashback.*), the host's app.* tools or mcp:*
// for every MCP tool. Within one app the most specific rule that matches wins, so "deny mcp:*" with "allow
// bank-cashback.*" shuts out every MCP server but the bank's; on a tie deny beats confirm and confirm beats allow.
// Across the apps of a chat a deny of any app wins, then a confirm of any, then an allow. A call no rule names is left
// to the chat's own permissions.

// Decision is the guard's answer for one call.
type Decision string

const (
	// DecideNone: no app has a rule for the call; the chat's own permissions decide.
	DecideNone    Decision = ""
	DecideAllow   Decision = "allow"
	DecideConfirm Decision = "confirm"
	DecideDeny    Decision = "deny"
)

func (d Decision) rank() int {
	switch d {
	case DecideDeny:
		return 3
	case DecideConfirm:
		return 2
	case DecideAllow:
		return 1
	}
	return 0
}

// Call is one tool call as the agent's hook reports it.
type Call struct {
	// Tool is the agent's own name of the tool: Bash, Edit, mcp__bank-cashback__create,
	// mcp__plugin_acme-cashback_bank-cashback__create.
	Tool string
	// Input is the call's arguments: for Bash, command; for the file tools, file_path.
	Input map[string]any
}

// Verdict is the decision with the app and the rule that made it, for the reason the agent reads.
type Verdict struct {
	Decision Decision
	App      string
	Rule     string
}

// Reason is the verdict in words for the agent.
func (v Verdict) Reason(appName string) string {
	switch v.Decision {
	case DecideDeny:
		return "The app " + appName + " turned on in this chat does not allow this tool (" + v.Rule + "). Do it another way, within the app's tools, or ask the person to turn the app off."
	case DecideConfirm:
		return "The app " + appName + " asks the person to confirm this call (" + v.Rule + ")."
	}
	return ""
}

// Decide is the verdict of the apps on in a chat for one call.
func Decide(apps []*Manifest, call Call) Verdict {
	best := Verdict{}
	for _, m := range apps {
		if m == nil || m.Agent == nil || m.Agent.Tools == nil {
			continue
		}
		d, rule := decideOne(m.Agent.Tools, PluginName(m.ID), call)
		if d.rank() > best.Decision.rank() {
			best = Verdict{Decision: d, App: m.ID, Rule: rule}
		}
	}
	return best
}

// decideOne is one app's decision: the most specific matching rule.
func decideOne(t *ToolPolicy, plugin string, call Call) (Decision, string) {
	bestScore, bestDecision, bestRule := -1, DecideNone, ""
	try := func(rules []string, d Decision) {
		for _, rule := range rules {
			score, got := match(rule, d, plugin, call)
			if score < 0 {
				continue
			}
			if score > bestScore || (score == bestScore && got.rank() > bestDecision.rank()) {
				bestScore, bestDecision, bestRule = score, got, rule
			}
		}
	}
	try(t.Deny, DecideDeny)
	try(t.Confirm, DecideConfirm)
	try(t.Allow, DecideAllow)
	return bestDecision, bestRule
}

// match says how specifically a rule of decision d names the call, and what it decides: -1 when it does not name it,
// otherwise a score that grows with the literal part of the rule (an exact name beats a mask, a mask with a longer
// prefix beats a shorter one, mcp:* and * are the least specific). plugin is the rule's app as a plugin: an allow names
// an MCP server of a plugin only when the plugin is the app's own, so one app cannot open another's tools.
func match(rule string, d Decision, plugin string, call Call) (int, Decision) {
	switch rule {
	case "*":
		return 0, d
	case "mcp:*":
		if strings.HasPrefix(call.Tool, "mcp__") {
			return 1, d
		}
		return -1, d
	}
	if name, arg, ok := strings.Cut(rule, "("); ok && strings.HasSuffix(arg, ")") {
		if name != call.Tool {
			return -1, d
		}
		pattern := strings.TrimSuffix(arg, ")")
		score := 1000 + len(strings.TrimRight(pattern, "*"))
		if call.Tool != "Bash" {
			if !glob(pattern, ruleInput(call)) {
				return -1, d
			}
			return score, d
		}
		return matchCommand(pattern, d, score, ruleInput(call))
	}
	server, tool, from, isMCP := mcpParts(call.Tool)
	var name string
	switch {
	case !isMCP:
		name = call.Tool
	case server == hostServer && from == "":
		// Claude writes a tool's dots as '_': data_get is the rule app.data.get.
		name = "app." + strings.ReplaceAll(tool, "_", ".")
	case from != "" && from != plugin && d == DecideAllow:
		// Another plugin's server: this app's allow does not reach it.
		return -1, d
	default:
		name = server + "." + tool
	}
	if !strings.HasSuffix(rule, "*") {
		if rule == name {
			return 500 + len(rule), d
		}
		return -1, d
	}
	prefix := strings.TrimSuffix(rule, "*")
	if strings.HasPrefix(name, prefix) {
		return 10 + len(prefix), d
	}
	return -1, d
}

// matchCommand matches a Bash rule against each simple command of the line, so `git status && git push` meets
// Bash(git push*). A deny or a confirm holds when any command matches; an allow only when all do. A line the guard
// cannot see into (eval, sh -c, a command substitution) does not get a deny's silence: the person confirms it instead.
// This is a guardrail for the agent, not a sandbox: the shell has more ways to run a command than a guard can read.
func matchCommand(pattern string, d Decision, score int, line string) (int, Decision) {
	parts, opaque := commands(line)
	if d == DecideAllow {
		if opaque || len(parts) == 0 {
			return -1, d
		}
		for _, p := range parts {
			if !glob(pattern, p) {
				return -1, d
			}
		}
		return score, d
	}
	for _, p := range parts {
		if glob(pattern, p) {
			return score, d
		}
	}
	if opaque {
		return score - 1, DecideConfirm
	}
	return -1, d
}

// commands splits a shell line into its simple commands (on ;, &, |, newlines and the parentheses of a subshell),
// each without its leading variable assignments and with a path to the program cut to its name; opaque is true when
// a part runs text the split cannot read.
func commands(line string) (parts []string, opaque bool) {
	if strings.ContainsAny(line, "`") || strings.Contains(line, "$(") {
		opaque = true
	}
	split := strings.FieldsFunc(line, func(r rune) bool {
		switch r {
		case ';', '&', '|', '\n', '(', ')', '{', '}':
			return true
		}
		return false
	})
	for _, p := range split {
		words := strings.Fields(p)
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		for len(words) > 0 && (wrappers[base(words[0])] || isAssignment(words[0])) {
			if strings.ContainsAny(words[0], programMeta) {
				opaque = true
			}
			if wrappers[base(words[0])] {
				// A wrapper's own arguments (nice -n 10, timeout 5) hide where the program starts.
				opaque = true
			}
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		if strings.ContainsAny(words[0], programMeta) {
			// A quoted, escaped, expanded or globbed program name ("git", g\it, $G) is not the name the rules name.
			opaque = true
		}
		words[0] = base(words[0])
		switch words[0] {
		case "eval", "sh", "bash", "zsh", "xargs", "source", ".":
			opaque = true
		}
		parts = append(parts, strings.Join(words, " "))
	}
	return parts, opaque
}

// wrappers are the programs that run the rest of their line.
var wrappers = map[string]bool{"command": true, "exec": true, "env": true, "nohup": true, "time": true, "nice": true,
	"sudo": true, "doas": true, "timeout": true, "stdbuf": true, "setsid": true, "ionice": true, "chrt": true,
	"taskset": true, "flock": true, "unbuffer": true, "busybox": true, "script": true, "watch": true, "chroot": true}

// programMeta are the characters that make a program word other than what it reads: quotes, escapes, expansions and
// globs. A word with one is checked as written, before its path is cut.
const programMeta = `"'\$*?[~{`

// base is a program word without its path.
func base(w string) string {
	if i := strings.LastIndex(w, "/"); i >= 0 {
		return w[i+1:]
	}
	return w
}

func isAssignment(w string) bool {
	i := strings.IndexByte(w, '=')
	return i > 0 && !strings.ContainsAny(w[:i], "-/.")
}

// hostServer is the MCP server of the host's app.* tools.
const hostServer = "harnessly-app"

// mcpParts reads an MCP tool name: mcp__<server>__<tool>, where a plugin's server is plugin_<plugin>_<server> and
// plugin names it.
func mcpParts(tool string) (server, name, plugin string, ok bool) {
	rest, ok := strings.CutPrefix(tool, "mcp__")
	if !ok {
		return "", "", "", false
	}
	server, name, ok = strings.Cut(rest, "__")
	if !ok {
		return "", "", "", false
	}
	if p, ok := strings.CutPrefix(server, "plugin_"); ok {
		// plugin_<plugin>_<server>: a plugin name is the app's id with '.' as '-', which has no '_'.
		if pl, s, found := strings.Cut(p, "_"); found {
			server, plugin = s, pl
		}
	}
	return server, name, plugin, true
}

// ruleInput is what a rule in parentheses is matched against: Bash's command, a file tool's path, a fetch's address.
func ruleInput(call Call) string {
	for _, key := range []string{"command", "file_path", "notebook_path", "path", "url", "pattern"} {
		if v, ok := call.Input[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// glob matches s against a pattern where * stands for any run of characters; Claude's own "git push:*" is "git push*".
func glob(pattern, s string) bool {
	pattern = strings.ReplaceAll(pattern, ":*", "*")
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return s == pattern
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		j := strings.Index(s, parts[i])
		if j < 0 {
			return false
		}
		s = s[j+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// Snapshot is the tool policy a chat's launch was given: its apps' rules as they were at the launch, and the folders
// the agent must not write (the registry and the apps' packages), so editing a manifest or turning an app off from
// inside the chat does not loosen the rules of the agent that runs. The launch writes it next to its settings and the
// guard reads it there: the hook decides alone, with nothing to wait for.
type Snapshot struct {
	Apps      []SnapshotApp `json:"apps"`
	Protected []string      `json:"protected"`
}

// SnapshotApp is one app of a snapshot.
type SnapshotApp struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Tools *ToolPolicy `json:"tools,omitempty"`
	// Context are the items the context hook sends (context.go).
	Context []ContextItem `json:"context,omitempty"`
}

// The subcommands of `harnessly app` (with their aliases, internal/hlcli app.go) that change what is on: the agent of a
// chat with apps does not run them.
//
// Sharing is the person's word too: the agent publishes to dev and beta (publish), and only a person promotes to stable,
// rolls a channel back, installs for the team, or hands an app out by link.
var selfCommands = map[string]bool{"enable": true, "on": true, "disable": true, "off": true, "install": true,
	"dev": true, "remove": true, "rm": true, "uninstall": true,
	"promote": true, "rollback": true, "team": true, "public": true, "share": true, "unshare": true, "follow": true, "sync": true, "update": true}

// publishesWide says whether the words after `publish` aim it past dev and beta: at the team's catalog or a space (a
// --team or --space flag with any value, spelled `--team x` or `--team=x`, with one dash or two), or at any channel but
// an explicit dev or beta. The channel is read the way the CLI reads its flags: `--channel stable`, `--channel=stable`.
func publishesWide(words []string) bool {
	channel := ""
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") {
			if w == "stable" {
				return true
			}
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(w, "-"), "=")
		switch name {
		case "team", "space", "public":
			return true
		case "channel", "c":
			if !hasVal && i+1 < len(words) {
				i++
				val = words[i]
			}
			channel = val
			if channel != "dev" && channel != "beta" {
				return true
			}
		}
	}
	return channel == "stable"
}

// selfCommand says whether one simple command is harnessly's app command with a subcommand that changes the apps. It
// leans to refusing: with the shell's quotes and escapes taken off every word, a harnessly line with an app (or apps)
// word and, anywhere after it, a word of selfCommands is one.
func selfCommand(part string) bool {
	words := strings.Fields(part)
	for i, w := range words {
		words[i] = strings.Map(func(r rune) rune {
			if r == '"' || r == '\'' || r == '\\' {
				return -1
			}
			return r
		}, w)
	}
	seen, app := false, false
	for i, w := range words {
		switch {
		case !seen:
			seen = strings.HasSuffix(w, "harnessly")
		case w == "app" || w == "apps":
			app = true
		case app && selfCommands[w] && !(w == "dev" && words[i-1] == "--channel"):
			// The dev channel is named like the dev command (`publish --channel dev`): as the value of --channel it is not one.
			return true
		case app && w == "publish" && publishesWide(words[i+1:]):
			return true
		}
	}
	return false
}

// Decide is the snapshot's verdict for one call, with the reason the agent reads.
func (s Snapshot) Decide(call Call) (Verdict, string) {
	if rule := s.selfChange(call); rule != "" {
		v := Verdict{Decision: DecideDeny, Rule: rule}
		return v, "The apps of this chat are turned on and off by the person, and their files are not edited from " +
			"inside it (" + rule + "). Ask the person to do it."
	}
	manifests := make([]*Manifest, 0, len(s.Apps))
	names := map[string]string{}
	for _, a := range s.Apps {
		manifests = append(manifests, &Manifest{ID: a.ID, Agent: &Agent{Tools: a.Tools}})
		names[a.ID] = a.Name
	}
	v := Decide(manifests, call)
	return v, v.Reason(names[v.App])
}

// selfChange is the rule a call breaks when it would change the apps of the chat from inside it; "" for none.
func (s Snapshot) selfChange(call Call) string {
	switch call.Tool {
	case "Bash":
		line := ruleInput(call)
		parts, opaque := commands(line)
		for _, p := range parts {
			if selfCommand(p) {
				return "Bash(harnessly app …)"
			}
		}
		if strings.Contains(line, "harnessly") && (opaque || strings.ContainsAny(line, "$*?[{")) {
			return "Bash(harnessly …)"
		}
		for _, dir := range s.Protected {
			for _, form := range protectedForms(dir) {
				if strings.Contains(line, form) {
					return "Bash(" + dir + ")"
				}
			}
		}
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		// The file the tool would write is judged by where it really leads: a link from the chat's own folder into the
		// registry is the registry.
		path := ruleInput(call)
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		forms := []string{filepath.Clean(path)}
		if real, err := realPath(path); err == nil {
			forms = append(forms, real)
		}
		for _, dir := range s.Protected {
			for _, d := range protectedForms(dir) {
				for _, f := range forms {
					if within(d, f) {
						return call.Tool + "(" + dir + ")"
					}
				}
			}
		}
	}
	return ""
}

// protectedForms are the ways a protected folder is written in a path: as the launch named it and as it really is (the
// data folder can be reached through a link, and a command or a tool may use either).
func protectedForms(dir string) []string {
	if dir == "" {
		return nil
	}
	forms := []string{filepath.Clean(dir)}
	if real, err := realPath(dir); err == nil && real != forms[0] {
		forms = append(forms, real)
	}
	return forms
}
