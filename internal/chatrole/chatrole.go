// Package chatrole holds what a chat may do besides the person's permission mode, set by the orchestration of delegated
// chats (internal/delegation): a delegated chat that must show its plan first may not change anything until the plan is
// approved, and a chat the person made a coordinator hands work out instead of doing it.
//
// The runtime marks a chat with one small file named by the tmux session its agent runs in; the agent's PreToolUse hook
// (`harnessly hook voice-guard`, the one every chat already has) reads it and refuses what the role does not allow, with
// words that tell the agent what to do instead. No file, or one the hook cannot read, lets every call go.
package chatrole

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dir is where, under the runtime's data folder, the chats with a role are marked.
const Dir = "chat-role"

// Role is what a chat is held to.
type Role string

const (
	// None: the chat does what its permission mode allows.
	None Role = ""
	// Plan: the chat reads and plans; it changes no file and runs no command that writes, until its plan is approved.
	Plan Role = "plan"
	// Coordinate: the chat hands work out (delegate_task, the team's task list), reads and checks; it edits no file and
	// runs no build or test suite itself.
	Coordinate Role = "coordinate"
)

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Mark sets a session's role; None takes the mark away.
func Mark(dir, session string, r Role) error {
	if !sessionRe.MatchString(session) {
		return errors.New("bad session name")
	}
	path := filepath.Join(dir, session)
	if r == None {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if r != Plan && r != Coordinate {
		return errors.New("unknown role")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(r), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Of is a session's role; None for no mark or one that cannot be read.
func Of(dir, session string) Role {
	if !sessionRe.MatchString(session) {
		return None
	}
	raw, err := os.ReadFile(filepath.Join(dir, session))
	if err != nil {
		return None
	}
	switch r := Role(strings.TrimSpace(string(raw))); r {
	case Plan, Coordinate:
		return r
	}
	return None
}

// editTools change files whatever their input says.
var editTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

// shellTools run a command line.
var shellTools = map[string]bool{"Bash": true, "shell": true, "exec_command": true, "local_shell": true}

// readWords are the commands that only look. sed and git are looked at closer (readOnlyStep).
var readWords = map[string]bool{"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "rg": true, "egrep": true,
	"find": true, "/usr/bin/find": true, "wc": true, "pwd": true, "echo": true, "printf": true, "jq": true, "tree": true, "file": true,
	"stat": true, "du": true, "df": true, "which": true, "type": true, "uniq": true, "cut": true, "tr": true, "cd": true,
	"diff": true, "true": true, "date": true, "realpath": true, "dirname": true, "basename": true,
	"nl": true, "column": true, "sleep": true, "test": true, "[": true}

var readGit = map[string]bool{"log": true, "status": true, "diff": true, "show": true, "blame": true, "grep": true,
	"ls-files": true, "rev-parse": true, "branch": true, "describe": true, "shortlog": true, "remote": true, "fetch": true, "worktree": true}

var (
	splitRe    = regexp.MustCompile(`\|\||&&|[|;\n]`)
	redirectRe = regexp.MustCompile(`(^|[^0-9&>])>>?\s*([^&\s]+)`)
	findWrite  = regexp.MustCompile(`\s-(delete|exec|execdir|ok|fprint\w*)\b`)
	// sedPrint is the one sed a look needs: print lines by number (sed -n '10,40p' file).
	sedPrint = regexp.MustCompile(`^sed -n ['"]?[0-9]+(,([0-9]+|\$))?p['"]?( [^-;'"][^;'"]*)?$`)
)

// substRe finds what runs another command inside a line: $(…), `…`, <(…), >(…), xargs.
var substRe = regexp.MustCompile("\\$\\(|`|<\\(|>\\(|(^|[\\s|;&])xargs\\b")

// readOnly tells whether a command line only looks: every step of it is a looking command and it writes no file. A line
// that runs commands inside it is not taken for a look.
func readOnly(command string) bool {
	if substRe.MatchString(command) {
		return false
	}
	for _, m := range redirectRe.FindAllStringSubmatch(command, -1) {
		if m[2] != "/dev/null" {
			return false
		}
	}
	for _, step := range splitRe.Split(command, -1) {
		if !readOnlyStep(strings.Fields(strings.TrimSpace(step))) {
			return false
		}
	}
	return true
}

func readOnlyStep(words []string) bool {
	for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "=") {
		words = words[1:] // VAR=value in front
	}
	if len(words) == 0 {
		return true
	}
	line := strings.Join(words, " ")
	switch w := strings.TrimLeft(words[0], "("); {
	case w == "git":
		return len(words) > 1 && readGit[words[1]] && !(words[1] == "branch" && strings.Contains(line, " -D")) &&
			!(words[1] == "worktree" && (len(words) < 3 || words[2] != "list")) &&
			!(words[1] == "remote" && len(words) > 2 && words[2] != "-v") &&
			!(words[1] == "branch" && (strings.ContainsAny(line, "'\"\\$`{}") && len(words) > 2 || !branchLook(words[2:])))
	case w == "sort":
		// Quotes and escapes would hide a flag from the words seen here ("-o" is a word that starts with a quote).
		return !strings.ContainsAny(line, "'\"\\$`{}") && sortLook(words[1:])
	case w == "sed":
		return sedPrint.MatchString(line)
	case w == "find" || w == "/usr/bin/find":
		return !findWrite.MatchString(line)
	case w == "harnessly":
		return len(words) > 1 && (words[1] == "issue" || words[1] == "issues" || words[1] == "page" || words[1] == "pages" ||
			words[1] == "connections" || words[1] == "status")
	default:
		return readWords[w]
	}
}

// branchLook tells a `git branch` that only lists: its words are listing flags, the patterns of --list or a commit of
// --contains / --merged; -d, -m, --set-upstream-to and a bare name change something.
func branchLook(words []string) bool {
	list := false
	for i := 0; i < len(words); i++ {
		switch w := words[i]; {
		case w == "-a" || w == "-r" || w == "-v" || w == "-vv" || w == "--all" || w == "--remotes" || w == "--show-current" ||
			w == "--no-color" || w == "--color" || strings.HasPrefix(w, "--format=") || strings.HasPrefix(w, "--sort="):
		case w == "-l" || w == "--list":
			list = true
		case w == "--contains" || w == "--merged" || w == "--no-merged" || w == "--points-at":
			i++
		case !strings.HasPrefix(w, "-") && list:
		default:
			return false
		}
	}
	return true
}

// sortLook tells a sort that only reads: every flag is one of a short list. Anything else (-o, --compress-program, a
// clustered -uo, a long prefix like --outp) is not taken for a look.
func sortLook(words []string) bool {
	for _, w := range words {
		if !strings.HasPrefix(w, "-") || w == "-" {
			continue
		}
		if strings.HasPrefix(w, "--") {
			switch w {
			case "--unique", "--reverse", "--numeric-sort", "--ignore-case", "--ignore-leading-blanks", "--human-numeric-sort",
				"--version-sort", "--stable", "--general-numeric-sort", "--month-sort", "--random-sort", "--dictionary-order":
				continue
			}
			if strings.HasPrefix(w, "--key=") || strings.HasPrefix(w, "--field-separator=") {
				continue
			}
			return false
		}
		// -k and -t carry their value in the same word or the next one; the other letters are plain switches.
		rest := w[1:]
		if rest[0] == 'k' || rest[0] == 't' {
			continue
		}
		for _, c := range rest {
			if !strings.ContainsRune("urnfbhVsgMRd", c) {
				return false
			}
		}
	}
	return true
}

// heavy are the commands a coordinator hands out: builds, test suites, type checks and linters.
var heavy = regexp.MustCompile(`(^|[\s;&|(])(go\s+(build|test|vet|run)|vitest|jest|pytest|tsc|eslint|cargo\s+(build|test)|` +
	`(pnpm|npm|yarn|bun)\s+(run\s+)?(build|test|lint|typecheck|check)|make(\s|$)|docker\s+build|gradle|mvn)`)

// Verdict is why a call is refused, "" when it may go.
func Verdict(r Role, tool, command string) string {
	switch r {
	case Plan:
		if editTools[tool] || shellTools[tool] && !readOnly(command) {
			return "This chat must have its plan approved before it changes anything: it may read and look around, but not " +
				"edit files or run commands that write. Send the plan with submit_plan (the tool of the harnessly MCP server), " +
				"end the turn with [[question]] and wait: once the plan is approved the edits are allowed and you are told to go on."
		}
	case Coordinate:
		if editTools[tool] {
			return "This chat is the coordinator: it does not edit files itself. Hand the change to a delegated chat " +
				"(delegate_task, or a task on the team's list with task_add) and check its result."
		}
		if shellTools[tool] && heavy.MatchString(command) {
			return "This chat is the coordinator: builds, test suites and linters run in the delegated chats, not here. " +
				"Give it to a delegated chat (send_to_conversation, or a check on the task with task_add) and read its result."
		}
	}
	return ""
}

// HookInput is the part of a PreToolUse hook's input read here (Claude Code's and Codex's alike).
type HookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command any `json:"command"`
	} `json:"tool_input"`
}

