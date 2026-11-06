package apps

import (
	"os"
	"path/filepath"
	"strings"
)

// Command kinds: a prompt is sent to the agent as the slash command; a view would open an app view in the pane (the
// manifest does not tell the two apart yet, so every command is a prompt).
const CommandPrompt = "prompt"

// ChatCommand is a command of the app as the page's «/» palette lists it.
type ChatCommand struct {
	// Name is the command without its slash.
	Name string `json:"name"`
	// Slash is what the agent is sent: /<plugin>:<name>, which no other command can take.
	Slash string `json:"slash"`
	// Description is the description of commands/<name>.md; empty when the file has none.
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

// ChatCommands are the chat.commands of the app, each with the description from its file.
func (m *Manifest) ChatCommands(dir string) []ChatCommand {
	out := []ChatCommand{}
	if m == nil || m.Chat == nil {
		return out
	}
	for _, cmd := range m.Chat.Commands {
		name := strings.TrimPrefix(cmd, "/")
		out = append(out, ChatCommand{
			Name:        name,
			Slash:       "/" + PluginName(m.ID) + ":" + name,
			Description: commandDescription(filepath.Join(dir, "commands", name+".md")),
			Kind:        CommandPrompt,
		})
	}
	return out
}

// commandDescription reads `description:` from the frontmatter of a command file; "" when there is none.
func commandDescription(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}
