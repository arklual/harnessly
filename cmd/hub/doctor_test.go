package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

func doctorEnv(t *testing.T, extra map[string]string) func(string) (string, bool) {
	t.Helper()
	m := map[string]string{
		"HUB_PUBLIC_URL": "http://hub.example.com", "HUB_DATA_DIR": t.TempDir(),
		"HUB_DATABASE_URL": "sqlite://" + filepath.Join(t.TempDir(), "hub.db"),
	}
	for k, v := range extra {
		m[k] = v
	}
	return env(m)
}

func TestDoctorWithExtensionsOff(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"doctor", "--offline"}, doctorEnv(t, nil), &out, &errOut); code != 0 || !strings.Contains(out.String(), "extensions     off") {
		t.Fatalf("code %d, out %q, err %q", code, out.String(), errOut.String())
	}
}

func TestDoctorShowsAdapterState(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ext.Description{Protocol: "harnessly.test/v1", Name: "acme", Version: "2.0"})
	}))
	defer good.Close()
	file := filepath.Join(t.TempDir(), "ext.json")
	write := func(urls ...string) {
		var specs []ext.Spec
		for i, u := range urls {
			specs = append(specs, ext.Spec{Name: "a" + string(rune('0'+i)), Kind: "test", URL: u, Protocol: "harnessly.test/v1"})
		}
		b, _ := json.Marshal(specs)
		if err := os.WriteFile(file, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	vars := map[string]string{"HUB_EXTENSIONS": "on", "HUB_EXTENSIONS_FILE": file}

	write(good.URL)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"doctor", "--offline"}, doctorEnv(t, vars), &out, &errOut); code != 0 || !strings.Contains(out.String(), "OK") || !strings.Contains(out.String(), "acme 2.0") {
		t.Fatalf("a good adapter: code %d, out %q, err %q", code, out.String(), errOut.String())
	}

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	write(good.URL, deadURL)
	out.Reset()
	if code := run(context.Background(), []string{"doctor", "--offline"}, doctorEnv(t, vars), &out, &errOut); code != 1 || !strings.Contains(out.String(), "UNREACHABLE") {
		t.Fatalf("a dead adapter: code %d, out %q", code, out.String())
	}
}

func TestExtensionsOnNeedsAFile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"doctor", "--offline"}, doctorEnv(t, map[string]string{"HUB_EXTENSIONS": "on"}), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "HUB_EXTENSIONS_FILE") {
		t.Fatalf("code %d, err %q", code, errOut.String())
	}
}
