package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
)

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestVersionAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"version"}, env(nil), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) == "" {
		t.Errorf("version: code %d, out %q", code, out.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"help"}, env(nil), &out, &errOut); code != 0 || !strings.Contains(out.String(), "check-config") {
		t.Errorf("help: code %d, out %q", code, out.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"launch"}, env(nil), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), `unknown command "launch"`) {
		t.Errorf("unknown: code %d, err %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"serve", "extra"}, env(nil), &out, &errOut); code != 2 {
		t.Errorf("serve with arguments: code %d, err %q", code, errOut.String())
	}
}

func TestServeWithoutAPublicURLSaysSo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, env(nil), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "HUB_PUBLIC_URL is required") {
		t.Errorf("code %d, err %q", code, errOut.String())
	}
}

func TestCheckConfigNeverPrintsASecret(t *testing.T) {
	vars := map[string]string{
		"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_DATA_DIR": t.TempDir(),
		"HUB_SMTP_URL":         "smtp://mailer:smtp-pass-1234@mail.example.com:587",
		"HUB_DATABASE_URL":     "postgres://hub:db-pass-1234@db.example.com/hub?sslmode=require",
		"HUB_GITHUB_CLIENT_ID": "gh-id", "HUB_GITHUB_CLIENT_SECRET": "gh-secret-1234",
		"HUB_OIDC_PROVIDERS": "keycloak", "HUB_OIDC_KEYCLOAK_ISSUER": "https://sso.example.com/realms/x",
		"HUB_OIDC_KEYCLOAK_CLIENT_ID": "hub", "HUB_OIDC_KEYCLOAK_CLIENT_SECRET": "oidc-secret-1234",
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"check-config"}, env(vars), &out, &errOut); code != 0 {
		t.Fatalf("code %d, err %q", code, errOut.String())
	}
	text := out.String()
	for _, secret := range []string{"smtp-pass-1234", "db-pass-1234", "gh-secret-1234", "oidc-secret-1234", "mailer"} {
		if strings.Contains(text, secret) {
			t.Errorf("check-config printed %q", secret)
		}
	}
	for _, want := range []string{"https://hub.example.com", "postgres", "mailed over SMTP", "GitHub         on", "Yandex ID      off", "keycloak"} {
		if !strings.Contains(text, want) {
			t.Errorf("check-config lacks %q:\n%s", want, text)
		}
	}
}

// A hub that would write sign-in codes to a log that others read does not start, and check-config says so by its exit
// code, until SMTP is set or the operator accepts the log with HUB_MAIL_LOG=allow.
func TestHubWithoutSMTPOnHTTPSIsRefused(t *testing.T) {
	vars := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_DATA_DIR": t.TempDir()}
	for _, cmd := range []string{"check-config", "serve"} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{cmd}, env(vars), &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "HUB_SMTP_URL is required") {
			t.Errorf("%s: code %d, err %q", cmd, code, errOut.String())
		}
	}
	vars["HUB_MAIL_LOG"] = "allow"
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"check-config"}, env(vars), &out, &errOut); code != 0 || !strings.Contains(out.String(), "HUB_MAIL_LOG=allow") {
		t.Errorf("with the override: code %d, out %q, err %q", code, out.String(), errOut.String())
	}
}

// Machines on a domain that shares its registrable domain with the hub are one site with it. The cloud edition refuses
// that unless the operator says HUB_ALLOW_SAME_SITE_RUNTIME=1, and check-config shows the warning either way.
func TestSameSiteRuntimeDomain(t *testing.T) {
	vars := map[string]string{"HUB_PUBLIC_URL": "https://app.harnessly.ru", "HUB_DATA_DIR": t.TempDir(), "HUB_SMTP_URL": "smtps://mail.example.com",
		"HUB_RUNTIME_DOMAIN": "run.harnessly.ru", "HUB_EDITION": "cloud",
		"HUB_ORGS": "on", "HUB_ACCESS_ENFORCE": "on", "HUB_RESOURCE_ACCESS": "on"}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"check-config"}, env(vars), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "one registrable domain") {
		t.Fatalf("the cloud on one site: code %d, err %q", code, errOut.String())
	}
	vars["HUB_ALLOW_SAME_SITE_RUNTIME"] = "1"
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"check-config"}, env(vars), &out, &errOut); code != 0 || !strings.Contains(out.String(), "WARNING") || !strings.Contains(out.String(), "HUB_ALLOW_SAME_SITE_RUNTIME is set") {
		t.Errorf("accepted: code %d, out %q, err %q", code, out.String(), errOut.String())
	}
}

