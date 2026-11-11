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

func runDoctorUpdate(t *testing.T, answer string, args ...string) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/update" || r.Header.Get("x-cli-token") != "tok" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok"}
	var out bytes.Buffer
	code := Run(append([]string{"doctor", "update"}, args...), Env{Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &out, Home: t.TempDir()})
	return out.String(), code
}

// doctor update shows the channel and what it offers, and the reason when the stable channel has nothing to offer.
func TestDoctorUpdateShowsTheChannel(t *testing.T) {
	for name, tc := range map[string]struct {
		answer string
		want   []string
	}{
		"stable with a newer release": {
			`{"current":"0.16.100","latest":"0.16.180","url":"https://example.org/v0.16.180","updateAvailable":true,"channel":"stable","checking":true,"source":"releases"}`,
			[]string{"version: 0.16.100", "channel stable (RUNTIME_UPDATE_CHANNEL)", "newer on this channel: 0.16.180  https://example.org/v0.16.180"}},
		"latest, nothing newer": {
			`{"current":"0.16.200","latest":"0.16.200","updateAvailable":false,"channel":"latest","checking":true,"source":"releases"}`,
			[]string{"channel latest", "this channel is at 0.16.200: nothing newer"}},
		"stable without a pointer": {
			`{"current":"0.16.100","updateAvailable":false,"channel":"stable","note":"No stable release is named at https://example.org/stable.json yet","checking":true,"source":"releases"}`,
			[]string{"channel stable", "[warn] No stable release is named at https://example.org/stable.json yet"}},
		"a machine of a hub": {
			`{"current":"0.16.100","updateAvailable":false,"checking":false,"source":"hub","machine":{"mode":"auto","checked":true,"upToDate":true,"version":"0.16.100"}}`,
			[]string{"the programs this machine's hub serves", "machine programs: auto mode, up to date (0.16.100)"}},
		"a fleet machine": {
			`{"current":"0.16.100","updateAvailable":false,"checking":false,"source":"fleet","machine":{"mode":"fleet","checked":true,"upToDate":true}}`,
			[]string{"a rollout brings whole releases", "machine programs: fleet mode, up to date"}},
		"a build from source": {
			`{"current":"dev","updateAvailable":false,"checking":false,"source":"none"}`,
			[]string{"this runtime looks for none"}},
	} {
		out, code := runDoctorUpdate(t, tc.answer)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", name, code, out)
		}
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: no %q in\n%s", name, want, out)
			}
		}
		if (name == "a machine of a hub" || name == "a fleet machine") && strings.Contains(out, "channel stable") {
			t.Errorf("%s: a channel is shown:\n%s", name, out)
		}
	}
	if out, code := runDoctorUpdate(t, `{}`, "--verbose"); code == 0 {
		t.Fatalf("an unknown flag was taken:\n%s", out)
	}
}
