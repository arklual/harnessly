package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sidechat"
)

// The background features on the machine's own agents: a model of Claude Code or Codex, through its CLI and
// the machine's sign-in (the person's plan), for a person with no provider's key. One call is one answer: no session
// kept, no tools, MCP servers, slash commands or hooks, in an empty folder.

// bgAgentCaller asks the machine's agents for the background features.
type bgAgentCaller struct {
	runtimeFile string
	envDir      string
}

func newBgAgentCaller(dataDir string) bgmodel.AgentCall {
	c := bgAgentCaller{runtimeFile: os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"), envDir: agentEnvDir(dataDir)}
	return c.call
}

var errBgAgentModel = errors.New("background: not a model of the agent")

func (c bgAgentCaller) call(ctx context.Context, agent, model string, call bgmodel.Call, onDelta func(string)) (string, error) {
	if model != "" && !agents.ModelIDRe.MatchString(model) {
		return "", errBgAgentModel
	}
	dir, err := os.MkdirTemp("", "hs-bg-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	var cmd *exec.Cmd
	stream := false
	if agent == bgmodel.AgentCodex {
		args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-c", `approval_policy="never"`}
		if model != "" {
			args = append(args, "-m", model)
		}
		cmd = exec.CommandContext(ctx, "codex", append(args, "-")...)
		cmd.Env = withAgentEnv(os.Environ(), c.envDir, agents.Codex)
		// Codex has no system prompt of its own here: the feature's rules come first in the prompt.
		cmd.Stdin = strings.NewReader(bgAgentPrompt(call))
	} else {
		rt := sidechat.ReadRuntime(c.runtimeFile)
		args := bgClaudeArgs(model, call.System)
		if onDelta != nil {
			stream = true
			args = append(args, "--output-format", "stream-json", "--include-partial-messages", "--verbose")
		}
		cmd = exec.CommandContext(ctx, rt.Binary, args...)
		cmd.Env = sidechat.Env(withAgentEnv(os.Environ(), c.envDir, agents.Claude), rt)
		cmd.Stdin = strings.NewReader(call.Prompt)
	}
	cmd.Dir = dir
	if !stream {
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(out))
		if onDelta != nil && text != "" {
			onDelta(text)
		}
		return text, nil
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	text := readClaudeStream(pipe, onDelta)
	if err := cmd.Wait(); err != nil && text == "" {
		return "", err
	}
	return text, nil
}

// bgClaudeArgs make Claude Code answer once with the feature's own system prompt and nothing of the machine's setup.
func bgClaudeArgs(model, system string) []string {
	args := []string{"--print", "--no-session-persistence", "--tools", "", "--strict-mcp-config", "--disable-slash-commands",
		"--settings", `{"disableAllHooks":true}`}
	if model != "" {
		args = append(args, "--model", model)
	}
	if system != "" {
		args = append(args, "--system-prompt", system)
	}
	return args
}

// bgAgentPrompt is the call as one prompt: its rules, then what it asks.
func bgAgentPrompt(call bgmodel.Call) string {
	if call.System == "" {
		return call.Prompt
	}
	return call.System + "\n\n" + call.Prompt
}

// readClaudeStream hands the text of Claude Code's stream-json to onDelta as it comes and answers the whole text: the
// text deltas, or the assistant's message when the stream had none.
func readClaudeStream(r io.Reader, onDelta func(string)) string {
	var deltas, whole strings.Builder
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		v, err := jsvalue.Parse(sc.Bytes())
		o, ok := v.(*jsvalue.Object)
		if err != nil || !ok {
			continue
		}
		switch o.String("type") {
		case "stream_event":
			inner := o.Object("event")
			if inner == nil || inner.String("type") != "content_block_delta" {
				continue
			}
			if d := inner.Object("delta"); d != nil && d.String("type") == "text_delta" && d.String("text") != "" {
				deltas.WriteString(d.String("text"))
				onDelta(d.String("text"))
			}
		case "assistant":
			if m := o.Object("message"); m != nil {
				for _, part := range m.Array("content") {
					if p, ok := part.(*jsvalue.Object); ok && p.String("type") == "text" {
						whole.WriteString(p.String("text"))
					}
				}
			}
		}
	}
	if deltas.Len() > 0 {
		return deltas.String()
	}
	if whole.Len() > 0 {
		onDelta(whole.String())
	}
	return whole.String()
}
