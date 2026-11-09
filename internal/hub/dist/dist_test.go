package dist

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newServer(t *testing.T, files map[string]string) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	New(dir, "https://hub.example.com", "1.2.3", "").Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, dir
}

func get(t *testing.T, url string, headers ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestInstallerGetsTheHubURLAfterTheShebang(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"install.sh": "#!/bin/sh\nset -eu\nmain() { :; }\nmain \"$@\"\n"})
	res, body := get(t, srv.URL+"/install")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/x-shellscript") {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	want := "#!/bin/sh\nHARNESSLY_HUB_URL='https://hub.example.com'\nset -eu\nmain() { :; }\nmain \"$@\"\n"
	if body != want {
		t.Errorf("script:\n%s", body)
	}
}

func TestInstallerGetsTheMachineImageWhenTheHubHasOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\nmain() { :; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(dir, "https://hub.example.com", "1", "registry.example.com/team/machine:1.0").Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_, body := get(t, srv.URL+"/install")
	want := "#!/bin/sh\nHARNESSLY_HUB_URL='https://hub.example.com'\nHARNESSLY_IMAGE='registry.example.com/team/machine:1.0'\nmain() { :; }\n"
	if body != want {
		t.Errorf("script:\n%s", body)
	}
}

func TestInstallerWithoutShebangAndQuoting(t *testing.T) {
	if got := shellQuote(`a'b`); got != `'a'\''b'` {
		t.Errorf("quote: %s", got)
	}
	srv, _ := newServer(t, map[string]string{"install.sh": "echo hi\n"})
	_, body := get(t, srv.URL+"/install")
	if body != "HARNESSLY_HUB_URL='https://hub.example.com'\necho hi\n" {
		t.Errorf("script: %q", body)
	}
}

func TestMissingInstaller(t *testing.T) {
	srv, _ := newServer(t, nil)
	res, body := get(t, srv.URL+"/install")
	if res.StatusCode != 404 || !strings.Contains(body, "not_found") {
		t.Errorf("%d %s", res.StatusCode, body)
	}
}

func TestManifestListsBinariesWithSums(t *testing.T) {
	srv, dir := newServer(t, map[string]string{
		"runtime-linux-amd64": "runtime bytes", "agent-linux-amd64": "agent bytes", "agent-darwin-arm64": "mac agent",
		"install.sh": "#!/bin/sh\n", "notes.txt": "x", "runtime-windows-amd64": "no", "agent-linux-amd64.sig": "no",
	})
	res, body := get(t, srv.URL+"/dl/manifest.json")
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var m struct {
		Version string
		Files   map[string]struct {
			SHA256 string
			Size   int64
		}
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("agent bytes"))
	if m.Version != "1.2.3" || len(m.Files) != 3 || m.Files["agent-linux-amd64"].SHA256 != hex.EncodeToString(sum[:]) || m.Files["agent-linux-amd64"].Size != 11 {
		t.Errorf("manifest: %s", body)
	}
	// A changed file gets a new sum.
	if err := os.WriteFile(filepath.Join(dir, "agent-linux-amd64"), []byte("agent bytes v2!"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, body = get(t, srv.URL+"/dl/manifest.json")
	sum = sha256.Sum256([]byte("agent bytes v2!"))
	if !strings.Contains(body, hex.EncodeToString(sum[:])) {
		t.Errorf("stale sum: %s", body)
	}
}

func TestManifestWithoutDist(t *testing.T) {
	h := New(filepath.Join(t.TempDir(), "nope"), "https://h", "1", "")
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if res, _ := get(t, srv.URL+"/dl/manifest.json"); res.StatusCode != 404 {
		t.Errorf("status %d", res.StatusCode)
	}
}

func TestBinaryDownloadAndRanges(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"runtime-linux-arm64": "0123456789"})
	res, body := get(t, srv.URL+"/dl/runtime-linux-arm64")
	if res.StatusCode != 200 || body != "0123456789" || res.Header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("%d %q %v", res.StatusCode, body, res.Header)
	}
	res, body = get(t, srv.URL+"/dl/runtime-linux-arm64", "Range", "bytes=2-4")
	if res.StatusCode != 206 || body != "234" {
		t.Errorf("range: %d %q", res.StatusCode, body)
	}
}

func TestOnlyKnownNamesAreServed(t *testing.T) {
	srv, dir := newServer(t, map[string]string{"install.sh": "#!/bin/sh\n", "secret.txt": "s", "agent-linux-amd64": "a"})
	if err := os.Mkdir(filepath.Join(dir, "agent-darwin-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "secret.txt", "..%2Finstall.sh", "agent-linux-amd64.sig", "agent-windows-amd64", "agent-darwin-amd64", "AGENT-linux-amd64", "agent-linux-amd64%00"} {
		if res, _ := get(t, srv.URL+"/dl/"+name); res.StatusCode != 404 {
			t.Errorf("/dl/%s: %d", name, res.StatusCode)
		}
	}
	if res, _ := get(t, srv.URL+"/dl/agent-linux-amd64"); res.StatusCode != 200 {
		t.Errorf("good name: %d", res.StatusCode)
	}
}

func TestReleaseManifestIsServedOnlyWhenTheFolderHoldsIt(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"runtime-linux-amd64": "x"})
	if res, _ := get(t, srv.URL+"/dl/release.json"); res.StatusCode != http.StatusNotFound {
		t.Errorf("without the file: %d", res.StatusCode)
	}
	srv, _ = newServer(t, map[string]string{"release.json": `{"schema":2}`, "release.json.sig": `{"alg":"ed25519"}`, "runtime-linux-amd64": "x"})
	res, body := get(t, srv.URL+"/dl/release.json")
	if res.StatusCode != 200 || body != `{"schema":2}` {
		t.Errorf("%d %q", res.StatusCode, body)
	}
	if res, body := get(t, srv.URL+"/dl/release.json.sig"); res.StatusCode != 200 || !strings.Contains(body, "ed25519") {
		t.Errorf("sig: %d %q", res.StatusCode, body)
	}
	// the old manifest is as it was: only binaries and sums
	_, m := get(t, srv.URL+"/dl/manifest.json")
	if strings.Contains(m, "release") {
		t.Errorf("manifest.json changed: %s", m)
	}
}

// The deployment's network policy: served as it is when valid, 404 without a file, refused when broken.
func TestNetworkPolicyIsServedWhenValid(t *testing.T) {
	good := `{"internalHosts":["*.example.corp"],"proxy":"http://proxy.example.corp:3128","noProxy":["localhost"]}`
	srv, _ := newServer(t, map[string]string{"network.json": good})
	res, body := get(t, srv.URL+"/dl/network.json")
	if res.StatusCode != 200 || body != good || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q", res.StatusCode, body)
	}
	srv, _ = newServer(t, nil)
	if res, _ := get(t, srv.URL+"/dl/network.json"); res.StatusCode != 404 {
		t.Fatalf("no file: %d", res.StatusCode)
	}
	for _, bad := range []string{`{`, `{"proxy":"ftp://x"}`, `{"caPem":"not a certificate"}`} {
		srv, _ = newServer(t, map[string]string{"network.json": bad})
		if res, _ := get(t, srv.URL+"/dl/network.json"); res.StatusCode != 500 {
			t.Fatalf("%s: %d", bad, res.StatusCode)
		}
	}
}
