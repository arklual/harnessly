package status

import (
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

// trustWindow is how long after a launch the screen is read for a trust dialog while the agent sent no event yet.
const trustWindow = 10 * time.Minute

// trustRepeat is how long an answered trust dialog is left to close before it is answered again.
const trustRepeat = 3000.0

// acceptTrust answers an agent's folder trust dialog with yes when it asks about the folder the tab runs in, or the
// repository holding it (Codex): the person chose that folder for the agent, and the dialog is not on the page, so
// otherwise the agent would wait at it unseen while messages pile up. True when the keys went out.
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
	m.mu.Lock()
	now := m.now()
	if last, ok := m.trustAnswered[tabID]; ok && now-last < trustRepeat {
		m.mu.Unlock()
		return false
	}
	m.trustAnswered[tabID] = now
	m.mu.Unlock()
	for _, key := range keys {
		if err := m.d.Tmux.SendRawKeys(session, key); err != nil {
			return false
		}
	}
	m.d.Log("info", "folder trust dialog answered yes", "tabId", tabID, "agent", prompt.Agent, "folder", prompt.FolderPath)
	return true
}
