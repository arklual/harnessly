package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/share"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A turn's summary for the notification «готово»: a few lines from the cheapest model on what the agent did,
// read from the person's message and the agent's replies only — the tools' input and output never go in. Claude runs
// on Haiku in a folder of its own and keeps its session there, so its cost is in the machine's spend like any call of
// Claude; Codex runs as for a title. The calls take the machine's one slot with the titles.

const (
	summaryCallTimeout   = 90 * time.Second
	summaryMessageRunes  = 1500
	summaryRepliesRunes  = 6000
	summaryMaxLines      = 4
	summaryLineRunes     = 160
	summaryTotalRunes    = 420
	summaryFolderPerm    = 0o700
	summaryNoSecretsRule = "Never quote commands, code, file contents, logs, paths, tokens, keys, passwords or links."
)

// v2Summarizer writes turn summaries in the titler's slot.
type v2Summarizer struct {
	titler *v2Titler
	// dir is where Claude runs and keeps the summaries' sessions.
	dir string
	// run is the CLI call; a test puts its own.
	run func(ctx context.Context, p wire.Provider, prompt string) (string, error)
}

func newV2Summarizer(t *v2Titler, dir string) *v2Summarizer {
	s := &v2Summarizer{titler: t, dir: dir}
	s.run = s.runCLI
	return s
}

func (s *v2Summarizer) Summarize(p wire.Provider, message string, replies []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), summaryCallTimeout)
	defer cancel()
	select {
	case s.titler.slot <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-s.titler.slot }()
	out, err := s.run(ctx, p, summaryPrompt(message, replies))
	if err != nil {
		return "", err
	}
	text := cleanSummary(out)
	if text == "" {
		return "", errors.New("the model answered with no summary")
	}
	redactor := share.NewRedactor(share.RedactorConfig{KnownSecrets: share.EnvSecrets(os.Environ(), "")})
	return redactor.Redact(text), nil
}

// lastRunes is the end of s: what the agent said last tells best what the turn came to.
func lastRunes(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-runes:])
}

func summaryPrompt(message string, replies []string) string {
	return "Sum up for a notification what an AI agent did in one turn of a chat: 2 to 4 short lines, in the language of " +
		"the person's message, plain text with no markdown and no list markers. Say what was done and what came of it; " +
		"if the agent asks the person something or waits for them, say what. " + summaryNoSecretsRule +
		" Reply with the lines only.\n\n" +
		"Person's message:\n" + cut(message, summaryMessageRunes) + "\n\n" +
		"Agent's replies:\n" + lastRunes(strings.Join(replies, "\n\n"), summaryRepliesRunes)
}

// cleanSummary keeps at most four lines of what the model said, without the markup and the code it may add anyway.
func cleanSummary(out string) string {
	var lines []string
	total := 0
	inCode := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#*->•· \t"))
		line = strings.ReplaceAll(strings.ReplaceAll(line, "**", ""), "`", "")
		if line == "" {
			continue
		}
		line = cut(line, summaryLineRunes)
		total += utf8.RuneCountInString(line)
		if total > summaryTotalRunes && len(lines) > 0 {
			break
		}
		lines = append(lines, line)
		if len(lines) == summaryMaxLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func (s *v2Summarizer) runCLI(ctx context.Context, p wire.Provider, prompt string) (string, error) {
	var cmd *exec.Cmd
	if p == wire.ProviderCodex {
		dir, err := os.MkdirTemp("", "hs-summary-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		cmd = exec.CommandContext(ctx, "codex", "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-c",
			`approval_policy="never"`, prompt)
		cmd.Env = withAgentEnv(os.Environ(), s.titler.envDir, agents.Codex)
		cmd.Dir = dir
	} else {
		if err := os.MkdirAll(s.dir, summaryFolderPerm); err != nil {
			return "", err
		}
		rt := sidechat.ReadRuntime(s.titler.runtimeFile)
		// The session is kept (no --no-session-persistence): its transcript is what the spend counts.
		cmd = exec.CommandContext(ctx, rt.Binary, "--print", "--model", "haiku", "--tools", "",
			"--strict-mcp-config", "--disable-slash-commands", "--settings", `{"disableAllHooks":true}`)
		cmd.Env = sidechat.Env(withAgentEnv(os.Environ(), s.titler.envDir, agents.Claude), rt)
		cmd.Stdin = strings.NewReader(prompt)
		cmd.Dir = s.dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
