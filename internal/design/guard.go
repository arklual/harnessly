package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The guard of project/canvas.json: in a project of a design library the file is built from meta/ and an edit by hand
// would be overwritten (or, worse, believed). An agent of the design tool is used to editing it, so a PreToolUse hook
// of the agent refuses the write and says which command to use. The guard reads the hook's JSON from stdin and needs
// nothing else: a project is a folder with project/ and artifact.json or meta/ next to it.

// GuardInput is the part of a PreToolUse hook's input the guard reads.
type GuardInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
	Cwd string `json:"cwd"`
}

var writingTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

// isLibraryCanvas: file is <project>/project/canvas.json of a design project (relative paths are taken from cwd).
func isLibraryCanvas(file, cwd string) bool {
	if file == "" {
		return false
	}
	if !filepath.IsAbs(file) && cwd != "" {
		file = filepath.Join(cwd, file)
	}
	file = filepath.Clean(file)
	if filepath.Base(file) != canvasJSONName || filepath.Base(filepath.Dir(file)) != ProjectDirname {
		return false
	}
	top := filepath.Dir(filepath.Dir(file))
	return exists(filepath.Join(top, ArtifactFile)) || exists(filepath.Join(top, MetaDirname))
}

// canvasWrites are the shell forms that write a file named canvas.json: a redirect into it, tee, an in-place edit,
// a move, copy or removal naming it, a script that writes or dumps with it on the line.
var canvasWrites = regexp.MustCompile(`>{1,2}\s*['"]?[^\s'"|;&]*canvas\.json|` +
	`\btee\b[^|;&]*canvas\.json|` +
	`\b(?:sed|perl)\s+[^|;&]*-[a-zA-Z]*i[^|;&]*canvas\.json|\bsed\s+[^|;&]*--in-place[^|;&]*canvas\.json|` +
	`\b(?:mv|cp|rm|truncate|install|dd|ln|rsync)\b[^|;&]*canvas\.json|` +
	`\b(?:python3?|node|ruby|deno|bun)\b[^|&]*canvas\.json[^|&]*\b(?:write|dump|writeFile)|` +
	`\b(?:python3?|node|ruby|deno|bun)\b[^|&]*\b(?:write|dump|writeFile)[^|&]*canvas\.json`)

// GuardCanvas says whether a tool call writes the built canvas.json of a design project, and the words for the agent.
func GuardCanvas(in GuardInput, cli string) (deny bool, reason string) {
	if cli == "" {
		cli = "harnessly"
	}
	reason = locale.Pick(
		"project/canvas.json of a design project is built from its meta/ and is read-only: an edit here would be overwritten. "+
			"Change the pages with `"+cli+" design page create|rename|remove|list <project> …`; the board files themselves (*.dc.html) are edited as usual.",
		"project/canvas.json проекта дизайна собирается из meta/ и только для чтения: правка здесь будет перезаписана. "+
			"Страницы меняются командами `"+cli+" design page create|rename|remove|list <проект> …`; сами доски (*.dc.html) правятся как обычно.")
	if writingTools[in.ToolName] {
		for _, f := range []string{in.ToolInput.FilePath, in.ToolInput.NotebookPath, in.ToolInput.Path} {
			if isLibraryCanvas(f, in.Cwd) {
				return true, reason
			}
		}
		return false, ""
	}
	if in.ToolName == "Bash" {
		c := in.ToolInput.Command
		if strings.Contains(c, ProjectDirname+"/"+canvasJSONName) && canvasWrites.MatchString(c) {
			return true, reason
		}
	}
	return false, ""
}

// GuardHookAnswer is the hook's answer to Claude Code: a refusal with its reason, or nothing.
func GuardHookAnswer(raw []byte, cli string) []byte {
	var in GuardInput
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	deny, reason := GuardCanvas(in, cli)
	if !deny {
		return nil
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
	return append(out, '\n')
}

// GuardCLI is the command name agents are told to run: RUNTIME_CLI_NAME when the deployment names one.
func GuardCLI() string {
	if v := strings.TrimSpace(os.Getenv("RUNTIME_CLI_NAME")); v != "" {
		return v
	}
	return "harnessly"
}
