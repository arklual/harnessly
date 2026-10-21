package hlcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kitTarball(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	files := map[string]string{
		"bundle.r18.js":      "window.AcmeDS={};",
		"design-system.json": `{"acme":{"id":"acme-ds","version":"` + version + `"}}`,
	}
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "./" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func kitRun(t *testing.T, env map[string]string, cwd string, args ...string) result {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(args, Env{Getenv: func(k string) string { return env[k] }, Home: t.TempDir(), Cwd: cwd,
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errs})
	return result{code, out.String(), errs.String()}
}

func TestDesignKitInstallFromAFileThenList(t *testing.T) {
	work, kits := t.TempDir(), filepath.Join(t.TempDir(), "kits")
	if err := os.WriteFile(filepath.Join(work, "kit.tar.gz"), kitTarball(t, "0.16.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": kits}
	r := kitRun(t, env, work, "design", "kit", "install", "acme-ds", "0.16.0", "kit.tar.gz")
	if r.code != 0 || !strings.Contains(r.out, "installed acme-ds 0.16.0") || !strings.Contains(r.out, "/kit/acme-ds/0.16.0/") {
		t.Fatalf("install: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(kits, "acme-ds", "0.16.0", "bundle.r18.js")); err != nil {
		t.Fatal(err)
	}
	r = kitRun(t, env, work, "design", "kit", "install", "acme-ds", "0.16.0", "kit.tar.gz")
	if r.code != 0 || !strings.Contains(r.out, "already installed with this content") {
		t.Fatalf("again: %+v", r)
	}
	r = kitRun(t, env, work, "design", "kit", "list")
	if r.code != 0 || r.out != "acme-ds\t0.16.0\n" {
		t.Fatalf("list: %+v", r)
	}
	r = kitRun(t, env, work, "--json", "design", "kit", "list")
	var parsed struct {
		Dir  string `json:"dir"`
		Kits []struct {
			ID string `json:"id"`
		} `json:"kits"`
	}
	if json.Unmarshal([]byte(r.out), &parsed) != nil || parsed.Dir != kits || len(parsed.Kits) != 1 {
		t.Fatalf("json list: %+v", r)
	}
}

func TestDesignKitInstallFromAnAddressSendsTheGivenHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		if gotToken != "glpat-x" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write(kitTarball(t, "0.16.0"))
	}))
	defer srv.Close()
	kits := t.TempDir()
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": kits}
	r := kitRun(t, env, t.TempDir(), "design", "kit", "install", "acme-ds", "0.16.0", srv.URL+"/kit.tar.gz")
	if r.code != 1 || !strings.Contains(r.errs, "401") {
		t.Fatalf("no token: %+v", r)
	}
	r = kitRun(t, env, t.TempDir(), "design", "kit", "install", "acme-ds", "0.16.0", srv.URL+"/kit.tar.gz", "--header", "PRIVATE-TOKEN: glpat-x")
	if r.code != 0 {
		t.Fatalf("with header: %+v", r)
	}
	env["HARNESSLY_KIT_TOKEN"] = "glpat-x"
	if r = kitRun(t, env, t.TempDir(), "design", "kit", "install", "acme-ds", "0.16.0", srv.URL+"/kit.tar.gz", "--force"); r.code != 0 {
		t.Fatalf("token from the environment: %+v", r)
	}
}

func TestDesignKitInstallRefusesBadInputWithAReason(t *testing.T) {
	work := t.TempDir()
	_ = os.WriteFile(filepath.Join(work, "kit.tar.gz"), kitTarball(t, "0.16.0"), 0o644)
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": filepath.Join(work, "kits")}
	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"design", "kit"}, 2, "design kit takes"},
		{[]string{"design", "kit", "install", "acme-ds"}, 2, "needs <id>"},
		{[]string{"design", "kit", "install", "Bad_ID", "0.16.0", "kit.tar.gz"}, 2, "not a kit id"},
		{[]string{"design", "kit", "install", "acme-ds", "latest", "kit.tar.gz"}, 2, "not a version"},
		{[]string{"design", "kit", "install", "acme-ds", "0.15.0", "kit.tar.gz"}, 1, "version 0.16.0, not 0.15.0"},
		{[]string{"design", "kit", "install", "acme-ds", "0.16.0", "missing.tar.gz"}, 1, "cannot read"},
		{[]string{"design", "kit", "install", "acme-ds", "0.16.0", "kit.tar.gz", "--nope"}, 2, "unknown option"},
	}
	for _, c := range cases {
		r := kitRun(t, env, work, c.args...)
		if r.code != c.code || !strings.Contains(r.errs, c.msg) {
			t.Errorf("%v: code %d, %q", c.args, r.code, r.errs)
		}
	}
}

func TestDesignKitDirectoryDefaultsToTheRuntimesDataFolder(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(work, "kit.tar.gz"), kitTarball(t, "0.16.0"), 0o644)
	r := kitRun(t, map[string]string{"HARNESSLY_DATA_DIR": data}, work, "design", "kit", "install", "acme-ds", "0.16.0", "kit.tar.gz")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(data, "design-kits", "acme-ds", "0.16.0", "bundle.r18.js")); err != nil {
		t.Fatal(err)
	}
}
