package hlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// doctor preview says which slot names do not resolve, with what to do, and exits 1.
func TestDoctorPreviewNXDOMAIN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/preview/check" || r.Header.Get("x-cli-token") != "tok" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"mode":"auto","source":"domain","domain":"preview.example.com","origins":[` +
			`{"origin":"https://s1-a.preview.example.com","host":"s1-a.preview.example.com","state":"not-found","error":"the name does not resolve (NXDOMAIN)"},` +
			`{"origin":"https://s2-a.preview.example.com","host":"s2-a.preview.example.com","state":"ok","addresses":["203.0.113.5"]}],` +
			`"hint":"the preview names are not in DNS: add a wildcard record"}`))
	}))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	var out bytes.Buffer
	code := Run([]string{"doctor", "preview"}, Env{Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &out, Home: t.TempDir()})
	if code != 1 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{"RUNTIME_PREVIEW_DOMAIN=preview.example.com", "[FAIL] https://s1-a.preview.example.com", "NXDOMAIN",
		"[ok  ] https://s2-a.preview.example.com  -> 203.0.113.5", "-> the preview names are not in DNS"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("no %q in\n%s", want, out.String())
		}
	}
}
