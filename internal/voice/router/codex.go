package router

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexAsker asks through the Codex CLI signed in with the person's ChatGPT plan (~/.codex): one non-interactive
// run in a read-only sandbox, its last message is the answer. It is the way when the runtime's own ChatGPT sign-in is
// not there, and the way the evals ask (evals/voice-router). An empty model is the plan's lightest, «gpt-5.6-luna».
func CodexAsker(bin, model, effort string) Asker {
	if bin == "" {
		bin = "codex"
	}
	if model == "" {
		model = "gpt-5.6-luna"
	}
	if effort == "" {
		effort = "low"
	}
	return func(ctx context.Context, instructions, prompt string) (string, error) {
		dir, err := os.MkdirTemp("", "voice-router-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "answer.txt")
		cmd := exec.CommandContext(ctx, bin, "exec", "-m", model, "-c", "model_reasoning_effort="+effort,
			"--skip-git-repo-check", "--ephemeral", "-s", "read-only", "-C", dir, "-o", out, "-")
		cmd.Stdin = strings.NewReader(instructions + "\n\nDo not run any command or read any file: answer from this " +
			"message only.\n\n" + prompt)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", errors.New("codex: " + clip(one(stderr.String()), 300))
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

// First asks the ways in order and answers the first that answers.
func First(ways ...Asker) Asker {
	return func(ctx context.Context, instructions, prompt string) (string, error) {
		var last error = errors.New("no way to ask a model")
		for _, w := range ways {
			if w == nil {
				continue
			}
			raw, err := w(ctx, instructions, prompt)
			if err == nil {
				return raw, nil
			}
			last = err
			if ctx.Err() != nil {
				break
			}
		}
		return "", last
	}
}
