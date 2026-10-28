package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LinkState is agent-link.json, what the running agent tells `harnessly-agent status`: the installer waits on it to
// learn that the machine is linked, and a person looks at it when a machine is not showing up. It holds no secret.
type LinkState struct {
	PID       int    `json:"pid"`
	MachineID string `json:"machineId"`
	HubURL    string `json:"hubUrl"`
	Linked    bool   `json:"linked"`
	// Since is when the link came up (epoch ms), 0 while there is none.
	Since int64 `json:"since,omitempty"`
	// Note says why there is no link: dialling, revoked, a refused signature…
	Note    string `json:"note,omitempty"`
	Version string `json:"version,omitempty"`
	// UpdatedAt is refreshed every few seconds; a file that stopped being refreshed belongs to an agent that is gone.
	UpdatedAt int64 `json:"updatedAt"`
}

const (
	linkStateFile  = "agent-link.json"
	linkStateEvery = 20 * time.Second
	linkStateStale = 3 * linkStateEvery
)

// LinkStatePath is where the agent keeps its state in a data folder.
func LinkStatePath(dataDir string) string { return filepath.Join(dataDir, linkStateFile) }

// Fresh says the agent that wrote it is still running.
func (s LinkState) Fresh(now time.Time) bool {
	return s.UpdatedAt != 0 && now.Sub(time.UnixMilli(s.UpdatedAt)) < linkStateStale
}

// WriteLinkState replaces the file whole, readable by the account only.
func WriteLinkState(path string, s LinkState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadLinkState reads the file; os.ErrNotExist (wrapped) when there is none.
func ReadLinkState(path string) (LinkState, error) {
	var s LinkState
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}
