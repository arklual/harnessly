package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const gatewaySecret = "gateway-secret-gateway-secret-1234"

func sitesEnv(t *testing.T, extra map[string]string) map[string]string {
	m := map[string]string{
		"HARNESSLY_SITES_DOMAIN": "sites.localhost", "SITES_DATA_DIR": t.TempDir(), "SITES_LISTEN": "127.0.0.1:0",
		"SITES_ADMIN_TOKEN": "admin-token-admin-token-1234", "SITES_GATEWAY_SECRET": gatewaySecret,
		"SITES_LOGIN_URL": "https://gateway.example.com/enter",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestSitesConfigRefusals(t *testing.T) {
	for name, drop := range map[string]string{"domain": "HARNESSLY_SITES_DOMAIN", "secret": "SITES_GATEWAY_SECRET"} {
		m := sitesEnv(t, nil)
		delete(m, drop)
		var out, errOut bytes.Buffer
		if code := runSites(context.Background(), []string{"check-config"}, env(m), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), drop) {
			t.Errorf("%s: code %d, err %q", name, code, errOut.String())
		}
	}
	short := sitesEnv(t, map[string]string{"SITES_GATEWAY_SECRET": "short"})
	var out, errOut bytes.Buffer
	if code := runSites(context.Background(), []string{"check-config"}, env(short), &out, &errOut); code != 2 {
		t.Errorf("short secret: %d", code)
	}
}

func TestSitesCheckConfigNeverPrintsASecret(t *testing.T) {
	var out, errOut bytes.Buffer
	m := sitesEnv(t, map[string]string{"HARNESSLY_SITES_S3_ENDPOINT": "https://s3.example.com", "HARNESSLY_SITES_S3_BUCKET": "sites",
		"HARNESSLY_SITES_S3_ACCESS_KEY": "AK-visible-no", "HARNESSLY_SITES_S3_SECRET_KEY": "s3-secret-key-1234"})
	if code := runSites(context.Background(), []string{"check-config"}, env(m), &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, errOut.String())
	}
	for _, secret := range []string{gatewaySecret, "admin-token-admin-token-1234", "s3-secret-key-1234", "AK-visible-no"} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("check-config printed %q", secret)
		}
	}
	if !strings.Contains(out.String(), "s3 https://s3.example.com/sites") {
		t.Errorf("output %q", out.String())
	}
}

func TestHubCheckConfigSaysHowSitesAreReached(t *testing.T) {
	base := map[string]string{"HUB_PUBLIC_URL": "https://hub.example.com", "HUB_DATA_DIR": t.TempDir(), "HUB_SMTP_URL": "smtp://mail.example.com"}
	for want, extra := range map[string]map[string]string{
		"path: https://hub.example.com/sites/<site>--<space>/": nil,
		"subdomain: <site>--<space>.sites.example.net":         {"HARNESSLY_SITES_DOMAIN": "sites.example.net"},
		"off (HARNESSLY_SITES=off)":                            {"HARNESSLY_SITES": "off"},
	} {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"check-config"}, env(m), &out, &errOut); code != 0 || !strings.Contains(out.String(), want) {
			t.Errorf("%q: code %d\n%s%s", want, code, out.String(), errOut.String())
		}
	}
}

// TestStandaloneSites is the gateway way of running sites: no hub, an admin token for the deploy, a gateway's tickets for the viewers.
func TestStandaloneSites(t *testing.T) {
	cfg, err := loadSitesConfig(env(sitesEnv(t, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	var logs safeBuffer
	go func() { done <- serveSites(ctx, cfg, ready, &logs) }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("did not start: %v %s", err, logs.String())
	case <-time.After(10 * time.Second):
		t.Fatal("did not start")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stopped with %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("did not stop")
		}
	})

	base := "http://" + addr
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path, host string, body any, hdr ...string) (int, []byte, http.Header) {
		var rd io.Reader
		switch b := body.(type) {
		case nil:
		case string:
			rd = strings.NewReader(b)
		default:
			raw, _ := json.Marshal(b)
			rd = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, base+path, rd)
		if host != "" {
			req.Host = host
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b, res.Header
	}
	admin := []string{"Authorization", "Bearer admin-token-admin-token-1234"}

	if code, _, _ := do("GET", "/healthz", "", nil); code != 200 {
		t.Fatalf("healthz %d", code)
	}
	if code, _, _ := do("POST", "/api/sites/acme", "", map[string]any{"name": "pitch"}); code != 401 {
		t.Fatalf("anonymous create: %d", code)
	}
	if code, b, _ := do("POST", "/api/sites/acme", "", map[string]any{"name": "pitch", "access": "team"}, admin...); code != 201 {
		t.Fatalf("create: %d %s", code, b)
	}
	html := "<h1>pitch</h1>"
	sum := sha256.Sum256([]byte(html))
	h := hex.EncodeToString(sum[:])
	code, b, _ := do("POST", "/api/sites/acme/pitch/deploys", "", map[string]any{"files": map[string]any{"index.html": map[string]any{"hash": h, "size": len(html)}}}, admin...)
	if code != 201 {
		t.Fatalf("begin: %d %s", code, b)
	}
	var d struct{ ID string }
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if code, b, _ := do("PUT", "/api/sites/acme/pitch/deploys/"+d.ID+"/blobs/"+h, "", html, admin...); code != 204 {
		t.Fatalf("blob: %d %s", code, b)
	}
	code, b, _ = do("POST", "/api/sites/acme/pitch/deploys/"+d.ID+"/commit", "", map[string]any{}, admin...)
	if code != 201 || !strings.Contains(string(b), "https://pitch--acme.sites.localhost") && !strings.Contains(string(b), "pitch--acme.sites.localhost") {
		t.Fatalf("commit: %d %s", code, b)
	}

	host := "pitch--acme.sites.localhost"
	// Without a ticket: sent to the gateway.
	code, _, hdr := do("GET", "/", host, nil, "Accept", "text/html")
	if code != 302 || !strings.HasPrefix(hdr.Get("Location"), "https://gateway.example.com/enter?host=") {
		t.Fatalf("anonymous: %d %v", code, hdr)
	}
	// With the gateway's ticket (the same code the `ticket` command runs).
	var tk, errOut bytes.Buffer
	if c := runSites(context.Background(), []string{"ticket", host, "someone"}, env(sitesEnv(t, nil)), &tk, &errOut); c != 0 {
		t.Fatalf("ticket: %d %s", c, errOut.String())
	}
	code, _, hdr = do("GET", "/?hl_site_ticket="+strings.TrimSpace(tk.String()), host, nil, "Accept", "text/html")
	cookie := hdr.Get("Set-Cookie")
	if code != 302 || !strings.HasPrefix(cookie, "hl_site=") && !strings.HasPrefix(cookie, "__Host-hl_site=") {
		t.Fatalf("redeem: %d %v", code, hdr)
	}
	code, b, _ = do("GET", "/", host, nil, "Cookie", strings.SplitN(cookie, ";", 2)[0])
	if code != 200 || string(b) != html {
		t.Fatalf("with the cookie: %d %q", code, b)
	}
	// The management API does not answer on a site's address, and a site's page does not answer on the API's.
	if code, _, _ := do("GET", "/api/sites/acme", host, nil, admin...); code == 200 {
		t.Fatal("the API answered on a site's origin")
	}
}