func TestCheckConfigReportsEveryProblem(t *testing.T) {
	var out, errOut bytes.Buffer
	vars := map[string]string{"HUB_PUBLIC_URL": "ftp://x", "HUB_SIGNUP": "maybe", "HUB_GITHUB_CLIENT_ID": "only-the-id"}
	if code := run(context.Background(), []string{"check-config"}, env(vars), &out, &errOut); code != 2 {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"HUB_PUBLIC_URL", "HUB_SIGNUP", "HUB_GITHUB_CLIENT_ID"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the error lacks %s: %q", want, errOut.String())
		}
	}
}

func TestHealthcheck(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) }))
	defer down.Close()
	addr := func(s *httptest.Server) string { return strings.TrimPrefix(s.URL, "http://") }

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"healthcheck"}, env(map[string]string{"HUB_LISTEN": addr(up)}), &out, &errOut); code != 0 {
		t.Errorf("up: code %d, err %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"healthcheck"}, env(map[string]string{"HUB_LISTEN": addr(down)}), &out, &errOut); code != 1 {
		t.Errorf("unhealthy: code %d", code)
	}
	// Wildcard listen addresses are asked on loopback.
	port := addr(up)[strings.LastIndex(addr(up), ":"):]
	if code := run(context.Background(), []string{"healthcheck"}, env(map[string]string{"HUB_LISTEN": "0.0.0.0" + port}), &out, &errOut); code != 0 {
		t.Errorf("0.0.0.0: code %d", code)
	}
	if code := run(context.Background(), []string{"healthcheck"}, env(map[string]string{"HUB_LISTEN": "garbage"}), &out, &errOut); code != 2 {
		t.Errorf("garbage: code %d", code)
	}
}

