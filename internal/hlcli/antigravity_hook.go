package hlcli

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
)

// antigravityHook is silent and best effort. A CLI outside an explicitly managed
// launch must never claim a tab just because it inherited a tmux session.
func (c *cli) antigravityHook(event string) {
	switch event {
	case "SessionStart", "PreInvocation", "PostInvocation", "Stop":
	default:
		return
	}
	conversation := strings.TrimSpace(c.env.Getenv("HARNESSLY_ANTIGRAVITY_CONVERSATION"))
	generation := strings.TrimSpace(c.env.Getenv("HARNESSLY_ANTIGRAVITY_GENERATION"))
	if conversation == "" || generation == "" || len(conversation) > 128 || len(generation) > 128 {
		return
	}
	expected := c.env.Getenv("HARNESSLY_ANTIGRAVITY_EXPECTED_UUID")
	if expected != "" && !agents.IsUUID(expected) {
		return
	}
	input := *c
	if c.env.Stdin == nil {
		return
	}
	input.env.Stdin = io.LimitReader(c.env.Stdin, (64<<10)+1)
	raw := input.hookInput()
	if len(raw) > 64<<10 {
		return
	}
	payload, err := antigravity.ParseHookPayload(c.env.Home, raw)
	if err != nil || payload.TranscriptPath == "" || expected != "" && expected != payload.SessionID {
		return
	}
	session := strings.TrimSpace(c.env.Getenv("HARNESSLY_SESSION"))
	if session == "" {
		session = c.env.Session()
	}
	if session == "" {
		return
	}
	body := map[string]any{"event": event, "conversation": conversation, "generation": generation, "expectedUUID": expected, "payload": payload}
	client := http.Client{Timeout: hookDeliverWait}
	if c.env.Client != nil {
		client = *c.env.Client
		client.Timeout = hookDeliverWait
	}
	deliver := *c
	deliver.env.Client = &client
	_, _, _ = deliver.call(http.MethodPost, "/api/status/hook", url.Values{"provider": {"antigravity"}, "tmuxSession": {session}}, body)
}
