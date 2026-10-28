package agents_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// anyShellText is a prompt that breaks naive quoting in one shell or another.
const anyShellText = "Привет, это 'тест'\nвторая строка \\ \\\\ \\' $HOME `id` \"q\" $(id)\tтаб\x01\x7f 🚀 %s %b \\c \\n конец"

// shellsUnderTest are the interactive shells a launch line is typed into, with the flags that skip their rc files.
// HARNESSLY_TEST_FISH points at a fish binary where none is on PATH.
func shellsUnderTest(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for name, flags := range map[string][]string{"sh": nil, "dash": nil, "bash": {"--norc", "--noprofile"}, "zsh": {"-f"}, "fish": {"--no-config"}} {
		bin, err := exec.LookPath(name)
		if name == "fish" && os.Getenv("HARNESSLY_TEST_FISH") != "" {
			bin, err = os.Getenv("HARNESSLY_TEST_FISH"), nil
		}
		if err != nil {
			continue
		}
		out[bin] = flags
	}
	if len(out) == 0 {
		t.Skip("no shell")
	}
	return out
}

// shell-any: a launch line runs the same in sh, dash, bash, zsh and fish — the env files sourced, the variables set,
// the prompt byte for byte, and the agent exec'd so it is the pane's foreground process.
func TestForAnyShellRunsTheSameInEveryShell(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `d 'q' \ x`)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agents.EnvFile(dir, agents.EnvScopeAll), []byte("FROM_ENV='from env file'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(dir, "agent")
	script := `#!/bin/sh
printf '%s' "$4" > "$OUT_DIR/prompt"
printf '%s|%s|%s' "$FROM_ENV" "$EDITORISH" "$1 $2" > "$OUT_DIR/env"
if [ -r /proc/$PPID/comm ]; then cat /proc/$PPID/comm > "$OUT_DIR/parent"; fi
`
	if err := os.WriteFile(agent, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	posix := agents.EnvSourcePrefix(dir, agents.Claude) + `EDITORISH="${NO_SUCH_VAR:-fallback}" APP_X='1' ` +
		agents.ShellQuote(agent) + " --model 'opus' " + agents.WithInitialPrompt(agents.Claude, "", anyShellText, "")[1:]
	line := agents.ForAnyShell(posix)
	if strings.ContainsAny(line, "\n\r") || !strings.HasPrefix(line, "sh -c ") {
		t.Fatalf("not one typed line: %q", line)
	}
	if agents.ForAnyShell(line) != line {
		t.Fatal("wrapped twice")
	}
	if inner, ok := agents.UnwrapAnyShell(line); !ok || !strings.Contains(inner, "exec env EDITORISH=") {
		t.Fatalf("%q %v", inner, ok)
	}
	for bin, flags := range shellsUnderTest(t) {
		t.Run(filepath.Base(bin), func(t *testing.T) {
			out := t.TempDir()
			cmd := exec.Command(bin, append(flags, "-c", line)...)
			cmd.Env = append(os.Environ(), "OUT_DIR="+out)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, b)
			}
			if got, _ := os.ReadFile(filepath.Join(out, "prompt")); string(got) != strings.TrimSpace(anyShellText) {
				t.Fatalf("prompt %q", got)
			}
			if got, _ := os.ReadFile(filepath.Join(out, "env")); string(got) != "from env file|fallback|--model opus" {
				t.Fatalf("env %q", got)
			}
			// Without the exec the agent's parent would be the sh of `sh -c`.
			if parent, err := os.ReadFile(filepath.Join(out, "parent")); err == nil && runtime.GOOS == "linux" &&
				filepath.Base(bin) != "sh" && filepath.Base(bin) != "dash" && strings.TrimSpace(string(parent)) == "sh" {
				t.Fatal("the agent is not exec'd")
			}
		})
	}
}

// shell-any: the quoting reads back as the same word in every shell, and AppendArgs keeps a wrapped line one command.
func TestAnyShellQuoteAndAppendArgs(t *testing.T) {
	for bin, flags := range shellsUnderTest(t) {
		for _, s := range []string{"", "plain", `a'b`, `a\b`, `\'`, `''\\`, "$HOME `x` \"y\" 🚀"} {
			out, err := exec.Command(bin, append(flags, "-c", "printf %s "+agents.AnyShellQuote(s))...).Output()
			if err != nil || string(out) != s {
				t.Errorf("%s %q: %q %v", filepath.Base(bin), s, out, err)
			}
		}
	}
	line := agents.ForAnyShell("claude --x")
	if got, _ := agents.UnwrapAnyShell(agents.AppendArgs(line, " --resume 'a b'")); got != "exec env claude --x --resume 'a b'" {
		t.Fatalf("%q", got)
	}
	if got := agents.AppendArgs("claude", " --y"); got != "claude --y" {
		t.Fatalf("%q", got)
	}
}
