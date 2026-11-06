package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// The weight of the apps in a chat. An app's instructions ride in the system prompt of every turn, and so do the
// descriptions of its tools and the `instructions` of its MCP servers; each app is capped on its own
// (MaxInstructionsBytes), and the chat has a budget for all of them together.

const (
	// ChatInstructionsBudgetBytes is the most the instructions of all the apps on in one chat should weigh together:
	// 48 KB, about 12k tokens. That is three apps at the 16 KB cap each, or a dozen of the usual 4 KB: past it the
	// agent's own work crowds out of a window every turn pays for. Past it the app is still turned on, with a warning:
	// the person decides what to take off.
	ChatInstructionsBudgetBytes = 48 << 10
	// AppContextWarnBytes is what an app's instructions, tool descriptions and server instructions may weigh together
	// before `harnessly app validate --strict` warns (not an error: it is the author's call, and a server can change).
	AppContextWarnBytes = 16 << 10
)

// EstimateTokens is the rough size in tokens of bytes of text: four bytes to a token. An estimate, not a count: it is
// there to compare apps and to warn, not to bill.
func EstimateTokens(bytes int) int { return (bytes + 3) / 4 }

// InstructionsBytes is what an installed app's instructions weigh in the prompt (as the launch cuts them).
func InstructionsBytes(a Installed) int {
	text, err := instructions(a)
	if err != nil {
		return 0
	}
	return len(text)
}

// ChatBudgetWarning is the warning for a chat whose apps' instructions weigh total bytes; "" while within the budget.
func ChatBudgetWarning(total int) string {
	if total <= ChatInstructionsBudgetBytes {
		return ""
	}
	return fmt.Sprintf("the instructions of the apps in this chat weigh ~%d tokens, over the budget of ~%d: every turn pays for them, take an app off to make room",
		EstimateTokens(total), EstimateTokens(ChatInstructionsBudgetBytes))
}

// MCPWeight is what a remote server of an app puts into the agent's context: its `instructions` and the names,
// descriptions and schemas of its tools (the first page of tools/list). Same transport rules as CallTool.
func MCPWeight(ctx context.Context, srv MCPServer) (instructionsBytes, toolsBytes int, err error) {
	if !strings.HasPrefix(srv.URL, "https://") {
		return 0, 0, fmt.Errorf("only a remote (https) server can be measured")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c := &mcpConn{url: srv.URL, headers: srv.Headers, http: &http.Client{
		Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: dialControl(false)}).DialContext},
		CheckRedirect: noRedirects,
	}}
	initRes, err := c.rpc(ctx, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "harnessly", "version": "1"},
	})
	if err != nil {
		return 0, 0, err
	}
	var init struct {
		Instructions string `json:"instructions"`
	}
	_ = json.Unmarshal(initRes, &init)
	if _, err := c.rpc(ctx, 0, "notifications/initialized", map[string]any{}); err != nil {
		return 0, 0, err
	}
	listRes, err := c.rpc(ctx, 2, "tools/list", map[string]any{})
	if err != nil {
		return len(init.Instructions), 0, err
	}
	return len(init.Instructions), ToolsListBytes(listRes), nil
}

// ToolsListBytes is the weight of a tools/list result: each tool's name, description and input schema.
func ToolsListBytes(result json.RawMessage) int {
	var list struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if json.Unmarshal(result, &list) != nil {
		return 0
	}
	n := 0
	for _, t := range list.Tools {
		n += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	return n
}
