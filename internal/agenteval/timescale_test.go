package agenteval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStandTimeScaleEnv: a compressed stand gives its agents the preload and the scale, and keeps a BUN_OPTIONS
// the setup set; a plain stand gives neither.
func TestStandTimeScaleEnv(t *testing.T) {
	for _, scale := range []float64{60, 0} {
		root, login := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(login, "claude-credentials.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := StandConfig{Root: root, Login: login, Harnessly: "/bin/true", TimeScale: scale, AgentEnv: map[string]string{"BUN_OPTIONS": "--smol"}}
		s := &Stand{Dir: root, Home: filepath.Join(root, "home"), Data: filepath.Join(root, "data"), Work: filepath.Join(root, "home", "work")}
		s.clock = standClock(cfg)
		for _, d := range []string{filepath.Join(s.Home, ".claude"), filepath.Join(s.Home, ".codex"), filepath.Join(s.Home, ".local", "bin"), filepath.Join(s.Data, "agent-env")} {
			if err := os.MkdirAll(d, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.writeHome(cfg); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(s.Data, "agent-env", "all.env"))
		if err != nil {
			t.Fatal(err)
		}
		env := string(b)
		preload := filepath.Join(root, "timescale.js")
		_, statErr := os.Stat(preload)
		if scale == 60 {
			if !strings.Contains(env, "HL_TIME_SCALE='60'") || !strings.Contains(env, "HL_TIME_EPOCH_MS='") || !strings.Contains(env, "BUN_OPTIONS='--preload "+preload+" --smol'") || statErr != nil {
				t.Fatalf("scale 60: env %s, preload %v", env, statErr)
			}
		} else if strings.Contains(env, "HL_TIME_SCALE") || strings.Contains(env, "--preload") || statErr == nil {
			t.Fatalf("plain clock: env %s, preload %v", env, statErr)
		}
	}
}
