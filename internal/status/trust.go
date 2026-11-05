package status

import (
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// trustWindow is how long after a launch the screen is read for a trust dialog while the agent sent no event yet.
const trustWindow = 10 * time.Minute

// trustRepeat is how long an answered trust dialog is left to close before it is answered again.
const trustRepeat = 3000.0

// trustSettle is how long an arrow is given to show on the screen before the dialog is read for the next step.
const trustSettle = 600.0

// acceptTrust answers an agent's folder trust dialog with yes when it asks about the folder the tab runs in, or the
// repository holding it (Codex): the person chose that folder for the agent, and the dialog is not on the page, so
// otherwise the agent would wait at it unseen while messages pile up. Each call takes one step, so Enter follows what
// the screen shows and not what the arrows were meant to do. True when the keys went out.
func (m *Manager) acceptTrust(tabID, session, pane string) bool {
	if m.d.AskFolderTrust {
		return false
	}
	prompt := screen.MatchTrustPrompt(pane)
	if prompt == nil {
		return false
	}
	cwd := strings.TrimSuffix(m.d.Tmux.SessionCwd(session), "/")
	folder := strings.TrimSuffix(prompt.FolderPath, "/")
	if cwd == "" || folder == "" || (cwd != folder && !strings.HasPrefix(cwd, folder+"/")) {
		return false
	}
	keys := screen.TrustAcceptKeys(pane)
	if keys == nil {
		return false
	}
	answers := slices.Contains(keys, "Enter")
	wait := trustSettle
	if answers {
		wait = trustRepeat
	}
	m.mu.Lock()
	now := m.now()
	if next, ok := m.trustNext[tabID]; ok && now < next {
		m.mu.Unlock()
		return false
	}
	m.trustNext[tabID] = now + wait
	m.mu.Unlock()
	for _, key := range keys {
		if err := m.d.Tmux.SendRawKeys(session, key); err != nil {
			return false
		}
	}
	if answers {
		m.d.Log("info", "folder trust dialog answered yes", "tabId", tabID, "agent", prompt.Agent, "folder", prompt.FolderPath)
	}
	return true
}
