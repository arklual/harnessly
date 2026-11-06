package apps

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// The Codex adapter of the app-guard hook. The decision is the same one Claude's hook gets (Snapshot.Decide); what
// differs is how a call arrives and how the answer is given:
//
//   - Codex names its tools its own way (shell commands, apply_patch, its file readers), so CodexCalls turns a call into
//     the Calls the apps' rules are written for (Bash, Edit, Write, Read, mcp__…) by the explicit table below. A tool the
//     table does not know is refused: a guard that let through what it cannot read would be open.
//   - Codex's PreToolUse hook has no "ask": permissionDecision "ask" and "allow" are rejected by the CLI and the call goes
//     on, so a rule that asks the person to confirm is a refusal here (a deliberate tightening, docs/build-apps/05-agents.md).
//     What the guard does not refuse it does not answer at all: silence is the only "go on" Codex accepts.
//   - The answer to a refusal is {"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny",
//     "permissionDecisionReason":…}} with a reason that is not empty (Codex rejects a deny without one).

// codexKind is what the table makes of a Codex tool.
type codexKind int

const (
	codexUnknown codexKind = iota
	codexShell
	codexStdin
	codexPatch
	codexRead
	codexImage
	codexSearch
	codexHarmless
)

// codexTools is the table of the tools Codex's PreToolUse reports, by the name in the hook's tool_name. Shell commands
// are Bash to the rules, apply_patch is the file tools (Edit and Write both), its readers and view_image are Read and
// Grep, web_search is WebSearch. Only update_plan and request_user_input have no effect on the machine or the apps and are
// not judged. mcp__* tools are not here: they keep their names.
var codexTools = map[string]codexKind{
	"Bash": codexShell, "shell": codexShell, "shell_command": codexShell, "local_shell": codexShell,
	"exec_command": codexShell, "unified_exec": codexShell, "container.exec": codexShell,
	"write_stdin": codexStdin,
	"apply_patch": codexPatch,
	"read_file":   codexRead, "list_dir": codexRead, "grep_files": codexRead,
	"view_image": codexImage, "web_search": codexSearch,
	"update_plan": codexHarmless, "request_user_input": codexHarmless,
}

// problemStdin marks input typed into a running process: whether it is allowed depends on the snapshot, not on the call.
const problemStdin = "stdin"

