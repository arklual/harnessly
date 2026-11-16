package hlcli

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/chatrole"
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
	if session == "" {
		return nil
	}
	// The chat's role comes first: a chat that may not change anything is refused whatever the voice says.
	role := chatrole.Of(filepath.Join(c.dataDir(), chatrole.Dir), session)
	voice := consent.Live(filepath.Join(c.dataDir(), consent.LiveDir), session, time.Now())
	if role == chatrole.None && !voice {
		return nil
	}
	in := c.hookInput()
	if out := chatrole.Answer(in, role); out != nil {
		_, _ = c.env.Stdout.Write(out)
		return nil
	}
	if !voice {
		return nil
	}
	if out := consent.Answer(in, codex); out != nil {
		_, _ = c.env.Stdout.Write(out)
	}
	return nil
}

// roleGuard is the PreToolUse hook of a Codex chat (`harnessly hook role-guard`): the chat's role alone (internal/chatrole),
// without the voice mode, which Codex chats do not take part in.
func (c *cli) roleGuard() error {
	session := strings.TrimSpace(c.env.Getenv("HARNESSLY_SESSION"))
	if session == "" && c.env.Session != nil {
		session = c.env.Session()
	}
	if session == "" {
		return nil
	}
	role := chatrole.Of(filepath.Join(c.dataDir(), chatrole.Dir), session)
	if role == chatrole.None {
		return nil
	}
	if out := chatrole.Answer(c.hookInput(), role); out != nil {
		_, _ = c.env.Stdout.Write(out)
	}
	return nil
}
