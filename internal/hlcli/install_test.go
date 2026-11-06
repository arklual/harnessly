package hlcli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeInstaller = "#!/bin/sh\necho \"preflight=$HARNESSLY_PREFLIGHT args=$*\"\n"

func runInstall(t *testing.T, env map[string]string, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := Run(append([]string{"install"}, args...), Env{
		Getenv: func(k string) string { return env[k] }, Home: t.TempDir(), Cwd: t.TempDir(),
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out, Environ: func() []string { return os.Environ() },
	})
	return code, out.String()
}

func TestInstallRunsTheScriptWithAPreflightAndPassesFlagsOn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(fakeInstaller), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := runInstall(t, nil, "--script", script, "--check", "--ca-file", "/x/ca.pem")
	if code != 0 || !strings.Contains(out, "preflight=1 args=--check --ca-file /x/ca.pem") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestInstallTakesTheScriptFromTheOfflineFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte(fakeInstaller), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := runInstall(t, nil, "--offline", dir)
	if code != 0 || !strings.Contains(out, "args=--offline "+dir) {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out = runInstall(t, map[string]string{"HARNESSLY_OFFLINE": dir})
	if code != 0 || !strings.Contains(out, "preflight=1") {
		t.Fatalf("env: exit %d\n%s", code, out)
	}
}

func TestInstallFetchesTheScriptFromTheMirror(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mirror/install.sh" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(fakeInstaller))
	}))
	t.Cleanup(srv.Close)
	code, out := runInstall(t, nil, "--mirror", srv.URL+"/mirror/", "--check")
	if code != 0 || !strings.Contains(out, "args=--mirror "+srv.URL+"/mirror/ --check") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out = runInstall(t, nil, "--mirror", srv.URL+"/nowhere")
	if code == 0 || !strings.Contains(out, "404") {
		t.Fatalf("a missing script: exit %d\n%s", code, out)
	}
}

func TestInstallSaysWhenTheScriptIsMissing(t *testing.T) {
	t.Parallel()
	code, out := runInstall(t, nil, "--script", filepath.Join(t.TempDir(), "none.sh"))
	if code == 0 || !strings.Contains(out, "is not there") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
