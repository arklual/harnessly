package hlcli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

func validateHook(t *testing.T, vs []apps.Validator, script string) func(event, stdin string) (string, string) {
	t.Helper()
	pkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkg, "check.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	snap := apps.Snapshot{Apps: []apps.SnapshotApp{{ID: "acme.review", Name: "Ревью", Dir: pkg, Validators: vs}}}
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	policy := filepath.Join(t.TempDir(), "policy-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.WriteFile(policy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return func(event, stdin string) (string, string) {
		var out, errOut bytes.Buffer
		code := Run([]string{"hook", "app-validate", "--policy", policy, "--event", event}, Env{Getenv: func(string) string { return "" },
			Home: t.TempDir(), Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out.String(), errOut.String()
	}
}

func TestAppValidateFailBlocksWithReason(t *testing.T) {
	run := validateHook(t, []apps.Validator{{ID: "v", On: "PostToolUse", Match: "Edit", Run: "check.sh", MaxRetries: 3}},
		"#!/bin/sh\ncat >/dev/null\necho 'Тесты красные'\nexit 2\n")
	out, _ := run("PostToolUse", `{"session_id":"s1","tool_name":"Edit","tool_input":{}}`)
	var r struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Decision != "block" || !strings.Contains(r.Reason, "Тесты красные") {
		t.Fatalf("want a block with the remark: %q", out)
	}
	if out, _ := run("PostToolUse", `{"session_id":"s1","tool_name":"Read"}`); out != "" {
		t.Errorf("another tool is not checked: %q", out)
	}
	if out, _ := run("Stop", `{"session_id":"s1"}`); out != "" {
		t.Errorf("another event is not checked: %q", out)
	}
}

func TestAppValidatePassIsSilentAndResets(t *testing.T) {
	pkgFlag := filepath.Join(t.TempDir(), "pass")
	run := validateHook(t, []apps.Validator{{ID: "v", On: "Stop", Run: "check.sh", MaxRetries: 2}},
		"#!/bin/sh\ncat >/dev/null\n[ -e "+pkgFlag+" ] && exit 0\necho nope\nexit 2\n")
	stop := `{"session_id":"s1","hook_event_name":"Stop"}`
	for i := 0; i < 2; i++ {
		if out, _ := run("Stop", stop); !strings.Contains(out, `"block"`) {
			t.Fatalf("failure %d must block: %q", i+1, out)
		}
	}
	if err := os.WriteFile(pkgFlag, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := run("Stop", stop); out != "" {
		t.Errorf("a pass is silent: %q", out)
	}
	_ = os.Remove(pkgFlag)
	// The pass reset the count: two blocks again before the release.
	for i := 0; i < 2; i++ {
		if out, _ := run("Stop", stop); !strings.Contains(out, `"block"`) {
			t.Fatalf("after a pass, failure %d must block: %q", i+1, out)
		}
	}
}

func TestAppValidateReleasesAfterRetries(t *testing.T) {
	run := validateHook(t, []apps.Validator{{ID: "v", On: "Stop", Run: "check.sh", MaxRetries: 2}},
		"#!/bin/sh\ncat >/dev/null\necho nope\nexit 2\n")
	first := `{"session_id":"s1","stop_hook_active":false}`
	again := `{"session_id":"s1","stop_hook_active":true}`
	if out, _ := run("Stop", first); !strings.Contains(out, "attempt 1 of 2") {
		t.Fatalf("1: %q", out)
	}
	if out, _ := run("Stop", again); !strings.Contains(out, "attempt 2 of 2") {
		t.Fatalf("a retry under stop_hook_active still blocks while retries remain: %q", out)
	}
	out, errOut := run("Stop", again)
	if out != "" || !strings.Contains(errOut, "no more retries") || !strings.Contains(errOut, "nope") {
		t.Errorf("out of retries: let go, say so on stderr: out=%q err=%q", out, errOut)
	}
	// The turn is over; a new one starts afresh.
	if out, _ := run("Stop", first); !strings.Contains(out, "attempt 1 of 2") {
		t.Errorf("a new turn: %q", out)
	}
	// Another chat has its own count.
	if out, _ := run("Stop", `{"session_id":"s2"}`); !strings.Contains(out, "attempt 1 of 2") {
		t.Errorf("per chat: %q", out)
	}
}

func TestAppValidatePostToolUseStaysReleasedUntilPass(t *testing.T) {
	run := validateHook(t, []apps.Validator{{ID: "v", On: "PostToolUse", Run: "check.sh", MaxRetries: 1}},
		"#!/bin/sh\ncat >/dev/null\necho nope\nexit 2\n")
	call := `{"session_id":"s1","tool_name":"Edit"}`
	if out, _ := run("PostToolUse", call); !strings.Contains(out, `"block"`) {
		t.Fatalf("first: %q", out)
	}
	for i := 0; i < 2; i++ {
		if out, _ := run("PostToolUse", call); out != "" {
			t.Errorf("released: %q", out)
		}
	}
}

func TestAppValidateBrokenScriptBlocksNothing(t *testing.T) {
	run := validateHook(t, []apps.Validator{{ID: "v", On: "Stop", Run: "check.sh"}}, "#!/bin/sh\nexit 1\n")
	out, errOut := run("Stop", `{"session_id":"s1"}`)
	if out != "" || !strings.Contains(errOut, "did not work") {
		t.Errorf("out=%q err=%q", out, errOut)
	}
}
