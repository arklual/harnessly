package hlcli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/release"
)

type upRig struct {
	hub      *httptest.Server
	dir, key string
	ext      string
}

func newUpRig(t *testing.T, protocol string) *upRig {
	t.Helper()
	cliBin := "#!/bin/sh\necho harnessly 2.0.0\n"
	hash := sha256.Sum256([]byte(cliBin))
	key := "harnessly-" + runtime.GOOS + "-" + runtime.GOARCH
	m := release.Manifest{Schema: 2, Version: "2.0.0", Data: release.Data{SchemaVersion: 14, Additive: true},
		Components: map[string]release.Component{release.CLI: {Version: "2.0.0", Files: map[string]release.File{
			key: {SHA256: hex.EncodeToString(hash[:]), Size: int64(len(cliBin))}}}},
		ExtensionAPI: map[string]release.Range{"harnessly.machine-provider": {Min: 1, Max: 1}}}
	body, _ := json.Marshal(m)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig, _ := release.Sign(priv, body)
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/release.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	mux.HandleFunc("/dl/release.json.sig", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(sig) })
	mux.HandleFunc("/dl/"+key, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(cliBin)) })
	r := &upRig{hub: httptest.NewServer(mux), dir: t.TempDir()}
	t.Cleanup(r.hub.Close)
	r.key = filepath.Join(r.dir, "release.pub")
	_ = os.WriteFile(r.key, []byte(base64.StdEncoding.EncodeToString(pub)), 0o644)
	_ = os.WriteFile(filepath.Join(r.dir, "harnessly"), []byte("#!/bin/sh\necho harnessly 1.0.0\n"), 0o755)
	r.ext = filepath.Join(r.dir, "extensions.json")
	_ = os.WriteFile(r.ext, []byte(`[{"name":"acme","kind":"machine-provider","url":"http://x.example","protocol":"`+protocol+`"}]`), 0o644)
	return r
}

func (r *upRig) run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(append([]string{"upgrade"}, args...), Env{Getenv: func(string) string { return "" }, Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func (r *upRig) cli() string {
	b, _ := os.ReadFile(filepath.Join(r.dir, "harnessly"))
	return string(b)
}

func TestUpgradePlanThenApply(t *testing.T) {
	r := newUpRig(t, "harnessly.machine-provider/v1")
	code, out, errs := r.run("plan", "--hub", r.hub.URL, "--key", r.key, "--dir", r.dir, "--extensions", r.ext)
	if code != 0 || !strings.Contains(out, "update") || !strings.Contains(out, "compatible") || !strings.Contains(out, "reversible") {
		t.Fatalf("plan %d\n%s\n%s", code, out, errs)
	}
	if !strings.Contains(r.cli(), "1.0.0") {
		t.Fatal("plan changed the program")
	}
	code, out, errs = r.run("apply", "--hub", r.hub.URL, "--key", r.key, "--dir", r.dir, "--extensions", r.ext)
	if code != 0 || !strings.Contains(r.cli(), "2.0.0") {
		t.Fatalf("apply %d\n%s\n%s", code, out, errs)
	}
	if code, _, errs := r.run("rollback", "--dir", r.dir); code != 0 || !strings.Contains(r.cli(), "1.0.0") {
		t.Fatalf("rollback %d %s", code, errs)
	}
}

func TestUpgradeRefusesIncompatibleExtensionAndBadSignature(t *testing.T) {
	r := newUpRig(t, "harnessly.machine-provider/v2")
	code, out, _ := r.run("plan", "--hub", r.hub.URL, "--key", r.key, "--dir", r.dir, "--extensions", r.ext)
	if code != 1 || !strings.Contains(out, "BLOCKED") {
		t.Fatalf("plan %d\n%s", code, out)
	}
	if code, _, _ := r.run("apply", "--hub", r.hub.URL, "--key", r.key, "--dir", r.dir, "--extensions", r.ext); code == 0 || !strings.Contains(r.cli(), "1.0.0") {
		t.Fatalf("apply went through (%d) or changed the program", code)
	}
	// a key that did not sign the manifest
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	wrong := filepath.Join(r.dir, "wrong.pub")
	_ = os.WriteFile(wrong, []byte(base64.StdEncoding.EncodeToString(other)), 0o644)
	if code, _, errs := r.run("plan", "--hub", r.hub.URL, "--key", wrong, "--dir", r.dir); code == 0 || !strings.Contains(errs, "pinned key") {
		t.Fatalf("wrong key accepted: %d %s", code, errs)
	}
	if code, _, errs := r.run("plan", "--hub", r.hub.URL, "--dir", r.dir); code == 0 || !strings.Contains(errs, "pinned key") {
		t.Fatalf("no key accepted: %d %s", code, errs)
	}
}

func TestUpgradeFailedCheckRollsBack(t *testing.T) {
	r := newUpRig(t, "harnessly.machine-provider/v1")
	code, _, errs := r.run("apply", "--hub", r.hub.URL, "--key", r.key, "--dir", r.dir, "--check-cmd", "exit 3")
	if code == 0 || !strings.Contains(errs, "rolled back") || !strings.Contains(r.cli(), "1.0.0") {
		t.Fatalf("%d %s\n%s", code, errs, r.cli())
	}
}
