package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/notifybots"
)

// notifyRefreshEvery is how often an agent's start asks the shared bot again, so that a Telegram connected on another
// machine reaches the prompts of this one.
const notifyRefreshEvery = time.Minute

// notifyPromptBlock is the Telegram part of an agent's prompt: the shared bot as it was last seen, and the person's own
// bots while they are allowed.
func notifyPromptBlock(store *notifybots.Store, shared *notifybots.Shared) string {
	st := shared.Cached()
	shared.RefreshSoon(notifyRefreshEvery)
	block := notifybots.SharedPromptBlock(st)
	if !notifybots.OwnBotsAllowed(notifyOwnBotsMode(), st.Enabled) {
		return block
	}
	bots, err := store.List()
	if err != nil {
		agent.Log("warn", "notify bots: not read for the prompt", agent.F{K: "err", V: err.Error()})
		return block
	}
	if own := notifybots.PromptBlock(bots); own != "" {
		if block != "" {
			block += "\n"
		}
		block += own
	}
	return block
}

// withNotifyBots gives an agent that starts the workspace prompt with the Telegram block after it, as it is at that
// moment: the text goes into a file of dir named by its hash. With no Telegram to write to it is the workspace's own
// file. Agents already running learn of a change with `harnessly notify list`.
func withNotifyBots(store *notifybots.Store, shared *notifybots.Shared, dir string, prompt func(string) string) func(string) string {
	return func(ws string) string {
		file := prompt(ws)
		block := notifyPromptBlock(store, shared)
		if block == "" {
			return file
		}
		text := ""
		if raw, err := os.ReadFile(file); err == nil {
			text = strings.TrimSpace(strings.ToValidUTF8(string(raw), "�"))
		}
		if text != "" {
			text += "\n\n"
		}
		text += block
		sum := sha256.Sum256([]byte(text))
		target := filepath.Join(dir, "notify-"+hex.EncodeToString(sum[:])[:16]+".md")
		if have, err := os.ReadFile(target); err == nil && string(have) == text {
			return target
		}
		if err := os.MkdirAll(dir, 0o700); err == nil {
			err = os.WriteFile(target, []byte(text), 0o600)
			if err == nil {
				return target
			}
		}
		agent.Log("warn", "notify bots: the prompt file was not written", agent.F{K: "dir", V: dir})
		return file
	}
}
