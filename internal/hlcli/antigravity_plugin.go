package hlcli

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// AntigravityHookConfig returns the official .agents/hooks.json fragment for a
// deployment-owned isolated workspace/plugin. It never installs it or modifies
// workspace/global user configuration. No credential or launch marker is saved:
// the hook reads auth from the usual CLI files and markers from its environment.
func AntigravityHookConfig(harnesslyBinary string) ([]byte, error) {
	if !filepath.IsAbs(harnesslyBinary) || strings.ContainsFunc(harnesslyBinary, func(r rune) bool { return r < 32 || r == 127 }) {
		return nil, errors.New("Antigravity hook executable must be an absolute path without control characters")
	}
	type handler struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	events := map[string][]handler{}
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "Stop"} {
		events[event] = []handler{{Type: "command", Command: agents.ShellQuote(harnesslyBinary) + " hook antigravity " + event, Timeout: 5}}
	}
	return json.MarshalIndent(map[string]any{"harnessly-native": events}, "", "  ")
}
