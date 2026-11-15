package hlcli

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/voice/consent"
)

// voiceGuard is the PreToolUse hook of the voice mode (`harnessly hook voice-guard [--agent codex]`): while
// the person talks to this chat by voice, a call that reaches outside waits for their «да» (internal/voice/consent).
// Without a call on, or on any doubt about the chat, it says nothing and the call goes as before.
func (c *cli) voiceGuard(codex bool) error {
	session := strings.TrimSpace(c.env.Getenv("HARNESSLY_SESSION"))
	if session == "" && c.env.Session != nil {
		session = c.env.Session()
	}
	if session == "" || !consent.Live(filepath.Join(c.dataDir(), consent.LiveDir), session, time.Now()) {
		return nil
	}
	if out := consent.Answer(c.hookInput(), codex); out != nil {
		_, _ = c.env.Stdout.Write(out)
	}
	return nil
}
