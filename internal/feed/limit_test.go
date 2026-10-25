package feed

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestClaudeLimitText(t *testing.T) {
	utc := time.Date(2026, 9, 26, 23, 7, 36, 0, time.UTC)
	cases := []struct {
		text   string
		window wire.LimitWindow
		reset  time.Time
	}{
		{"You've hit your session limit · resets 11:50pm (UTC)", wire.LimitSession, time.Date(2026, 9, 26, 23, 50, 0, 0, time.UTC)},
		{"You've hit your session limit · resets 4:10am (UTC)", wire.LimitSession, time.Date(2026, 9, 27, 4, 10, 0, 0, time.UTC)},
		{"You've hit your weekly limit · resets 5am (UTC)", wire.LimitWeekly, time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC)},
		{"You've hit your weekly limit · resets Oct 7, 5am (UTC)", wire.LimitWeekly, time.Date(2026, 10, 7, 5, 0, 0, 0, time.UTC)},
		{"You've hit your session limit · resets 2am (Europe/Moscow)", wire.LimitSession, time.Date(2026, 9, 26, 23, 0, 0, 0, time.UTC).Add(24 * time.Hour)},
		{"5-hour limit reached ∙ resets 11:59pm (UTC)", wire.LimitSession, time.Date(2026, 9, 26, 23, 59, 0, 0, time.UTC)},
		{"Claude AI usage limit reached|1790466600", wire.LimitOther, time.Unix(1790466600, 0)},
	}
	for _, c := range cases {
		l, ok := ClaudeLimitText(c.text, utc)
		if !ok || l.Window != c.window || l.ResetsAt != c.reset.UnixMilli() {
			t.Errorf("%q: %+v %v, want %s %s", c.text, l, ok, c.window, c.reset)
		}
	}
	for _, text := range []string{
		"API Error: Request rejected (429) · This request would exceed your account's rate limit. Please try again later.",
		"API Error: 500 Internal server error",
	} {
		if l, ok := ClaudeLimitText(text, utc); ok {
			t.Errorf("%q read as a usage limit: %+v", text, l)
		}
	}
}

func TestCodexLimitText(t *testing.T) {
	at := time.Date(2026, 10, 25, 15, 4, 0, 0, time.UTC)
	cases := []struct {
		text  string
		reset time.Time
	}{
		{"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 6:05 PM.", time.Date(2026, 10, 25, 18, 5, 0, 0, time.UTC)},
		{"You've hit your usage limit. Try again at Oct 27th, 2026 9:30 AM.", time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)},
		{"You've hit your usage limit. Try again in 2 days 3 hours 5 minutes.", at.Add(51*time.Hour + 5*time.Minute)},
		{"You've hit your usage limit. Try again later.", time.Time{}},
	}
	for _, c := range cases {
		got := codexReset(c.text, at, time.UTC)
		want := wire.Ms(0)
		if !c.reset.IsZero() {
			want = c.reset.UnixMilli()
		}
		if got != want {
			t.Errorf("%q: %s, want %s", c.text, time.UnixMilli(got).UTC(), c.reset)
		}
	}
}

// The refusal line as Claude Code 2.1.282 writes it: the turn ends on a notice of the limit, its reset read off
// quotaLimits, not off the words.
func TestLimitEndOfAClaudeTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	body := `{"uuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-09-26T23:07:30.000Z","type":"user","message":{"role":"user","content":"go on"}}
{"uuid":"00000000-0000-4000-8000-000000000002","parentUuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-09-26T23:07:36.830Z","type":"assistant","message":{"model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","type":"message","content":[{"type":"text","text":"You've hit your session limit · resets 11:50pm (UTC)"}]},"quotaLimits":{"status":"rejected","resetsAt":1790466600,"unifiedRateLimitFallbackAvailable":false,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"error":"rate_limit","isApiErrorMessage":true,"apiErrorStatus":429}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	l, detail, ok := f.LimitEnd("")
	if !ok || l.Window != wire.LimitSession || l.ResetsAt != 1790466600_000 || detail != "You've hit your session limit · resets 11:50pm (UTC)" {
		t.Fatalf("limit end: %+v %q %v", l, detail, ok)
	}
	if _, ok := f.ErrorEnd(""); ok {
		t.Fatal("a usage limit taken for an error end")
	}
}
