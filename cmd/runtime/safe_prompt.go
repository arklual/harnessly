package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// withSafe gives an agent that starts the workspace prompt with the safe's block after it (how to ask for a secret and
// use it without it passing through the conversation): the text goes into a file of dir named by its hash.
func withSafe(on bool, dir string, prompt func(string) string) func(string) string {
	if !on {
		return prompt
	}
	return func(ws string) string {
		file := prompt(ws)
		text := ""
		if raw, err := os.ReadFile(file); err == nil {
			text = strings.TrimSpace(strings.ToValidUTF8(string(raw), "�"))
		}
		if text != "" {
			text += "\n\n"
		}
		text += secrets.PromptBlock()
		sum := sha256.Sum256([]byte(text))
		target := filepath.Join(dir, "safe-"+hex.EncodeToString(sum[:])[:16]+".md")
		if have, err := os.ReadFile(target); err == nil && string(have) == text {
			return target
		}
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if err = os.WriteFile(target, []byte(text), 0o600); err == nil {
				return target
			}
		}
		agent.Log("warn", "safe: the prompt file was not written", agent.F{K: "dir", V: dir})
		return file
	}
}