// CodexCalls reads one PreToolUse input of Codex into the calls the rules decide. skip is true for a call no rule can
// concern (a harmless tool). A non-empty problem says why the call cannot be read; the caller refuses it.
func CodexCalls(raw []byte) (calls []Call, skip bool, problem string) {
	var in struct {
		Tool  string         `json:"tool_name"`
		Input map[string]any `json:"tool_input"`
		Cwd   string         `json:"cwd"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Tool == "" {
		return nil, false, "the tool call could not be read"
	}
	if strings.HasPrefix(in.Tool, "mcp__") {
		return []Call{{Tool: foldHostServer(in.Tool), Input: in.Input}}, false, ""
	}
	switch codexTools[in.Tool] {
	case codexShell:
		line, ok := codexCommand(in.Input)
		if !ok {
			return nil, false, "the command of the shell call could not be read"
		}
		return []Call{{Tool: "Bash", Input: map[string]any{"command": line}}}, false, ""
	case codexStdin:
		chars, _ := in.Input["chars"].(string)
		if chars == "" {
			// A poll of a process that is running: it writes nothing.
			return nil, true, ""
		}
		// Text typed into a running process may be a command given a character at a time, which no piece of it shows.
		return nil, false, problemStdin
	case codexPatch:
		paths, ok := patchPaths(in.Input, in.Cwd)
		if !ok {
			return nil, false, "the files of the patch could not be read"
		}
		for _, p := range paths {
			// Edit and Write are both asked: an app that restricts either restricts the patch, whichever of them it names.
			for _, tool := range []string{"Edit", "Write"} {
				calls = append(calls, Call{Tool: tool, Input: map[string]any{"file_path": p}})
			}
		}
		return calls, false, ""
	case codexRead:
		p := firstString(in.Input, "file_path", "path", "dir_path")
		if p == "" {
			return nil, false, "the path of the read call could not be read"
		}
		tool := "Read"
		if in.Tool != "read_file" {
			tool = "Grep"
		}
		return []Call{{Tool: tool, Input: map[string]any{"path": absPath(p, in.Cwd)}}}, false, ""
	case codexImage:
		p := firstString(in.Input, "path", "file_path")
		if p == "" {
			return nil, false, "the path of the image could not be read"
		}
		return []Call{{Tool: "Read", Input: map[string]any{"path": absPath(p, in.Cwd)}}}, false, ""
	case codexSearch:
		q, _ := in.Input["query"].(string)
		return []Call{{Tool: "WebSearch", Input: map[string]any{"query": q}}}, false, ""
	case codexHarmless:
		return nil, true, ""
	}
	return nil, false, "the tool " + in.Tool + " is not one the apps' rules know"
}

// foldHostServer reads the host's app server the way the rules name it: a tool name may spell its hyphen as '_'.
func foldHostServer(tool string) string {
	const folded = "mcp__harnessly_app__"
	if rest, ok := strings.CutPrefix(tool, folded); ok {
		return "mcp__" + hostServer + "__" + rest
	}
	return tool
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func absPath(p, cwd string) string {
	if filepath.IsAbs(p) || cwd == "" {
		return p
	}
	return filepath.Join(cwd, p)
}

// codexCommand is the shell line of a shell call: `command` is a string or an argv (["bash","-lc","git status"], whose
// script is the line the rules read), exec_command calls it `cmd`.
func codexCommand(input map[string]any) (string, bool) {
	for _, key := range []string{"command", "cmd"} {
		switch v := input[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v, true
			}
		case []any:
			words := make([]string, 0, len(v))
			for _, w := range v {
				s, ok := w.(string)
				if !ok {
					return "", false
				}
				words = append(words, s)
			}
			if len(words) == 0 {
				return "", false
			}
			// Only [shell, -c|-lc|-cl, script] is the script itself; any other argv (trailing words, -xc, -o … -c) is
			// judged as the one line it would run as.
			if len(words) == 3 && isShellName(words[0]) && (words[1] == "-c" || words[1] == "-lc" || words[1] == "-cl") {
				return words[2], true
			}
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = quoteWord(w)
			}
			return strings.Join(quoted, " "), true
		}
	}
	return "", false
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// quoteWord leaves a plain word as it is and single-quotes any other, so the line reads as the argv ran.
func quoteWord(w string) string {
	if plainWord.MatchString(w) {
		return w
	}
	return agents.ShellQuote(w)
}

func isShellName(w string) bool {
	switch base(w) {
	case "bash", "sh", "zsh", "dash":
		return true
	}
	return false
}

// patchPaths are the files an apply_patch call names: Add File, Update File, Delete File and Move to lines, made
// absolute against the call's folder. A patch that names none, or a relative path with no folder to start from, cannot be
// judged.
func patchPaths(input map[string]any, cwd string) ([]string, bool) {
	patch := firstString(input, "command", "input", "patch")
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File:", "*** Update File:", "*** Delete File:", "*** Move to:"} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				p = strings.TrimSpace(p)
				if p == "" || (!filepath.IsAbs(p) && cwd == "") {
					return nil, false
				}
				out = append(out, absPath(p, cwd))
			}
		}
	}
	return out, len(out) > 0
}

// CodexAnswer is what the guard writes to Codex for one PreToolUse input: nil lets the call go on to Codex's own
// permissions, anything else is a refusal.
func (s Snapshot) CodexAnswer(raw []byte) []byte {
	out, _, _ := s.CodexJudge(raw)
	return out
}

// CodexJudge is CodexAnswer with the rule that refused the call and the call's tool, for the journal; the verdict is
// empty when no app's rule decided (a call that goes on, or one the guard cannot read).
func (s Snapshot) CodexJudge(raw []byte) ([]byte, Verdict, string) {
	calls, skip, problem := CodexCalls(raw)
	if problem == problemStdin {
		if !s.judgesShell() {
			return nil, Verdict{}, ""
		}
		return CodexDeny("Interactive input into a shell is not checked by the rules of the apps of this chat (it can be a command typed a character at a time): run the command whole."), Verdict{}, ""
	}
	if problem != "" {
		return CodexDeny("The app guard cannot let this call through: " + problem + ", and the apps of this chat ask for their rules to hold."), Verdict{}, ""
	}
	if skip {
		return nil, Verdict{}, ""
	}
	if reason := s.unknownPlugin(calls); reason != "" {
		return CodexDeny(reason), Verdict{}, ""
	}
	best, reason, tool := Verdict{}, "", ""
	for _, call := range calls {
		v, r := s.Decide(call)
		if v.Decision.rank() > best.Decision.rank() {
			best, reason, tool = v, r, call.Tool
		}
	}
	switch best.Decision {
	case DecideDeny:
		return CodexDeny(reason), best, tool
	case DecideConfirm:
		return CodexDeny(reason + " Codex has no step where the person confirms a call, so it is refused: do it another way, or ask the person."), best, tool
	}
	return nil, Verdict{}, ""
}

// judgesShell says whether the snapshot has anything to say about shell commands: a Bash rule of any app (or the rule *),
// or folders the agent must not write.
func (s Snapshot) judgesShell() bool {
	if len(s.Protected) > 0 {
		return true
	}
	for _, a := range s.Apps {
		if a.Tools == nil {
			continue
		}
		for _, rules := range [][]string{a.Tools.Allow, a.Tools.Deny, a.Tools.Confirm} {
			for _, r := range rules {
				if r == "*" || r == "Bash" || strings.HasPrefix(r, "Bash(") {
					return true
				}
			}
		}
	}
	return false
}

// unknownPlugin refuses a call of an MCP server that claims to be a plugin's (plugin_<plugin>_<server>) of an app that
// is not on in the chat: Codex has no plugins, so the name cannot be one of the launch's own.
func (s Snapshot) unknownPlugin(calls []Call) string {
	for _, call := range calls {
		_, _, plugin, ok := mcpParts(call.Tool)
		if !ok || plugin == "" {
			if ok && strings.HasPrefix(strings.TrimPrefix(call.Tool, "mcp__"), "plugin_") {
				return "The app guard cannot read which app the tool " + call.Tool + " belongs to, so it does not let the call through."
			}
			continue
		}
		known := false
		for _, a := range s.Apps {
			known = known || PluginName(a.ID) == plugin
		}
		if !known {
			return "The tool " + call.Tool + " belongs to an app that is not on in this chat."
		}
	}
	return ""
}

// CodexDeny is the answer that refuses a call, with the reason Codex needs.
func CodexDeny(reason string) []byte {
	if strings.TrimSpace(reason) == "" {
		reason = "The apps of this chat do not allow this call."
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
	return append(out, '\n')
}
