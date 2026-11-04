package paneauto

import "strings"

// IsBrowserTool recognizes runtime browser tools in Claude MCP names and Codex's
// normalized code-mode names. Optional server names follow RUNTIME_MCP_SERVER.
func IsBrowserTool(tool string, servers ...string) bool {
	server := "harnessly"
	if len(servers) > 0 && servers[0] != "" {
		server = servers[0]
	}
	tool = strings.TrimPrefix(tool, "functions.")
	tool = strings.TrimPrefix(tool, "mcp__")
	name, verb, ok := strings.Cut(tool, "__")
	return ok && (name == server || name == strings.ReplaceAll(server, "-", "_")) &&
		strings.HasPrefix(verb, "browser_") && len(verb) > len("browser_")
}
