package hlcli

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// selftest runs the runtime's self-test of this machine (GET /api/cli/selftest): a throwaway pane started as a tab's
// pane, a program typed into it and a key sent to it; with --agent also a real Claude (--claude PATH: that binary) that
// asks a question and gets its answer. Exit 1 when a step failed, so a script after a rollout can tell.
func (c *cli) selftest(args []string) error {
	q := url.Values{}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--agent":
			q.Set("agent", "1")
		case args[i] == "--claude" && i+1 < len(args):
			q.Set("agent", "1")
			q.Set("claude", args[i+1])
			i++
		default:
			return usageErr("selftest takes --agent and --claude PATH: harnessly selftest [--agent] [--claude PATH] [--json]")
		}
	}
	raw, err := c.request("GET", "/api/cli/selftest", q, nil)
	if err != nil {
		return err
	}
	var res struct {
		OK     bool `json:"ok"`
		Checks []struct {
			ID     string `json:"id"`
			OK     bool   `json:"ok"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("the runtime's answer is not a self-test: %w", err)
	}
	if c.json {
		_, _ = c.env.Stdout.Write(raw)
		c.out("\n")
	} else {
		for _, ch := range res.Checks {
			mark := "ok  "
			if !ch.OK {
				mark = "FAIL"
			}
			c.out("%s %s (%s)\n", mark, ch.Title, ch.ID)
			if ch.Detail != "" {
				c.out("     %s\n", ch.Detail)
			}
		}
	}
	if !res.OK {
		return exitCode(exitError)
	}
	return nil
}
