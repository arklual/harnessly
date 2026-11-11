package apps

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// What a Codex chat's launch gets of the apps on in it (agents.AppLauncher.Codex). The rules, the snapshot, the token,
// the grant and the wall are the ones of a Claude Code chat; what differs is how Codex takes them:
//
//   - the guard is a `-c hooks.PreToolUse` entry on the launch line (its input and answer are Codex's, the hook says
//     `--agent codex`), so there is no settings file for the agent to edit;
//   - the apps' MCP servers are `-c mcp_servers.<name>.*` (Codex has no plugins): a server that uses a secret goes
//     through the runtime's proxy as for Claude; a local one that uses a secret is left out, since a value in a command
//     line is readable by every process of the machine;
//   - the instructions go with Codex's developer instructions, and the skills as a list of files to read first (Codex has
//     no skill loader of the apps' kind).

// codexServerName is a name Codex's config takes as a key without quotes.
var codexServerName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Codex is what a Codex chat with the apps adds to its launch.
func (l *Launcher) Codex(conversation string, ids []string) (agents.CodexApps, error) {
	var out agents.CodexApps
	protected, err := l.Registry.ProtectedDirs()
	if err != nil {
		return out, err
	}
	snap := Snapshot{Protected: protected}
	dir := filepath.Join(l.Registry.Root, "launch")
	var installed []Installed
	var prompt strings.Builder
	for _, id := range ids {
		a, err := l.Registry.Get(id)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		if a.Manifest == nil || !a.Manifest.HasTarget(TargetChat) {
			return out, fmt.Errorf("app %s does not work in a chat", id)
		}
		installed = append(installed, a)
		snap.Apps = append(snap.Apps, SnapshotApp{ID: a.ID, Name: a.Manifest.Name, Tools: toolsOf(a.Manifest), Context: ContextItems(a.Manifest),
			Hooks: a.Manifest.Allows("hooks.agent"), Strict: a.Manifest.Allows("hooks.agent") && a.Manifest.Allows("hooks.strict")})
		text, err := instructions(a)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		skills := skillList(a)
		if text != "" || skills != "" {
			fmt.Fprintf(&prompt, "## App: %s (%s)\n\n", a.Manifest.Name, a.ID)
			if text != "" {
				prompt.WriteString(text + "\n\n")
			}
			prompt.WriteString(skills)
		}
	}
	if prompt.Len() > 0 {
		out.Instructions = "# Apps on in this chat\n\nThe person turned these apps on in this chat. Keep to what each says; a tool the app " +
			"does not allow is refused by its guard, and a call the app asks the person to confirm is refused too (there is no " +
			"confirmation step here): do it another way within the app's tools.\n\n" + strings.TrimRight(prompt.String(), "\n") + "\n"
	}
	rawSnap, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return out, err
	}
	policy, err := writeByContent(dir, "policy", ".json", rawSnap)
	if err != nil {
		return out, err
	}
	out.Guard = l.guardCommand() + " --policy " + agents.ShellQuote(policy) + " --agent codex"
	if l.GuardProbe != nil {
		// Codex lets a call through when its hook fails, so the chat does not start without a guard that answers.
		if err := l.GuardProbe(out.Guard); err != nil {
			return out, fmt.Errorf("the apps' guard does not answer, so the chat does not start with apps on (%s): %w", out.Guard, err)
		}
	}
	token, session, sealDir := "", "", dir
	l.dropLegacyLaunchFiles(dir)
	if l.AgentToken != nil {
		file, tok, sess, err := l.agentTokenFile()
		if err != nil {
			return out, fmt.Errorf("the agent's token: %w", err)
		}
		token, session = tok, sess
		sealDir = l.sessionDir(session)
		if err := writeGrant(sealDir, ids, conversation); err != nil {
			return out, fmt.Errorf("the apps' grant: %w", err)
		}
		if err := writeChatStatus(sealDir, l.Registry.ChatStatuses(conversation)); err != nil {
			return out, fmt.Errorf("the documents' status: %w", err)
		}
		out.TokenFile = file
		out.Env = l.agentEnv(file)
	}
	if token != "" || l.Secrets != nil {
		l.sweepLaunches()
	}
	for _, a := range installed {
		servers, err := l.codexServers(a, token)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", a.ID, err)
		}
		out.Servers = append(out.Servers, servers...)
	}
	var own []string
	if token != "" {
		own = []string{sealDir}
	}
	wrap, err := l.wall(ids, snap.Protected, token != "", own)
	if err != nil {
		return out, err
	}
	out.Wrap = wrap
	return out, nil
}

// skillList names the skills of an app as files to read, for an agent that has no skill loader of the apps' kind.
func skillList(a Installed) string {
	if a.Manifest.Agent == nil || len(a.Manifest.Agent.Skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Skills of this app: read the file of a skill before you use it.\n\n")
	for _, s := range a.Manifest.Agent.Skills {
		fmt.Fprintf(&b, "- skill %s: %s (read it before using)\n", s, filepath.Join(a.Dir, "skills", s, "SKILL.md"))
	}
	b.WriteString("\n")
	return b.String()
}

// codexServers are the MCP servers of an app's package as Codex is told about them, by name.
func (l *Launcher) codexServers(a Installed, token string) ([]agents.CodexAppServer, error) {
	pkg, _ := CachedPackage(a.Dir)
	if pkg == nil || len(pkg.MCP) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(pkg.MCP))
	for name := range pkg.MCP {
		names = append(names, name)
	}
	sort.Strings(names)
	origin, tokenHeaders := "", []string(nil)
	var out []agents.CodexAppServer
	for _, name := range names {
		decl := pkg.MCP[name]
		full := "plugin_" + PluginName(a.ID) + "_" + name
		if !codexServerName.MatchString(full) {
			jsonlog.Log("warn", "apps: a server is left out of the Codex launch: its name is not a key Codex takes", jsonlog.F{K: "app", V: a.ID}, jsonlog.F{K: "server", V: name})
			continue
		}
		srv := agents.CodexAppServer{Name: full}
		if secrets := decl.Secrets(); len(secrets) > 0 {
			listed := true
			for _, n := range secrets {
				listed = listed && a.Manifest.hasSecret(n)
			}
			if !listed || token == "" || decl.Command != "" {
				jsonlog.Log("info", "apps: a server that uses a secret is left out of the Codex launch (a local one is never given a value on a command line)",
					jsonlog.F{K: "app", V: a.ID}, jsonlog.F{K: "server", V: name})
				continue
			}
			if origin == "" {
				origin, tokenHeaders = l.proxyOrigin()
			}
			if origin == "" || len(tokenHeaders) == 0 {
				jsonlog.Log("warn", "apps: a server that uses a secret is left out: the runtime's address is unknown", jsonlog.F{K: "app", V: a.ID})
				continue
			}
			srv.URL = origin + ProxyPrefix + a.ID + "/" + name
			srv.TokenHeaders = tokenHeaders
			out = append(out, srv)
			continue
		}
		srv.URL, srv.Command, srv.Args, srv.Env, srv.Headers = decl.URL, decl.Command, decl.Args, decl.Env, decl.Headers
		if srv.URL == "" && srv.Command == "" {
			continue
		}
		out = append(out, srv)
	}
	return out, nil
}
