package main

import (
	"context"
	"os"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// deckOneShot is the model call a deck's dictation is tidied with: one tool-less, session-less call of the agent's
// CLI, as the titler makes (v2_titler.go), in the titler's turn. Claude on its cheap model; the binary and login come
// from the side-chat config (RUNTIME_SIDE_CHAT_CONFIG), which a test points at a fake binary.
func deckOneShot(d deps) func(ctx context.Context, prompt string) (string, error) {
	t := newV2Titler(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"), agentEnvDir(d.DataDir))
	return func(ctx context.Context, prompt string) (string, error) {
		return t.oneShot(ctx, wire.ProviderClaude, prompt)
	}
}
