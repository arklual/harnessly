package hlcli

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"time"
)

// The chats of other agents, on this machine and the fleet's others, and messages to them (internal/peers).

type peerChat struct {
	Ref         string `json:"ref"`
	Title       string `json:"title"`
	Machine     string `json:"machine"`
	MachineName string `json:"machineName"`
	Owner       string `json:"owner"`
	State       string `json:"state"`
}

type peerResult struct {
	ID     string    `json:"id"`
	State  string    `json:"state"`
	Reason string    `json:"reason"`
	Detail string    `json:"detail"`
	To     *peerChat `json:"to"`
}

// peerReasons are the refusals in words an agent can act on.
var peerReasons = map[string]string{
	"off":          "the other side takes no messages from agents",
	"blocked":      "the other side blocked this machine or its owner",
	"not-allowed":  "the other side takes messages only from its list, and this machine is not on it",
	"unverified":   "the other side could not verify this machine",
	"unknown-chat": "there is no such chat (list them with: harnessly peers)",
	"agent-gone":   "that chat has no running agent to read it",
	"empty":        "the message is empty",
	"too-long":     "the message is too long (16000 characters at most)",
	"rate-limited": "too many messages to that machine in the last minute, wait a little",
	"no-relay":     "this machine has no way to the other machines (RUNTIME_PEER_RELAY_URL is not set)",
	"unreachable":  "the other machine did not answer",
	"bad-ref":      "the address is <machine>/<conversation id>, as harnessly peers lists them",
	"failed":       "the chat did not take the message",
	"rejected":     "the owner of that machine did not accept the message",
	"expired":      "the message waited for the owner's approval past its time",
	"revoked":      "the message was taken back before the owner decided",
}

func (c *cli) peers(args []string) error {
	if len(args) > 0 && args[0] == "send" {
		return c.peerSend(args[1:])
	}
	if len(args) > 0 && args[0] == "revoke" {
		return c.peerRevoke(args[1:])
	}
	if len(args) > 0 && args[0] != "list" && args[0] != "ls" {
		return usageErr("peers takes list, send or revoke: harnessly peers [list], harnessly peers send [--ttl 2h] [--id ID] REF TEXT..., harnessly peers revoke REF ID")
	}
	raw, err := c.request("GET", "/api/cli/peers", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Chats  []peerChat `json:"chats"`
		Errors []string   `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	for _, ch := range out.Chats {
		c.out("%s\t%s\t%s · %s\t%s\n", ch.Ref, ch.State, orDash(ch.Owner), orDash(ch.MachineName), orDash(ch.Title))
	}
	if len(out.Chats) == 0 {
		c.out("no chats you may write to\n")
	}
	for _, e := range out.Errors {
		c.out("! %s\n", e)
	}
	return nil
}

func (c *cli) peerSend(args []string) error {
	body := map[string]any{"session": c.session()}
	for len(args) > 1 && (args[0] == "--ttl" || args[0] == "--id") {
		switch args[0] {
		case "--ttl":
			d, err := time.ParseDuration(args[1])
			if err != nil || d <= 0 {
				return usageErr("--ttl is how long the message may wait for the owner's approval, like 30m or 2h")
			}
			body["ttl"] = int64(d / time.Second)
		case "--id":
			// The same id again never delivers twice: it answers what came of the first.
			body["id"] = args[1]
		}
		args = args[2:]
	}
	if len(args) < 2 {
		return usageErr("peers send needs the chat and the text: harnessly peers send REF TEXT... (or - to read stdin)")
	}
	ref, text := args[0], strings.Join(args[1:], " ")
	if text == "-" {
		raw, err := io.ReadAll(io.LimitReader(c.env.Stdin, 1<<20))
		if err != nil {
			return err
		}
		text = string(raw)
	}
	body["to"], body["text"] = ref, text
	raw, err := c.request("POST", "/api/cli/peers/send", url.Values{}, body)
	if err != nil {
		return err
	}
	return c.peerAnswer(raw, ref)
}

func (c *cli) peerRevoke(args []string) error {
	if len(args) != 2 {
		return usageErr("peers revoke needs the chat and the message id: harnessly peers revoke REF ID")
	}
	raw, err := c.request("POST", "/api/cli/peers/revoke", url.Values{}, map[string]any{"to": args[0], "id": args[1]})
	if err != nil {
		return err
	}
	return c.peerAnswer(raw, args[0])
}

// peerAnswer prints what came of a message: delivered, held for the other owner (with the id to follow or take it
// back), or the refusal in words.
func (c *cli) peerAnswer(raw []byte, ref string) error {
	if c.json {
		c.printRaw(raw)
	}
	var r peerResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if r.State == "delivered" {
		if !c.json {
			who := ref
			if r.To != nil {
				who = strings.Join(nonEmpty(r.To.Owner, r.To.MachineName, r.To.Title), " · ") + " (" + r.To.Ref + ")"
			}
			c.out("delivered → %s\n", who)
		}
		return nil
	}
	if r.State == "held" {
		if !c.json {
			c.out("held → %s waits for its owner's approval; id %s (send again with --id %s to see what came of it, take it back with: harnessly peers revoke %s %s)\n",
				ref, r.ID, r.ID, ref, r.ID)
		}
		return nil
	}
	reason := peerReasons[r.Reason]
	if reason == "" {
		reason = r.Reason
	}
	if r.Detail != "" {
		reason += " (" + r.Detail + ")"
	}
	return fail("refused: %s — %s", r.Reason, reason)
}

func nonEmpty(xs ...string) []string {
	out := []string{}
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
