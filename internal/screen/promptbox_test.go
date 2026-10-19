package screen

import "testing"

func TestIsPromptBoxReady(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want bool
	}{
		{"footer under the box", "╭──╮\n│ >  │\n╰──╯\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n", true},
		{"shortcuts footer", "> \n  ? for shortcuts\n", true},
		{"notices and agents below the footer", lines(
			"❯ ", "────", "  ws │ ctx ━━─── 22%", "  ⏵⏵ bypass permissions on · 1 shell · 4 memories · ← 1 agent",
			"  ● main", "  ◯ general-purpose  Comparing traces    31m 28s", "        ✔ Update installed · Restart to update", "   /rc",
		), true},
		{"shell still up", "vibe@host:~$ claude --resume 123\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := IsPromptBoxReady(c.pane); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestIsLaunchedPromptBoxReady(t *testing.T) {
	launch := "agent@host:/work$ claude --settings /home/agent/.app/hooks.json --append-system-prompt-file /home/agent/p.md --dangerously-skip-permissions --model haiku"
	old := []string{"❯ ", "────────────────", "  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents"}
	fresh := []string{" ▐▛███▛█   Claude Code v2.1.296", "❯ Try \"fix typecheck errors\"", "────────────────", "  ⏵⏵ bypass permissions on (shift+tab to cycle)"}
	join := func(parts ...[]string) string {
		var all []string
		for _, p := range parts {
			all = append(all, p...)
		}
		return lines(all...)
	}

	// The footer of the Claude that ran before sits above the shell and the launch line.
	killed := join(old, []string{"", "Resume this session with:", "claude --resume 465ef7e9-0a97", launch})
	if !IsPromptBoxReady(killed) || IsLaunchedPromptBoxReady(killed) {
		t.Error("the previous agent's footer counts for the new one")
	}
	if !IsLaunchedPromptBoxReady(join(old, []string{launch}, fresh)) {
		t.Error("the new agent drawn below its launch line is not ready")
	}
	if !IsLaunchedPromptBoxReady(join(fresh)) {
		t.Error("a launch line that scrolled away refuses a drawn prompt box")
	}
	if IsLaunchedPromptBoxReady(launch+"\n") || IsLaunchedPromptBoxReady("") {
		t.Error("ready with nothing drawn")
	}
}
