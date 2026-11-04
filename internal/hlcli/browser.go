package hlcli

import (
	"encoding/json"
	"strings"
)

func (c *cli) browser(args []string) error {
	if len(args) == 0 {
		c.out("%s", browserHelp)
		return nil
	}
	name := args[0]
	if !strings.HasPrefix(name, "browser_") {
		name = "browser_" + name
	}
	values := map[string]any{}
	if len(args) > 1 {
		if len(args) != 3 || args[1] != "--json" {
			return usageErr("browser <tool> [--json '{...}']")
		}
		if err := json.Unmarshal([]byte(args[2]), &values); err != nil || values == nil {
			return usageErr("--json needs an argument object")
		}
	}
	session := c.session()
	if session == "" {
		return fail("run browser inside the chat's tmux terminal")
	}
	raw, err := c.request("POST", "/api/cli/browser/"+name, nil, map[string]any{"session": session, "arguments": values})
	if err != nil {
		return err
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	c.printRaw(raw)
	if result.IsError {
		return fail("browser tool refused the call (see content above)")
	}
	return nil
}
