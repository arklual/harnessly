package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	titleCallTimeout = 90 * time.Second
	titleMaxRunes    = 60
	titleInputRunes  = 700
)

// v2Titler makes a short title for a chat whose agent wrote none: one cheap, tool-less, session-less call of the agent's
// own CLI (Claude on Haiku; Codex's default model). The calls take turns: the machine is small and a chat's title can wait.
type v2Titler struct {
	runtimeFile string
	slot        chan struct{}
	// run is the CLI call; a test puts its own.
	run func(ctx context.Context, p wire.Provider, prompt string) (string, error)
}

func newV2Titler(runtimeFile string) *v2Titler {
	t := &v2Titler{runtimeFile: runtimeFile, slot: make(chan struct{}, 1)}
	t.run = t.runCLI
	return t
}

func (t *v2Titler) Title(p wire.Provider, firstMessage, firstAnswer string) (string, error) {
	t.slot <- struct{}{}
	defer func() { <-t.slot }()
	ctx, cancel := context.WithTimeout(context.Background(), titleCallTimeout)
	defer cancel()
	out, err := t.run(ctx, p, titlePrompt(firstMessage, firstAnswer))
	if err != nil {
		return "", err
	}
	title := cleanTitle(out)
	if title == "" {
		return "", errors.New("the agent answered with no title")
	}
	return title, nil
}

func cut(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	return string([]rune(s)[:runes]) + "…"
}

func titlePrompt(message, answer string) string {
	return "Name this chat for a sidebar list: 2 to 6 words, in the language of the person's message, no quotes, no final " +
		"period, say what the chat is about, not how it starts. Reply with the title only.\n\n" +
		"Person's first message:\n" + cut(message, titleInputRunes) + "\n\nAgent's first answer:\n" + cut(answer, titleInputRunes)
}

// cleanTitle takes the first non-empty line of what the agent said, without the quotes, markup and final period it adds.
func cleanTitle(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "#*-> \t"))
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "Title:"), "Название:"))
		line = strings.Trim(line, "\"'`«»“”*. ")
		if line != "" {
			return cut(line, titleMaxRunes)
		}
	}
	return ""
}

func (t *v2Titler) runCLI(ctx context.Context, p wire.Provider, prompt string) (string, error) {
	dir, err := os.MkdirTemp("", "hs-title-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	var cmd *exec.Cmd
	if p == wire.ProviderCodex {
		cmd = exec.CommandContext(ctx, "codex", "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-c",
			`approval_policy="never"`, prompt)
		cmd.Env = os.Environ()
	} else {
		rt := sidechat.ReadRuntime(t.runtimeFile)
		cmd = exec.CommandContext(ctx, rt.Binary, "--print", "--model", "haiku", "--no-session-persistence", "--tools", "",
			"--strict-mcp-config", "--disable-slash-commands", "--settings", `{"disableAllHooks":true}`)
		cmd.Env = sidechat.Env(os.Environ(), rt)
		cmd.Stdin = strings.NewReader(prompt)
	}
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
