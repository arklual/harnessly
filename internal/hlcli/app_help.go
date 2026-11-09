package hlcli

// `harnessly app <sub> --help` and a bare `harnessly app` print help, never an error. The texts follow the
// APPS section of helpText; commandHelp["app <sub>"] is what the command line looks up.

// appSubHelp is the usage of each subcommand of `harnessly app`: the line, then what it does.
var appSubHelp = map[string]string{
	"docs": `harnessly app docs [PAGE] | export DIR

The guide to writing apps, built into this binary (needs no runtime): start with the first chapter, "From zero to a working
view in 5 minutes". "export DIR" writes the guide and the example packages into DIR, to search and copy.
`,
	"catalog": `harnessly app catalog [SECTION]

The targets, contributions, rights, bridge methods and agent tools an app can use. With a section's name, only that one.
`,
	"sdk": `harnessly app sdk DIR

Writes the SDK files (app-sdk.js, app-sdk.d.ts) into DIR: for a view on the app's own server (an https entry), which serves them itself.
For a package view the SDK is already at /__hl/app-sdk.js; for types in the editor use the npm package @harnessly/app-sdk.
`,
	"new": `harnessly app new ID [--template chat|pane-chat|widget|vite] [--name NAME] [--dir DIR]

Writes a new package that passes validate as it is, e.g. harnessly app new acme.cashback --template pane-chat.
  chat       instructions and a skill for the agent, no view
  pane-chat  a view in the right pane (and a screen of its own) in the look of harnessly, with loading and error states
  widget     a card the agent shows in the chat feed
  vite       a view built with Vite + React + TypeScript into ui/
`,
	"validate": `harnessly app validate [DIR] [--strict]

Checks the package; each problem names its field. --strict: warnings fail too. "app check" is the same command.
`,
	"install": `harnessly app install [DIR] | --channel C ID | --team ID [--channel stable]

Installs a copy of the package on this machine (later edits do not reach it). With --channel, follows a channel of the
catalog; with --team an owner installs the catalog's app for the whole team. Rights are shown before they are given.
`,
	"dev": `harnessly app dev [DIR]

Installs the folder itself for development: a chat started after an edit gets it. A view reloads on its own; a changed
manifest or instructions.md is read by a chat that starts after it.
`,
	"list": `harnessly app list [--json]

The installed apps: id, version, name, targets; "dev <dir>" marks the ones installed from a folder.
`,
	"remove": `harnessly app remove ID

Uninstalls an app from this machine.
`,
	"enable": `harnessly app enable ID

Turns an app on in the chat this tab runs; the agent gets it when it starts next. "app disable ID" turns it off.
`,
	"disable": `harnessly app disable ID

Turns an app off in the chat this tab runs. "app enable ID" turns it on.
`,
	"connect": `harnessly app connect ID CONNECTION [--off]

Gives the app one of the person's connections (a tracker, a wiki, a code host), or takes it back with --off.
`,
	"explain": `harnessly app explain ID

Why the app is on or off here, layer by layer: machine, team, setup, activation, this chat, the wall; ✗ marks the layer that keeps it off.
`,
	"logs": `harnessly app logs ID [--limit N]

What the app's views reported: console errors, exceptions, failed bridge calls (the last N, 50 by default).
`,
	"publish": `harnessly app publish [DIR] --channel dev|beta|stable [--team]

Publishes a new, immutable version on a channel. --team: also to the team's catalog.
`,
	"promote": `harnessly app promote VERSION --channel beta|stable [DIR]

Moves a channel up to a version (stable asks the evals). See also: app rollback, app channels.
`,
	"rollback": `harnessly app rollback --channel C [ID]

Moves a channel back to its previous version.
`,
	"channels": `harnessly app channels [ID]

Where the channels of an app (dev, beta, stable) point.
`,
	"update": `harnessly app update [--yes] [ID]

Brings the installs that follow a channel to what the channel points at now. Without ID, all of them.
`,
	"team": `harnessly app team [list|publish|uninstall]

The team's catalog: what an owner has published to everyone, and what is installed for the whole team.
`,
	"public": `harnessly app public [list [QUERY]] | publish ID VERSION | install ID [--yes]

The public catalog: browse, put a version on stable in it, install for yourself (the rights are shown first).
`,
	"share": `harnessly app share ID [--view main] [--ttl 7d] [--write]

Makes a link to a standalone app (target "app"): opens with no account, no chat, no agent. --write lets the guest change
the app's data. See also: app shares, app unshare.
`,
	"shares": `harnessly app shares

The links made with app share and when they end.
`,
	"unshare": `harnessly app unshare LINK

Revokes a link made with app share.
`,
	"eval": `harnessly app eval [DIR] [--runs N] [--case NAME] [--threshold 0.8] [--no-baseline] [--no-judge]

Runs evals/*.json on a real agent (needs a Claude Code login); exits 1 under the threshold. The report is left in .harnessly/eval.json.
`,
	"mock-mcp": `harnessly app mock-mcp FIXTURE.json

Serves an MCP server from a fixture file, the way an eval mocks the app's own server.
`,
}

// appSubNames lists every subcommand of `harnessly app`, for the usage line of a mistyped one.
const appSubNames = "docs, catalog, sdk, new, validate (check), install, dev, list, remove, enable, disable, connect, explain, " +
	"publish, promote, rollback, channels, update, share, shares, unshare, team, public, eval or mock-mcp"

// appHelpAliases are the names that print another subcommand's help.
var appHelpAliases = map[string]string{"check": "validate", "ls": "list", "rm": "remove", "uninstall": "remove", "on": "enable",
	"off": "disable", "why": "explain"}

func init() {
	for sub, text := range appSubHelp {
		commandHelp["app "+sub] = text
	}
	for alias, sub := range appHelpAliases {
		commandHelp["app "+alias] = appSubHelp[sub]
	}
}

// appHelpFor is the help a call of `harnessly app` asks for: "" when it asks for none. A bare `app` asks for the whole help.
func appHelpFor(args []string) string {
	if len(args) == 0 {
		return commandHelp["app"]
	}
	sub, rest := args[0], args[1:]
	asked := func(a string) bool { return a == "--help" || a == "-h" || a == "help" }
	if asked(sub) {
		if len(rest) > 0 {
			if t, ok := commandHelp["app "+rest[0]]; ok {
				return t
			}
		}
		return commandHelp["app"]
	}
	for _, a := range rest {
		if asked(a) {
			if t, ok := commandHelp["app "+sub]; ok {
				return t
			}
			return commandHelp["app"]
		}
	}
	return ""
}
