package hlcli

const helpText = `harnessly - the workspace's command line for agents

You are running in a tab of harnessly: a workspace where a person works next to coding agents. This command shows
things to that person in the right pane of the conversation, and lets you look at and control the other tabs of the
workspace. It finds the runtime on this machine by itself: no setup, no login.

SHOW THINGS TO THE PERSON
  harnessly open <target>           open it in the right pane; the person sees it at once, or when they come back
  harnessly panel show|hide         show or hide the whole right pane
  harnessly status                  what you asked the pane for, what the person has seen, what the pane shows

  <target> is one of:
    https://<docs host>/a/<slug>    a published document        (a link you got from the artifacts CLI)
    https://<boards host>/b/<slug>?k=<key>   a board            (a link you got from the boards helper)
    4400, localhost:4400/path       a dev server on this machine, in the pane's browser
    /home/me/work/report.md         a file (relative paths are taken from the current folder)
    https://<host>/<group>/<project>/-/merge_requests/12   a merge request or commit with its diff (GitLab)
    PROJ-123                        a tracker task, when the deployment has a tracker
    files|changes|terminal [path]   a tab by kind; other kinds: overview tools browser disk artifacts gitlab
                                    subagents economics plan peers design side-chat gallery task

  Documents and boards you publish usually open by themselves once your answer names their address; use open
  for everything else, or when the person asks to see something again.

TABS OF THIS WORKSPACE
  harnessly workspaces              list workspaces (id, name, folders)
  harnessly tab list                list the tabs of your workspace
  harnessly tab create [-t TYPE] [-n NAME] [--prompt TEXT] [--model M] [--effort E]
                                    create a tab. TYPE: terminal (default), claude-code, codex-cli, agent-sessions,
                                    web-browser, diff. With --prompt, a claude-code or codex-cli tab starts the agent
                                    with that task the platform's way (hooks, status, workspace prompt included).
                                    Prints the new tab's id.
  harnessly tab send TAB_ID TEXT... send text into a tab ("-" reads it from stdin)
  harnessly tab status TAB_ID       is it alive, what runs in it, is the agent working or waiting
  harnessly tab result TAB_ID       the tab's visible screen: read what a delegated agent answered
  harnessly tab close TAB_ID        close a tab

HTTP API
  harnessly api-guide               the runtime's HTTP API for agents (paths and payloads)

OPTIONS (anywhere on the line)
  -w, --workspace ID    the workspace to act in (default: the one your own tab belongs to)
  --json                print the runtime's answer as JSON

Start another agent only with "tab create -t claude-code|codex-cli --prompt TEXT": a "claude ..." or "codex ..."
typed into a shell tab with "tab send" starts it without the platform's hooks and its state is unreliable.

Exit codes: 0 done, 1 refused or failed (the reason is printed), 2 wrong usage.
`

var commandHelp = map[string]string{
	"open": `harnessly open <target>

Open something in the right pane of your conversation. The request is kept with the conversation: it works while the
person's page is closed, and the pane opens it when they come back, on any device. A tab the person closed stays
closed: each request opens once.

  harnessly open https://docs.example.com/a/release-plan          a document
  harnessly open 'https://boards.example.com/b/arch?k=abc123'      a board
  harnessly open 4400                                              a dev server of this machine (localhost:4400)
  harnessly open ./report.md                                       a file
  harnessly open changes /home/me/work/repo                        the changes tab for a folder
  harnessly open files                                             a tab by kind, no target

Only the addresses of the workspace's own documents and boards, ports of this machine and feature stands open in the
pane's browser; the runtime says what it refused and why.
`,
	"tab": `harnessly tab list|create|send|status|result|close

  harnessly tab list
  harnessly tab create -t codex-cli -n review --prompt "Review the diff in /home/me/work/repo and list problems"
  harnessly tab status <tab id>
  harnessly tab result <tab id>
  harnessly tab send <tab id> "also check the tests"
  harnessly tab close <tab id>

The tab id is the first column of "tab list" and what "tab create" prints. Types: terminal, claude-code, codex-cli,
agent-sessions, web-browser, diff (claude and codex are accepted for the agents).
`,
	"panel": `harnessly panel show|hide

Show or hide the whole right pane for the person.
`,
	"status": `harnessly status

What you asked the pane for in the last day (and whether the person has seen it) and, when their page is open, which
tabs the pane has and which is in front.
`,
	"workspaces": "harnessly workspaces\n\nList the workspaces: id, name and folders. -w takes the id.\n",
	"api-guide":  "harnessly api-guide\n\nPrint the runtime's HTTP API for agents: the /api/cli routes with their payloads.\n",
}
