package hlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishCLI(t *testing.T, handler http.HandlerFunc) (*cli, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"HARNESSLY_DATA_DIR": dir, "ARTIFACTS_URL": "https://docs.example.test"}
	if handler != nil {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		u, _ := url.Parse(srv.URL)
		agentJSON := `{"port":"` + u.Port() + `","token":"t"}`
		if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	c := &cli{env: Env{Getenv: func(k string) string { return env[k] }, Home: dir, Cwd: dir, Stdout: &out, Stderr: &out}}
	return c, &out, dir
}

func TestDocsPublishGoesThroughAgent(t *testing.T) {
	var got map[string]any
	c, out, dir := publishCLI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/docs/publish" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"status":200,"body":{"slug":"plan-k3x9","version":1,"created":true}}`))
	})
	_ = os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Release plan\n\ntext\n"), 0o644)
	if err := c.run([]string{"docs", "publish", "plan.md"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got["title"] != "Release plan" || got["format"] != "md" || got["visibility"] != "private" {
		t.Fatalf("body: %v", got)
	}
	if !strings.Contains(out.String(), "https://docs.example.test/a/plan-k3x9\n") || !strings.Contains(out.String(), "published plan-k3x9") {
		t.Fatalf("out: %q", out)
	}
}

func TestDocsPublishUpdateSlug(t *testing.T) {
	var got map[string]any
	c, out, dir := publishCLI(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"status":200,"body":{"slug":"plan-k3x9","version":2,"created":false}}`))
	})
	_ = os.WriteFile(filepath.Join(dir, "p.html"), []byte("<h1>x</h1>"), 0o644)
	if err := c.run([]string{"docs", "publish", "p.html", "--title", "Plan", "--slug", "plan-k3x9", "--note", "v2"}); err != nil {
		t.Fatal(err)
	}
	if got["slug"] != "plan-k3x9" || got["format"] != "html" || got["note"] != "v2" || !strings.Contains(out.String(), "updated plan-k3x9, version 2") {
		t.Fatalf("%v %q", got, out)
	}
}

func TestDocsPublishWithoutHubSaysSo(t *testing.T) {
	c, _, dir := publishCLI(t, nil)
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n"), 0o644)
	err := c.run([]string{"docs", "publish", "a.md"})
	if err == nil || !strings.Contains(err.Error(), "no hub") || !strings.Contains(err.Error(), "artifacts publish") {
		t.Fatalf("%v", err)
	}
	c2, _, dir2 := publishCLI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":503,"body":{"error":"documents are not set up"}}`))
	})
	_ = os.WriteFile(filepath.Join(dir2, "a.md"), []byte("# A\n"), 0o644)
	err = c2.run([]string{"docs", "publish", "a.md"})
	if err == nil || !strings.Contains(err.Error(), "not set up") || !strings.Contains(err.Error(), "artifacts publish") {
		t.Fatalf("%v", err)
	}
}

func TestDocsPublishAddressFromTheDocsHost(t *testing.T) {
	for _, tc := range []struct{ artifacts, host, want string }{
		{"https://docs.example.test/", "", "https://docs.example.test"},
		{"", "a.example.test", "https://a.example.test"},
		{"", "", ""},
		{"", "a.example.test/x", ""},
	} {
		env := map[string]string{"ARTIFACTS_URL": tc.artifacts, "RUNTIME_DOCS_HOST": tc.host}
		c := &cli{env: Env{Getenv: func(k string) string { return env[k] }}}
		if got := c.docsBase(); got != tc.want {
			t.Errorf("%+v: %q", tc, got)
		}
	}
}
