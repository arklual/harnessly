package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The agent's scenario (HRN-795): a real Claude of a given binary, on the cheapest model, in a throwaway pane, asks one
// question with AskUserQuestion; the screen is read the way the typist reads it, the second option is chosen with its
// digit the way the question card answers, and the agent's reply must name that option. It sees what a stand-in cannot:
// a new Claude whose dialog the runtime no longer reads, whose keys no longer answer it, or that does not start.
//
//	agent_question  the question dialog is recognised on the screen, with its options
//	agent_answer    the chosen option reaches the agent, which says so and finishes its turn

const (
	agentStartWait  = 90 * time.Second // the first paint, the login and the model's first turn
	agentAnswerWait = 60 * time.Second
	agentPoll       = 500 * time.Millisecond
	agentModel      = "haiku"
	agentMark       = "HS-SELFTEST-PICKED-"
)

// agentPrompt asks for one question and a reply that proves which option came back. No single quotes: it goes into the
// pane's shell inside them.
const agentPrompt = "This is an automated self-test of the terminal harness. Call the AskUserQuestion tool exactly once " +
	"with one question, header Selftest, text: Pick one, multiSelect false, and exactly two options labelled Alpha and " +
	"Bravo. When the answer comes back, reply with only " + agentMark + " followed by the label that was chosen, in " +
	"capitals, and nothing else. Do not use any other tool."

// agentPanes is what the agent's scenario needs of tmux on top of the terminal's self-test.
type agentPanes interface {
	selftestPanes
	CaptureStyled(name string) (string, bool)
}

type agentSelftest struct {
	selftest
	panes agentPanes
	// Binary is the Claude to try: an absolute path (a candidate of the updater), else what runtime.json names.
	Binary string
	// Env is the environment the agent starts with (sidechat.Env: no updates, the machine's login).
	Env []string
	// Dir is the folder the agent starts in; it is made and trusted once.
	Dir string
}

// claudeSelftestDir is where the scenario runs: one folder of the runtime's own, so the trust dialog is answered once
// per machine and nothing of a project is near.
func claudeSelftestDir(dataDir string) string { return filepath.Join(dataDir, "selftest", "claude") }

// newAgentSelftest tries the Claude of runtime.json, or binary when it is an absolute path.
func newAgentSelftest(t *tmux.Tmux, dataDir, binary string) (agentSelftest, error) {
	rt := sidechat.ReadRuntime(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"))
	if binary != "" {
		if !filepath.IsAbs(binary) {
			return agentSelftest{}, errors.New("the Claude to try must be an absolute path")
		}
		rt.Binary = binary
	}
	// The agent's network setup (proxies and the like) as a tab's agent gets it, then the side chat's switches.
	env := sidechat.Env(withAgentEnv(nil, agentEnvDir(dataDir), agents.Claude), rt)
	return agentSelftest{selftest: newSelftest(t), panes: t, Binary: rt.Binary, Env: agentEnvPairs(env),
		Dir: claudeSelftestDir(dataDir)}, nil
}

// agentEnvPairs keeps the variables sidechat.Env sets on top of an empty base: those go on the agent's command line.
func agentEnvPairs(env []string) []string {
	var out []string
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && k != "" {
			out = append(out, kv)
		}
	}
	return out
}

func quoteArg(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// script is a small sh file that starts the agent with its environment, model, no settings of the person (no hooks or
// plugins of theirs reach a self-test) and the prompt. The environment may hold proxies and keys: it goes into a 0700
// file of the runtime's own and never onto the pane's command line, its screen or the shell's history.
func (a agentSelftest) script() string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, kv := range a.Env {
		k, v, _ := strings.Cut(kv, "=")
		b.WriteString("export " + k + "=" + quoteArg(v) + "\n")
	}
	b.WriteString("exec " + quoteArg(a.Binary) + " --model " + agentModel + " --setting-sources project " + quoteArg(agentPrompt) + "\n")
	return b.String()
}

// command is the line typed into the pane's shell: the script, by its path alone.
func (a agentSelftest) command(script string) string {
	return "cd " + quoteArg(a.Dir) + " && " + quoteArg(script)
}

// look reads the pane the way the typist does.
func (a agentSelftest) look(name string) typist.Screen {
	styled, ok := a.panes.CaptureStyled(name)
	if !ok {
		return typist.Screen{}
	}
	return typist.Read(styled, wire.ProviderClaude)
}

