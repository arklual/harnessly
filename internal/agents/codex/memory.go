package codex

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// Codex memories: what Codex learns in a chat (the person's choices, how they like things done) is kept in
// ~/.codex/memories and read by the chats that follow. The feature is `features.memories` (`codex features list`:
// stable, off by default; the same as `--enable memories`). harnessly starts every Codex chat with it on, for everyone,
// the open-source build too, so an agent that learned «человек не хочет виджет прогресса для рендеров» stays quiet in
// the next chat as the Claude agents do with their memory notes.
//
// The person's word wins: a `features.memories` in their config.toml (Settings → Codex → «Память», or by hand) is left
// as it is, and RUNTIME_CODEX_MEMORY=off keeps the default away for a machine (a corporate one, say).

// MemoriesEnv is the machine's switch of the default: "off", "0" or "false" starts chats without it.
const MemoriesEnv = "RUNTIME_CODEX_MEMORY"

// MemoryArgs are the arguments that turn Codex memories on for a launch on Codex's own account: nothing when the
// person's config.toml sets the key (either way), when the machine turned the default off, or for a launch on a vendor's
// profile (its memory pipeline is the OpenAI account's).
func (c LaunchConfig) memoryArgs(prof agents.CodexProfile) []string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(MemoriesEnv))) {
	case "off", "0", "false", "no":
		return nil
	}
	if prof.KeyEnv != "" || len(prof.Args) > 0 || prof.Home != "" {
		return nil
	}
	if c.Hooks != nil && configSetsMemories(c.Hooks.UserConfig) {
		return nil
	}
	return []string{"-c", "features.memories=true"}
}

// configSetsMemories says whether the person's config.toml has `features.memories`, whatever its value.
func configSetsMemories(path string) bool {
	if path == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc map[string]any
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return false
	}
	features, _ := doc["features"].(map[string]any)
	_, set := features["memories"]
	return set
}
