package screen

import "testing"

func TestCodexLimitLine(t *testing.T) {
	const notice = "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at\n  6:05 PM.\n"
	composer := "\n› Ask Codex to do anything\n\n  gpt-5.6-sol high · ~/work\n"
	if got := CodexLimitLine("• Working on it\n\n" + notice + composer); got != "You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at 6:05 PM." {
		t.Fatalf("got %q", got)
	}
	if got := CodexLimitLine(notice + "\n• Done: the tests pass.\n" + composer); got != "" {
		t.Fatalf("an older notice taken: %q", got)
	}
	if got := CodexLimitLine("■ stream error: reconnecting\n" + composer); got != "" {
		t.Fatalf("another error taken: %q", got)
	}
	if got := CodexLimitLine(notice); got != "" {
		t.Fatalf("no composer, taken: %q", got)
	}
}
