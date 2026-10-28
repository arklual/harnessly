package hlcli

import (
	"encoding/json"
	"io"
	"strings"
)

// Telegram notifications (internal/notifybots): the person's Telegram connected to the shared bot, and bots of their
// own where those are allowed. The runtime (or the cloud) holds the tokens and sends; this command only gives the text
// and, for a bot of the person's own, its name.

type notifyShared struct {
	Enabled     bool   `json:"enabled"`
	Bot         string `json:"bot"`
	Linked      bool   `json:"linked"`
	Chat        string `json:"chat"`
	Instruction string `json:"instruction"`
}

type notifyBot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	Chat        string `json:"chat"`
	Bound       bool   `json:"bound"`
	Instruction string `json:"instruction"`
}

// notifyReasons are the refusals in words an agent can act on.
var notifyReasons = map[string]string{
	"not-found":     "there is no bot by that name (list them with: harnessly notify list)",
	"ambiguous":     "more than one bot has that name: use its id from harnessly notify list",
	"not-bound":     "the bot has no chat yet: the person binds one in Settings → Notifications",
	"empty":         "the message is empty",
	"too-long":      "the message is too long (16000 characters at most)",
	"rate-limited":  "too many messages through this bot in the last minute, wait a little",
	"chat-gone":     "the bot cannot write to its chat any more (removed from the group or blocked): tell the person",
	"invalid-token": "Telegram no longer takes the bot's token: the person replaces it in Settings → Notifications",
	"unreachable":   "Telegram did not answer, try again later",
	"rejected":      "Telegram refused the message",
	"failed":        "the runtime could not read its bots",
	"not-linked":    "the person has not connected Telegram: Settings → Notifications → «Connect Telegram»",
	"no-shared":     "there is no shared bot here: send through a bot of the person's own with --bot (harnessly notify list)",
	"offline":       "the platform did not answer, try again later",
	"disabled":      "bots of the person's own are switched off: send without --bot",
}

func (c *cli) notify(args []string) error {
	if len(args) > 0 && args[0] == "send" {
		return c.notifySend(args[1:])
	}
	if len(args) > 0 && args[0] != "list" && args[0] != "ls" {
		return usageErr("notify takes list or send: harnessly notify [list], harnessly notify send [--bot NAME] TEXT...")
	}
	raw, err := c.request("GET", "/api/cli/notify/bots", nil, nil)
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
		return nil
	}
	var out struct {
		Bots   []notifyBot   `json:"bots"`
		Shared *notifyShared `json:"shared"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if sh := out.Shared; sh != nil && sh.Enabled {
		if sh.Linked {
			c.out("Telegram\t@%s → %s\t(send without --bot)\n", sh.Bot, sh.Chat)
		} else {
			c.out("Telegram\t@%s (not connected: the person connects it in Settings → Notifications)\n", sh.Bot)
		}
		instruction := strings.TrimSpace(sh.Instruction)
		if instruction == "" {
			instruction = "(no instruction: only when the person asks)"
		}
		for _, line := range strings.Split(instruction, "\n") {
			c.out("\t%s\n", line)
		}
	}
	for _, b := range out.Bots {
		chat := "→ " + b.Chat
		if !b.Bound {
			chat = "(no chat yet)"
		}
		c.out("%s\t%s\t@%s %s\n", b.ID, b.Name, b.Username, chat)
		instruction := strings.TrimSpace(b.Instruction)
		if instruction == "" {
			instruction = "(no instruction: only when the person asks)"
		}
		for _, line := range strings.Split(instruction, "\n") {
			c.out("\t%s\n", line)
		}
	}
	if len(out.Bots) == 0 && (out.Shared == nil || !out.Shared.Enabled) {
		c.out("no bots: the person links them in Settings → Notifications\n")
	}
	return nil
}

func (c *cli) notifySend(args []string) error {
	bot, silent := "", false
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--bot" || a == "-b":
			if i+1 >= len(args) {
				return usageErr("--bot needs the bot's name or id")
			}
			i++
			bot = args[i]
		case strings.HasPrefix(a, "--bot="):
			bot = strings.TrimPrefix(a, "--bot=")
		case a == "--silent" || a == "-s":
			silent = true
		default:
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return usageErr("notify send needs the text: harnessly notify send [--bot NAME] TEXT... (or - to read stdin)")
	}
	text := strings.Join(words, " ")
	if text == "-" {
		raw, err := io.ReadAll(io.LimitReader(c.env.Stdin, 1<<20))
		if err != nil {
			return err
		}
		text = string(raw)
	}
	status, raw, err := c.call("POST", "/api/cli/notify/send", nil, map[string]any{"session": c.session(), "bot": bot, "text": text, "silent": silent})
	if err != nil {
		return err
	}
	if c.json {
		c.printRaw(raw)
	}
	var r struct {
		Bot      string `json:"bot"`
		Chat     string `json:"chat"`
		Messages int    `json:"messages"`
		Error    string `json:"error"`
		Detail   string `json:"detail"`
	}
	_ = json.Unmarshal(raw, &r)
	if status >= 200 && status < 300 {
		if !c.json {
			c.out("sent → %s", r.Chat)
			if r.Bot != "" {
				c.out(" (%s)", r.Bot)
			}
			if r.Messages > 1 {
				c.out(", %d messages", r.Messages)
			}
			c.out("\n")
		}
		return nil
	}
	reason := notifyReasons[r.Error]
	if reason == "" {
		// Not a refusal of the bots (the token, the runtime starting): the common words for it.
		return apiError(status, raw)
	}
	if r.Detail != "" {
		reason += " (" + r.Detail + ")"
	}
	return fail("refused: %s — %s", r.Error, reason)
}
