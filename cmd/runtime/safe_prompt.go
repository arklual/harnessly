package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// connectionsPromptBlock tells every agent where the person's trackers and wikis are and how to read them without
// their tokens (Settings → Connections, internal/connections).
const connectionsPromptBlock = `## Trackers and wikis (connections)

The person's trackers, wikis and code hosts (Jira, Confluence, YouTrack, Linear, GitLab, GitHub, …) are connected in
Settings → Connections. Read them with the ` + "`harnessly`" + ` command, never with a token of your own:

- ` + "`harnessly connections`" + ` — what is connected (id, product, address, state, which one is primary).
- ` + "`harnessly issues [--conn ID] [--mine] [--open] [--project KEY] [TEXT…]`" + ` — search issues (the primary tracker by
  default).
- ` + "`harnessly issue KEY|URL`" + ` — one issue with its description and last comments; a link is read by the connection it
  belongs to.
- ` + "`harnessly pages [--conn ID] TEXT…`" + ` and ` + "`harnessly page ID|URL`" + ` — search and read wiki pages.

Add ` + "`--json`" + ` for the raw answer. Never ask the person for a tracker's or a wiki's token in the chat. When the service
you need is not connected (or answers "denied"), ask the person to connect it in Settings → Connections.

The MCP servers the person connected there (Atlassian, Linear, Notion, Sentry, GitHub, …) are already your tools, named
` + "`mcp__mcp-<name>__<tool>`" + ` (in Codex, the server ` + "`mcp-<name>`" + `); they sign in for you. When a service you need has no such
tools, ask the person to connect its MCP server in Settings → Connections and to start a new chat.`

// withSafe gives an agent that starts the workspace prompt with the safe's block after it (how to ask for a secret and
// use it without it passing through the conversation) and the connections' block: the text goes into a file of dir
// named by its hash.
func withSafe(on bool, dir string, prompt func(string) string) func(string) string {
	if !on {
		return prompt
	}
	return func(ws string) string {
		file := prompt(ws)
		text := ""
		if raw, err := os.ReadFile(file); err == nil {
			text = strings.TrimSpace(strings.ToValidUTF8(string(raw), "�"))
		}
		if text != "" {
			text += "\n\n"
		}
		text += secrets.PromptBlock() + "\n\n" + connectionsPromptBlock
		sum := sha256.Sum256([]byte(text))
		target := filepath.Join(dir, "safe-"+hex.EncodeToString(sum[:])[:16]+".md")
		if have, err := os.ReadFile(target); err == nil && string(have) == text {
			return target
		}
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if err = os.WriteFile(target, []byte(text), 0o600); err == nil {
				return target
			}
		}
		agent.Log("warn", "safe: the prompt file was not written", agent.F{K: "dir", V: dir})
		return file
	}
}
