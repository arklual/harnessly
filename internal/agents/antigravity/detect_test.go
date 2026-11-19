package antigravity

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/proc"
)

func TestDetectorConfiguredBinaryExactPath(t *testing.T) {
	const configured = "/tmp/native-probe/agy-eligibility-test"
	for _, tc := range []struct {
		name, binary, executable string
		running                  bool
	}{
		{"configured copy", configured, configured, true},
		{"unconfigured copy", "", configured, false},
		{"different directory", configured, "/tmp/other/agy-eligibility-test", false},
		{"same basename only", configured, "agy-eligibility-test", false},
		{"relative configured path", "agy-eligibility-test", "agy-eligibility-test", false},
		{"arbitrary agy name", configured, "/tmp/another-agy-wrapper", false},
		{"standard binary", configured, "agy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Detector{Home: t.TempDir(), Binary: tc.binary, Installed: func() bool { return true }, Procs: proc.Procs{
				Args: func(int) (string, bool) { return tc.executable + " --conversation " + testID, true },
				Argv: func(int) ([]string, bool) { return []string{tc.executable, "--conversation", testID}, true },
			}}
			info := d.Detect([]int{123}, false)
			if (info.Status == agents.Running) != tc.running {
				t.Fatalf("status %s running %v", info.Status, tc.running)
			}
			if tc.running && info.SessionID != testID {
				t.Fatal("lost explicit session binding")
			}
			if tc.executable == configured && MatchesProcess(tc.executable, nil) {
				t.Fatal("global process matcher broadened")
			}
		})
	}
}
