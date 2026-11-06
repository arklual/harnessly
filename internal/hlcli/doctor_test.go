package hlcli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func runDoctor(t *testing.T, ffmpeg string, args ...string) (string, int) {
	t.Helper()
	env := map[string]string{"RUNTIME_DESIGN_FFMPEG": ffmpeg}
	var out bytes.Buffer
	code := Run(append([]string{"doctor"}, args...), Env{Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &out, Home: t.TempDir()})
	return out.String(), code
}

func TestDoctorRenderReportsTheFailedStepAndExits1(t *testing.T) {
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	t.Setenv("RUNTIME_DESIGN_CHROME", "")
	out, code := runDoctor(t, filepath.Join(t.TempDir(), "no-ffmpeg"), "render")
	if code != 1 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "[FAIL]") && !strings.Contains(out, "RND-10") {
		t.Fatalf("no failed step in\n%s", out)
	}
	if !strings.Contains(out, "NOT ready") || !strings.Contains(out, "->") {
		t.Fatalf("no verdict or hint in\n%s", out)
	}
}

func TestDoctorRenderJSON(t *testing.T) {
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	out, code := runDoctor(t, filepath.Join(t.TempDir(), "no-ffmpeg"), "render", "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var rep struct {
		OK    bool `json:"ok"`
		Steps []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rep); err != nil || rep.OK || len(rep.Steps) != 4 {
		t.Fatalf("%v %+v\n%s", err, rep, out)
	}
}

func TestDoctorUsage(t *testing.T) {
	if _, code := runDoctor(t, "", "nonsense"); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if out, code := runDoctor(t, ""); code != 0 || !strings.Contains(out, "doctor render") {
		t.Fatalf("help: %d %q", code, out)
	}
}
