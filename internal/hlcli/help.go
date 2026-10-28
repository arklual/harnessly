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
    https://github.com/<owner>/<repo>/pull/12   a pull request, commit or issue (GitHub), same tab
    PROJ-123                        a tracker task, when the deployment has a tracker
    files|changes|terminal [path]   a tab by kind; other kinds: overview tools browser disk artifacts gitlab
                                    subagents economics plan peers design side-chat gallery task

  harnessly deliver <path> [--title T]  hand a file to the person (zip, docx, pdf, md ...): a card in the chat that
                                    opens it on the right and downloads it. Only files the person asked to receive.

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

OTHER AGENTS (this machine and the fleet's other machines)
  harnessly peers                   the chats you may write to: REF, busy|idle|gone, owner · machine, title
  harnessly peers send REF TEXT...  a message to that chat's agent ("-" reads it from stdin). Prints "delivered → …",
                                    or exits 1 with "refused: <reason> — <why>". The other agent reads it as a
                                    <peer-message> with your machine, owner and chat, and answers by building the same command from its ref.

TELEGRAM NOTIFICATIONS (the person's Telegram, connected in Settings → Notifications)
  harnessly notify                  the person's Telegram and any bots of their own: chat, and the person's instruction
                                    of when to write
  harnessly notify send [--bot NAME] [--silent] TEXT...
                                    a message to the person's Telegram, or to the chat of their bot NAME ("-" reads it
                                    from stdin). **bold**, ` + "`code`" + `, code blocks and [links](https://…) are kept;
                                    long text goes as several messages. Write only as the instruction says. Prints
                                    "sent → …", or exits 1 with "refused: <reason> — <why>".

PUBLISH A SITE
  harnessly site deploy <dir> --name NAME   upload a folder (a build's dist/) as a site and print its address
  harnessly site list|versions|rollback    see the sites, their versions, go back to an earlier one
                                    needs --url (the hub) and a deploy token; harnessly site --help has the rest

SECRETS (the safe of this machine: tokens and keys the person put in, outside the chat)
  harnessly secret list             the names in the safe and when each was put and last used (never a value)
  harnessly secret ask NAME --why "what it is for" [--replace]
                                    ask the person for a secret: they get a card to put it in the safe; waits and
                                    prints "ready: NAME" ("waiting": run it again). Never ask for a secret in the chat.
  harnessly secret run --env VAR=NAME [--file VAR=NAME]... -- COMMAND ARGS...
                                    run a command with secrets in its environment (--file: VAR is the path of a
                                    temporary file with the value); the values are masked in its output
  harnessly secret file NAME [--ttl 15m]
                                    print the path of a short-lived file (0600) with the value, for a tool that wants
                                    a path. There is no command that prints a value.

ASK A PERSON (through the same bots; the person sets who and where in Settings → Notifications)
  harnessly ask people              the people you may tag, who may approve, the chats, the default wait
  harnessly ask --to NAME [--option A --option B] [--details TEXT] [--timeout 30m] QUESTION...
                                    asks in the chat with the person tagged and waits for the answer: prints
                                    "asked ask_… → …" at once, then "answered by @x: …". Exit 4: no answer in time
  harnessly ask approve --to NAME [--link URL] [--details TEXT] WHAT...
                                    an approval with buttons: exit 0 approved, 3 changes requested or only a comment
  harnessly ask wait|status|cancel ask_…   wait again, show the replies (new ones marked), call it off

DESIGN PROJECTS
  harnessly design page list <project>        the pages of a design project: id, title, board file
  harnessly design page create <project> --title TITLE [--file NAME.dc.html] [--route /path] [--variant-of PAGE]
                                    a new page: a permanent id and a board file made from the project's board template
  harnessly design page rename <project> <page> [--title TITLE] [--file NAME.dc.html]
                                    a new title (and file name); the id, and the page's claims, stay
  harnessly design page remove <project> <page>
                                    the board file and the page go to the library's .trash/
  harnessly design notes get <project> [<page>]
                                    a presentation's speaker notes: every slide with its length, or one slide's text
  harnessly design notes set <project> <page> [--file PATH|-]
                                    save a slide's speaker notes (the text from a file or stdin)
  harnessly design kit install ID VERSION TARBALL|URL    install a UI kit version for boards ("help design kit")
  harnessly design kit list                              the installed kits
  harnessly design timeline DESIGN [--from T] [--to T] [--around T --window 30s]
                                    what happened in the person's preview of the design: pages, clicks, scrolling,
                                    tweaks, console, slow requests, long tasks, dropped frames (default: last 5 minutes)
  harnessly design moment DESIGN T [--window 5s]
                                    one time in detail: page, viewport, last actions, errors, requests, jank around it.
                                    T is RFC3339, HH:MM:SS (today), -2m (ago) or now
  harnessly design migrate [--dry-run] [--library DIR] [<project>]
                                    give every project of a library its meta/ (pages with ids); board files untouched
  harnessly design kit install ID VERSION TARBALL|URL    install a UI kit version for boards ("design kit --help")
  harnessly design kit list                              the installed kits
  harnessly design engine fetch     fetch the board engine with your own Claude account ("design engine --help")
  harnessly design engine status    what this machine has for designs: engine, browser, ffmpeg
  <page> is a page's id, its file or its title. project/canvas.json is built from meta/: do not edit it by hand.

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
  harnessly open design landing                                    a design of this machine's library (its id,
                                                                   its folder or a file in it, or its title)
  harnessly open files                                             a tab by kind, no target

Only the addresses of the workspace's own documents and boards, ports of this machine and feature stands open in the
pane's browser; the runtime says what it refused and why.
`,
	"deliver": `harnessly deliver <path> [--title TITLE]

Hand a file to the person. The conversation gets a card with the file's name and size: a tap opens it in the right pane
(a zip shows what is inside, with each file to download; md and html open as documents; pdf and pictures as they are),
the icon beside it downloads the file. The right pane opens it by itself once.

  harnessly deliver ./report.zip
  harnessly deliver /tmp/contract.docx --title "Contract, final"

Deliver only a file the person asked to receive ("put it in a zip", "send me the docx"), not every file you wrote or
changed: those stay in the Files tab and make no card. The file must lie inside the workspace (or /tmp).
Run it again after you change the file, to hand over the new version.
`,
	"secret": `harnessly secret list|ask|run|file

The safe keeps the tokens, passwords and keys the person gave the agents of this machine, encrypted, and hands them to
commands by name. The person puts a secret in Settings → Safe, or on the card your ask puts on their screen.

  harnessly secret list
  harnessly secret ask github-token --why "push the release branch to GitHub"
  harnessly secret ask openai-key --why "the old key is revoked" --replace
  harnessly secret run --env GITHUB_TOKEN=github-token -- gh pr create --fill
  harnessly secret run --env TOKEN=gitlab-token -- sh -c 'curl -sf -H "PRIVATE-TOKEN: $TOKEN" https://gitlab.example/api/v4/user'
  harnessly secret run --file KUBECONFIG=kubeconfig -- kubectl get pods
  harnessly secret file deploy-key --ttl 10m

ask waits up to 100 seconds (--wait N, 0 to 600) and prints "ready: NAME", "waiting: ..." (run it again to keep
waiting), or exits 1 with "declined". A name the safe already has is ready at once, unless --replace.
run passes the command's exit code on and masks the values in its output; quote $VAR inside sh -c, never echo it.
Never print a value, write it into the workspace or a commit, or ask the person to paste it into the chat.
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
	"design": `harnessly design page list|create|rename|remove, harnessly design notes get|set, harnessly design migrate

A design project with meta/ keeps one file per page, meta/pages/<id>.json, under a permanent id (a ULID); its
project/canvas.json and artifact.json are built from meta/ by the runtime. Change the pages with these commands:

  harnessly design page list shop-admin
  harnessly design page create shop-admin --title "Payouts" --route /payouts
  harnessly design page create shop-admin --title "Payouts, dark" --variant-of "Payouts"
  harnessly design page rename shop-admin 01JB8Z6E7XQ4M2N3P5R6S7T8V9 --title "Payouts list"
  harnessly design page remove shop-admin "Payouts, dark"

A presentation (a project of the kind slides) keeps each slide's speaker notes in meta/notes/<page>.md. Read and write
them with the commands, not by hand: a slide is named by its id, board file or title.

  harnessly design notes get shop-deck
  harnessly design notes get shop-deck "Pricing"
  harnessly design notes set shop-deck "Pricing" --file -    (the text on stdin)

design migrate works on the library folder itself, without the runtime: every project without meta/ gets one built
from what the library shows now (same boards, order, titles, sizes). It writes only meta/, project/canvas.json and
artifact.json, never a board or an asset, and gives the same ids however often it runs. Use --dry-run first.

  harnessly design migrate --dry-run --library /srv/design
  harnessly design migrate --library /srv/design
`,
	"hook": `harnessly hook design-guard | design-release

design-guard is a Claude Code PreToolUse hook: reads the hook's JSON on stdin and refuses a Write, Edit or shell command
that writes project/canvas.json of a design project, telling the agent to use "design page". It also takes (or renews)
the chat's lease on a board (*.dc.html) the call writes, and refuses with the holder's name when another chat holds it,
or when this chat's own lease was gone meanwhile (read the file again). Prints nothing otherwise.
design-release is the Stop hook: the turn ended, the chat lets go of its leases. See docs/design-pages.md for the
settings.json entries.
`,
	"peers": `harnessly peers [list] | harnessly peers send REF TEXT...

  harnessly peers
  harnessly peers send dev-department/tab-3f9c "Готово: модель по умолчанию берётся из настроек, выкатил 1.131.0"
  printf '%s' "$LONG_TEXT" | harnessly peers send runner-2/tab-1a2b -

REF is <machine>/<conversation id> as the list prints it (a bare conversation id is this machine's). A message arrives
as <peer-message person=… machine=… chat=… ref=…>…</peer-message>: build the answer yourself from its ref, as
"harnessly peers send <ref> …"; the envelope carries no command to copy. The
other side decides who may write to it (Settings → Machines → «Связь между агентами»); a refusal says why.
`,
	"notify": `harnessly notify [list] | harnessly notify send [--bot NAME] [--silent] TEXT...

  harnessly notify
  harnessly notify send "Готово: миграция прошла, MR !231 ждёт вашего ревью"
  printf '%s' "$REPORT" | harnessly notify send --silent -
  harnessly notify send --bot "Алерты" "Готово"

Without --bot the message goes to the Telegram the person connected (harnessly's own bot writes it). NAME is a bot of
the person's own, where those are allowed: its name as the list prints it (any case), its @username or its id. No token
is ever on your side; the message is the bot's, not yours, so write it to the person: short, what happened and what is
needed from them. Follow the person's instruction of when to write, and never send routine progress. At most 20
messages a minute; 16000 characters at most.
`,
	"ask": `harnessly ask [approve] --to NAME [--bot BOT] [--option TEXT]... [--details TEXT] [--link URL] [--timeout D] QUESTION...
harnessly ask people | list | wait ID | status ID | cancel ID

  harnessly ask --to Anna "Which port does the staging API listen on? I see 8080 and 3000 in the configs"
  harnessly ask --to @lead --option "Ship today" --option "Wait for QA" "The fix is ready, CI is green. Ship now?"
  harnessly ask approve --to Lead --link https://example.com/rfc/42 "RFC: move sessions to the new store"

Ask when you are truly stuck on something only a person knows or must decide: not for routine progress, not for
what you can find out yourself. The question goes to a Telegram chat through a bot the person linked, with the person
tagged (NAME is a contact from "harnessly ask people", or any @username unless the person allowed only contacts);
they answer with a button, a reply, or in harnessly's page. Write it to them: one question, the context they need to
answer without opening anything, the options you see.

The command waits (up to --timeout: minutes or 30m/2h; the person's default otherwise) and prints the answer.
In Claude Code run it as a background Bash command (run_in_background) and go on with what does not depend on it:
you are told when it ends, and the person sees "waiting for an answer from …" beside the chat. Stopping that task
calls the question off. --no-wait prints the id only; "harnessly ask wait ID" waits for it later.
Exit codes: 0 answered / approved, 3 approval not given (read the comment), 4 no answer in time or called off,
1 refused (the reason says why: rate limit, no bot, unknown person…). Without an answer, decide yourself if it is safe
and say so, or stop and leave the question for the person.
`,
	"workspaces": "harnessly workspaces\n\nList the workspaces: id, name and folders. -w takes the id.\n",
	"api-guide":  "harnessly api-guide\n\nPrint the runtime's HTTP API for agents: the /api/cli routes with their payloads.\n",
}
