package hooksetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// WorkspaceIDPlaceholder stands for the workspace's id in a prompt template.
const WorkspaceIDPlaceholder = "{{workspaceId}}"

// promptFiles are the files each agent CLI is given, in each workspace's folder; agents started earlier hold the paths.
var promptFiles = []struct{ provider, name string }{{"claude", "claude-prompt.md"}, {"codex", "codex-prompt.md"}}

var workspaceIDRe = regexp.MustCompile(`^[\w-]+$`)

// Prompts are the per-workspace system prompts of the agent CLIs by provider ("claude", "codex"). Their text is the
// platform's own, so it comes from a file the deployment names: a JSON object of templates.
type Prompts map[string]string

// LoadPrompts reads the templates file.
func LoadPrompts(file string) (Prompts, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var p Prompts
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// Text is the provider's prompt for one workspace; false when there is no template for it.
func (p Prompts) Text(provider, workspaceID string) (string, bool) {
	t, ok := p[provider]
	if !ok {
		return "", false
	}
	return strings.ReplaceAll(t, WorkspaceIDPlaceholder, workspaceID), true
}

// Write writes a workspace's prompt files (<dataDir>/workspaces/<id>/<provider>-prompt.md), each only when its text
// changed, as the previous runtime did on start and on a new workspace.
func (p Prompts) Write(dataDir, workspaceID string) error {
	if !workspaceIDRe.MatchString(workspaceID) {
		return errors.New("hooksetup: bad workspace id")
	}
	dir := filepath.Join(dataDir, "workspaces", workspaceID)
	var errs []error
	for _, f := range promptFiles {
		text, ok := p.Text(f.provider, workspaceID)
		if !ok {
			continue
		}
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		file := filepath.Join(dir, f.name)
		if existing, err := os.ReadFile(file); err == nil && string(existing) == text {
			continue
		}
		errs = append(errs, os.WriteFile(file, []byte(text), 0o666))
	}
	return errors.Join(errs...)
}