func (in HookInput) command() string {
	switch c := in.ToolInput.Command.(type) {
	case string:
		return c
	case []any:
		words := make([]string, 0, len(c))
		for _, w := range c {
			if s, ok := w.(string); ok {
				words = append(words, s)
			}
		}
		// Codex gives ["bash", "-lc", "<line>"]: the line is what runs.
		if len(words) == 3 && (words[0] == "bash" || words[0] == "sh" || words[0] == "zsh") && strings.HasPrefix(words[1], "-") {
			return words[2]
		}
		return strings.Join(words, " ")
	}
	return ""
}

// Answer is the hook's answer for a chat of role r to the call raw: nil lets it go, else a refusal the agent reads.
func Answer(raw []byte, r Role) []byte {
	if r == None {
		return nil
	}
	var in HookInput
	if json.Unmarshal(raw, &in) != nil || in.ToolName == "" {
		return nil
	}
	why := Verdict(r, in.ToolName, in.command())
	if why == "" {
		return nil
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": why,
	}})
	return out
}

// FailedText is why a call of a chat with a role is refused when the hook itself could not run.
const FailedText = "The role check of this chat (plan or coordinator) could not run, so the call is refused to be safe. " +
	"Try again; if it keeps failing, tell the person that the harnessly hook of this chat is broken."

// GuardCommand wraps a hook's command line so that it fails closed for a chat with a role: when the command fails
// (crashed, missing, killed) and the chat's session is marked under dir, the call is refused with FailedText; a chat
// without a role is let through as before. It is one shell group, so a caller may add redirections after it.
func GuardCommand(cmd, dir string) string {
	deny, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": FailedText,
	}})
	return "{ hl_in=$(cat); hl_out=$(printf '%s' \"$hl_in\" | " + cmd + " 2>/dev/null); hl_rc=$?; " +
		"if [ $hl_rc -eq 0 ]; then printf '%s' \"$hl_out\"; else " +
		"hl_s=\"${HARNESSLY_SESSION:-$(tmux display-message -p '#{session_name}' 2>/dev/null)}\"; " +
		"if [ -n \"$hl_s\" ] && [ -f " + shellQuote(dir) + "/\"$hl_s\" ]; then printf '%s' " + shellQuote(string(deny)) + "; fi; fi; }"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
