package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/diag"
)

func doctorOut(t *testing.T, vars map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), append([]string{"doctor"}, args...), doctorEnv(t, vars), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestDoctorChecksTheDatabaseAndSaysWhatToDo(t *testing.T) {
	code, out, _ := doctorOut(t, nil, "--offline")
	if code != 0 || !strings.Contains(out, "database") || !strings.Contains(out, "no file yet") {
		t.Fatalf("a new sqlite database: code %d, out %s", code, out)
	}
	code, out, _ = doctorOut(t, map[string]string{"HUB_DATABASE_URL": "postgres://hub@127.0.0.1:1/hub"}, "--offline")
	for _, want := range []string{"FAIL", diag.DBUnreachable, "what to do:", "docs/hub/doctor.md#hub-d001"} {
		if code != 1 || !strings.Contains(out, want) {
			t.Fatalf("an unreachable database lacks %q: code %d, out %s", want, code, out)
		}
	}
}

func TestDoctorSeesTheHubBehindAProxyOnAPath(t *testing.T) {
	hub := http.NewServeMux()
	hub.HandleFunc("GET /harnessly/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	good := httptest.NewServer(hub)
	defer good.Close()
	vars := map[string]string{"HUB_PUBLIC_URL": good.URL + "/harnessly", "HUB_RUNTIME_DOMAIN": "run.example.net"}
	code, out, errOut := doctorOut(t, vars)
	if code != 0 || !strings.Contains(out, "public address ok") || !strings.Contains(out, "clock          ok") {
		t.Fatalf("code %d, out %s, err %s", code, out, errOut)
	}

	// The same path with no runtime domain is a warning that does not fail the run.
	delete(vars, "HUB_RUNTIME_DOMAIN")
	code, out, _ = doctorOut(t, vars)
	if code != 0 || !strings.Contains(out, diag.BaseNoRuntime) {
		t.Fatalf("a path without a runtime domain: code %d, out %s", code, out)
	}

	// A proxy that does not pass the path, one that answers its own page, one that redirects.
	for name, h := range map[string]http.HandlerFunc{
		"404":    http.NotFound,
		"a page": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>welcome to nginx</html>")) },
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/harnessly/healthz/", http.StatusMovedPermanently)
		},
	} {
		srv := httptest.NewServer(h)
		code, out, _ = doctorOut(t, map[string]string{"HUB_PUBLIC_URL": srv.URL + "/harnessly"})
		srv.Close()
		if code != 1 || !strings.Contains(out, diag.AddressUnreach) {
			t.Errorf("%s: code %d, out %s", name, code, out)
		}
	}
}

func TestDoctorWarnsOfAClockThatIsOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", time.Now().Add(-10*time.Minute).UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	code, out, _ := doctorOut(t, map[string]string{"HUB_PUBLIC_URL": srv.URL})
	if code != 0 || !strings.Contains(out, diag.ClockSkew) {
		t.Fatalf("code %d, out %s", code, out)
	}
}

func TestDoctorWarnsOfHTTPSWithoutTrustedProxies(t *testing.T) {
	code, out, _ := doctorOut(t, map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_SMTP_URL": "smtp://mail.example.com", "HUB_TRUSTED_PROXIES": ","}, "--offline")
	if code != 0 || !strings.Contains(out, diag.ProxyNoTrusted) {
		t.Fatalf("code %d, out %s", code, out)
	}
}

// What stops the hub says a code and what to do.
func TestStartupErrorsCarryACode(t *testing.T) {
	for _, tc := range []struct {
		vars map[string]string
		code string
	}{
		{map[string]string{"HUB_PUBLIC_URL": "https://example.com/api"}, diag.AddressInvalid},
		{map[string]string{"HUB_PUBLIC_URL": "hub.example.com"}, diag.AddressInvalid},
		{map[string]string{"HUB_EDITION": "nope"}, diag.ConfigInvalid},
	} {
		for _, cmd := range []string{"check-config", "serve", "doctor"} {
			var out, errOut bytes.Buffer
			vars := map[string]string{"HUB_PUBLIC_URL": "http://hub.example.com", "HUB_DATA_DIR": t.TempDir()}
			for k, v := range tc.vars {
				vars[k] = v
			}
			if code := run(context.Background(), []string{cmd}, env(vars), &out, &errOut); code != 2 ||
				!strings.Contains(errOut.String(), tc.code) || !strings.Contains(errOut.String(), "what to do:") || !strings.Contains(errOut.String(), "docs/hub/doctor.md#"+strings.ToLower(tc.code)) {
				t.Errorf("%s %v: code %d, err %q", cmd, tc.vars, code, errOut.String())
			}
		}
	}
}

func TestAPortThatIsTakenIsNamed(t *testing.T) {
	taken := httptest.NewServer(http.NotFoundHandler())
	defer taken.Close()
	var out, errOut bytes.Buffer
	vars := map[string]string{"HUB_PUBLIC_URL": "http://hub.example.com", "HUB_DATA_DIR": t.TempDir(), "HUB_LISTEN": strings.TrimPrefix(taken.URL, "http://")}
	if code := run(context.Background(), []string{"serve"}, env(vars), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), diag.PortBusy) {
		t.Fatalf("code %d, err %q", code, errOut.String())
	}
}
