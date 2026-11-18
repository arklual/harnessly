package hlcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A deep run folder puts the stands in a short one: tmux's socket must fit.
func TestStandRootFitsTheSockets(t *testing.T) {
	short := "/home/a/.harnessly-eval/runs/x"
	if got := standRoot(short, "s"); got != filepath.Join(short, "stands") {
		t.Fatalf("short: %s", got)
	}
	deep := "/" + strings.Repeat("d", 90) + "/runs/20261117-210456-codex-base3"
	got := standRoot(deep, "20261117-210456")
	if strings.HasPrefix(got, deep) || len(got)+standSockets > 107 {
		t.Fatalf("deep: %s", got)
	}
}

// The login copies keep the access, never a refresh token, and are made once.
func TestCopyLoginsDropsRefreshTokens(t *testing.T) {
	home, dir := t.TempDir(), filepath.Join(t.TempDir(), "login")
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"a","refreshToken":"r","expiresAt":1}}`), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"x"},"other":1}`), 0o600)
	if err := copyLogins(home, dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "claude-credentials.json"))
	var cred map[string]map[string]any
	_ = json.Unmarshal(raw, &cred)
	if cred["claudeAiOauth"]["accessToken"] != "a" || cred["claudeAiOauth"]["refreshToken"] != nil {
		t.Fatalf("credentials: %s", raw)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "claude-account.json")); !strings.Contains(string(b), `"oauthAccount"`) || strings.Contains(string(b), "other") {
		t.Fatalf("account: %s", b)
	}
	if st, _ := os.Stat(filepath.Join(dir, "claude-credentials.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}
