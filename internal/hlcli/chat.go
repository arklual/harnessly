package hlcli

import (
	"encoding/json"
	"strings"
)

// chat is `harnessly chat test ID|current [--off]`: the mark of a test chat, which the sidebar leaves out.
func (c *cli) chat(args []string) error {
	if len(args) == 0 || args[0] != "test" {
		return usageErr("harnessly chat test ID|current [--off]")
	}
	off := false
	rest := make([]string, 0, len(args))
	for _, a := range args[1:] {
		if a == "--off" {
			off = true
			continue
		}
		rest = append(rest, a)
	}
	f, err := parseFlags(rest, nil)
	if err != nil {
		return err
	}
	if len(f.pos) != 1 {
		return usageErr("chat test needs a chat id, or current for the chat this tab runs")
	}
	body := map[string]any{"test": !off}
	if f.pos[0] == "current" {
		session := c.session()
		if session == "" {
			return errNoPane
		}
		body["session"] = session
	} else {
		body["id"] = f.pos[0]
	}
	status, raw, err := c.call("POST", "/api/cli/chat/test", nil, body)
	if err != nil {
		return err
	}
	if status == 404 {
		return fail("no such chat, or no chat runs in this tab")
	}
	if status >= 300 {
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var r struct {
		Conversation string `json:"conversation"`
		Test         bool   `json:"test"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.Test {
		c.out("%s is a test chat: the sidebar leaves it out\n", strings.TrimSpace(r.Conversation))
	} else {
		c.out("%s is an ordinary chat again\n", strings.TrimSpace(r.Conversation))
	}
	return nil
}