// TestServe starts the real hub on a free port with SQLite in a temp folder, signs in with the code from its log, and
// stops it.
func TestServe(t *testing.T) {
	data := t.TempDir()
	cfg, err := config.Load(env(map[string]string{
		"HUB_PUBLIC_URL": "http://127.0.0.1:18599", "HUB_LISTEN": "127.0.0.1:0", "HUB_DATA_DIR": data, "HUB_PASSKEY": "off",
		"HUB_DIST_DIR": filepath.Join(data, "dist"), "HUB_LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	logs := &safeBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ready, logs) }()

	var base string
	select {
	case a := <-ready:
		base = "http://" + a
	case err := <-done:
		t.Fatalf("serve returned: %v\n%s", err, logs)
	case <-time.After(30 * time.Second):
		t.Fatal("the hub did not start")
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	get := func(path string) (int, string) {
		res, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	post := func(path string, body any) (int, string) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	if code, body := get("/healthz"); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Errorf("healthz: %d %s", code, body)
	}
	if code, body := get("/readyz"); code != 200 {
		t.Errorf("readyz: %d %s", code, body)
	}
	if code, body := get("/api/config"); code != 200 || !strings.Contains(body, `"mode":"log"`) {
		t.Errorf("config: %d %s", code, body)
	}
	// Without the page built in, the root says how to get one instead of failing.
	if code, body := get("/"); code != 200 || !strings.Contains(body, "HUB_WEB_DIR") {
		t.Errorf("root: %d %s", code, body)
	}

	if code, body := post("/api/auth/email/start", map[string]string{"email": "first@example.com", "intent": "signup"}); code != 200 {
		t.Fatalf("start: %d %s", code, body)
	}
	m := regexp.MustCompile(`DEV email code[^\n]*code=(\d{6})`).FindStringSubmatch(logs.String())
	if m == nil {
		t.Fatalf("no code in the log:\n%s", logs)
	}
	if !strings.Contains(logs.String(), "level=WARN msg=\"DEV email code") {
		t.Errorf("the code line is not marked DEV at warn level:\n%s", logs)
	}
	code, body := post("/api/auth/email/verify", map[string]string{"email": "first@example.com", "code": m[1]})
	if code != 200 || !strings.Contains(body, `"isNew":true`) || !strings.Contains(body, `"isAdmin":true`) {
		t.Fatalf("verify: %d %s", code, body)
	}
	if code, body := get("/api/me"); code != 200 || !strings.Contains(body, "first@example.com") {
		t.Errorf("me: %d %s", code, body)
	}

	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the hub did not stop")
	}
	if !strings.Contains(logs.String(), "hub stopping") {
		t.Errorf("no stop line in the log:\n%s", logs)
	}
}

func TestServeRefusesABusyAddress(t *testing.T) {
	busy := httptest.NewServer(http.NotFoundHandler())
	defer busy.Close()
	data := t.TempDir()
	cfg, err := config.Load(env(map[string]string{
		"HUB_PUBLIC_URL": "http://127.0.0.1:18599", "HUB_LISTEN": strings.TrimPrefix(busy.URL, "http://"), "HUB_DATA_DIR": data,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := serve(context.Background(), cfg, nil, io.Discard); err == nil {
		t.Error("serve started on an address in use")
	}
}

// The numbers are served on a listener of their own and never on the public one, which checks nobody at /metrics.
func TestMetricsAreServedApartFromThePublicListener(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	metricsAddr := free.Addr().String()
	free.Close()
	data := t.TempDir()
	cfg, err := config.Load(env(map[string]string{
		"HUB_PUBLIC_URL": "http://127.0.0.1:18599", "HUB_LISTEN": "127.0.0.1:0", "HUB_DATA_DIR": data, "HUB_PASSKEY": "off",
		"HUB_DIST_DIR": filepath.Join(data, "dist"), "HUB_METRICS_LISTEN": metricsAddr, "HUB_TUNNEL_MEMORY_MB": "64",
	}))
	if err != nil {
		t.Fatal(err)
	}
	logs := &safeBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ready, logs) }()
	var public string
	select {
	case a := <-ready:
		public = "http://" + a
	case err := <-done:
		t.Fatalf("serve returned: %v\n%s", err, logs)
	case <-time.After(30 * time.Second):
		t.Fatal("the hub did not start")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	get := func(url string) (int, string, error) {
		res, err := client.Get(url)
		if err != nil {
			return 0, "", err
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b), nil
	}
	if _, _, err := get(public + "/healthz"); err != nil {
		t.Fatal(err)
	}
	var status int
	var body string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if status, body, err = get("http://" + metricsAddr + "/metrics"); err == nil {
			break
		}
	}
	if err != nil || status != http.StatusOK {
		t.Fatalf("GET /metrics on the metrics listener: %d %v\n%s", status, err, logs)
	}
	for _, want := range []string{
		"# TYPE harnessly_hub_links gauge", "harnessly_hub_links 0\n", "harnessly_hub_history_lag_seconds 0\n",
		"harnessly_hub_usage_lag_seconds 0\n", "# TYPE harnessly_hub_background_panics_total counter",
		"harnessly_hub_tunnel_memory_bytes 0\n", "harnessly_hub_tunnel_memory_limit_bytes 6.7108864e+07\n",
		"# TYPE harnessly_hub_tunnels_refused_total counter", "# TYPE harnessly_hub_tunnels_cut_total counter",
		`harnessly_hub_http_requests_total{route="health",class="2xx"} `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the metrics lack %q", want)
		}
	}
	if status, _, _ := get("http://" + metricsAddr + "/healthz"); status != http.StatusNotFound {
		t.Errorf("the metrics listener answered /healthz with %d, want 404", status)
	}
	status, body, err = get(public + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "harnessly_hub_") {
		t.Errorf("the public listener serves the metrics (%d)", status)
	}

	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the hub did not stop")
	}
}
