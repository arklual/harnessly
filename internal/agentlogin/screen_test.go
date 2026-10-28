package agentlogin

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClaudeLoginURL(t *testing.T) {
	plain := fixture(t, "claude_login.txt")
	want := "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=org%3Acreate_api_key+user%3Aprofile+user%3Ainference+user%3Asessions%3Aclaude_code+user%3Amcp_servers+user%3Afile_upload+user%3Aplugins&code_challenge=EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE123&code_challenge_method=S256&state=EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE456"
	tests := []struct {
		name, screen, want string
	}{
		{"the screen of the real CLI", plain, want},
		{"with colours and a hyperlink escape", fixture(t, "claude_login_ansi.txt"), "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a&state=abc"},
		{"before the address is printed", "Opening browser to sign in…\n", ""},
		{"empty", "", ""},
		{"another page is not the sign-in", "docs: https://docs.anthropic.com/en/docs/claude-code\nvisit: https://example.com/oauth/authorize?x=1\n", ""},
		{"a look-alike host", "visit: https://claude.com.evil.example/oauth/authorize?x=1\n", ""},
		{"a sentence ends after the address", "open https://claude.ai/oauth/authorize?code=true.", "https://claude.ai/oauth/authorize?code=true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClaudeLoginURL(tc.screen); got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestCodexDevice(t *testing.T) {
	url, code := CodexDevice(fixture(t, "codex_device.txt"))
	if url != "https://auth.openai.com/codex/device" || code != "H3CF-2WRYQ" {
		t.Errorf("%q %q", url, code)
	}
	// Coloured, the way the terminal draws it.
	coloured := "1. Open this link\n   \x1b[94mhttps://auth.openai.com/codex/device\x1b[39m\n\n2. Enter this one-time code \x1b[90m(expires in 15 minutes)\x1b[39m\n   \x1b[94mK7QF-2M9P\x1b[39m\n"
	if url, code := CodexDevice(coloured); url != "https://auth.openai.com/codex/device" || code != "K7QF-2M9P" {
		t.Errorf("%q %q", url, code)
	}
	// The address first, the code not yet: not enough.
	if url, code := CodexDevice("1. Open this link\n   https://auth.openai.com/codex/device\n"); url == "" || code != "" {
		t.Errorf("%q %q", url, code)
	}
	for _, screen := range []string{"", "Welcome to Codex [v0.152.0]\nK7QF-2M9P\n", "visit https://example.com/device\nK7QF-2M9P"} {
		if url, code := CodexDevice(screen); url != "" || code != "" {
			t.Errorf("%q: %q %q", screen, url, code)
		}
	}
}

func TestStripANSIAndLastLines(t *testing.T) {
	if got := StripANSI("\x1b[31mred\x1b[0m \x1b]8;;http://x\x07link\x1b]8;;\x07 \x1b(B\r\n"); got != "red link \n" {
		t.Errorf("%q", got)
	}
	screen := "\n\nOpening\n\n  OAuth error: invalid code  \n\n\n"
	if got := LastLines(screen, 5); got != "Opening / OAuth error: invalid code" {
		t.Errorf("%q", got)
	}
	if got := LastLines("a\nb\nc\nd", 2); got != "c / d" {
		t.Errorf("%q", got)
	}
}

func TestScrub(t *testing.T) {
	got := scrub("failed at https://claude.com/cai/oauth/authorize?state=zzz with code abc-123 end", "abc-123")
	if strings.Contains(got, "zzz") || strings.Contains(got, "abc-123") || !strings.Contains(got, "failed at") {
		t.Errorf("%q", got)
	}
	if long := scrub(strings.Repeat("я", 400), ""); len([]rune(long)) != 301 {
		t.Errorf("%d", len([]rune(long)))
	}
}
