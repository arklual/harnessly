package hlcli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/sites"
)

const siteAdmin = "admin-token-admin-token-1234"

// sitesServer is a real sites module behind HTTP: the CLI is tested against what it will talk to.
func sitesServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	blobs, _ := sites.NewFolderBlobs(t.TempDir())
	key := []byte("0123456789abcdef0123456789abcdef")
	svc, err := sites.New(sites.Deps{DB: db, Blobs: blobs, Config: sites.Config{Domain: "sites.test", Secure: true, Key: key},
		Auth: &sites.GatewayAuthority{AdminToken: siteAdmin, HMACTickets: sites.NewHMACTickets(key, nil)}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runSite(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{Getenv: func(string) string { return "" }, Home: t.TempDir(), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Cwd: t.TempDir()}
	code = Run(args, env)
	return out.String(), errOut.String(), code
}

func writeTree(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSiteDeployListRollback(t *testing.T) {
	srv := sitesServer(t)
	conn := []string{"--url", srv.URL, "--token", siteAdmin, "--space", "acme", "--name", "shop"}
	dir := writeTree(t, map[string]string{
		"index.html": "one", "css/a.css": "body{}", ".git/config": "never", "node_modules/x/index.js": "never",
	})
	out, errOut, code := runSite(t, append([]string{"site", "deploy", dir}, conn...)...)
	if code != 0 || strings.TrimSpace(out) != "https://shop--acme.sites.test/" {
		t.Fatalf("deploy: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "created the site shop") || !strings.Contains(errOut, "2 files") {
		t.Errorf("progress %q", errOut)
	}

	// The same folder again uploads nothing; a changed file uploads that file only.
	_, errOut, code = runSite(t, append([]string{"site", "deploy", dir}, conn...)...)
	if code != 0 || !strings.Contains(errOut, "0 to upload") {
		t.Fatalf("again: %d %q", code, errOut)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = runSite(t, append([]string{"site", "deploy", dir}, conn...)...)
	if code != 0 || !strings.Contains(errOut, "1 to upload") {
		t.Fatalf("changed: %d %q", code, errOut)
	}

	out, _, code = runSite(t, append([]string{"site", "versions"}, conn...)...)
	if code != 0 || strings.Count(out, "\n") != 3 || !strings.HasPrefix(out, "* v3") {
		t.Fatalf("versions: %d %q", code, out)
	}
	out, _, code = runSite(t, append([]string{"site", "rollback", "v1"}, conn...)...)
	if code != 0 || strings.TrimSpace(out) != "https://shop--acme.sites.test/" {
		t.Fatalf("rollback: %d %q", code, out)
	}
	out, _, _ = runSite(t, append([]string{"site", "versions"}, conn...)...)
	if !strings.Contains(out, "* v1") {
		t.Fatalf("v1 is current: %q", out)
	}
	out, _, code = runSite(t, "site", "list", "--url", srv.URL, "--token", siteAdmin, "--space", "acme")
	if code != 0 || !strings.HasPrefix(out, "shop\thttps://shop--acme.sites.test/\tv1") {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestSiteDeployWithADeployToken(t *testing.T) {
	srv := sitesServer(t)
	// An administrator makes the site and its token (what the UI does).
	req := func(method, path, body string) string {
		r, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+siteAdmin)
		r.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	req("POST", "/api/sites/acme", `{"name":"manual"}`)
	tok := req("POST", "/api/sites/acme/manual/tokens", `{"name":"ci"}`)
	token := tok[strings.Index(tok, "hsite_"):]
	token = token[:strings.IndexByte(token, '"')]

	dir := writeTree(t, map[string]string{"index.html": "docs"})
	out, errOut, code := runSite(t, "site", "deploy", dir, "--url", srv.URL, "--token", token)
	if code != 0 || strings.TrimSpace(out) != "https://manual--acme.sites.test/" {
		t.Fatalf("deploy with a token only: %d %q %q", code, out, errOut)
	}
	if _, errOut, code = runSite(t, "site", "deploy", dir, "--url", srv.URL, "--token", token, "--name", "other"); code == 0 || !strings.Contains(errOut, `for the site "manual"`) {
		t.Fatalf("wrong name: %d %q", code, errOut)
	}
	if _, errOut, code = runSite(t, "site", "deploy", dir, "--url", srv.URL, "--token", "hsite_nope"); code == 0 || !strings.Contains(errOut, "token") {
		t.Fatalf("wrong token: %d %q", code, errOut)
	}
}

func TestSiteDeployRefusesSecretsAndSaysWhich(t *testing.T) {
	srv := sitesServer(t)
	conn := []string{"--url", srv.URL, "--token", siteAdmin, "--space", "acme", "--name", "shop"}
	dir := writeTree(t, map[string]string{"index.html": "x", ".env": "A=1"})
	_, errOut, code := runSite(t, append([]string{"site", "deploy", dir}, conn...)...)
	if code == 0 || !strings.Contains(errOut, ".env") || !strings.Contains(errOut, "--allow-secrets") {
		t.Fatalf("%d %q", code, errOut)
	}
	if out, errOut, code := runSite(t, append([]string{"site", "deploy", dir, "--allow-secrets"}, conn...)...); code != 0 {
		t.Fatalf("allowed: %d %q %q", code, out, errOut)
	}
}

func TestSiteNeedsServerAndToken(t *testing.T) {
	dir := writeTree(t, map[string]string{"index.html": "x"})
	if _, errOut, code := runSite(t, "site", "deploy", dir, "--token", "t"); code == 0 || !strings.Contains(errOut, "--url") {
		t.Errorf("no url: %d %q", code, errOut)
	}
	if _, errOut, code := runSite(t, "site", "deploy", dir, "--url", "http://x"); code == 0 || !strings.Contains(errOut, "token") {
		t.Errorf("no token: %d %q", code, errOut)
	}
	if _, errOut, code := runSite(t, "site", "deploy", "/nonexistent-dir-xyz", "--url", "http://x", "--token", "t", "--name", "a", "--space", "b"); code == 0 || errOut == "" {
		t.Errorf("no folder: %d %q", code, errOut)
	}
}
