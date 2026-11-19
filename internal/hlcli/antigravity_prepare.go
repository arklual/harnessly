package hlcli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// PrepareAntigravityPlugin creates only a fresh, deployment-owned plugin folder.
// Installing it into the official CLI is a separate explicit user action.
func PrepareAntigravityPlugin(parent, hostBinary string) (string, error) {
	hooks, err := AntigravityHookConfig(hostBinary)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(parent, "antigravity-")
	if err != nil {
		return "", err
	}
	manifest, _ := json.MarshalIndent(map[string]any{"$schema": "https://antigravity.google/schemas/v1/plugin.json", "name": "harnessly-native", "displayName": "Harnessly native agent", "version": "0.1.0", "description": "Launch-bound native session hooks and host MCP tools"}, "", "  ")
	mcp, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"harnessly": map[string]any{"command": hostBinary, "args": []string{"antigravity-mcp"}}}}, "", "  ")
	for name, raw := range map[string][]byte{"plugin.json": manifest, "hooks.json": hooks, "mcp_config.json": mcp} {
		if err := os.WriteFile(filepath.Join(dir, name), append(raw, '\n'), 0600); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func (c *cli) antigravityPrepare(args []string) error {
	if len(args) != 1 || args[0] != "prepare" {
		return usageErr("antigravity prepare creates a native hook/MCP plugin without installing it")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := PrepareAntigravityPlugin(filepath.Join(c.dataDir(), "native-plugins"), binary)
	if err != nil {
		return err
	}
	if c.json {
		return json.NewEncoder(c.env.Stdout).Encode(map[string]any{"plugin": dir, "installed": false})
	}
	c.out("Prepared native Antigravity plugin: %s\n", dir)
	c.out("No user configuration changed. Install explicitly with: agy plugin install %s\n", agents.ShellQuote(dir))
	c.out("Remove later with: agy plugin uninstall harnessly-native\n")
	return nil
}
