package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agentskills"
	"github.com/ly-solutions/harnessly/internal/hooksetup"
	"github.com/ly-solutions/harnessly/internal/integrations"
)

// The skills of the workspace (RUNTIME_AGENT_SKILLS, default on): the harnessly command's skill, boards and artifacts for
// Claude Code and Codex, written into harnessly's own folders when the runtime starts, and a default workspace prompt
// that tells both agents what they have. See docs/integrations.md.

// agentBinDir is the folder of launchers (artifacts, boards, harnessly) on the agents' PATH: RUNTIME_AGENT_BIN_DIR, else
// ~/.local/bin, which login shells put on the PATH when it exists.
func agentBinDir(home string) string {
	if v := os.Getenv("RUNTIME_AGENT_BIN_DIR"); v != "" {
		return v
	}
	return filepath.Join(home, ".local", "bin")
}

func codexHomeDir(home string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

// paneBinDir is RUNTIME_PANE_BIN_DIR, else the launchers' folder while the skills are on.
func paneBinDir(home string) string {
	if v := os.Getenv("RUNTIME_PANE_BIN_DIR"); v != "" {
		return v
	}
	if agentskills.Enabled(os.Getenv) {
		return agentBinDir(home)
	}
	return ""
}

// provisionAgentSkills writes the skills for the services this runtime is configured with. Failures are logged: agents
// start without the skills they could not get.
func provisionAgentSkills(dataDir string) {
	if !agentskills.Enabled(os.Getenv) {
		agent.Log("info", "agent skills are off (RUNTIME_AGENT_SKILLS)")
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		agent.Log("warn", "agent skills not installed: no home folder", agent.F{K: "err", V: err.Error()})
		return
	}
	integ := integrations.FromEnv(os.Getenv)
	opts := agentskills.Options{
		DataDir: dataDir, CodexHome: codexHomeDir(home), BinDir: agentBinDir(home),
		Boards: integ.BoardsURL != "", Artifacts: integ.ArtifactsURL != "",
	}
	if exe, err := os.Executable(); err == nil {
		opts.Harnessly = filepath.Join(filepath.Dir(exe), "harnessly")
	}
	res, err := agentskills.Provision(opts)
	if err != nil {
		agent.Log("warn", "agent skills partly not installed", agent.F{K: "err", V: err.Error()})
	}
	for _, p := range res.Kept {
		agent.Log("info", "agent skills: left a file that is not harnessly's or was edited", agent.F{K: "file", V: p})
	}
	if res.Changed() {
		agent.Log("info", "agent skills installed", agent.F{K: "written", V: len(res.Written)}, agent.F{K: "removed", V: len(res.Removed)})
	}
}

// skillsPluginDir is the Claude plugin folder, "" while the skills are off.
func skillsPluginDir(dataDir string) string {
	if !agentskills.Enabled(os.Getenv) {
		return ""
	}
	return agentskills.ClaudePluginDir(dataDir)
}

// defaultWorkspacePrompts are the prompts of both agents when the deployment names none (RUNTIME_WORKSPACE_PROMPTS_FILE):
// where they are, the harnessly command, and the services they were given.
func defaultWorkspacePrompts() hooksetup.Prompts {
	integ := integrations.FromEnv(os.Getenv)
	return hooksetup.Prompts{
		"claude": workspacePromptText("Claude Code", integ),
		"codex":  workspacePromptText("Codex", integ),
	}
}

func workspacePromptText(agentName string, integ integrations.Config) string {
	var b strings.Builder
	b.WriteString("# harnessly workspace\n\n")
	b.WriteString("You are " + agentName + ", running in a tab of harnessly: a workspace where a person works next to coding agents. " +
		"The conversation is on the left; a right pane beside it shows what you make (documents, boards, a browser, files, changes); " +
		"other tabs run terminals and other agents.\n\n")
	b.WriteString("- **Workspace ID**: `{{workspaceId}}`\n\n")
	b.WriteString("## The `harnessly` command\n\n")
	b.WriteString("It is on your PATH and finds the workspace by itself; there is nothing to log in to.\n\n")
	b.WriteString("```bash\n" +
		"harnessly open <link|file|port>   # show it to the person in the right pane (a document, a board, ./report.md, 4400)\n" +
		"harnessly tab list                # the tabs of this workspace\n" +
		"harnessly tab create -t codex-cli --prompt \"<the whole task>\"   # start another agent (claude-code | codex-cli | terminal)\n" +
		"harnessly tab status <id>         # is it working or waiting\n" +
		"harnessly tab result <id>         # what its screen shows: read its answer\n" +
		"harnessly --help                  # everything, with examples\n" +
		"```\n\n")
	b.WriteString("Show the person what they should look at: after you publish a document or draw a board, or when a dev server is running, " +
		"`harnessly open` it. Start another agent only with `tab create --prompt`; a `claude` or `codex` command typed into a shell tab " +
		"starts it without the workspace's hooks.\n\n")
	b.WriteString("## How your answers render\n\n")
	b.WriteString("The conversation draws two fenced blocks instead of showing them as code: a block marked `mermaid` becomes a diagram " +
		"(flowchart, sequence, state, ER, gantt), and a block marked `html` becomes a page in a sandboxed frame, with a toggle to its source. " +
		"Use them when a picture or a small page explains better than text. The page is one self-contained file: inline CSS, " +
		"images as `data:` URIs; it has no network access and your scripts do not run in it. Keep a diagram to what fits on a screen. " +
		"A link to a document or board alone on its line shows as a card.\n\n")
	b.WriteString(delegationPromptText(agentName))
	if integ.ArtifactsURL != "" || integ.BoardsURL != "" {
		b.WriteString("## Documents and boards\n\n")
		if integ.ArtifactsURL != "" {
			b.WriteString("- Documents: the `artifacts` command (catalog at " + integ.ArtifactsURL + "). Search before you write " +
				"(`artifacts find …`), publish with `artifacts publish --file report.md --title \"…\"`, update the same slug for a new version. " +
				"Give the person the link it prints.\n")
		}
		if integ.BoardsURL != "" {
			b.WriteString("- Boards: the `boards` command (server at " + integ.BoardsURL + "). Read a board from its link with `boards read <link>`, " +
				"draw a diagram with `boards create \"<title>\" graph.json`, edit with `boards edit <link> -`.\n")
		}
		b.WriteString("- Their tokens are already in your environment (`ARTIFACTS_URL`, `ARTIFACTS_TOKEN`, `BOARDS_URL`, `BOARDS_TOKEN`); never print them.\n\n")
	}
	b.WriteString("## Skills\n\n")
	b.WriteString("Skills for the above are installed for you: `harnessly`")
	if integ.BoardsURL != "" {
		b.WriteString(", `boards`")
	}
	if integ.ArtifactsURL != "" {
		b.WriteString(", `artifacts`")
	}
	b.WriteString(". Read a skill before its first use in a conversation.\n")
	return b.String()
}

// delegationPromptText is when to hand work to another agent through the MCP server's delegation tools, and when to keep
// it in the agent's own session.
func delegationPromptText(agentName string) string {
	own := "Your own subagents (the Agent tool) stay the way for a quick subtask of your own kind inside this session: " +
		"a search, a review, a parallel edit whose result only you need."
	if agentName == "Codex" {
		own = "Work you can finish yourself in a few steps stays in this session."
	}
	return "## Delegating to another agent\n\n" +
		"The `harnessly` MCP server has `delegate_task`: it starts another agent — Claude Code or Codex, any model of this machine — " +
		"in a new chat the person sees nested under this one, and `read_conversation`, `send_to_conversation`, `wait_conversation` " +
		"and `list_children` follow it up. Use it when the work wants another provider or model than yours, runs long and the person " +
		"should be able to watch it, or is work the person may take over. " + own + " " +
		"A delegated agent sees nothing of this chat: give it the whole task. Pass `notify: true` to be woken with its answer, " +
		"or `wait_conversation` for it; check what it reports before you build on it.\n\n"
}
