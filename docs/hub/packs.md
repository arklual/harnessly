# Environment packs

An **environment pack** (`pack`) is a versioned folder of a git repository that an organization puts in its
environment as one item: tools for the chats' `PATH`, hooks of Claude Code, MCP servers, skills and the rules of the
safe the pack needs. It is how a company's own set (what used to be installed by hand from a private fork) reaches
the machines **without a new harnessly**: the pack is in its own repository, the hub only names it.

Related: [environment.md](environment.md) (the catalog, machine setup), [apps-guest-host.md](apps-guest-host.md) (apps
are a different kind: UI), [../safe.md](../safe.md) (the safe).

## The folder

The item's `source` is that of an `app`: a repository of the list (`repo`) or `repoUrl` (https) + `branch`, and `path`
(the pack's folder). The folder holds `harnessly/pack.json`:

```json
{
  "schema": 1,
  "id": "acme",
  "name": "Acme",
  "version": "1.0.0",
  "tools": [{ "name": "acme-cli", "path": "tools/cli.sh" }],
  "hooks": [{ "id": "guard", "event": "PreToolUse", "matcher": "Bash", "command": "python3 ${PACK}/hooks/guard.py", "timeout": 10 }],
  "mcp": "mcp.json",
  "skills": "skills",
  "safe": {
    "secrets": [{ "name": "acme-token", "why": "access to Acme", "optional": false }],
    "never": ["acme-token"]
  }
}
```

| Key | Meaning |
| --- | --- |
| `id` | `[a-z][a-z0-9-]{0,31}`; names the folder and the plugin `pack-<id>`. Two items with one id: the second fails |
| `version` | `x.y.z`; shown in the step's message («поставлен: acme 1.0.0») |
| `tools` | files of the folder put on the chats' `PATH` under `name` (Claude Code adds the plugin's `bin/`) |
| `hooks` | `event` is one of `hubproto.PackHookEvents`; `command` runs through the shell, `${PACK}` is the installed folder; `timeout` seconds (0 = 10) |
| `mcp` | a file `{"mcpServers": {…}}`; its servers come with the plugin |
| `skills` | a folder of `<name>/SKILL.md` |
| `safe.secrets` | secrets of the person's safe the pack's tools read; a missing one that is not `optional` is **asked for** (the safe's card, as for a script's `secrets`) and the step waits (pending) |
| `safe.never` | secrets no chat is to be given in its conversation; written to `harnessly-safe.json` next to the plugin |

Unknown keys, bad paths (`..`, absolute), links in the folder and folders over 2000 files / 16 MiB are refused. The
values of secrets never come with a pack.

## On the machine

Setup (`internal/envsetup/pack.go`) installs the folder as a Claude Code plugin at `<link>/environment/packs/<id>`:
`.claude-plugin/plugin.json` (`pack-<id>`, the version), `bin/`, `hooks/hooks.json`, `.mcp.json`, `skills/`,
`harnessly-safe.json` and `harnessly-pack.json` (the mark: item, version, hash of everything installed). The folder is
built beside and swapped in, so a chat never meets half of it. The runtime gives every such folder to the new chats as
`--plugin-dir`, beside the hub's environment plugin.

- **Updates without a core update.** The pack is the repository's current commit. A pack with `repoUrl` is looked at
  like a script of a repository outside the list (every half an hour, `heads.go`); a moved branch is a reason for a
  run. A run whose hash of the folder is the same says «уже стоит» and writes nothing.
- **Version.** The step's message goes to the hub's environment screen and `env.state`; the mark holds the same.
- **Taking off.** After a full run, pack folders whose item left the list are removed (only those with our mark).
- **Sandbox runs** and checks (`look`) install nothing.
- A machine does not run hooks of a chat that was already open: they reach the next chat.

## Rights and flags

- Only an organization's admins may add a pack (the layers of tenant and team are refused: a pack runs hooks on every
  tool call of the people's chats).
- The flag **`env.packs`** (per organization, off by default; `PUT /api/orgs/{org}/features/env.packs`, an instance administrator) must be on to create or re-source a pack
  item. Off: the endpoints answer `404 not_enabled`, and nothing is sent.
- The hub sends pack items only to machines whose `hello` has the feature **`environment.packs`**; an older agent gets
  the list without them (it could not hash it the same way).

## A company's own pack

Keep it outside harnessly, in a repository of its own (or a folder of an existing one), and add an item with
`repoUrl` and `path`. A new version is a commit there; bump `version` in `pack.json` when anything changes.
