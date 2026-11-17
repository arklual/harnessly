package hlcli

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/delegation"
)

// chat is `harnessly chat test ID|current [--off]`: the mark of a test chat, which the sidebar leaves out.
func (c *cli) chat(args []string) error {
	if len(args) > 0 && args[0] == "read" {
		return c.chatRead(args[1:])
	}
	if len(args) > 0 && args[0] == "delete" {
		return c.chatDelete(args[1:])
	}
	if len(args) == 0 || args[0] != "test" {
		return usageErr("harnessly chat read ID|URL [--before TURN] [-n N] | chat test ID|current [--off] | chat delete ID")
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

// chatRead is a chat's transcript by its id or link (GitHub #86), as read_conversation gives it: any chat of
// this machine from the runtime, another of the person's from the hub's history. It never needs a browser session.
func (c *cli) chatRead(args []string) error {
	f, err := parseFlags(args, map[string]string{"before": "before", "n": "limit", "limit": "limit"})
	if err != nil || len(f.pos) != 1 {
		return usageErr("chat read ID|URL [--before TURN] [-n N]")
	}
	id, err := delegation.ConversationRef(f.pos[0])
	if err != nil {
		return usageErr("chat read: %v", err)
	}
	limit := 40
	if s := f.values["limit"]; s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > 100 {
			return usageErr("chat read: -n is 1 to 100")
		}
	}
	body := map[string]any{"session": c.session(), "id": id, "before": f.values["before"], "limit": limit}
	status, raw, err := c.call("POST", "/api/cli/chat/read", nil, body)
	if err != nil {
		return err
	}
	if status >= 300 {
		var e struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Detail != "" {
			return fail("%s", e.Detail)
		}
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var r struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return fail("the machine returned invalid JSON")
	}
	c.out("%s\n", strings.TrimRight(r.Text, "\n"))
	return nil
}

// chatDelete is `harnessly chat delete ID`: a chat this one delegated goes, as the sidebar's «Удалить» does, and frees
// its place under the limit of delegated chats.
func (c *cli) chatDelete(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageErr("chat delete needs the id of a chat this one delegated")
	}
	session := c.session()
	if session == "" {
		return errNoPane
	}
	status, raw, err := c.call("POST", "/api/cli/chat/delete", nil, map[string]any{"session": session, "id": args[0]})
	if err != nil {
		return err
	}
	switch {
	case status == 404:
		return fail("no such chat, or no chat runs in this tab")
	case status == 403 && strings.Contains(string(raw), "not-yours"):
		return fail("only a chat this one delegated can be deleted from here")
	case status >= 300:
		return apiError(status, raw)
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	c.out("%s deleted (it can be restored from the bin)\n", args[0])
	return nil
}