func (a agentSelftest) run(ctx context.Context) []machineCheck {
	question := machineCheck{ID: "agent_question", Title: "Агент: вопрос агента распознаётся"}
	answer := machineCheck{ID: "agent_answer", Title: "Агент: ответ на вопрос доходит"}
	out := func() []machineCheck { return []machineCheck{question, answer} }
	skip := func() { answer.Detail = "не проверено: предыдущий шаг не прошёл" }
	fail := func(why string) []machineCheck { question.Detail = why; skip(); return out() }

	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return fail("папка проверки не создалась: " + err.Error())
	}
	if _, err := os.Stat(a.Binary); err != nil && filepath.IsAbs(a.Binary) {
		return fail("нет Claude: " + a.Binary)
	}
	name := selftestName(a.panes.Names().Prefix)
	if err := a.panes.CreateSession(name, 120, 40, a.Dir); err != nil {
		return fail("панель не создалась: " + err.Error())
	}
	defer a.panes.KillSession(name)
	if !a.waitFor(ctx, selftestShellWait, func() bool { return tmux.SafeShells[a.panes.PaneCurrentCommand(name)] }) {
		return fail("оболочка панели не поднялась")
	}
	script := filepath.Join(a.Dir, "."+name+".sh")
	if err := os.WriteFile(script, []byte(a.script()), 0o700); err != nil {
		return fail("скрипт запуска не записался: " + err.Error())
	}
	defer os.Remove(script)
	if err := a.panes.SendKeys(name, a.command(script)); err != nil {
		return fail("команда не ушла в панель: " + err.Error())
	}

	// The agent takes the foreground first; until then the shell there is not the agent gone.
	if !a.waitForEvery(ctx, selftestShellWait, agentPoll, func() bool { return !tmux.SafeShells[a.panes.PaneCurrentCommand(name)] }) {
		return fail("агент не запустился или сразу закрылся: " + lastLines(a.look(name).Plain, 3))
	}

	// The dialog: the folder's trust is answered on the way, the way a tab answers it.
	var s typist.Screen
	exited := false
	found := a.waitForEvery(ctx, agentStartWait, agentPoll, func() bool {
		if tmux.SafeShells[a.panes.PaneCurrentCommand(name)] {
			exited = true
			return true
		}
		s = a.look(name)
		if keys := screen.TrustAcceptKeys(s.Plain); keys != nil {
			_ = a.panes.SendRawKeys(name, keys[0])
			return false
		}
		return s.Menu == typist.MenuQuestion
	})
	switch {
	case exited:
		return fail("агент закрылся, не задав вопроса: " + lastLines(s.Plain, 3))
	case !found:
		return fail("за " + agentStartWait.String() + " на экране не распознан вопрос: " + lastLines(s.Plain, 3))
	case !hasOption(s.Options, "Bravo"):
		return fail("вопрос распознан, но без варианта Bravo: " + strings.Join(s.Options, " | "))
	}
	question.OK = true

	// The answer: the second option's digit, as the card sends a single question's choice.
	if err := a.panes.SendRawKeys(name, "2"); err != nil {
		answer.Detail = "клавиша не ушла в панель: " + err.Error()
		return out()
	}
	if !a.waitForEvery(ctx, agentAnswerWait, agentPoll, func() bool {
		s = a.look(name)
		return strings.Contains(s.Plain, agentMark+"BRAVO") && s.PromptReady()
	}) {
		switch {
		case strings.Contains(s.Plain, agentMark+"ALPHA"):
			answer.Detail = "агент получил не тот вариант (Alpha вместо Bravo)"
		case s.Menu == typist.MenuQuestion:
			answer.Detail = "вопрос остался открытым после ответа цифрой"
		default:
			answer.Detail = "агент не подтвердил ответ: " + lastLines(s.Plain, 3)
		}
		return out()
	}
	answer.OK = true
	return out()
}

// waitForEvery is waitFor with its own poll.
func (s selftest) waitForEvery(ctx context.Context, wait, poll time.Duration, cond func() bool) bool {
	until := s.now().Add(wait)
	for {
		if cond() {
			return true
		}
		if ctx.Err() != nil || !s.now().Before(until) {
			return false
		}
		s.sleep(poll)
	}
}

func hasOption(options []string, label string) bool {
	for _, o := range options {
		if strings.Contains(screen.StripNumberPrefix(o), label) {
			return true
		}
	}
	return false
}

// lastLines is the last n non-blank lines of a screen, joined, for a detail.
func lastLines(plain string, n int) string {
	var keep []string
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	if len(keep) == 0 {
		return "экран пуст"
	}
	s := strings.Join(keep, " / ")
	if r := []rune(s); len(r) > 240 {
		s = string(r[:240]) + "…"
	}
	return s
}
