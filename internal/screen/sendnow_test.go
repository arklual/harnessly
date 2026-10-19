package screen

import (
	"strings"
	"testing"
)

// Claude Code 2.1.296 frames with a message in its queue (testdata/tui/claude-2.1.296-*), plus 2.1.288.

var (
	queuedFrame = SendNowFrame{InputLine: true, Queued: true}
	clearFrame  = SendNowFrame{InputLine: true}
)

const bashText = "After that reply with exactly QUEUED-MSG-1 as the first word"

// A permission prompt shaped like Claude's, a message queued above it.
var permissionFrame = lines(
	"❯ "+bashText,
	"  ctrl+x ctrl+s to send now",
	"",
	" Bash command",
	"",
	"   rm -rf build",
	"   Remove the build directory",
	"",
	" Do you want to proceed?",
	" ❯ 1. Yes",
	"   2. Yes, and don't ask again for rm commands in /work",
	"   3. No",
	"",
	" Esc to cancel · Tab to amend · ctrl+e to explain",
)

func TestObserveSendNow(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    SendNowFrame
	}{
		{"queue behind a running Bash command", "claude-2.1.296-queued-bash.ansi.txt", queuedFrame},
		{"queue behind a streaming answer", "claude-2.1.296-queued-stream.ansi.txt", queuedFrame},
		{"queue behind a running MCP call", "claude-2.1.296-queued-mcp.ansi.txt", queuedFrame},
		{"two queued messages", "claude-2.1.296-queued-two.ansi.txt", queuedFrame},
		{"queue while PostToolUse hooks run", "claude-2.1.296-queued-post-tool-hook.ansi.txt", queuedFrame},
		{"narrow pane with the footer cut off", "claude-2.1.296-queued-narrow.ansi.txt", queuedFrame},
		{"a draft next to the queue", "claude-2.1.296-queued-draft.ansi.txt", SendNowFrame{Input: "my unsent draft", InputLine: true, Queued: true}},
		{"0.3 s after the chord the queue is still there", "claude-2.1.296-queued-bash-chord-pending.ansi.txt", queuedFrame},
		{"Bash: queue gone once taken", "claude-2.1.296-queued-bash-after-send-now.ansi.txt", clearFrame},
		{"two: queue gone once taken", "claude-2.1.296-queued-two-after-send-now.ansi.txt", clearFrame},
		{"MCP: queue gone once taken", "claude-2.1.296-queued-mcp-after-send-now.ansi.txt", clearFrame},
		{"no queue, chord pressed for nothing", "claude-2.1.296-send-now-nothing-queued.ansi.txt", clearFrame},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ObserveSendNow(fixture(t, c.fixture)); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestObserveSendNowClaude2288(t *testing.T) {
	if !ObserveSendNow(fixture(t, "claude-queued-before-escape.ansi.txt")).Queued {
		t.Error("2.1.288 queue before Esc not seen")
	}
	if ObserveSendNow(fixture(t, "claude-queued-after-escape.ansi.txt")).Queued {
		t.Error("2.1.288 queue after Esc still seen")
	}
}

func TestQueueOnlyFromPromptBox(t *testing.T) {
	printed := []string{
		"● Press the green button to send now",
		"  To change it later: Press up to edit queued messages",
		"  ctrl+x ctrl+s to send now",
	}
	for i := range 14 {
		printed = append(printed, "  line "+string(rune('a'+i)))
	}
	printed = append(printed,
		"✽ Swirling… (24s · ↓ 253 tokens)",
		"",
		strings.Repeat("─", 40),
		"❯ \x1b[2mTry \"how do I log an error?\"\x1b[0m",
		strings.Repeat("─", 40),
		"  ⏵⏵ bypass permissions on · esc to interrupt",
	)
	if ObserveSendNow(lines(printed...)).Queued {
		t.Error("the agent's own output read as the queue")
	}
}

func TestObserveSendNowPermissionPrompt(t *testing.T) {
	// An open permission prompt stops "send now" before any key.
	if got := ObserveSendNow(permissionFrame); !got.Menu {
		t.Errorf("got %+v", got)
	}
}
