package handoff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// NoteTimeout is how long an agent has to write a note.
const NoteTimeout = 180 * time.Second

// NoteRun is one note to write: the agent of Provider asked Prompt on a throwaway fork of its session SessionID, in
// Cwd, with no tools that change anything. Nothing of it is added to the session.
type NoteRun struct {
	Provider         wire.Provider
	SessionID, Cwd   string
	Model, Prompt    string
	BaseEnv          []string
	Timeout          time.Duration
	Claude           sidechat.Runtime
	ClaudeProjectDir string
	CodexBinary      string
	Log              func(level, msg string, kv ...any)
	askClaude        func(ctx context.Context, run sidechat.Run, onEvent func(sidechat.Event))
}

// ErrNoNote: the agent wrote nothing; the error's text says why when the CLI did.
var ErrNoNote = errors.New("the agent wrote no note")

// WriteNote asks the agent for the note and returns it. Claude answers from a fork that is never saved and has no
// tools (internal/sidechat); Codex from `codex exec fork --ephemeral` in a read-only sandbox with its hooks off.
func WriteNote(ctx context.Context, run NoteRun) (string, error) {
	if run.Timeout == 0 {
		run.Timeout = NoteTimeout
	}
	if !agents.IsUUID(run.SessionID) {
		return "", errors.New("no agent session to fork")
	}
	if run.Provider == wire.ProviderCodex {
		return codexNote(ctx, run)
	}
	ask := run.askClaude
	if ask == nil {
		ask = sidechat.Ask
	}
	var text, failure string
	ask(ctx, sidechat.Run{SessionID: run.SessionID, Cwd: run.Cwd, Prompt: run.Prompt, Model: run.Model, Runtime: run.Claude,
		BaseEnv: run.BaseEnv, Timeout: run.Timeout, ProjectDir: run.ClaudeProjectDir, Log: run.Log},
		func(e sidechat.Event) {
			switch e.Type {
			case "done":
				text = e.Text
			case "error":
				failure = e.Message
			}
		})
	if text = strings.TrimSpace(text); text != "" {
		return text, nil
	}
	if failure != "" {
		return "", errors.New(failure)
	}
	return "", ErrNoNote
}

// CodexNoteArgs are the arguments of `codex` that write a note from a fork of session: never saved, read-only, no
// approvals and no hooks (the fork is no conversation of the runtime's), the last message into out, the prompt on stdin.
func CodexNoteArgs(session, model, out string) []string {
	args := []string{"exec", "fork", session, "--ephemeral", "--skip-git-repo-check", "--disable", "hooks",
		"-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`}
	if model != "" && agents.ModelIDRe.MatchString(model) {
		args = append(args, "-m", model)
	}
	return append(args, "-o", out, "-")
}

func codexNote(ctx context.Context, run NoteRun) (string, error) {
	dir, err := os.MkdirTemp("", "handoff-note-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "note.md")
	bin := run.CodexBinary
	if bin == "" {
		bin = "codex"
	}
	ctx, cancel := context.WithTimeout(ctx, run.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, CodexNoteArgs(run.SessionID, run.Model, out)...)
	cmd.Dir = run.Cwd
	cmd.Env = run.BaseEnv
	cmd.Stdin = strings.NewReader(run.Prompt)
	cmd.WaitDelay = 3 * time.Second
	var stderr tailBuffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	raw, _ := os.ReadFile(out)
	if text := strings.TrimSpace(string(raw)); text != "" {
		return text, nil
	}
	if ctx.Err() != nil {
		return "", errors.New("the agent did not write the note in time")
	}
	if runErr != nil {
		if run.Log != nil {
			run.Log("warn", "codex note failed", "err", runErr.Error(), "stderr", stderr.String())
		}
		return "", errors.New("Codex could not write the note: " + lastLine(stderr.String()))
	}
	return "", ErrNoNote
}

// tailBuffer keeps the last 4000 bytes written.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 4000 {
		t.b = t.b[len(t.b)-4000:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.b) }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if r := []rune(line); len(r) > 200 {
		line = string(r[:200]) + "…"
	}
	return line
}
